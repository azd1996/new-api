package relay

import (
	"encoding/binary"
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

// buildClaudeCacheSkeleton reduces a final outbound Claude Messages body to its
// prompt-cache structure. Blocks are walked in the order Anthropic builds the
// cache prefix (tools, system, messages); each block is hashed over its raw
// JSON bytes with any cache_control field removed, so moving a breakpoint does
// not change the content fingerprint. No prompt text is kept.
func buildClaudeCacheSkeleton(body []byte) *model.CacheSkeleton {
	root := gjson.ParseBytes(body)
	row := &model.CacheSkeleton{
		Ts:       time.Now(),
		Model:    root.Get("model").String(),
		Thinking: root.Get("thinking.type").String(),
		Effort:   root.Get("output_config.effort").String(),
	}
	// Thinking config and effort are rendered into the prompt, so a change
	// invalidates the cached message prefix; max_tokens/temperature are not.
	row.ParamFp = xxhash.Sum64String(row.Model + "\x00" + root.Get("tool_choice").Raw + "\x00" +
		root.Get("thinking").Raw + "\x00" + row.Effort)

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
		row.BlkHash = append(row.BlkHash, xxhash.Sum64String(raw))
		row.BlkKind = append(row.BlkKind, kind)
		row.BlkName = append(row.BlkName, name)
		row.BlkLen = append(row.BlkLen, uint32(len(block.Raw)))
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
	// lane separates conversations that share one session id (Claude Code's
	// main loop, sub-agents and small helper calls): same model, tools and
	// leading system block.
	lane := xxhash.New()
	_, _ = lane.WriteString(row.Model)
	var buf [8]byte
	for i := 0; i < len(row.BlkHash) && i <= toolsEnd; i++ {
		binary.LittleEndian.PutUint64(buf[:], row.BlkHash[i])
		_, _ = lane.Write(buf[:])
	}
	row.Lane = lane.Sum64()

	root.Get("messages").ForEach(func(_, msg gjson.Result) bool {
		role := msg.Get("role").String()
		row.MsgStart = append(row.MsgStart, uint16(len(row.BlkHash)))
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

// finishClaudeCacheSkeleton fills the response-side fields of a skeleton built
// before the upstream call and hands it to the background writer. usage is nil
// when the attempt failed before a usable response.
func finishClaudeCacheSkeleton(c *gin.Context, info *relaycommon.RelayInfo, row *model.CacheSkeleton, usage *dto.Usage, apiErr *types.NewAPIError) {
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
	model.EnqueueCacheSkeleton(row)
}
