package relay

import (
	"encoding/binary"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/cespare/xxhash/v2"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// claudeDefaultCacheTTL is the TTL Anthropic applies to a cache_control
// breakpoint that omits "ttl".
const claudeDefaultCacheTTL = "5m"

// skeletonBuilder appends blocks to a skeleton in cache prefix order. A block
// is hashed over hashRaw (its JSON with cache markers removed) and sized by
// its original bytes.
type skeletonBuilder struct {
	row *model.CacheSkeleton
}

func (b skeletonBuilder) add(kind, name, hashRaw string, size int) {
	b.row.BlkHash = append(b.row.BlkHash, xxhash.Sum64String(hashRaw))
	b.row.BlkKind = append(b.row.BlkKind, kind)
	b.row.BlkName = append(b.row.BlkName, name)
	b.row.BlkLen = append(b.row.BlkLen, uint32(size))
}

func (b skeletonBuilder) startMessage() {
	b.row.MsgStart = append(b.row.MsgStart, uint16(len(b.row.BlkHash)))
}

// setLane separates conversations that share one session id (a client's main
// loop, sub-agents and small helper calls): same model, tools and the block
// right after the tools (the leading system / instructions block).
func (b skeletonBuilder) setLane(toolsEnd int) {
	lane := xxhash.New()
	_, _ = lane.WriteString(b.row.Model)
	var buf [8]byte
	for i := 0; i < len(b.row.BlkHash) && i <= toolsEnd; i++ {
		binary.LittleEndian.PutUint64(buf[:], b.row.BlkHash[i])
		_, _ = lane.Write(buf[:])
	}
	b.row.Lane = lane.Sum64()
}

// buildClaudeCacheSkeleton reduces a final outbound Claude Messages body to its
// prompt-cache structure. Blocks are walked in the order Anthropic builds the
// cache prefix (tools, system, messages); each block is hashed over its raw
// JSON bytes with any cache_control field removed, so moving a breakpoint does
// not change the content fingerprint. No prompt text is kept.
func buildClaudeCacheSkeleton(body []byte) *model.CacheSkeleton {
	root := gjson.ParseBytes(body)
	row := &model.CacheSkeleton{
		Ts:       time.Now(),
		Provider: model.SkeletonProviderClaude,
		Model:    root.Get("model").String(),
		Thinking: root.Get("thinking.type").String(),
		Effort:   root.Get("output_config.effort").String(),
	}
	// Thinking config and effort are rendered into the prompt, so a change
	// invalidates the cached message prefix; max_tokens/temperature are not.
	row.ParamFp = xxhash.Sum64String(row.Model + "\x00" + root.Get("tool_choice").Raw + "\x00" +
		root.Get("thinking").Raw + "\x00" + row.Effort)
	b := skeletonBuilder{row: row}

	addBlock := func(kind, name string, block gjson.Result) {
		raw := block.Raw
		if cc := block.Get("cache_control"); cc.IsObject() {
			row.BpIdx = append(row.BpIdx, uint16(len(row.BlkHash)))
			ttl := cc.Get("ttl").String()
			if ttl == "" {
				ttl = claudeDefaultCacheTTL
			}
			row.BpTtl = append(row.BpTtl, ttl)
			if stripped, err := sjson.Delete(raw, "cache_control"); err == nil {
				raw = stripped
			}
		}
		b.add(kind, name, raw, len(block.Raw))
	}

	root.Get("tools").ForEach(func(_, tool gjson.Result) bool {
		addBlock("tool", tool.Get("name").String(), tool)
		return true
	})
	toolsEnd := len(row.BlkHash)
	if system := root.Get("system"); system.Type == gjson.String {
		addBlock("system.text", "", system)
	} else {
		system.ForEach(func(_, block gjson.Result) bool {
			addBlock("system."+block.Get("type").String(), "", block)
			return true
		})
	}
	b.setLane(toolsEnd)

	root.Get("messages").ForEach(func(_, msg gjson.Result) bool {
		role := msg.Get("role").String()
		b.startMessage()
		content := msg.Get("content")
		if content.Type == gjson.String {
			addBlock(role+".text", "", content)
			return true
		}
		content.ForEach(func(_, block gjson.Result) bool {
			blockType := block.Get("type").String()
			name := ""
			if blockType == "tool_use" {
				name = block.Get("name").String()
			}
			addBlock(role+"."+blockType, name, block)
			return true
		})
		return true
	})

	// Top-level automatic caching places the breakpoint on the last block.
	if cc := root.Get("cache_control"); cc.IsObject() && len(row.BlkHash) > 0 {
		ttl := cc.Get("ttl").String()
		if ttl == "" {
			ttl = claudeDefaultCacheTTL
		}
		row.BpIdx = append(row.BpIdx, uint16(len(row.BlkHash)-1))
		row.BpTtl = append(row.BpTtl, ttl)
	}
	return row
}

// buildClaudeCacheCoverage stores a canonical, marker-free representation for
// byte-level before/after coverage analysis. Plain string message content is
// represented as one text block so gateway-added wrappers do not look like a
// content change, and cache_control itself is excluded from the byte count.
func buildClaudeCacheCoverage(body []byte) *model.CacheSkeleton {
	root := gjson.ParseBytes(body)
	row := &model.CacheSkeleton{
		Model: root.Get("model").String(),
	}
	row.BaselineParamFp = xxhash.Sum64String(row.Model + "\x00" + root.Get("tool_choice").Raw + "\x00" +
		root.Get("thinking").Raw + "\x00" + root.Get("output_config.effort").String())
	row.ParamFp = row.BaselineParamFp
	messageIndex := -1
	add := func(kind string, block gjson.Result) {
		raw := block.Raw
		if block.IsObject() {
			var fields map[string]any
			if common.Unmarshal([]byte(raw), &fields) == nil {
				delete(fields, "cache_control")
				if canonical, err := common.Marshal(fields); err == nil {
					raw = string(canonical)
				}
			}
		}
		row.CoverageHash = append(row.CoverageHash, xxhash.Sum64String(
			strconv.Itoa(messageIndex)+"\x00"+kind+"\x00"+raw))
		row.CoverageLen = append(row.CoverageLen, uint32(len(raw)))
		if block.Get("cache_control").IsObject() {
			row.CoverageBpIdx = append(row.CoverageBpIdx, uint16(len(row.CoverageHash)-1))
			ttl := block.Get("cache_control.ttl").String()
			if ttl == "" {
				ttl = claudeDefaultCacheTTL
			}
			row.CoverageBpTtl = append(row.CoverageBpTtl, ttl)
		}
	}
	addText := func(kind string, content gjson.Result) {
		block, err := sjson.Set(`{"type":"text"}`, "text", content.String())
		if err != nil {
			return
		}
		add(kind, gjson.Parse(block))
	}
	root.Get("tools").ForEach(func(_, tool gjson.Result) bool {
		add("tool", tool)
		return true
	})
	if root.Get("system").Type == gjson.String {
		addText("system.text", root.Get("system"))
	} else if root.Get("system").IsArray() {
		root.Get("system").ForEach(func(_, block gjson.Result) bool {
			add("system."+block.Get("type").String(), block)
			return true
		})
	}
	root.Get("messages").ForEach(func(_, msg gjson.Result) bool {
		messageIndex++
		content := msg.Get("content")
		if content.Type == gjson.String {
			addText(msg.Get("role").String()+".text", content)
			return true
		}
		content.ForEach(func(_, block gjson.Result) bool {
			add(msg.Get("role").String()+"."+block.Get("type").String(), block)
			return true
		})
		return true
	})
	if root.Get("cache_control").IsObject() && len(row.CoverageHash) > 0 {
		row.CoverageBpIdx = append(row.CoverageBpIdx, uint16(len(row.CoverageHash)-1))
		ttl := root.Get("cache_control.ttl").String()
		if ttl == "" {
			ttl = claudeDefaultCacheTTL
		}
		row.CoverageBpTtl = append(row.CoverageBpTtl, ttl)
	}
	return row
}

// newOpenAICacheSkeleton fills the fields shared by the Chat Completions and
// Responses skeletons. OpenAI caches the longest previously seen prompt prefix
// automatically, so there are no breakpoints; prompt_cache_key steers routing
// to a machine holding that prefix.
func newOpenAICacheSkeleton(root gjson.Result, effort string) (*model.CacheSkeleton, skeletonBuilder) {
	row := &model.CacheSkeleton{
		Ts:              time.Now(),
		Provider:        model.SkeletonProviderOpenAI,
		Model:           root.Get("model").String(),
		Effort:          effort,
		Retention:       root.Get("prompt_cache_retention").String(),
		ParentRequestId: root.Get("previous_response_id").String(),
	}
	if key := root.Get("prompt_cache_key").String(); key != "" {
		row.CacheKeyHash = xxhash.Sum64String(key)
	}
	row.ParamFp = xxhash.Sum64String(row.Model + "\x00" + root.Get("tool_choice").Raw + "\x00" + effort)
	return row, skeletonBuilder{row: row}
}

// addOpenAITools records the tool definitions, which lead the cached prefix.
func (b skeletonBuilder) addOpenAITools(tools gjson.Result) {
	tools.ForEach(func(_, tool gjson.Result) bool {
		name := tool.Get("function.name").String()
		if name == "" {
			name = tool.Get("name").String()
		}
		b.add("tool", name, tool.Raw, len(tool.Raw))
		return true
	})
}

// addOpenAIContent records one message's content: a plain string or an array
// of typed parts.
func (b skeletonBuilder) addOpenAIContent(role string, content gjson.Result) {
	if content.Type == gjson.String {
		b.add(role+".text", "", content.Raw, len(content.Raw))
		return
	}
	// gjson iterates a scalar (e.g. a null content) once as itself.
	if !content.IsArray() {
		return
	}
	content.ForEach(func(_, part gjson.Result) bool {
		b.add(role+"."+part.Get("type").String(), "", part.Raw, len(part.Raw))
		return true
	})
}

// buildOpenAIChatCacheSkeleton reduces a final outbound Chat Completions body
// to its prompt-cache structure: tools, the structured-output schema, then
// every message part and tool call. No prompt text is kept.
func buildOpenAIChatCacheSkeleton(body []byte) *model.CacheSkeleton {
	root := gjson.ParseBytes(body)
	row, b := newOpenAICacheSkeleton(root, root.Get("reasoning_effort").String())
	b.addOpenAITools(root.Get("tools"))
	if schema := root.Get("response_format"); schema.Get("type").String() == "json_schema" {
		b.add("schema", schema.Get("json_schema.name").String(), schema.Raw, len(schema.Raw))
	}
	toolsEnd := len(row.BlkHash)
	root.Get("messages").ForEach(func(_, msg gjson.Result) bool {
		role := msg.Get("role").String()
		b.startMessage()
		start := len(row.BlkHash)
		b.addOpenAIContent(role, msg.Get("content"))
		msg.Get("tool_calls").ForEach(func(_, call gjson.Result) bool {
			b.add(role+".tool_call", call.Get("function.name").String(), call.Raw, len(call.Raw))
			return true
		})
		if len(row.BlkHash) == start {
			b.add(role+".message", "", msg.Raw, len(msg.Raw))
		}
		return true
	})
	b.setLane(toolsEnd)
	return row
}

// buildOpenAIResponsesCacheSkeleton reduces a final outbound Responses body to
// its prompt-cache structure: tools, instructions, the structured-output
// schema, then every input item. With previous_response_id the earlier turns
// live upstream and only the new input is visible here.
func buildOpenAIResponsesCacheSkeleton(body []byte) *model.CacheSkeleton {
	root := gjson.ParseBytes(body)
	row, b := newOpenAICacheSkeleton(root, root.Get("reasoning.effort").String())
	b.addOpenAITools(root.Get("tools"))
	toolsEnd := len(row.BlkHash)
	if instructions := root.Get("instructions"); instructions.Exists() && instructions.Type != gjson.Null {
		b.add("instructions", "", instructions.Raw, len(instructions.Raw))
	}
	if schema := root.Get("text.format"); schema.Get("type").String() == "json_schema" {
		b.add("schema", schema.Get("name").String(), schema.Raw, len(schema.Raw))
	}
	input := root.Get("input")
	if input.Type == gjson.String {
		b.startMessage()
		b.add("user.text", "", input.Raw, len(input.Raw))
		input = gjson.Result{}
	}
	input.ForEach(func(_, item gjson.Result) bool {
		b.startMessage()
		itemType := item.Get("type").String()
		switch itemType {
		case "", "message":
			start := len(row.BlkHash)
			b.addOpenAIContent(item.Get("role").String(), item.Get("content"))
			if len(row.BlkHash) == start {
				b.add(item.Get("role").String()+".message", "", item.Raw, len(item.Raw))
			}
		case "function_call", "custom_tool_call":
			b.add("assistant."+itemType, item.Get("name").String(), item.Raw, len(item.Raw))
		case "function_call_output", "custom_tool_call_output":
			b.add("tool."+itemType, "", item.Raw, len(item.Raw))
		case "reasoning":
			b.add("assistant.reasoning", "", item.Raw, len(item.Raw))
		default:
			b.add("item."+itemType, item.Get("name").String(), item.Raw, len(item.Raw))
		}
		return true
	})
	b.setLane(toolsEnd)
	return row
}

// finishCacheSkeleton fills the response-side fields of a skeleton built
// before the upstream call and hands it to the background writer. usage is nil
// when the attempt failed before a usable response.
func finishCacheSkeleton(c *gin.Context, info *relaycommon.RelayInfo, row *model.CacheSkeleton, usage *dto.Usage, apiErr *types.NewAPIError) {
	row.EndTs = time.Now()
	row.Status = "ok"
	if apiErr != nil || usage == nil {
		row.Status = "error"
	}
	row.RequestId = c.GetString(common.RequestIdKey)
	row.Attempt = uint8(min(info.RetryIndex, 255))
	row.Node = common.NodeName
	row.TokenId = uint32(max(info.TokenId, 0))
	if info.ChannelMeta != nil {
		row.ChannelId = uint32(max(info.ChannelId, 0))
		row.KeyIdx = uint16(max(info.ChannelMultiKeyIndex, 0))
	}
	if sid := common.GetContextKeyString(c, constant.ContextKeySessionID); sid != sessionIDNotFound {
		row.SessionId = strings.TrimSpace(sid)
	}
	if usage != nil {
		cacheRead := max(usage.PromptTokensDetails.CachedTokens, 0)
		cacheCreation := max(usage.PromptTokensDetails.CacheCreationTokensTotal(), 0)
		// Anthropic-semantic input_tokens already exclude cache reads and
		// writes; OpenAI-style prompt totals include them.
		input := usage.PromptTokens
		if usage.UsageSemantic != dto.BillingUsageSemanticAnthropic {
			input -= cacheRead + cacheCreation
		}
		row.CacheRead = uint32(cacheRead)
		row.CacheCreation = uint32(cacheCreation)
		row.Input = uint32(max(input, 0))
	}
	if model.CacheSkeletonEnabled() {
		model.EnqueueCacheSkeleton(row)
	}
}
