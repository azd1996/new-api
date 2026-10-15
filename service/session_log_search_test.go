package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateSessionLogSearch(t *testing.T) {
	const day = int64(86400)
	base := SessionLogSearchParams{SessionID: "s", Username: "u", StartSec: 1000, EndSec: 1000 + day}
	cases := []struct {
		name    string
		mutate  func(*SessionLogSearchParams)
		wantErr bool
	}{
		{"valid", func(*SessionLogSearchParams) {}, false},
		{"missing username", func(p *SessionLogSearchParams) { p.Username = "" }, true},
		{"missing session", func(p *SessionLogSearchParams) { p.SessionID = "" }, true},
		{"empty range", func(p *SessionLogSearchParams) { p.EndSec = p.StartSec }, true},
		{"inverted range", func(p *SessionLogSearchParams) { p.EndSec = p.StartSec - 1 }, true},
		{"exactly 10 days", func(p *SessionLogSearchParams) { p.EndSec = p.StartSec + 10*day }, false},
		{"over 10 days", func(p *SessionLogSearchParams) { p.EndSec = p.StartSec + 10*day + 1 }, true},
		{"notfound within 1 day", func(p *SessionLogSearchParams) {
			p.SessionID = SessionSearchNotFound
			p.EndSec = p.StartSec + day
		}, false},
		{"notfound over 1 day", func(p *SessionLogSearchParams) {
			p.SessionID = SessionSearchNotFound
			p.EndSec = p.StartSec + day + 1
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := base
			tc.mutate(&p)
			err := ValidateSessionLogSearch(p)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSessionCacheKeyIncludesAllDimensions(t *testing.T) {
	a := SessionLogSearchParams{SessionID: "s", Username: "u", StartSec: 1, EndSec: 2}
	assert.Equal(t, "s|u|1|2", a.CacheKey())

	byRange := a
	byRange.EndSec = 3
	assert.NotEqual(t, a.CacheKey(), byRange.CacheKey())

	byUser := a
	byUser.Username = "u2"
	assert.NotEqual(t, a.CacheKey(), byUser.CacheKey())

	bySession := a
	bySession.SessionID = "s2"
	assert.NotEqual(t, a.CacheKey(), bySession.CacheKey())
}
