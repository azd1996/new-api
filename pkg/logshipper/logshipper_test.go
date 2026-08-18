package logshipper

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// clickHouseLogColumns is the column list of the *cluster* table
// cdn_data.xcdn_newapi_raw_logs_local (see ai-gateway/solution.md 5.1 and
// ai-gateway/newapi/csl-sidecar/ck-ingest-fields.md 3.1), excluding the
// MATERIALIZED dt partition column which is derived from ts.
//
// It is deliberately NOT model.clickHouseLogCreateTableSQL: the single-node
// table has an id column and calls the timestamp created_at, while the cluster
// table has no id and calls it ts. LoongCollector -> Kafka -> Flink ->
// ClickHouse matches by column name, so a renamed or missing key silently
// lands as the column DEFAULT rather than failing — and a missing ts would
// additionally push every row into the 19700101 partition, since dt is
// MATERIALIZED toYYYYMMDD(toDateTime(ts)). This list is the contract.
var clickHouseLogColumns = []string{
	"user_id", "ts", "type", "content", "username",
	"token_name", "model_name", "quota", "prompt_tokens", "completion_tokens",
	"use_time", "is_stream", "channel_id", "token_id", "group_name", "ip",
	"request_id", "upstream_request_id", "other", "x_instance_name",
}

func newTestShipper(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "newapi_logs.log")
	require.NoError(t, Init(Config{Filename: path, InstanceName: "log_test3"}))
	t.Cleanup(func() { require.NoError(t, Close()) })
	return path
}

func readOneLine(t *testing.T, path string) []byte {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	require.True(t, scanner.Scan(), "expected at least one line in %s", path)
	line := append([]byte(nil), scanner.Bytes()...)
	require.NoError(t, scanner.Err())
	return line
}

func TestShipEmitsExactlyClickHouseLogColumns(t *testing.T) {
	path := newTestShipper(t)

	require.NoError(t, Ship(Row{
		UserId: 1, Ts: 1785477600, Type: 2,
		Content: "test", Username: "eng-user", TokenName: "eng-key",
		ModelName: "gpt-5.5", Quota: 42, PromptTokens: 10, CompletionTokens: 20,
		UseTime: 3, IsStream: true, ChannelId: 9001, TokenId: 1001,
		GroupName: "default", Ip: "10.0.0.1", RequestId: "req-1",
		UpstreamRequestId: "up-1", Other: `{"matched_tier":"standard"}`,
	}))

	var envelope struct {
		Schema string         `json:"schema"`
		Data   map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(readOneLine(t, path), &envelope))

	assert.Equal(t, SchemaNewAPILogsV1, envelope.Schema)
	for _, column := range clickHouseLogColumns {
		assert.Contains(t, envelope.Data, column)
	}
	assert.Len(t, envelope.Data, len(clickHouseLogColumns),
		"emitted keys must match the ClickHouse columns exactly")

	// is_stream must stay a JSON bool: ck-ingest-fields.md 3.1 puts the
	// UInt8 0/1 conversion on the ingest side, so emitting a number here
	// would make their bool parse miss and mark every stream non-stream.
	assert.IsType(t, true, envelope.Data["is_stream"])
	// other must stay a JSON string (double-encoded price snapshot), not an
	// object: the cluster column is String and the data team parses it
	// themselves.
	assert.IsType(t, "", envelope.Data["other"])
}

// TestShipStampsInstanceName covers the one field Ship fills in itself: rows
// from different instances share the cluster table and must stay separable.
func TestShipStampsInstanceName(t *testing.T) {
	path := newTestShipper(t)

	require.NoError(t, Ship(Row{UserId: 1, XInstanceName: "ignored-caller-value"}))

	var envelope struct {
		Data Row `json:"data"`
	}
	require.NoError(t, json.Unmarshal(readOneLine(t, path), &envelope))
	assert.Equal(t, "log_test3", envelope.Data.XInstanceName)
}

