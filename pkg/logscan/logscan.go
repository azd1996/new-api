// Package logscan is a generic, business-agnostic engine for long-running log
// scans. It splits a time range into fixed windows, runs a caller-supplied
// ScanChunk over each window, and streams matched rows (opaque JSON lines) into
// a local cache file so the result survives the originating request and can be
// served again without rescanning. It knows nothing about sessions, SQL, or
// HTTP; "what to query" is entirely the caller's ScanChunk.
package logscan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// Window is a half-open [Start, End) slice of the scanned range. Units are
// caller-defined (e.g. unix seconds); the engine only does arithmetic on them.
type Window struct {
	Start int64
	End   int64
}

// ScanChunk returns the matched rows for one window as opaque JSON lines. The
// engine appends them verbatim and never parses them.
type ScanChunk func(ctx context.Context, win Window) ([][]byte, error)

// State is the lifecycle of a scan job.
type State string

const (
	StateRunning State = "running"
	StateDone    State = "done"
	StateError   State = "error"
)

// Status is the observable progress of a job, persisted to a sidecar meta file.
type Status struct {
	State   State  `json:"state"`
	Total   int    `json:"total"`
	Done    int    `json:"done"`
	Matched int    `json:"matched"`
	Err     string `json:"err,omitempty"`
	KeyHint string `json:"key_hint,omitempty"`
}

// JobSpec describes one scan. CacheKey is the canonical identity used for the
// cache file (hashed into the filename); KeyHint is a human-readable label kept
// only in the meta file.
type JobSpec struct {
	CacheKey  string
	KeyHint   string
	Start     int64
	End       int64
	Chunk     int64
	Force     bool
	ScanChunk ScanChunk
}

// Config configures an Engine. Unset fields fall back to defaults in New.
type Config struct {
	BaseDir       string           // directory holding cache/meta files
	MaxConcurrent int              // max concurrently running jobs (default 2)
	TTL           time.Duration    // files older than this are removed by Cleanup (default 24h)
	DefaultChunk  int64            // window size used when JobSpec.Chunk <= 0 (default 3600)
	Now           func() time.Time // injectable clock (default time.Now)
}

// Engine orchestrates chunked scans. It is safe for concurrent use.
type Engine struct {
	cfg      Config
	sem      chan struct{}
	mu       sync.Mutex
	inflight map[string]bool
}

// ErrInvalidSpec is returned by Submit when the spec is missing a CacheKey or
// ScanChunk.
var ErrInvalidSpec = errors.New("logscan: invalid job spec")

// New builds an Engine, applying defaults for unset config fields.
func New(cfg Config) *Engine {
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 2
	}
	if cfg.TTL <= 0 {
		cfg.TTL = 24 * time.Hour
	}
	if cfg.DefaultChunk <= 0 {
		cfg.DefaultChunk = 3600
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Engine{
		cfg:      cfg,
		sem:      make(chan struct{}, cfg.MaxConcurrent),
		inflight: make(map[string]bool),
	}
}

func (e *Engine) name(cacheKey string) string { return common.Sha1([]byte(cacheKey))[:16] }

func (e *Engine) logPath(name string) string  { return filepath.Join(e.cfg.BaseDir, name+".log") }
func (e *Engine) ingPath(name string) string  { return filepath.Join(e.cfg.BaseDir, name+".log.ing") }
func (e *Engine) metaPath(name string) string { return filepath.Join(e.cfg.BaseDir, name+".meta") }

// totalWindows counts the half-open windows covering [start, end) at the given
// chunk size.
func totalWindows(start, end, chunk int64) int {
	if end <= start || chunk <= 0 {
		return 0
	}
	span := end - start
	n := span / chunk
	if span%chunk != 0 {
		n++
	}
	return int(n)
}

// Submit starts (or serves from cache) a scan for spec and returns a handle
// equal to spec.CacheKey; poll with Query and read with Read. A completed cache
// is served as-is unless spec.Force, which backs up the old result and rescans.
func (e *Engine) Submit(spec JobSpec) (string, error) {
	if spec.CacheKey == "" || spec.ScanChunk == nil {
		return "", ErrInvalidSpec
	}
	if spec.Chunk <= 0 {
		spec.Chunk = e.cfg.DefaultChunk
	}
	if err := os.MkdirAll(e.cfg.BaseDir, 0o755); err != nil {
		return "", err
	}
	name := e.name(spec.CacheKey)

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.inflight[name] {
		return spec.CacheKey, nil // already running in this process; poll it
	}
	if _, err := os.Stat(e.logPath(name)); err == nil {
		if !spec.Force {
			return spec.CacheKey, nil // serve cache
		}
		e.backup(name)
	}
	_ = os.Remove(e.ingPath(name)) // drop any stale partial

	total := totalWindows(spec.Start, spec.End, spec.Chunk)
	_ = e.writeMeta(name, Status{State: StateRunning, Total: total, KeyHint: spec.KeyHint})
	if f, err := os.Create(e.ingPath(name)); err == nil {
		_ = f.Close() // so Read mid-run finds a (possibly empty) file
	}

	e.inflight[name] = true
	go e.run(name, spec, total)
	return spec.CacheKey, nil
}

