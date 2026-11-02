package relay

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	// claudeClearAtBeta enables clear_at on mid-conversation system messages;
	// without it the API rejects clear_at as an unknown field.
	claudeClearAtBeta = "mid-conversation-system-clear-at-2026-08-21"
	// claudeMaxCacheBreakpoints is Anthropic's per-request breakpoint limit.
	claudeMaxCacheBreakpoints = 4

	cacheBreakpointAdded = "added"
	cacheBreakpointMoved = "moved"
)

// claudeVolatileTailPatterns mark a trailing reminder whose text changes every
// turn (Claude Code's per-turn token budget), so a breakpoint on it is never
// read back.
var claudeVolatileTailPatterns = []string{"<total_tokens>"}

// prepareClaudeTurnScopedMessages makes clear_at messages valid upstream: the
// beta header is required, and a turn-scoped (next_user_message) message may
// not carry cache_control, so those breakpoints are dropped. It reports
// whether any message carries clear_at.
func prepareClaudeTurnScopedMessages(jsonData []byte) ([]byte, bool, error) {
	hasClearAt := false
	var paths []string
	gjson.GetBytes(jsonData, "messages").ForEach(func(i, msg gjson.Result) bool {
		clearAt := msg.Get("clear_at")
		if !clearAt.Exists() {
			return true
		}
		hasClearAt = true
		if clearAt.String() != "next_user_message" || !msg.Get("content").IsArray() {
			return true
		}
		msg.Get("content").ForEach(func(j, block gjson.Result) bool {
			if block.Get("cache_control").Exists() {
				paths = append(paths, fmt.Sprintf("messages.%d.content.%d.cache_control", i.Int(), j.Int()))
			}
			return true
		})
		return true
	})
	var err error
	for _, path := range paths {
		if jsonData, err = sjson.DeleteBytes(jsonData, path); err != nil {
			return jsonData, hasClearAt, err
		}
	}
	return jsonData, hasClearAt, nil
}

// claudeBlockRef addresses one content block: message index and block index
// (-1 when the content is a plain string).
type claudeBlockRef struct {
	msg, block int
}

func (r claudeBlockRef) path() string {
	if r.block < 0 {
		return fmt.Sprintf("messages.%d.content", r.msg)
	}
	return fmt.Sprintf("messages.%d.content.%d", r.msg, r.block)
}

// claudeCacheableBlock reports whether a breakpoint may sit on the block:
// Anthropic rejects cache_control on thinking blocks and empty text.
func claudeCacheableBlock(block gjson.Result) bool {
	switch block.Get("type").String() {
	case "thinking", "redacted_thinking":
		return false
	case "text":
		return block.Get("text").String() != ""
	}
	return true
}

// lastClaudeCacheableBlock finds the last block of message msg before block
// index `before` (len(content) for the whole message) that may carry a
// breakpoint.
func lastClaudeCacheableBlock(messages gjson.Result, msg, before int) (claudeBlockRef, bool) {
	content := messages.Get(fmt.Sprintf("%d.content", msg))
	if content.Type == gjson.String {
		return claudeBlockRef{msg: msg, block: -1}, content.String() != "" && before > 0
	}
	blocks := content.Array()
	for j := min(before, len(blocks)) - 1; j >= 0; j-- {
		if claudeCacheableBlock(blocks[j]) {
			return claudeBlockRef{msg: msg, block: j}, true
		}
	}
	return claudeBlockRef{}, false
}

