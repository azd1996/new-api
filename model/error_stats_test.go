package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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
	other.SetAdmin("error_detailed_type", "requests-donot-satisfy-constrains")
	other.SetAdmin("error_attribution", "upstream")
	other.SetAdmin("error_note", "request constraint rejection")
	other.SetAdmin("customer_id", "cust-7")

	var o errorSignatureOther
	require.NoError(t, common.Unmarshal([]byte(other.JSONString()), &o))

	assert.Equal(t, "abc123", o.signatureHash())
	assert.Equal(t, "ValidationException: bad input", o.signature())
	assert.Equal(t, "cust-7", o.customerId())
	assert.Equal(t, "requests-donot-satisfy-constrains", o.detailedErrorType("abc123"))
	assert.Equal(t, "upstream", o.attribution("abc123"))
	assert.Equal(t, "request constraint rejection", o.note("abc123"))
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

// TestGetErrorSignatureExamples pins the raw-error sampling used by AI rule
// generation: newest first, error logs only, blank contents filtered, and the
// signature hash matched exactly.
func TestGetErrorSignatureExamples(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Log{}))
	oldLogDB := LOG_DB
	LOG_DB = db
	t.Cleanup(func() { LOG_DB = oldLogDB })

	require.NoError(t, db.Create([]Log{
		{Id: 1, Type: LogTypeError, ErrorSignatureHash: "hash-a", Content: "raw two", CreatedAt: 200},
		{Id: 2, Type: LogTypeError, ErrorSignatureHash: "hash-a", Content: "raw one", CreatedAt: 300},
		{Id: 3, Type: LogTypeError, ErrorSignatureHash: "hash-b", Content: "other", CreatedAt: 400},
		{Id: 4, Type: LogTypeConsume, ErrorSignatureHash: "hash-a", Content: "consume", CreatedAt: 500},
		{Id: 5, Type: LogTypeError, ErrorSignatureHash: "hash-a", Content: "   ", CreatedAt: 600},
	}).Error)

	contents, err := GetErrorSignatureExamples("hash-a", 3)
	require.NoError(t, err)
	assert.Equal(t, []string{"raw one", "raw two"}, contents)

	contents, err = GetErrorSignatureExamples("", 3)
	require.NoError(t, err)
	assert.Empty(t, contents)
}

// TestErrorStatFilterStatusCodeFallsBackToOtherJSON pins
// applyErrorStatFilter's status_code narrowing to match the dedicated indexed
// column for rows written since the column was added, while still finding
// older error rows that only carry it in the Other JSON.
func TestErrorStatFilterStatusCodeFallsBackToOtherJSON(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Log{}))

	require.NoError(t, db.Create([]Log{
		{Id: 1, Type: LogTypeError, StatusCode: 429, Other: `{"status_code":429}`},
		{Id: 2, Type: LogTypeError, Other: `{"status_code":429}`},
		{Id: 3, Type: LogTypeError, StatusCode: 500, Other: `{"status_code":500}`},
	}).Error)

	var rows []errorStatScanRow
	require.NoError(t, applyErrorStatFilter(db.Table("logs"), ErrorStatFilter{StatusCode: 429}).
		Select("created_at, model_name, channel_id, other").Order("id").Scan(&rows).Error)
	assert.Len(t, rows, 2)
}
