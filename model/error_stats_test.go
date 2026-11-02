package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestErrorSignatureOtherReadsLogOtherAdminInfo locks the 明细错误 read path to
// the LogOther write format. RecordErrorLog stores the signature, its hash, and
// the decoded customer under the admin_info section via SetAdmin, so the stats
// aggregation must read them from there. A regression here makes every error
// row look signature-less, which empties the detailed-error page even though
// error logs are being written.
func TestErrorSignatureOtherReadsLogOtherAdminInfo(t *testing.T) {
	other := NewLogOther()
	other.SetPublic("status_code", 400)
	other.SetAdmin("error_signature", "ValidationException: bad input")
	other.SetAdmin("error_signature_hash", "abc123")
	other.SetAdmin("customer_id", "cust-7")

	var o errorSignatureOther
	require.NoError(t, common.Unmarshal([]byte(other.JSONString()), &o))

	assert.Equal(t, "abc123", o.signatureHash())
	assert.Equal(t, "ValidationException: bad input", o.signature())
	assert.Equal(t, "cust-7", o.customerId())
}

// TestErrorSignatureOtherLegacyTopLevel keeps error logs written before the
// LogOther restructure (flat top-level keys) readable through the same path.
func TestErrorSignatureOtherLegacyTopLevel(t *testing.T) {
	legacy := `{"error_signature":"old sig","error_signature_hash":"deadbeef","customer_id":"cust-9"}`

	var o errorSignatureOther
	require.NoError(t, common.Unmarshal([]byte(legacy), &o))

	assert.Equal(t, "deadbeef", o.signatureHash())
	assert.Equal(t, "old sig", o.signature())
	assert.Equal(t, "cust-9", o.customerId())
}
