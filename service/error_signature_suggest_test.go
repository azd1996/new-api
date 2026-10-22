package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSuggestErrorSignatureRules verifies the suggester flags residual variable
// tokens (long numbers, org ids, retry delays) left in already-normalized
// signatures, orders them by match count, and omits detectors with no match.
func TestSuggestErrorSignatureRules(t *testing.T) {
	signatures := []string{
		"status_code=429, Rate limit reached for org-AbC123XyZ on tokens: Limit 30000, Used 29000. Please try again in 2ms",
		"status_code=429, Rate limit reached for org-Zzz999Yyy on tokens: Limit 45000, Used 44000. Please try again in 5ms",
		"status_code=400, ValidationException: bad input",
	}

	got := SuggestErrorSignatureRules(signatures)
	byName := make(map[string]SuggestedRule, len(got))
	for _, r := range got {
		byName[r.Name] = r
	}

	// long-number matches the two rate-limit signatures (Limit/Used values).
	longNum, ok := byName["long-number"]
	require.True(t, ok, "expected a long-number suggestion")
	assert.Equal(t, 2, longNum.MatchCount)
	assert.Equal(t, `\b\d{5,}\b`, longNum.Pattern)
	assert.Equal(t, "*", longNum.Replacement)
	assert.True(t, longNum.Enabled)
	assert.NotEmpty(t, longNum.Example)

	orgID, ok := byName["org-id"]
	require.True(t, ok, "expected an org-id suggestion")
	assert.Equal(t, 2, orgID.MatchCount)

	// email never appears, so it must not be suggested.
	_, hasEmail := byName["email"]
	assert.False(t, hasEmail)

	// Results are ordered by descending match count.
	for i := 1; i < len(got); i++ {
		assert.GreaterOrEqual(t, got[i-1].MatchCount, got[i].MatchCount)
	}
}

// TestSuggestErrorSignatureRulesEmpty confirms no suggestions for clean input.
func TestSuggestErrorSignatureRulesEmpty(t *testing.T) {
	got := SuggestErrorSignatureRules([]string{"status_code=400, ValidationException: bad input"})
	assert.Empty(t, got)
}