// addClaudeFallbackCacheBreakpoint opts an otherwise unmarked prompt into caching.
// Inspect protocol marker positions before any gateway-added breakpoints, and
// leave even malformed/null client markers alone. Default TTL remains 5m; the
// independent extended-TTL policy may upgrade the resulting marker afterward.
func addClaudeFallbackCacheBreakpoint(body []byte) ([]byte, bool, error) {
	root := gjson.ParseBytes(body)
	if root.Get("cache_control").Exists() {
		return body, false, nil
	}
	if messages := root.Get("messages"); messages.Exists() && !messages.IsArray() {
		return body, false, nil
	}
	refs := claudeCacheBlockRefs(body)
	for _, ref := range refs {
		if ref.block >= 0 && root.Get(ref.path()+".cache_control").Exists() {
			return body, false, nil
		}
	}
	messages := root.Get("messages").Array()
	for _, message := range messages {
		if message.Get("cache_control").Exists() {
			return body, false, nil
		}
	}
	for i := len(refs) - 1; i >= 0; i-- {
		ref := refs[i]
		path := ref.path()
		if ref.prefix == "" {
			clearAt := messages[ref.msg].Get("clear_at")
			if clearAt.Exists() && clearAt.String() != "never" {
				continue
			}
		}
		if ref.block < 0 {
			path = ref.prefix
			if path == "" {
				path = fmt.Sprintf("messages.%d.content", ref.msg)
			}
			text := root.Get(path)
			if text.Type != gjson.String || text.String() == "" {
				continue
			}
			block, err := sjson.Set(`{"type":"text"}`, "text", text.String())
			if err != nil {
				return body, false, err
			}
			body, err = sjson.SetRawBytes(body, path, []byte("["+block+"]"))
			if err != nil {
				return body, false, err
			}
			path += ".0"
		} else if block := root.Get(path); !block.IsObject() || !claudeCacheableBlock(block) {
			continue
		}
		out, err := sjson.SetRawBytes(body, path+".cache_control",
			[]byte(`{"type":"ephemeral","ttl":"5m"}`))
		return out, err == nil, err
	}
	return body, false, nil
}

// addClaudeStableBaseBreakpoint protects tools and the leading system block
// independently of growing message history. It preserves existing breakpoints
// and uses only a free slot. A foundation is only useful for caching once the
// provider's minimum prefix length is met.
func addClaudeStableBaseBreakpoint(body []byte) ([]byte, bool, error) {
	paths := claudeCacheControlPaths(body)
	count := len(paths)
	if gjson.GetBytes(body, "cache_control").IsObject() {
		count++ // Reserve a slot for the automatic trailing breakpoint.
	}
	if count >= claudeMaxCacheBreakpoints {
		return body, false, nil
	}
	// A 1h breakpoint must precede any 5m breakpoint when TTLs are mixed.
	ttl := claudeDefaultCacheTTL
	for _, path := range append(paths, "cache_control") {
		if gjson.GetBytes(body, path+".ttl").String() == claudeExtendedCacheTTL {
			ttl = claudeExtendedCacheTTL
		}
	}
	path := ""
	system := gjson.GetBytes(body, "system")
	if system.IsArray() {
		// Do not skip a dynamic leading block and call a later block stable.
		first := system.Get("0")
		if first.IsObject() && claudeCacheableBlock(first) &&
			!strings.Contains(first.Get("text").String(), "<total_tokens>") {
			path = "system.0"
		}
	} else if system.Type == gjson.String && system.String() != "" &&
		!strings.Contains(system.String(), "<total_tokens>") {
		block, err := sjson.Set(`{"type":"text"}`, "text", system.String())
		if err != nil {
			return body, false, err
		}
		body, err = sjson.SetRawBytes(body, "system", []byte("["+block+"]"))
		if err != nil {
			return body, false, err
		}
		path = "system.0"
	}
	if path == "" {
		tools := gjson.GetBytes(body, "tools").Array()
		if len(tools) > 0 {
			path = fmt.Sprintf("tools.%d", len(tools)-1)
		}
	}
	if path == "" || gjson.GetBytes(body, path+".cache_control").Exists() {
		return body, false, nil
	}
	out, err := sjson.SetRawBytes(body, path+".cache_control",
		[]byte(fmt.Sprintf(`{"type":"ephemeral","ttl":%q}`, ttl)))
	return out, err == nil, err
}

