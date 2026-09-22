package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestParseMySQLVersion(t *testing.T) {
	cases := []struct {
		in                  string
		major, minor, patch int
	}{
		{"8.0.36", 8, 0, 36},
		{"5.6.51-log", 5, 6, 51},
		{"5.7.44-0ubuntu0.18.04.1", 5, 7, 44},
		{"5.7.8", 5, 7, 8},
		{"8.4.0", 8, 4, 0},
		{"", 0, 0, 0},
	}
	for _, c := range cases {
		ma, mi, pa := parseMySQLVersion(c.in)
		assert.Equalf(t, c.major, ma, "major of %q", c.in)
		assert.Equalf(t, c.minor, mi, "minor of %q", c.in)
		assert.Equalf(t, c.patch, pa, "patch of %q", c.in)
	}
}

func TestMySQLVersionSupportsJSON(t *testing.T) {
	cases := []struct {
		major, minor, patch int
		want                bool
	}{
		{5, 6, 51, false}, // MySQL 5.6: no native JSON
		{5, 7, 7, false},  // just before JSON was introduced
		{5, 7, 8, true},   // JSON introduced in 5.7.8
		{5, 7, 44, true},
		{8, 0, 36, true},
		{8, 4, 0, true},
	}
	for _, c := range cases {
		assert.Equalf(t, c.want, mysqlVersionSupportsJSON(c.major, c.minor, c.patch),
			"%d.%d.%d", c.major, c.minor, c.patch)
	}
}

func TestJSONColumnType(t *testing.T) {
	orig := mysqlSupportsNativeJSON
	t.Cleanup(func() { mysqlSupportsNativeJSON = orig })

	mysqlDB := &gorm.DB{Config: &gorm.Config{Dialector: mysql.New(mysql.Config{})}}
	pgDB := &gorm.DB{Config: &gorm.Config{Dialector: postgres.New(postgres.Config{})}}
	sqliteDB := &gorm.DB{Config: &gorm.Config{Dialector: sqlite.Open(":memory:")}}

	// When the server supports native JSON (also the default for PG/SQLite), every
	// dialect keeps the "json" column type so existing deployments see no DDL change.
	mysqlSupportsNativeJSON = true
	assert.Equal(t, "json", jsonColumnType(mysqlDB))
	assert.Equal(t, "json", jsonColumnType(pgDB))
	assert.Equal(t, "json", jsonColumnType(sqliteDB))

	// Only a confirmed pre-5.7.8 MySQL server downgrades to longtext; PostgreSQL and
	// SQLite are unaffected.
	mysqlSupportsNativeJSON = false
	assert.Equal(t, "longtext", jsonColumnType(mysqlDB))
	assert.Equal(t, "json", jsonColumnType(pgDB))
	assert.Equal(t, "json", jsonColumnType(sqliteDB))
}

// TestJSONValueScanValueRoundTrip guards that JSONValue survives a Value()/Scan()
// round trip from both []byte (json/text driver output) and string, which is the
// contract that lets the column type switch between json and longtext transparently.
func TestJSONValueScanValueRoundTrip(t *testing.T) {
	orig := JSONValue(`{"items":["gpt-4o","gpt-3.5-turbo"]}`)

	v, err := orig.Value()
	require.NoError(t, err)
	raw, ok := v.([]byte)
	require.True(t, ok)

	var fromBytes JSONValue
	require.NoError(t, fromBytes.Scan(raw))
	assert.JSONEq(t, string(orig), string(fromBytes))

	var fromString JSONValue
	require.NoError(t, fromString.Scan(string(raw)))
	assert.JSONEq(t, string(orig), string(fromString))

	var fromNil JSONValue
	require.NoError(t, fromNil.Scan(nil))
	assert.Nil(t, fromNil)
}

func TestPropertiesScanValueRoundTrip(t *testing.T) {
	orig := Properties{
		Input:             "hello",
		UpstreamModelName: "gpt-4o",
		OriginModelName:   "gpt-4o-mini",
	}
	v, err := orig.Value()
	require.NoError(t, err)
	raw, ok := v.([]byte)
	require.True(t, ok)

	var got Properties
	require.NoError(t, got.Scan(raw))
	assert.Equal(t, orig, got)
}

func TestTaskPrivateDataScanValueRoundTrip(t *testing.T) {
	orig := TaskPrivateData{
		Key:            "sk-secret",
		UpstreamTaskID: "upstream-123",
		ResultURL:      "https://example.com/result.mp4",
		BillingSource:  "wallet",
		TokenId:        7,
	}
	v, err := orig.Value()
	require.NoError(t, err)
	raw, ok := v.([]byte)
	require.True(t, ok)

	var got TaskPrivateData
	require.NoError(t, got.Scan(raw))
	assert.Equal(t, orig, got)
}

func TestChannelInfoScanValueRoundTrip(t *testing.T) {
	orig := ChannelInfo{
		IsMultiKey:         true,
		MultiKeySize:       2,
		MultiKeyStatusList: map[int]int{0: 1, 1: 0},
	}
	v, err := orig.Value()
	require.NoError(t, err)
	raw, ok := v.([]byte)
	require.True(t, ok)

	var got ChannelInfo
	require.NoError(t, got.Scan(raw))
	assert.Equal(t, orig, got)
}
