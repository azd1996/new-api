package openai

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/retryrule"
	"github.com/QuantumNous/new-api/types"
)

// retryBodyGuardMaxBytes caps how much of a success body is scanned for the
// retry-override body match, to keep the check cheap on large responses.
const retryBodyGuardMaxBytes = 16 * 1024

// retryOverridePrecommitMaxChunks bounds how many leading stream chunks are held
// in the pre-commit buffer before forcing a commit. A small default guards
// against a misconfigured channel (rule present but no stream-buffer size set)
// buffering an unbounded number of chunks.
const retryOverridePrecommitMaxChunks = 3

// precommitChunks resolves the per-channel pre-commit buffer size, falling back
// to retryOverridePrecommitMaxChunks when unset (0) or negative. ChannelSetting
// is promoted from the embedded *ChannelMeta, so it is only safe to read when
// ChannelMeta is non-nil.
func precommitChunks(info *relaycommon.RelayInfo) int {
	if info != nil && info.ChannelMeta != nil {
		if n := info.ChannelSetting.RetryOverrideStreamPrecommitChunks; n > 0 {
			return n
		}
	}
	return retryOverridePrecommitMaxChunks
}

// precommitGuard implements the "buffer before commit" strategy for streaming
// retry-override body detection. Before the commit point (the first real content
// chunk, or a chunk-count cap), chunks are held and scanned; if a rule matches,
// nothing has been forwarded to the client so the relay loop can fall back
// cleanly. Once committed, buffered chunks are flushed in order and subsequent
// chunks pass through untouched (the guard no longer intercepts).
type precommitGuard struct {
	info       *relaycommon.RelayInfo
	statusCode int
	maxChunks  int
	buffer     []string
	committed  bool
	matched    *types.NewAPIError
}

// newPrecommitGuard builds a guard for one streaming attempt. When the channel
// has no retry_override rules the guard starts committed, so every chunk passes
// through with zero buffering — i.e. no behavior change for the common case.
func newPrecommitGuard(info *relaycommon.RelayInfo, statusCode int) *precommitGuard {
	g := &precommitGuard{
		info:       info,
		statusCode: statusCode,
		maxChunks:  precommitChunks(info),
	}
	if info == nil || info.ChannelMeta == nil || len(info.ChannelSetting.RetryOverride) == 0 {
		g.committed = true
	}
	return g
}

// feed handles one upstream chunk. isContent tells the guard whether this chunk
// is a commit signal (the first non-preamble chunk for the format). process is
// the handler's normal per-chunk logic (parse + forward + usage accounting).
// It returns stop=true when a rule matched in the pre-commit window; the caller
// must then abort the upstream scan (sr.Stop) so no further chunks are read.
func (g *precommitGuard) feed(data string, isContent bool, process func(string)) (stop bool) {
	if g.committed {
		process(data)
		return false
	}
	if len(data) > 0 {
		if e := retryBodyGuardError(g.info, g.statusCode, common.StringToByteSlice(data)); e != nil {
			g.matched = e
			return true
		}
	}
	g.buffer = append(g.buffer, data)
	if isContent || len(g.buffer) >= g.maxChunks {
		g.commit(process)
	}
	return false
}

// commit flushes all buffered chunks in order and switches to pass-through mode.
func (g *precommitGuard) commit(process func(string)) {
	g.committed = true
	for _, d := range g.buffer {
		process(d)
	}
	g.buffer = nil
}

// finish flushes any still-buffered chunks after the upstream stream ends without
// matching (e.g. a short response that finished inside the pre-commit window).
func (g *precommitGuard) finish(process func(string)) {
	if !g.committed {
		g.commit(process)
	}
}

// retryBodyGuardError inspects a successful (2xx) upstream response body against
// the channel's retry_override rules. When a rule's conditions match the body
// (e.g. a rate-limit message returned with HTTP 200), it returns a synthesized
// retryable error carrying the body, so the relay loop enters the retry-override
// fallback path (which, for a 200 trigger, always falls back to the next
// channel). Returns nil when there is nothing to do.
func retryBodyGuardError(info *relaycommon.RelayInfo, statusCode int, body []byte) *types.NewAPIError {
	if info == nil || info.ChannelMeta == nil {
		return nil
	}
	ops := info.ChannelSetting.RetryOverride
	if len(ops) == 0 {
		return nil
	}
	snippet := body
	if len(snippet) > retryBodyGuardMaxBytes {
		snippet = snippet[:retryBodyGuardMaxBytes]
	}
	reqCtx := relaycommon.BuildParamOverrideContext(info)
	if reqCtx == nil {
		reqCtx = map[string]any{}
	}
	reqCtx["relay_format"] = string(info.RelayFormat)
	respCtx := retryrule.SuccessContext(string(info.RelayFormat), statusCode, string(snippet))
	if m, ok := reqCtx["model"]; ok {
		respCtx["model"] = m
	}
	if !retryrule.ShouldTriggerOnBody(ops, reqCtx, respCtx) {
		return nil
	}
	return types.NewErrorWithStatusCode(errors.New(string(snippet)), types.ErrorCodeBadResponseStatusCode, statusCode)
}

// isChatRolePreamble reports whether a chat-completions stream chunk is a
// role-only preamble delta (e.g. {"role":"assistant"} with no content). It is the
// leading chunk that the duplicate-preamble drop switch swallows on a fallback
// continuation, so the client does not see a second assistant-role preamble.
func isChatRolePreamble(resp *dto.ChatCompletionsStreamResponse) bool {
	if resp == nil || len(resp.Choices) == 0 {
		return false
	}
	choice := resp.Choices[0]
	if choice.FinishReason != nil {
		return false
	}
	delta := choice.Delta
	return delta.Role != "" && delta.GetContentString() == "" &&
		delta.GetReasoningContent() == "" && len(delta.ToolCalls) == 0
}

// isChatStreamCommit reports whether a chat-completions stream chunk is past the
// role-only preamble — the first non-preamble chunk is the pre-commit buffer's
// commit point.
func isChatStreamCommit(data string) bool {
	var probe dto.ChatCompletionsStreamResponse
	if err := common.UnmarshalJsonStr(data, &probe); err != nil {
		return true // unknown/garbled → commit rather than keep buffering
	}
	return !isChatRolePreamble(&probe)
}