// fixClaudeCacheBreakpoints moves the prompt-cache breakpoint off a volatile
// trailing message (a mid-conversation system reminder the client rewrites or
// clears every turn) onto the last stable block before it, so the next turn
// can read the cached prefix. A breakpoint is added there while slots remain;
// when all four are used, or the tail is turn-scoped (clear_at), the tail
// breakpoint is moved instead. A trailing system message without a known
// volatile marker only gains a breakpoint, never loses its own. Returns the
// rewritten body and "added", "moved" or "" when nothing changed.
func fixClaudeCacheBreakpoints(jsonData []byte) ([]byte, string, error) {
	messages := gjson.GetBytes(jsonData, "messages")
	msgs := messages.Array()
	if len(msgs) < 2 {
		return jsonData, "", nil
	}
	lastIdx := len(msgs) - 1
	last := msgs[lastIdx]
	content := last.Get("content")

	// Locate the tail breakpoint and judge how sure we are it is volatile.
	tail := claudeBlockRef{msg: -1}
	volatileText := false
	if content.IsArray() {
		for j, block := range content.Array() {
			if block.Get("cache_control").IsObject() {
				tail = claudeBlockRef{msg: lastIdx, block: j}
			}
			for _, pattern := range claudeVolatileTailPatterns {
				if strings.Contains(block.Get("text").String(), pattern) {
					volatileText = true
				}
			}
		}
	}
	clearAt := last.Get("clear_at").String()
	turnScoped := last.Get("role").String() == "system" && clearAt != "" && clearAt != "never"
	var target claudeBlockRef
	found := false
	switch {
	case tail.msg < 0:
		return jsonData, "", nil
	case last.Get("role").String() == "system":
		// Prefer the previous message (normally the user turn ending in a
		// tool_result), then the assistant turn before it.
		for m := lastIdx - 1; m >= max(lastIdx-2, 0) && !found; m-- {
			target, found = lastClaudeCacheableBlock(messages, m, math.MaxInt)
		}
	case volatileText:
		// A reminder appended to a user message: use the block before it.
		target, found = lastClaudeCacheableBlock(messages, lastIdx, tail.block)
	default:
		return jsonData, "", nil
	}
	if !found {
		return jsonData, "", nil
	}
	targetBlock := gjson.GetBytes(jsonData, target.path())
	if targetBlock.Get("cache_control").Exists() {
		return jsonData, "", nil
	}

	// The new breakpoint keeps the tail's type and TTL.
	cacheControl := gjson.GetBytes(jsonData, tail.path()+".cache_control").Raw
	count := len(claudeCacheControlPaths(jsonData))
	if gjson.GetBytes(jsonData, "cache_control").IsObject() {
		count++
	}
	full := count >= claudeMaxCacheBreakpoints
	move := turnScoped || (volatileText && full)
	if full && !move {
		return jsonData, "", nil
	}

	var err error
	if target.block < 0 {
		// A string content becomes one text block, which renders the same.
		textBlock, _ := sjson.Set(`{"type":"text"}`, "text", targetBlock.String())
		if jsonData, err = sjson.SetRawBytes(jsonData, target.path(), []byte("["+textBlock+"]")); err != nil {
			return jsonData, "", err
		}
		target.block = 0
	}
	if jsonData, err = sjson.SetRawBytes(jsonData, target.path()+".cache_control", []byte(cacheControl)); err != nil {
		return jsonData, "", err
	}
	if !move {
		return jsonData, cacheBreakpointAdded, nil
	}
	if jsonData, err = sjson.DeleteBytes(jsonData, tail.path()+".cache_control"); err != nil {
		return jsonData, "", err
	}
	return jsonData, cacheBreakpointMoved, nil
}

