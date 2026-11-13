package service

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// errorSignatureRulesOptionKey is the Option row that persists the admin-edited
// normalization rule set. Kept in sync with the controller constant of the same
// value; defined here so the cross-instance poller can read it without importing
// the controller package.
const errorSignatureRulesOptionKey = "ErrorSignatureRules"

// SignatureRule is one ordered user classification rule. Pattern is matched
// against the complete raw error string; the first matching enabled rule
// determines the detailed error type (Name) and attribution.
type SignatureRule struct {
	Name        string `json:"name"`
	Pattern     string `json:"pattern"`
	Attribution string `json:"attribution"`
	Note        string `json:"note"`
	Enabled     bool   `json:"enabled"`
}

// maxSignatureLen caps the normalized signature so a pathologically long
// upstream error body cannot bloat the log row or the stats group key.
const maxSignatureLen = 2000

type compiledSignatureRule struct {
	name        string
	re          *regexp.Regexp
	attribution string
	note        string
}

type fallbackSignatureRule struct {
	re          *regexp.Regexp
	replacement string
}

// ErrorClassification is the write-time result used by error logs, stats and
// Prometheus. UserRuleMatched distinguishes a persisted classification from a
// fallback-only signature.
type ErrorClassification struct {
	Signature         string
	SignatureHash     string
	DetailedErrorType string
	Attribution       string
	Note              string
	UserRuleMatched   bool
}

var (
	signatureRulesMu  sync.RWMutex
	signatureRules    []compiledSignatureRule
	currentRules      []SignatureRule
	signatureRulesSet bool
)

var fallbackSignatureRules = []fallbackSignatureRule{
	{re: regexp.MustCompile(`\s+`), replacement: " "},
	{re: regexp.MustCompile(` *\(request id: [^)]*\)`), replacement: ""},
	{re: regexp.MustCompile(`(encrypted content ).*?( could not be verified)`), replacement: "${1}*${2}"},
	{re: regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`), replacement: "********-****-****-****-************"},
	{re: regexp.MustCompile(`\b(toolu|rs|resp|msg|item|fc|call|chatcmpl|req)_[A-Za-z0-9_]{6,}`), replacement: "${1}_*"},
	{re: regexp.MustCompile(`\b[0-9a-fA-F]{16,}\b`), replacement: "*"},
	{re: regexp.MustCompile(`(?i)(request[ -]?id:\s*)[A-Za-z0-9._-]+`), replacement: "${1}*"},
	{re: regexp.MustCompile(`(?i)(x-amzn-requestid:\s*)[A-Za-z0-9._-]+`), replacement: "${1}*"},
	{re: regexp.MustCompile(`(?i)((?:apim|x-ms)-request-id:\s*)[A-Za-z0-9._-]+`), replacement: "${1}*"},
	{re: regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}\S*`), replacement: "*"},
}

// SetErrorSignatureRules compiles and installs the active rule set. On any
// compile error it returns the error without changing the current rules, so a
// bad admin edit can never break signature generation. Disabled rules are
// skipped at compile time.
func SetErrorSignatureRules(rules []SignatureRule) error {
	compiled := make([]compiledSignatureRule, 0, len(rules))
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			return err
		}
		compiled = append(compiled, compiledSignatureRule{
			name:        r.Name,
			attribution: r.Attribution,
			note:        r.Note,
			re:          re,
		})
	}
	stored := make([]SignatureRule, len(rules))
	copy(stored, rules)
	signatureRulesMu.Lock()
	signatureRules = compiled
	currentRules = stored
	signatureRulesSet = true
	signatureRulesMu.Unlock()
	return nil
}

func ensureSignatureRules() {
	signatureRulesMu.RLock()
	ready := signatureRulesSet
	signatureRulesMu.RUnlock()
	if ready {
		return
	}
	// An empty user rule set is valid: fallback normalizers are hard-coded.
	_ = SetErrorSignatureRules(nil)
}

func truncateErrorSignature(content string) string {
	ensureSignatureRules()
	s := strings.TrimSpace(content)
	if len(s) > maxSignatureLen {
		s = strings.ToValidUTF8(s[:maxSignatureLen], "")
	}
	return s
}

// ClassifyError matches user rules against the complete raw error string. The
// first match determines the detailed error type and attribution. Only when no
// user rule matches are the built-in fallback normalizers applied.
func ClassifyError(content string) ErrorClassification {
	if content == "" {
		return ErrorClassification{}
	}
	ensureSignatureRules()
	signatureRulesMu.RLock()
	rules := signatureRules
	signatureRulesMu.RUnlock()
	for _, rule := range rules {
		if rule.re.MatchString(content) {
			signature := truncateErrorSignature(content)
			return ErrorClassification{
				Signature:         signature,
				SignatureHash:     ErrorSignatureHash(signature),
				DetailedErrorType: rule.name,
				Attribution:       rule.attribution,
				Note:              rule.note,
				UserRuleMatched:   true,
			}
		}
	}

	signature := content
	for _, rule := range fallbackSignatureRules {
		signature = rule.re.ReplaceAllString(signature, rule.replacement)
	}
	signature = truncateErrorSignature(signature)
	return ErrorClassification{
		Signature:     signature,
		SignatureHash: ErrorSignatureHash(signature),
	}
}