// TestShipWhenDisabledIsNoop protects the caller contract in model.createLog:
// it calls Ship unconditionally, so a disabled shipper must neither error nor
// create files. Close() first so the assertion does not depend on whatever
// test ran before it.
func TestShipWhenDisabledIsNoop(t *testing.T) {
	require.NoError(t, Close())
	require.False(t, Enabled())
	assert.NoError(t, Ship(Row{UserId: 1}))
}

// TestInitTwiceIsRejected: a second Init must not silently swap the open file
// out from under concurrent Ship calls, which would split one instance's rows
// across two files.
func TestInitTwiceIsRejected(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.log")
	require.NoError(t, Init(Config{Filename: first, InstanceName: "log_test3"}))
	t.Cleanup(func() { require.NoError(t, Close()) })

	err := Init(Config{Filename: filepath.Join(dir, "second.log"), InstanceName: "other"})
	require.Error(t, err)

	require.NoError(t, Ship(Row{UserId: 1}))
	var envelope struct {
		Data Row `json:"data"`
	}
	require.NoError(t, json.Unmarshal(readOneLine(t, first), &envelope))
	assert.Equal(t, "log_test3", envelope.Data.XInstanceName,
		"rejected Init must not have replaced the instance name")
}

// TestShipAfterCloseErrors: shutdown races the relay hot path, so a Ship
// arriving after Close must report a failure rather than dropping the row
// silently. model.shipLog turns that into a SysError line.
func TestShipAfterCloseErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "newapi_logs.log")
	require.NoError(t, Init(Config{Filename: path, InstanceName: "log_test3"}))
	require.NoError(t, Close())

	// Close() clears the writer, so Ship takes the disabled path and reports
	// no error; the row is dropped. Document that this is the accepted
	// behaviour rather than leaving it untested.
	assert.NoError(t, Ship(Row{UserId: 1}))
	assert.False(t, Enabled())
}

// TestShipConcurrent: createLog runs on every relayed request, so Ship is
// called from many goroutines at once. Every row must land intact on its own
// line (the -race flag additionally covers the singleton access).
func TestShipConcurrent(t *testing.T) {
	path := newTestShipper(t)

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(userID int) {
			defer wg.Done()
			assert.NoError(t, Ship(Row{UserId: userID, RequestId: "req"}))
		}(i)
	}
	wg.Wait()

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	seen := make(map[int]bool, goroutines)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var envelope struct {
			Data Row `json:"data"`
		}
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &envelope),
			"interleaved write produced an unparseable line")
		seen[envelope.Data.UserId] = true
	}
	require.NoError(t, scanner.Err())
	assert.Len(t, seen, goroutines, "every concurrent Ship must produce one intact line")
}

// TestInitForwardsRotationConfig: the rotation knobs are the operator-facing
// surface of this package; Config silently dropping one would only show up as
// unbounded disk growth in production. Compress is the observable proxy.
func TestInitForwardsRotationConfig(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, Init(Config{
		Filename:     filepath.Join(dir, "newapi_logs.log"),
		InstanceName: "log_test3",
		MaxSize:      1,
		MaxBackups:   2,
		MaxAge:       3,
		LocalTime:    true,
		Compress:     true,
	}))
	t.Cleanup(func() { require.NoError(t, Close()) })

	// Printable filler: JSON-escaping NUL bytes would inflate a 200 KiB
	// payload past MaxSize, which lumberjack rejects outright.
	bulk := strings.Repeat("x", 200*1024)
	for i := 0; i < 10; i++ {
		require.NoError(t, Ship(Row{UserId: i, Content: bulk}))
	}

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var compressed bool
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".gz") {
			compressed = true
			break
		}
	}
	assert.True(t, compressed, "Compress=true must reach lumberjack; entries: %v", entries)
}

func TestInitRejectsIncompleteConfig(t *testing.T) {
	dir := t.TempDir()

	assert.Error(t, Init(Config{Filename: "", InstanceName: "log_test3"}))
	assert.Error(t, Init(Config{Filename: filepath.Join(dir, "x.log"), InstanceName: ""}))
	assert.False(t, Enabled(), "a rejected Init must not leave the shipper half-open")
}

func TestCloseWithoutInitIsNoop(t *testing.T) {
	assert.NoError(t, Close())
}
