package dto

import (
	"fmt"
	"regexp"
	"strings"
)

// RetryRuleTargetOriginalChannel retries the rewritten request against the same
// channel that just failed, instead of advancing to a lower-priority channel.
const RetryRuleTargetOriginalChannel = "original_channel"

// RetryRule describes an error-triggered request rewrite followed by a retry.
//
// It is a per-channel config stored inside ChannelSettings (the channel
// `setting` JSON column). Unlike param_override, which rewrites every outgoing
// request unconditionally, a RetryRule only fires after an upstream error whose
// shape matches Match, rewrites the request body once, and retries.
//
// Transform reuses the param_override operation vocabulary: each element is one
// operation (the same shape as a channel param_override "operations" entry, e.g.
// {"mode":"prune_objects","path":"messages.#.content","value":{"where":{"type":"thinking"}}}).
// It is intentionally kept as free-form maps so this package (dto) does not
// depend on relay/common; the retry-rule engine converts and applies them via
// the existing override executor. Deep per-operation mode validation therefore
// happens at apply time in the engine; Validate here only enforces structure.
type RetryRule struct {
	Name      string           `json:"name,omitempty"`
	Match     RetryRuleMatch   `json:"match"`
	Transform []map[string]any `json:"transform,omitempty"`
	Retry     RetryRuleRetry   `json:"retry,omitempty"`
}

// RetryRuleMatch decides when a rule fires; it is evaluated against the upstream
// error (status code + error message), optionally scoped to a relay format.
type RetryRuleMatch struct {
	StatusCodes []int  `json:"status_codes,omitempty"`
	ErrorRegex  string `json:"error_regex,omitempty"`
	RelayFormat string `json:"relay_format,omitempty"`
}

// RetryRuleRetry controls how the retry is performed after the transform.
// Target defaults to the original channel when empty.
type RetryRuleRetry struct {
	Target      string `json:"target,omitempty"`
	MaxAttempts int    `json:"max_attempts,omitempty"`
}

// Validate checks the structural validity of a single rule. It rejects rules
// that could never match or could never rewrite anything, and rejects an
// invalid error regex or unsupported retry target so bad rules fail at save
// time rather than silently at request time.
func (r RetryRule) Validate() error {
	if len(r.Match.StatusCodes) == 0 && strings.TrimSpace(r.Match.ErrorRegex) == "" {
		return fmt.Errorf("retry rule %q: match must specify status_codes or error_regex", r.Name)
	}
	for _, code := range r.Match.StatusCodes {
		if code < 100 || code > 599 {
			return fmt.Errorf("retry rule %q: invalid status code %d", r.Name, code)
		}
	}
	if strings.TrimSpace(r.Match.ErrorRegex) != "" {
		if _, err := regexp.Compile(r.Match.ErrorRegex); err != nil {
			return fmt.Errorf("retry rule %q: invalid error_regex: %w", r.Name, err)
		}
	}
	if len(r.Transform) == 0 {
		return fmt.Errorf("retry rule %q: transform must contain at least one operation", r.Name)
	}
	for i, op := range r.Transform {
		mode, _ := op["mode"].(string)
		if strings.TrimSpace(mode) == "" {
			return fmt.Errorf("retry rule %q: transform[%d] missing mode", r.Name, i)
		}
	}
	if r.Retry.Target != "" && r.Retry.Target != RetryRuleTargetOriginalChannel {
		return fmt.Errorf("retry rule %q: unsupported retry target %q", r.Name, r.Retry.Target)
	}
	if r.Retry.MaxAttempts < 0 {
		return fmt.Errorf("retry rule %q: max_attempts must be >= 0", r.Name)
	}
	return nil
}

// ValidateRetryRules validates every rule in a channel's RetryRules list.
func ValidateRetryRules(rules []RetryRule) error {
	for i := range rules {
		if err := rules[i].Validate(); err != nil {
			return err
		}
	}
	return nil
}
