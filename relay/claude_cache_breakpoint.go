package relay

import (
	"fmt"
	"math"
	"strings"

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
	full := len(claudeCacheControlPaths(jsonData)) >= claudeMaxCacheBreakpoints
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