// backup renames a completed result aside so a Force run can rescan cleanly.
func (e *Engine) backup(name string) {
	ts := strconv.FormatInt(e.cfg.Now().UnixNano(), 10)
	_ = os.Rename(e.logPath(name), e.logPath(name)+".bak."+ts)
}

// run executes the scan on a background context (not the caller's request
// context) under the concurrency semaphore, streaming matched rows into the
// .ing file and promoting it to the final result on success.
func (e *Engine) run(name string, spec JobSpec, total int) {
	defer func() {
		e.mu.Lock()
		delete(e.inflight, name)
		e.mu.Unlock()
	}()

	e.sem <- struct{}{}
	defer func() { <-e.sem }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	status := Status{State: StateRunning, Total: total, KeyHint: spec.KeyHint}
	f, err := os.OpenFile(e.ingPath(name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		status.State = StateError
		status.Err = err.Error()
		_ = e.writeMeta(name, status)
		return
	}

	for i := 0; i < total; i++ {
		win := Window{Start: spec.Start + int64(i)*spec.Chunk}
		win.End = win.Start + spec.Chunk
		if win.End > spec.End {
			win.End = spec.End
		}
		rows, scanErr := spec.ScanChunk(ctx, win)
		if scanErr != nil {
			_ = f.Close()
			status.State = StateError
			status.Err = scanErr.Error()
			_ = e.writeMeta(name, status)
			return
		}
		for _, row := range rows {
			_, _ = f.Write(row)
			_, _ = f.Write([]byte{'\n'})
		}
		status.Matched += len(rows)
		status.Done = i + 1
		_ = e.writeMeta(name, status)
	}

	if err := f.Close(); err != nil {
		status.State = StateError
		status.Err = err.Error()
		_ = e.writeMeta(name, status)
		return
	}
	if err := os.Rename(e.ingPath(name), e.logPath(name)); err != nil {
		status.State = StateError
		status.Err = err.Error()
		_ = e.writeMeta(name, status)
		return
	}
	status.State = StateDone
	_ = e.writeMeta(name, status)
}

// Query returns the current status for a cache key, or false if unknown.
func (e *Engine) Query(cacheKey string) (Status, bool) {
	name := e.name(cacheKey)
	if st, err := e.readMeta(name); err == nil {
		return st, true
	}
	if _, err := os.Stat(e.logPath(name)); err == nil {
		return Status{State: StateDone}, true
	}
	return Status{}, false
}

// Read returns a page of result lines (0-based page, size>0) and the total line
// count. It prefers the completed result, falling back to the partial file.
func (e *Engine) Read(cacheKey string, page, size int) ([][]byte, int) {
	name := e.name(cacheKey)
	data, err := os.ReadFile(e.logPath(name))
	if err != nil {
		if data, err = os.ReadFile(e.ingPath(name)); err != nil {
			return nil, 0
		}
	}
	lines := splitLines(data)
	total := len(lines)
	if size <= 0 || page < 0 {
		return nil, total
	}
	start := page * size
	if start >= total {
		return [][]byte{}, total
	}
	end := start + size
	if end > total {
		end = total
	}
	return lines[start:end], total
}

func splitLines(data []byte) [][]byte {
	out := [][]byte{}
	for _, ln := range strings.Split(string(data), "\n") {
		if ln != "" {
			out = append(out, []byte(ln))
		}
	}
	return out
}

// Cleanup removes files whose modtime is older than the configured TTL and
// returns the number removed. Intended to run periodically.
func (e *Engine) Cleanup() int {
	entries, err := os.ReadDir(e.cfg.BaseDir)
	if err != nil {
		return 0
	}
	cutoff := e.cfg.Now().Add(-e.cfg.TTL)
	removed := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		if os.Remove(filepath.Join(e.cfg.BaseDir, entry.Name())) == nil {
			removed++
		}
	}
	return removed
}

func (e *Engine) writeMeta(name string, st Status) error {
	data, err := common.Marshal(st)
	if err != nil {
		return err
	}
	tmp := e.metaPath(name) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, e.metaPath(name))
}

func (e *Engine) readMeta(name string) (Status, error) {
	data, err := os.ReadFile(e.metaPath(name))
	if err != nil {
		return Status{}, err
	}
	var st Status
	if err := common.Unmarshal(data, &st); err != nil {
		return Status{}, err
	}
	return st, nil
}
