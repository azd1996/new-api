package common

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// masterKeyHex is the demo master key from the trace-tag design doc: the 32-byte
// increasing sequence 00 01 02 ... 1e 1f. The decoded vectors below were
// produced by the downstream gateway's reference implementation with this key,
// so they pin our Decode against an externally-produced ciphertext.
const masterKeyHex = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

func TestTraceTagDecode(t *testing.T) {
	keys, err := newTraceTagKeys(masterKeyHex)
	require.NoError(t, err)

	const tag = "C7J42H35CV56G2VKWLMPH72GEKF24S72RCDZOG6KAXXIJ5Q5D2K7LNKIH3R7D5KGLDOMTOX3XGH7HBRLCWQMCZKGHY7EXQBCLVWUATI"
	requestID, customerID, err := keys.Decode(tag)
	require.NoError(t, err)
	assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", requestID)
	assert.Equal(t, "api-key-004", customerID)

	// Case normalization along the downstream link must not break decoding.
	lower := "c7j42h35cv56g2vkwlmph72gekf24s72rcdzog6kaxxij5q5d2k7lnkih3r7d5kgldomtox3xgh7hbrlcwqmczkghy7exqbclvwuati"
	reqLower, custLower, err := keys.Decode(lower)
	require.NoError(t, err)
	assert.Equal(t, requestID, reqLower)
	assert.Equal(t, customerID, custLower)
}

func TestTraceTagDecodeRejectsTampered(t *testing.T) {
	keys, err := newTraceTagKeys(masterKeyHex)
	require.NoError(t, err)

	// Flip the first character (part of the SIV): the re-derived SIV no longer
	// matches the stored one, so authentication fails. (A tail character can be
	// non-canonical in base32 and decode to the same bytes, so we tamper the
	// head where every bit is significant.)
	tampered := "D7J42H35CV56G2VKWLMPH72GEKF24S72RCDZOG6KAXXIJ5Q5D2K7LNKIH3R7D5KGLDOMTOX3XGH7HBRLCWQMCZKGHY7EXQBCLVWUATI"
	_, _, err = keys.Decode(tampered)
	assert.Error(t, err)

	_, _, err = keys.Decode("AAAA") // shorter than the 16-byte SIV
	assert.Error(t, err)

	_, _, err = keys.Decode("not base32 @@@")
	assert.Error(t, err)
}

func TestNewTraceTagKeysRejectsBadKey(t *testing.T) {
	_, err := newTraceTagKeys("zz")
	assert.Error(t, err)

	_, err = newTraceTagKeys("00010203") // valid hex, wrong length
	assert.Error(t, err)
}

func TestCustomerFromContext(t *testing.T) {
	// Install a known key set so the known tag below decodes to customer
	// "api-key-004", and consume the once so GetTraceTagKeys keeps it.
	keys, err := newTraceTagKeys(masterKeyHex)
	require.NoError(t, err)
	previousKeys, previousOnce := traceTagKeys, traceTagOnce
	traceTagKeys, traceTagOnce = keys, sync.Once{}
	traceTagOnce.Do(func() {}) // consume it so GetTraceTagKeys keeps the installed keys
	t.Cleanup(func() { traceTagKeys, traceTagOnce = previousKeys, previousOnce })

	const knownTag = "C7J42H35CV56G2VKWLMPH72GEKF24S72RCDZOG6KAXXIJ5Q5D2K7LNKIH3R7D5KGLDOMTOX3XGH7HBRLCWQMCZKGHY7EXQBCLVWUATI"

	newContext := func(t *testing.T, headerValue, tokenCustomer, userCustomer string) *gin.Context {
		t.Helper()
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		if headerValue != "" {
			ctx.Request.Header.Set(TraceTagHeader, headerValue)
		}
		SetContextKey(ctx, constant.ContextKeyTokenCustomerID, tokenCustomer)
		SetContextKey(ctx, constant.ContextKeyUserCustomerID, userCustomer)
		return ctx
	}

	t.Run("no trace tag and no binding", func(t *testing.T) {
		customerID, source := CustomerFromContext(newContext(t, "", "", ""))
		assert.Empty(t, customerID)
		assert.Empty(t, source)
	})

	t.Run("token binding is used when no trace tag exists", func(t *testing.T) {
		customerID, source := CustomerFromContext(newContext(t, "", "customer-a", ""))
		assert.Equal(t, "customer-a", customerID)
		assert.Equal(t, CustomerSourceToken, source)
	})

	t.Run("user binding is used when no token binding exists", func(t *testing.T) {
		customerID, source := CustomerFromContext(newContext(t, "", "", "customer-u"))
		assert.Equal(t, "customer-u", customerID)
		assert.Equal(t, CustomerSourceUser, source)
	})

	t.Run("token binding wins over the user binding", func(t *testing.T) {
		customerID, source := CustomerFromContext(newContext(t, "", "customer-a", "customer-u"))
		assert.Equal(t, "customer-a", customerID)
		assert.Equal(t, CustomerSourceToken, source)
	})

	t.Run("decoded trace tag wins over token and user bindings", func(t *testing.T) {
		customerID, source := CustomerFromContext(newContext(t, knownTag, "customer-a", "customer-u"))
		assert.Equal(t, "api-key-004", customerID)
		assert.Equal(t, CustomerSourceTraceTag, source)
	})

	t.Run("token binding is used when the trace tag fails authentication", func(t *testing.T) {
		customerID, source := CustomerFromContext(newContext(t, "TAMPERED", "customer-a", "customer-u"))
		assert.Equal(t, "customer-a", customerID)
		assert.Equal(t, CustomerSourceToken, source)
	})

	t.Run("an empty decoded trace-tag customer falls back to the token binding", func(t *testing.T) {
		customerID, source := CustomerFromContext(newContext(t, "", "customer-a", "customer-u"))
		assert.Equal(t, "customer-a", customerID)
		assert.Equal(t, CustomerSourceToken, source)
	})
}
