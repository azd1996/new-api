package service

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

// SignatureRule is one ordered normalization step applied to a raw error
// message to derive its stable "signature". Each rule is a pure regexp
// substitution: every match of Pattern is replaced by Replacement (which may
// reference capture groups via $1 / ${1}). Rules are admin-editable and seeded
// with DefaultErrorSignatureRules; because new-api runs on Go's RE2 engine
// there is no catastrophic backtracking to guard against.
type SignatureRule struct {
	Name        string `json:"name"`
	Pattern     string `json:"pattern"`
	Replacement string `json:"replacement"`
	Enabled     bool   `json:"enabled"`
}

// maxSignatureLen caps the normalized signature so a pathologically long
// upstream error body cannot bloat the log row or the stats group key.
const maxSignatureLen = 2000

type compiledSignatureRule struct {
	name        string
	re          *regexp.Regexp
	replacement string
}

var (
	signatureRulesMu  sync.RWMutex
	signatureRules    []compiledSignatureRule
	currentRules      []SignatureRule
	signatureRulesSet bool
)

// DefaultErrorSignatureRules returns the seed rule set. Order matters: whitespace
// is collapsed first, our own request-id suffixes are stripped, then request-id
// shaped tokens (UUID, long hex, key-anchored ids) and timestamps are masked.
func DefaultErrorSignatureRules() []SignatureRule {
	return []SignatureRule{
		{Name: "collapse-whitespace", Pattern: `\s+`, Replacement: " ", Enabled: true},
		{Name: "strip-newapi-request-id", Pattern: ` *\(request id: [^)]*\)`, Replacement: "", Enabled: true},
		{Name: "uuid", Pattern: `[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`, Replacement: "********-****-****-****-************", Enabled: true},
		{Name: "long-hex", Pattern: `\b[0-9a-fA-F]{16,}\b`, Replacement: "*", Enabled: true},
		{Name: "request-id-by-key", Pattern: `(?i)(request[ -]?id:\s*)[A-Za-z0-9._-]+`, Replacement: "${1}*", Enabled: true},
		{Name: "amzn-request-id", Pattern: `(?i)(x-amzn-requestid:\s*)[A-Za-z0-9._-]+`, Replacement: "${1}*", Enabled: true},
		{Name: "ms-request-id", Pattern: `(?i)((?:apim|x-ms)-request-id:\s*)[A-Za-z0-9._-]+`, Replacement: "${1}*", Enabled: true},
		{Name: "iso-timestamp", Pattern: `\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}\S*`, Replacement: "*", Enabled: true},
	}
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
		compiled = append(compiled, compiledSignatureRule{name: r.Name, re: re, replacement: r.Replacement})
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
	// Defaults are static and always compile; the error is impossible here.
	_ = SetErrorSignatureRules(DefaultErrorSignatureRules())
}

// NormalizeErrorSignature derives a stable signature from a raw error message by
// applying the active ordered regexp rules. Identical upstream errors that
// differ only by request ids / timestamps collapse to the same signature.
func NormalizeErrorSignature(content string) string {
	if content == "" {
		return ""
	}
	ensureSignatureRules()
	signatureRulesMu.RLock()
	rules := signatureRules
	signatureRulesMu.RUnlock()

	s := content
	for _, r := range rules {
		s = r.re.ReplaceAllString(s, r.replacement)
	}
	s = strings.TrimSpace(s)
	if len(s) > maxSignatureLen {
		s = strings.ToValidUTF8(s[:maxSignatureLen], "")
	}
	return s
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
