// Package retryrule powers error-triggered request rewrites ("retry override").
//
// A channel's RetryOverride is an ordered list of four-phase rules
// (dto.RetryRule). Rules are priority-ordered and short-circuit as a group: the
// FIRST rule whose phase1 & phase2 both pass fully decides the outcome, and
// evaluation stops there.
//
//   - phase1 (request condition): matched against the request context
//     (model / relay_format / group, etc.). If it fails, the rule is skipped.
//   - phase2 (response condition): matched against the upstream response context
//     ({status_code, error_message, response_body, relay_format, model}).
//   - phase3 (request rewrite): a list of param-override operations applied to
//     the request body when phase1 & phase2 pass.
//   - phase4 (retry action): what to do next — retry the same channel, fall back
//     to the next channel, or terminate and return a configured error.
//
// phase1/2/3 conditions share the exact param-override semantics by delegating
// to relay/common.EvaluateConditions — there is no second condition evaluator.
package retryrule

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"golang.org/x/net/http/httpguts"
)

// Retry actions decide, on a matched retry-override, what happens next:
//
//   - ActionRetrySameChannel (default): pin the current channel and retry it once
//     without consuming retry budget.
//   - ActionFallbackNextChannel: do not retry the current channel; hand off to the
//     normal retry/fallback loop so the request advances to the next channel.
//   - ActionReturnError: stop retrying entirely and return a configured HTTP
//     status (and optional message) to the caller.
const (
	ActionRetrySameChannel    = "retry_same_channel"
	ActionFallbackNextChannel = "fallback_next_channel"
	ActionReturnError         = "return_error"
)

// defaultReturnErrorStatus is used for ActionReturnError when the rule omits
// status_code or sets it outside the valid HTTP range.
const defaultReturnErrorStatus = http.StatusTooManyRequests

// managedReturnErrorHeaders are owned by the response writer or the relay error
// renderer and cannot be set by a rule, matching the param-override return_error
// constraints.
var managedReturnErrorHeaders = map[string]struct{}{
	"Content-Length":    {},
	"Transfer-Encoding": {},
	"Connection":        {},
	"Keep-Alive":        {},
	"Trailer":           {},
	"Upgrade":           {},
	"Proxy-Connection":  {},
	"Content-Type":      {},
	"Content-Encoding":  {},
}

// sanitizeReturnErrorHeaders canonicalizes the configured header names and drops
// entries that are malformed or reserved. Dropping rather than failing keeps a
// bad header from disabling the whole rule: the status/message override still
// applies.
func sanitizeReturnErrorHeaders(configured map[string]string) map[string]string {
	if len(configured) == 0 {
		return nil
	}
	headers := make(map[string]string, len(configured))
	for name, value := range configured {
		if !httpguts.ValidHeaderFieldName(name) || !httpguts.ValidHeaderFieldValue(value) {
			continue
		}
		canonical := http.CanonicalHeaderKey(name)
		if _, managed := managedReturnErrorHeaders[canonical]; managed {
			continue
		}
		headers[canonical] = value
	}
	if len(headers) == 0 {
		return nil
	}
	return headers
}

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

// Decision is the outcome of evaluating a channel's retry-override rules against
// one failed or suspect response. Because rules are priority-ordered, it carries
// the result of the single first-matching rule.
type Decision struct {
	Matched       bool
	Rewrites      []map[string]any
	Action        string
	ReturnStatus  int    // only meaningful when Action == ActionReturnError
	ReturnMessage string // only meaningful when Action == ActionReturnError
	// ReturnHeaders are the sanitized response_headers of a return_error rule;
	// they reach the caller only if this error is the one returned.
	ReturnHeaders map[string]string
	MatchedIndex  int // index into rules of the matched rule, -1 when none
}

// Applicable reports whether the matched rule's action can still run for this
// request. Only retry_same_channel is limited: it fires at most once per request
// (sameChannelRetried, i.e. RelayInfo.RetryFallbackDone). return_error and
// fallback_next_channel stay applicable after that one retry, so a rule such as
// "200 rate limit -> 429" still applies on the request a same-channel retry made.
func (d Decision) Applicable(sameChannelRetried bool) bool {
	if !d.Matched {
		return false
	}
	return d.Action != ActionRetrySameChannel || !sameChannelRetried
}

// CollectRewrites evaluates rules in order and returns the Decision for the FIRST
// rule whose phase1 (reqCtx) and phase2 (respCtx) both pass. That rule fully
// decides the outcome: its phase4 action (ActionRetrySameChannel default,
// ActionFallbackNextChannel, or ActionReturnError with a resolved status), its
// phase3 rewrites, and its index (for auditing). Evaluation stops at the first
// match; later rules are not consulted. When no rule matches, Matched is false
// and Action defaults to ActionRetrySameChannel. The resolved action always
// honors the matched rule's phase4, including for 200-body triggers.
func CollectRewrites(rules []dto.RetryRule, reqCtx, respCtx map[string]any) Decision {
	reqDoc := marshalContext(reqCtx)
	respDoc := marshalContext(respCtx)
	for i := range rules {
		rule := rules[i]
		if !rulePhasesPass(rule, reqDoc, respDoc) {
			continue
		}
		action, status, message, headers := resolveAction(rule.Phase4RetryAction)
		rewrites := make([]map[string]any, 0, len(rule.Phase3RequestRewrite))
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
		return Decision{
			Matched:       true,
			Rewrites:      rewrites,
			Action:        action,
			ReturnStatus:  status,
			ReturnMessage: message,
			ReturnHeaders: headers,
			MatchedIndex:  i,
		}
	}
	return Decision{Action: ActionRetrySameChannel, MatchedIndex: -1}
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

// resolveAction reads a rule's phase4 action, defaulting to (and falling back to
// on nil/unknown values) ActionRetrySameChannel. For ActionReturnError it also
// resolves the status code (defaultReturnErrorStatus when missing/out of range),
// the optional custom message, and the optional response headers.
func resolveAction(a *dto.RetryAction) (action string, returnStatus int, returnMessage string, returnHeaders map[string]string) {
	if a == nil {
		return ActionRetrySameChannel, 0, "", nil
	}
	switch a.Action {
	case ActionFallbackNextChannel:
		return ActionFallbackNextChannel, 0, "", nil
	case ActionReturnError:
		status := a.StatusCode
		if status < 100 || status > 599 {
			status = defaultReturnErrorStatus
		}
		return ActionReturnError, status, a.StatusMsg, sanitizeReturnErrorHeaders(a.ResponseHeaders)
	default:
		return ActionRetrySameChannel, 0, "", nil
	}
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