// retainClaudeCacheBreakpoints restores breakpoints from the latest successful
// cache write when the corresponding prefix is still unchanged. The state is
// only an observation of a prior write; it does not claim that Anthropic
// exposes the exact breakpoint that was persisted.
func retainClaudeCacheBreakpoints(jsonData []byte, previous *claudeCacheBreakpointState) ([]byte, bool, error) {
	if previous == nil || len(previous.BlkHash) == 0 || len(previous.BpIdx) == 0 {
		return jsonData, false, nil
	}
	current := buildClaudeCacheSkeleton(jsonData)
	if current.ParamFp != previous.ParamFp {
		return jsonData, false, nil
	}
	refs := claudeCacheBlockRefs(jsonData)
	if len(refs) != len(current.BlkHash) {
		return jsonData, false, nil
	}
	changed := false
	now := time.Now()
	// Prefer the longest reusable prefix when only a few slots are free.
	for i := len(previous.BpIdx) - 1; i >= 0; i-- {
		index := int(previous.BpIdx[i])
		if index >= len(current.BlkHash) || index >= len(refs) || index >= len(previous.BlkHash) ||
			!slices.Equal(current.BlkHash[:index+1], previous.BlkHash[:index+1]) {
			continue
		}
		if i < len(previous.BpExpires) && !now.Before(previous.BpExpires[i]) {
			continue
		}
		if len(previous.BlkKind) > 0 {
			if index >= len(previous.BlkKind) || index >= len(current.BlkKind) ||
				!slices.Equal(current.BlkKind[:index+1], previous.BlkKind[:index+1]) {
				continue
			}
			previousStarts := slices.DeleteFunc(slices.Clone(previous.MsgStart), func(start uint16) bool { return int(start) > index })
			currentStarts := slices.DeleteFunc(slices.Clone(current.MsgStart), func(start uint16) bool { return int(start) > index })
			if !slices.Equal(previousStarts, currentStarts) {
				continue
			}
		}
		if !previous.Expires.IsZero() && !now.Before(previous.Expires) {
			continue
		}
		count := len(claudeCacheControlPaths(jsonData))
		if gjson.GetBytes(jsonData, "cache_control").IsObject() {
			count++
		}
		if count >= claudeMaxCacheBreakpoints {
			break
		}
		if refs[index].block < 0 {
			continue
		}
		path := refs[index].path() + ".cache_control"
		if !claudeCacheableBlock(gjson.GetBytes(jsonData, refs[index].path())) {
			continue
		}
		if refs[index].prefix == "" && gjson.GetBytes(jsonData, fmt.Sprintf("messages.%d.clear_at", refs[index].msg)).Exists() {
			continue
		}
		if gjson.GetBytes(jsonData, path).IsObject() {
			continue
		}
		ttl := claudeDefaultCacheTTL
		if i < len(previous.BpTtl) && previous.BpTtl[i] != "" {
			ttl = previous.BpTtl[i]
		}
		if ttl != claudeDefaultCacheTTL && ttl != claudeExtendedCacheTTL {
			continue
		}
		// Preserve the retained TTL and skip anchors that would put a 1h
		// marker after a 5m marker, including automatic trailing caching.
		ordered := true
		for j, ref := range refs {
			cc := gjson.GetBytes(jsonData, ref.path()+".cache_control")
			if !cc.IsObject() {
				continue
			}
			existingTTL := cc.Get("ttl").String()
			if (j < index && ttl == claudeExtendedCacheTTL && existingTTL != claudeExtendedCacheTTL) ||
				(j > index && ttl == claudeDefaultCacheTTL && existingTTL == claudeExtendedCacheTTL) {
				ordered = false
				break
			}
		}
		if cc := gjson.GetBytes(jsonData, "cache_control"); cc.IsObject() &&
			ttl == claudeDefaultCacheTTL && cc.Get("ttl").String() == claudeExtendedCacheTTL {
			ordered = false
		}
		if !ordered {
			continue
		}
		cacheControl := fmt.Sprintf(`{"type":"ephemeral","ttl":%q}`, ttl)
		var err error
		if jsonData, err = sjson.SetRawBytes(jsonData, path, []byte(cacheControl)); err != nil {
			return jsonData, changed, err
		}
		changed = true
	}
	return jsonData, changed, nil
}

type claudeCacheBlockRef struct {
	prefix string
	msg    int
	block  int
}

func (r claudeCacheBlockRef) path() string {
	if r.prefix != "" {
		return fmt.Sprintf("%s.%d", r.prefix, r.block)
	}
	return fmt.Sprintf("messages.%d.content.%d", r.msg, r.block)
}

func claudeCacheBlockRefs(jsonData []byte) []claudeCacheBlockRef {
	refs := make([]claudeCacheBlockRef, 0)
	appendBlocks := func(prefix string, blocks gjson.Result) {
		if !blocks.IsArray() {
			return
		}
		for index := range blocks.Array() {
			refs = append(refs, claudeCacheBlockRef{prefix: prefix, block: index})
		}
	}
	appendBlocks("tools", gjson.GetBytes(jsonData, "tools"))
	system := gjson.GetBytes(jsonData, "system")
	if system.IsArray() {
		appendBlocks("system", system)
	} else if system.Type == gjson.String {
		refs = append(refs, claudeCacheBlockRef{prefix: "system", block: -1})
	}
	gjson.GetBytes(jsonData, "messages").ForEach(func(index, message gjson.Result) bool {
		content := message.Get("content")
		if content.IsArray() {
			for block := range content.Array() {
				refs = append(refs, claudeCacheBlockRef{msg: int(index.Int()), block: block})
			}
		} else if content.Type == gjson.String {
			refs = append(refs, claudeCacheBlockRef{msg: int(index.Int()), block: -1})
		}
		return true
	})
	return refs
}
