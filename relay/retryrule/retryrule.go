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
// against: the upstream response status code and error message.
func ResponseContext(apiErr *types.NewAPIError, relayFormat string) map[string]any {
	ctx := map[string]any{"relay_format": relayFormat}
	if apiErr != nil {
		ctx["status_code"] = apiErr.StatusCode
		ctx["error_message"] = apiErr.Error()
	}
	return ctx
}

// CollectRewrites evaluates each operation's conditions against the response
// context and returns the matching operations with their "conditions"/"logic"/
// "action" keys removed, so they can be applied unconditionally to the request
// body on retry. The bool reports whether any operation matched (i.e. whether to
// retry). The action reports where to retry: ActionFallbackNextChannel if any
// matched operation requests it, otherwise ActionRetrySameChannel.
func CollectRewrites(ops []map[string]any, ctx map[string]any) ([]map[string]any, bool, string) {
	matched := make([]map[string]any, 0, len(ops))
	action := ActionRetrySameChannel
	for _, op := range ops {
		if !operationMatches(op, ctx) {
			continue
		}
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
		matched = append(matched, clone)
	}
	return matched, len(matched) > 0, action
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
