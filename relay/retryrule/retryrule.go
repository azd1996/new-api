// Package retryrule powers error-triggered request rewrites ("retry override").
//
// A channel's RetryOverride is a list of param-override operations. Unlike
// ParamOverride, each operation's conditions are evaluated against the upstream
// RESPONSE context ({status_code, error_message, relay_format}). On an upstream
// error, matching operations are collected (with their conditions stripped) and
// applied to the request body, then the request is retried once on the same
// channel.
//
// Condition evaluation is self-contained here (supporting full/contains/prefix/
// suffix, matching the param-override modes the feature needs) so this package
// depends only on types and does not require exporting override internals.
package retryrule

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/types"
)

// Retry actions decide, on a matched retry-override, where the rewritten request
// is retried:
//
//   - ActionRetrySameChannel (default): pin the current channel and retry it once
//     without consuming retry budget.
//   - ActionFallbackNextChannel: do not retry the current channel; hand off to the
//     normal retry/fallback loop so the request advances to the next channel.
const (
	ActionRetrySameChannel    = "retry_same_channel"
	ActionFallbackNextChannel = "fallback_next_channel"
)

// ResponseContext builds the document that retry-override conditions match
// against on an error: the upstream response status code and error message. When
// the status is 200 (a body-detected pseudo error, see SuccessContext), the error
// message doubles as response_body so `response_body` conditions keep matching in
// the retry hook.
func ResponseContext(apiErr *types.NewAPIError, relayFormat string) map[string]any {
	ctx := map[string]any{"relay_format": relayFormat}
	if apiErr != nil {
		ctx["status_code"] = apiErr.StatusCode
		ctx["error_message"] = apiErr.Error()
		if apiErr.StatusCode == 200 {
			ctx["response_body"] = apiErr.Error()
		}
	}
	return ctx
}

// SuccessContext builds the match document for a successful (2xx) upstream
// response whose body may still carry an embedded error (e.g. a rate-limit
// message returned with HTTP 200). Conditions match against `response_body`.
func SuccessContext(relayFormat string, statusCode int, body string) map[string]any {
	return map[string]any{
		"relay_format":  relayFormat,
		"status_code":   statusCode,
		"response_body": body,
	}
}

// ShouldTriggerOnBody reports whether any operation's conditions match the given
// context. It is used on the success path to decide whether a 200 response body
// should be treated as a retryable error.
func ShouldTriggerOnBody(ops []map[string]any, ctx map[string]any) bool {
	for _, op := range ops {
		if operationMatches(op, ctx) {
			return true
		}
	}
	return false
}

// CollectRewrites evaluates each operation's conditions against the context and
// returns the matching operations with their "conditions"/"logic"/"action" keys
// removed, so they can be applied unconditionally to the request body on retry.
// The bool reports whether any operation matched (i.e. whether to retry), even
// when a matched operation carries no rewrite (conditions+action only). The
// action reports where to retry: ActionFallbackNextChannel if any matched
// operation requests it, otherwise ActionRetrySameChannel. When the trigger is a
// 200 response body (ctx.status_code == 200), the action is forced to
// ActionFallbackNextChannel — retrying the same rate-limited channel is pointless.
func CollectRewrites(ops []map[string]any, ctx map[string]any) ([]map[string]any, bool, string) {
	rewrites := make([]map[string]any, 0, len(ops))
	matched := false
	action := ActionRetrySameChannel
	for _, op := range ops {
		if !operationMatches(op, ctx) {
			continue
		}
		matched = true
		if resolveAction(op) == ActionFallbackNextChannel {
			action = ActionFallbackNextChannel
		}
		clone := make(map[string]any, len(op))
		for k, v := range op {
			if k == "conditions" || k == "logic" || k == "action" {
				continue
			}
			clone[k] = v
		}
		// Only stage operations that actually rewrite the body; a conditions+action
		// only rule (pure fallback) matches but stages no rewrite.
		if _, ok := clone["mode"]; ok {
			rewrites = append(rewrites, clone)
		}
	}
	if matched && toString(ctx["status_code"]) == "200" {
		action = ActionFallbackNextChannel
	}
	return rewrites, matched, action
}

// resolveAction reads an operation's action, defaulting to (and falling back to
// on unknown values) ActionRetrySameChannel.
func resolveAction(op map[string]any) string {
	raw, _ := op["action"].(string)
	switch strings.TrimSpace(raw) {
	case ActionFallbackNextChannel:
		return ActionFallbackNextChannel
	default:
		return ActionRetrySameChannel
	}
}

// operationMatches reports whether an operation's conditions match the context.
// An operation without conditions always matches (same as param-override).
func operationMatches(op map[string]any, ctx map[string]any) bool {
	raw, ok := op["conditions"]
	if !ok {
		return true
	}
	conds, ok := raw.([]any)
	if !ok || len(conds) == 0 {
		return true
	}
	useOr := false
	if l, ok := op["logic"].(string); ok && strings.EqualFold(l, "OR") {
		useOr = true
	}
	for _, c := range conds {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		res := conditionMatches(cm, ctx)
		if useOr && res {
			return true
		}
		if !useOr && !res {
			return false
		}
	}
	// AND with no failures -> true; OR with no successes -> false.
	return !useOr
}

func conditionMatches(cond map[string]any, ctx map[string]any) bool {
	path, _ := cond["path"].(string)
	if path == "" {
		return false
	}
	actual, exists := ctx[path]
	if !exists {
		if pass, ok := cond["pass_missing_key"].(bool); ok && pass {
			return true
		}
		return false
	}
	mode, _ := cond["mode"].(string)
	res := compareValues(toString(actual), toString(cond["value"]), strings.ToLower(mode))
	if invert, ok := cond["invert"].(bool); ok && invert {
		return !res
	}
	return res
}

func compareValues(actual, want, mode string) bool {
	switch mode {
	case "", "full":
		return actual == want
	case "contains":
		return strings.Contains(actual, want)
	case "prefix":
		return strings.HasPrefix(actual, want)
	case "suffix":
		return strings.HasSuffix(actual, want)
	default:
		return false
	}
}

func toString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case bool:
		return strconv.FormatBool(x)
	default:
		return ""
	}
}
