package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/logscan"
)

const (
	// SessionSearchNotFound is the sentinel session id stored for
	// session-capable requests that carried no resolvable session id.
	SessionSearchNotFound = "notfound"

	sessionSearchMaxDays         = 10
	sessionSearchNotFoundMaxDays = 1
	sessionSearchChunkSeconds    = 3600
	sessionSearchWindowLimit     = 10000
)

// SessionLogSearchParams is one admin session-search request.
type SessionLogSearchParams struct {
	SessionID string
	Username  string
	StartSec  int64
	EndSec    int64
	Force     bool
}

// CacheKey is the canonical identity of a search (session + user + range), so a
// different time range caches to a different file.
func (p SessionLogSearchParams) CacheKey() string {
	return strings.Join([]string{
		p.SessionID, p.Username,
		strconv.FormatInt(p.StartSec, 10), strconv.FormatInt(p.EndSec, 10),
	}, "|")
}

// ValidateSessionLogSearch enforces the required fields and the time-range caps
// (<=10 days, or <=1 day when searching the "notfound" sentinel).
func ValidateSessionLogSearch(p SessionLogSearchParams) error {
	if strings.TrimSpace(p.Username) == "" {
		return errors.New("username is required")
	}
	if strings.TrimSpace(p.SessionID) == "" {
		return errors.New("session_id is required")
	}
	if p.StartSec <= 0 || p.EndSec <= 0 || p.EndSec <= p.StartSec {
		return errors.New("a valid time range is required")
	}
	maxDays := int64(sessionSearchMaxDays)
	if p.SessionID == SessionSearchNotFound {
		maxDays = sessionSearchNotFoundMaxDays
	}
	if p.EndSec-p.StartSec > maxDays*86400 {
		return fmt.Errorf("time range must not exceed %d day(s)", maxDays)
	}
	return nil
}

var (
	sessionSearchEngine     *logscan.Engine
	sessionSearchEngineOnce sync.Once
)

// sessionLogSearchEngine lazily builds the shared engine, rooted at
// LOG_DIR/session_search.
func sessionLogSearchEngine() *logscan.Engine {
	sessionSearchEngineOnce.Do(func() {
		dir := ""
		if common.LogDir != nil {
			dir = *common.LogDir
		}
		sessionSearchEngine = logscan.New(logscan.Config{
			BaseDir:       filepath.Join(dir, "session_search"),
			MaxConcurrent: 2,
			TTL:           24 * time.Hour,
			DefaultChunk:  sessionSearchChunkSeconds,
		})
	})
	return sessionSearchEngine
}

func sessionLogSearchAvailable() bool {
	return common.LogDir != nil && strings.TrimSpace(*common.LogDir) != ""
}

// StartSessionLogSearch validates and submits a search, returning the current
// status. A completed cache is served unless p.Force triggers a rescan.
func StartSessionLogSearch(p SessionLogSearchParams) (logscan.Status, error) {
	if err := ValidateSessionLogSearch(p); err != nil {
		return logscan.Status{}, err
	}
	if !sessionLogSearchAvailable() {
		return logscan.Status{}, errors.New("log directory is not configured; session search is unavailable")
	}
	eng := sessionLogSearchEngine()
	spec := logscan.JobSpec{
		CacheKey: p.CacheKey(),
		KeyHint:  p.SessionID,
		Start:    p.StartSec,
		End:      p.EndSec,
		Chunk:    sessionSearchChunkSeconds,
		Force:    p.Force,
		ScanChunk: func(_ context.Context, win logscan.Window) ([][]byte, error) {
			logs, err := model.SearchLogsBySessionWindow(p.Username, win.Start, win.End, p.SessionID, sessionSearchWindowLimit)
			if err != nil {
				return nil, err
			}
			rows := make([][]byte, 0, len(logs))
			for _, lg := range logs {
				b, err := common.Marshal(lg)
				if err != nil {
					continue
				}
				rows = append(rows, b)
			}
			return rows, nil
		},
	}
	if _, err := eng.Submit(spec); err != nil {
		return logscan.Status{}, err
	}
	st, _ := eng.Query(p.CacheKey())
	return st, nil
}

// QuerySessionLogSearch returns the current status for a prior search.
func QuerySessionLogSearch(p SessionLogSearchParams) (logscan.Status, bool) {
	if !sessionLogSearchAvailable() {
		return logscan.Status{}, false
	}
	return sessionLogSearchEngine().Query(p.CacheKey())
}

// ReadSessionLogSearch returns a page of result rows (verbatim log JSON) and
// the total number of matched rows.
func ReadSessionLogSearch(p SessionLogSearchParams, page, size int) ([]json.RawMessage, int) {
	if !sessionLogSearchAvailable() {
		return nil, 0
	}
	lines, total := sessionLogSearchEngine().Read(p.CacheKey(), page, size)
	items := make([]json.RawMessage, 0, len(lines))
	for _, ln := range lines {
		items = append(items, json.RawMessage(ln))
	}
	return items, total
}
