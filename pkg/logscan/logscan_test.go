package logscan

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// waitFinished polls until the job reaches a terminal state or the deadline.
func waitFinished(t *testing.T, e *Engine, key string) Status {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if st, ok := e.Query(key); ok && (st.State == StateDone || st.State == StateError) {
			return st
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("job %q did not finish in time", key)
	return Status{}
}

// windowStartScanner returns a ScanChunk that emits one JSON row per window
// (the window start) and counts how many times it was invoked.
func windowStartScanner(calls *int64) ScanChunk {
	return func(_ context.Context, win Window) ([][]byte, error) {
		atomic.AddInt64(calls, 1)
		return [][]byte{[]byte(fmt.Sprintf(`{"w":%d}`, win.Start))}, nil
	}
}

func TestRunSplitsWindowsAndPaginates(t *testing.T) {
	e := New(Config{BaseDir: t.TempDir(), MaxConcurrent: 2})
	var calls int64
	_, err := e.Submit(JobSpec{
		CacheKey:  "k1",
		Start:     0,
		End:       10,
		Chunk:     3, // windows: [0,3) [3,6) [6,9) [9,10) => 4
		ScanChunk: windowStartScanner(&calls),
	})
	require.NoError(t, err)

	st := waitFinished(t, e, "k1")
	assert.Equal(t, StateDone, st.State)
	assert.Equal(t, 4, st.Total)
	assert.Equal(t, 4, st.Done)
	assert.Equal(t, 4, st.Matched)
	assert.Equal(t, int64(4), atomic.LoadInt64(&calls))

	all, total := e.Read("k1", 0, 10)
	assert.Equal(t, 4, total)
	require.Len(t, all, 4)
	assert.Equal(t, `{"w":0}`, string(all[0]))
	assert.Equal(t, `{"w":3}`, string(all[1]))
	assert.Equal(t, `{"w":6}`, string(all[2]))
	assert.Equal(t, `{"w":9}`, string(all[3]))

	page0, total := e.Read("k1", 0, 2)
	assert.Equal(t, 4, total)
	assert.Equal(t, [][]byte{[]byte(`{"w":0}`), []byte(`{"w":3}`)}, page0)
	page1, _ := e.Read("k1", 1, 2)
	assert.Equal(t, [][]byte{[]byte(`{"w":6}`), []byte(`{"w":9}`)}, page1)
	page2, _ := e.Read("k1", 2, 2)
	assert.Empty(t, page2)
}

func TestEmptyRangeCompletesWithoutScanning(t *testing.T) {
	e := New(Config{BaseDir: t.TempDir()})
	var calls int64
	_, err := e.Submit(JobSpec{CacheKey: "empty", Start: 5, End: 5, Chunk: 1, ScanChunk: windowStartScanner(&calls)})
	require.NoError(t, err)

	st := waitFinished(t, e, "empty")
	assert.Equal(t, StateDone, st.State)
	assert.Equal(t, 0, st.Total)
	assert.Equal(t, int64(0), atomic.LoadInt64(&calls))
	_, total := e.Read("empty", 0, 10)
	assert.Equal(t, 0, total)
}

func TestInvalidSpec(t *testing.T) {
	e := New(Config{BaseDir: t.TempDir()})
	_, err := e.Submit(JobSpec{CacheKey: "", ScanChunk: windowStartScanner(new(int64))})
	assert.ErrorIs(t, err, ErrInvalidSpec)
	_, err = e.Submit(JobSpec{CacheKey: "x", ScanChunk: nil})
	assert.ErrorIs(t, err, ErrInvalidSpec)
}

func TestCacheHitSkipsRescan(t *testing.T) {
	e := New(Config{BaseDir: t.TempDir()})
	var calls int64
	spec := JobSpec{CacheKey: "cached", Start: 0, End: 6, Chunk: 3, ScanChunk: windowStartScanner(&calls)}
	_, err := e.Submit(spec)
	require.NoError(t, err)
	waitFinished(t, e, "cached")
	require.Equal(t, int64(2), atomic.LoadInt64(&calls))

	// Second submit (non-force) must serve the cache without rescanning.
	_, err = e.Submit(spec)
	require.NoError(t, err)
	st := waitFinished(t, e, "cached")
	assert.Equal(t, StateDone, st.State)
	assert.Equal(t, int64(2), atomic.LoadInt64(&calls))
}

func TestForceBacksUpAndRescans(t *testing.T) {
	dir := t.TempDir()
	e := New(Config{BaseDir: dir})
	var calls int64
	spec := JobSpec{CacheKey: "forced", Start: 0, End: 6, Chunk: 3, ScanChunk: windowStartScanner(&calls)}
	_, err := e.Submit(spec)
	require.NoError(t, err)
	waitFinished(t, e, "forced")
	require.Equal(t, int64(2), atomic.LoadInt64(&calls))

	spec.Force = true
	_, err = e.Submit(spec)
	require.NoError(t, err)
	st := waitFinished(t, e, "forced")
	assert.Equal(t, StateDone, st.State)
	assert.Equal(t, int64(4), atomic.LoadInt64(&calls), "force should rescan")

	backups, _ := filepath.Glob(filepath.Join(dir, "*.log.bak.*"))
	assert.NotEmpty(t, backups, "force should back up the previous result")
}

func TestScanErrorMarksErrorState(t *testing.T) {
	e := New(Config{BaseDir: t.TempDir()})
	scan := func(_ context.Context, _ Window) ([][]byte, error) {
		return nil, errors.New("boom")
	}
	_, err := e.Submit(JobSpec{CacheKey: "err", Start: 0, End: 3, Chunk: 1, ScanChunk: scan})
	require.NoError(t, err)
	st := waitFinished(t, e, "err")
	assert.Equal(t, StateError, st.State)
	assert.Contains(t, st.Err, "boom")
}

func TestConcurrencyCapSerializesJobs(t *testing.T) {
	e := New(Config{BaseDir: t.TempDir(), MaxConcurrent: 1})

	entered := make(chan struct{})
	release := make(chan struct{})
	blockingScan := func(_ context.Context, _ Window) ([][]byte, error) {
		entered <- struct{}{}
		<-release
		return [][]byte{[]byte(`{"a":1}`)}, nil
	}
	_, err := e.Submit(JobSpec{CacheKey: "A", Start: 0, End: 1, Chunk: 1, ScanChunk: blockingScan})
	require.NoError(t, err)
	<-entered // A now holds the only concurrency slot

	var bCalls int64
	_, err = e.Submit(JobSpec{CacheKey: "B", Start: 0, End: 1, Chunk: 1, ScanChunk: windowStartScanner(&bCalls)})
	require.NoError(t, err)

	// B cannot run while A holds the single slot.
	assert.Equal(t, int64(0), atomic.LoadInt64(&bCalls))
	stB, ok := e.Query("B")
	require.True(t, ok)
	assert.Equal(t, StateRunning, stB.State)

	close(release) // let A finish, freeing the slot for B
	assert.Equal(t, StateDone, waitFinished(t, e, "A").State)
	assert.Equal(t, StateDone, waitFinished(t, e, "B").State)
	assert.Equal(t, int64(1), atomic.LoadInt64(&bCalls))
}

func TestCleanupRemovesExpiredFiles(t *testing.T) {
	dir := t.TempDir()
	e := New(Config{BaseDir: dir, TTL: 24 * time.Hour})
	var calls int64
	_, err := e.Submit(JobSpec{CacheKey: "old", Start: 0, End: 3, Chunk: 3, ScanChunk: windowStartScanner(&calls)})
	require.NoError(t, err)
	waitFinished(t, e, "old")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	old := time.Now().Add(-48 * time.Hour)
	for _, entry := range entries {
		require.NoError(t, os.Chtimes(filepath.Join(dir, entry.Name()), old, old))
	}

	removed := e.Cleanup()
	assert.Equal(t, len(entries), removed)
	remaining, _ := os.ReadDir(dir)
	assert.Empty(t, remaining)
}
