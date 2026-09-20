package openai

import (
	"errors"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/retryrule"
	"github.com/QuantumNous/new-api/types"
)

// retryBodyGuardMaxBytes caps how much of a success body is scanned for the
// retry-override body match, to keep the check cheap on large responses.
const retryBodyGuardMaxBytes = 16 * 1024

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
	ctx := retryrule.SuccessContext(string(info.RelayFormat), statusCode, string(snippet))
	if !retryrule.ShouldTriggerOnBody(ops, ctx) {
		return nil
	}
	return types.NewErrorWithStatusCode(errors.New(string(snippet)), types.ErrorCodeBadResponseStatusCode, statusCode)
}