// NormalizeErrorSignature keeps the existing API for callers that only need a
// stable signature.
func NormalizeErrorSignature(content string) string {
	return ClassifyError(content).Signature
}

// ErrorSignatureHash returns a short stable fingerprint of a signature, used as
// the indexed group/filter key while the full signature is stored separately
// for display. An empty signature hashes to the empty string so "no signature"
// rows stay distinguishable.
func ErrorSignatureHash(signature string) string {
	if signature == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(signature))
	return hex.EncodeToString(sum[:])[:32]
}

// GetEffectiveErrorSignatureRules returns the rule set currently in effect
// (admin-configured if set, otherwise the seeded defaults). It is the source
// for the admin "edit rules" UI.
func GetEffectiveErrorSignatureRules() []SignatureRule {
	ensureSignatureRules()
	signatureRulesMu.RLock()
	defer signatureRulesMu.RUnlock()
	out := make([]SignatureRule, len(currentRules))
	copy(out, currentRules)
	return out
}

// ApplyErrorSignatureRulesJSON installs a persisted rule set from its JSON
// array form. An empty string is a no-op (defaults stay in effect). Invalid
// JSON or an uncompilable pattern returns an error and leaves rules unchanged.
func ApplyErrorSignatureRulesJSON(jsonStr string) error {
	if strings.TrimSpace(jsonStr) == "" {
		return nil
	}
	var rules []SignatureRule
	if err := common.Unmarshal([]byte(jsonStr), &rules); err != nil {
		return err
	}
	return SetErrorSignatureRules(rules)
}

// ApplyPersistedErrorSignatureRules compiles the admin-persisted rule set from
// the ErrorSignatureRules option, falling back to the built-in defaults when it
// is empty. Call once at startup after InitOptionMap so error logs are
// fingerprinted with the operator's rules from the very first request. Without
// it the compiled rules stay at the defaults until SyncErrorSignatureRules
// first polls — and never advance when SYNC_FREQUENCY disables that poller,
// which silently shifts every signature hash and unlinks the admin-configured
// 明细错误类型 / 错误归因 labels.
func ApplyPersistedErrorSignatureRules() error {
	common.OptionMapRWMutex.RLock()
	jsonStr := common.OptionMap[errorSignatureRulesOptionKey]
	common.OptionMapRWMutex.RUnlock()
	if strings.TrimSpace(jsonStr) == "" {
		return SetErrorSignatureRules(nil)
	}
	return ApplyErrorSignatureRulesJSON(jsonStr)
}

var (
	rulesSyncMu          sync.Mutex
	lastAppliedRulesJSON string
	lastAppliedRulesSet  bool
)

// SyncErrorSignatureRules periodically re-applies the persisted
// ErrorSignatureRules option so a rule set edited on another instance
// propagates here without a restart. Unlike plain key-value options (read from
// common.OptionMap at use time), the normalization rules are COMPILED into an
// in-process cache and are only (re)compiled at startup and in the editing
// instance's HTTP handler — so other instances would otherwise keep using their
// stale compiled rules even though they see the new option value. This mirrors
// model.SyncOptions / SyncErrorAliasCache; frequency is the SYNC_FREQUENCY
// interval in seconds, a non-positive value disables polling. A change guard
// skips the regexp recompile when the option value is unchanged.
func SyncErrorSignatureRules(frequency int) {
	if frequency <= 0 {
		return
	}
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		common.OptionMapRWMutex.RLock()
		jsonStr := common.OptionMap[errorSignatureRulesOptionKey]
		common.OptionMapRWMutex.RUnlock()

		rulesSyncMu.Lock()
		unchanged := lastAppliedRulesSet && jsonStr == lastAppliedRulesJSON
		rulesSyncMu.Unlock()
		if unchanged {
			continue
		}

		var err error
		if strings.TrimSpace(jsonStr) == "" {
			// Option cleared or never set: remove user classification rules
			// instead of leaving whatever was last compiled in place.
			err = SetErrorSignatureRules(nil)
		} else {
			err = ApplyErrorSignatureRulesJSON(jsonStr)
		}
		if err != nil {
			common.SysError("failed to sync error signature rules: " + err.Error())
			continue
		}
		rulesSyncMu.Lock()
		lastAppliedRulesJSON = jsonStr
		lastAppliedRulesSet = true
		rulesSyncMu.Unlock()
	}
}
