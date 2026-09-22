// Package retryrule powers error-triggered request rewrites ("retry override").
//
// A channel's RetryOverride is an ordered list of four-phase rules
// (dto.RetryRule). Each rule short-circuits through its phases:
//
//   - phase1 (request condition): matched against the request context
//     (model / relay_format / group, etc.). If it fails, the rule is skipped.
//   - phase2 (response condition): matched against the upstream response context
//     ({status_code, error_message, response_body, relay_format, model}).
//   - phase3 (request rewrite): a list of param-override operations applied to
//     the request body when phase1 & phase2 pass.
//   - phase4 (retry action): where to retry (same channel vs next channel).
//
// phase1/2/3 conditions share the exact param-override semantics by delegating
// to relay/common.EvaluateConditions — there is no second condition evaluator.
package retryrule

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
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

// ResponseContext builds the phase2 match document for an upstream error: status
// code and error message. When the status is 200 (a body-detected pseudo error,
// see SuccessContext), the error message doubles as response_body so
// `response_body` conditions keep matching in the retry hook.
func ResponseContext(statusCode int, errorMessage, relayFormat string) map[string]any {
	ctx := map[string]any{
		"relay_format":  relayFormat,
		"status_code":   statusCode,
		"error_message": errorMessage,
	}
	if statusCode == 200 {
		ctx["response_body"] = errorMessage
	}
	return ctx
}

// SuccessContext builds the phase2 match document for a successful (2xx) upstream
// response whose body may still carry an embedded error (e.g. a rate-limit
// message returned with HTTP 200). Conditions match against `response_body`.
func SuccessContext(relayFormat string, statusCode int, body string) map[string]any {
	return map[string]any{
		"relay_format":  relayFormat,
		"status_code":   statusCode,
		"response_body": body,
	}
}

// ShouldTriggerOnBody reports whether any rule's phase1+phase2 conditions match.
// It is used on the success path to decide whether a 200 response body should be
// treated as a retryable error.
func ShouldTriggerOnBody(rules []dto.RetryRule, reqCtx, respCtx map[string]any) bool {
	reqDoc := marshalContext(reqCtx)
	respDoc := marshalContext(respCtx)
	for i := range rules {
		if rulePhasesPass(rules[i], reqDoc, respDoc) {
			return true
		}
	}
	return false
}

// CollectRewrites evaluates each rule's phase1 (reqCtx) and phase2 (respCtx). For
// each matching rule it stages the phase3 rewrite operations to apply to the
// request body on retry, reports whether any rule matched, resolves the retry
// action (ActionFallbackNextChannel if any matched rule requests it, otherwise
// ActionRetrySameChannel), and returns the indices (into rules) of every matched
// rule so callers can audit which rule triggered. When the trigger is a 200
// response body (respCtx.status_code == 200) the action is forced to
// ActionFallbackNextChannel — retrying the same rate-limited channel is pointless.
func CollectRewrites(rules []dto.RetryRule, reqCtx, respCtx map[string]any) ([]map[string]any, bool, string, []int) {
	reqDoc := marshalContext(reqCtx)
	respDoc := marshalContext(respCtx)
	rewrites := make([]map[string]any, 0)
	matchedIndices := make([]int, 0)
	matched := false
	action := ActionRetrySameChannel
	for i := range rules {
		rule := rules[i]
		if !rulePhasesPass(rule, reqDoc, respDoc) {
			continue
		}
		matched = true
		matchedIndices = append(matchedIndices, i)
		if resolveAction(rule.Phase4RetryAction) == ActionFallbackNextChannel {
			action = ActionFallbackNextChannel
		}
		for _, rewrite := range rule.Phase3RequestRewrite {
			if rewrite == nil {
				continue
			}
			// Only stage operations that actually rewrite the body; a rule with an
			// empty phase3 still matches (pure detect + action) but stages nothing.
			if _, ok := rewrite["mode"]; ok {
				rewrites = append(rewrites, rewrite)
			}
		}
	}
	if matched && toString(respCtx["status_code"]) == "200" {
		action = ActionFallbackNextChannel
	}
	return rewrites, matched, action, matchedIndices
}

// rulePhasesPass reports whether a rule's phase1 (against reqDoc) and phase2
// (against respDoc) conditions both pass. A missing/empty phase always passes. A
// condition that fails to parse is treated as not matching (never fatal).
func rulePhasesPass(rule dto.RetryRule, reqDoc, respDoc []byte) bool {
	return phasePass(rule.Phase1RequestCondition, reqDoc) &&
		phasePass(rule.Phase2ResponseConditions, respDoc)
}

func phasePass(phase *dto.RetryRulePhaseCondition, doc []byte) bool {
	if phase == nil || len(phase.Conditions) == 0 {
		return true
	}
	ok, err := relaycommon.EvaluateConditions(doc, "", phase.Conditions, phase.Logic)
	if err != nil {
		return false
	}
	return ok
}

// resolveAction reads a rule's action, defaulting to (and falling back to on
// unknown values) ActionRetrySameChannel.
func resolveAction(raw string) string {
	if raw == ActionFallbackNextChannel {
		return ActionFallbackNextChannel
	}
	return ActionRetrySameChannel
}

func marshalContext(ctx map[string]any) []byte {
	if len(ctx) == 0 {
		return []byte("{}")
	}
	data, err := common.Marshal(ctx)
	if err != nil {
		return []byte("{}")
	}
	return data
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
