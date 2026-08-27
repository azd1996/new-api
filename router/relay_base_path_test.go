package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayBasePathAllows(t *testing.T) {
	cases := []struct {
		path    string
		allowed bool
	}{
		{"/v1/chat/completions", true},
		{"/v1/messages", true},
		{"/v1/models", true},
		{"/v1/videos", true},
		{"/v1beta/models/gemini-2.0-flash:generateContent", true},
		{"/mj/submit/imagine", true},
		{"/mj-fast/mj/submit/imagine", true},
		{"/suno/submit/music", true},
		{"/kling/v1/videos/text2video", true},
		{"/jimeng/", true},
		{"/api/log/hourly", true},
		{"/api/log/self/hourly", true},

		{"/", false},
		{"/api/status", false},
		{"/api/user/self", false},
		{"/api/channel/", false},
		{"/api/log/", false},
		{"/api/log/search", false},
		{"/api/log/self", false},
		{"/pg/chat/completions", false},
		{"/dashboard/billing/usage", false},
		{"/assets/index.js", false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.allowed, RelayBasePathAllows(tc.path), "path %s", tc.path)
	}
}

func TestWrapRelayBasePath(t *testing.T) {
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(r.URL.Path))
	})

	t.Run("no base path leaves every path untouched", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/user/self", nil)
		rec := httptest.NewRecorder()
		WrapRelayBasePath("", upstream).ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "/api/user/self", rec.Body.String())
	})

	handler := WrapRelayBasePath("/gateway", upstream)
	cases := []struct {
		name       string
		target     string
		wantStatus int
		wantBody   string
	}{
		{"allowed relay path is stripped", "/gateway/v1/chat/completions", http.StatusOK, "/v1/chat/completions"},
		{"allowed csl path is stripped", "/gateway/api/log/hourly", http.StatusOK, "/api/log/hourly"},
		{"blocked admin path", "/gateway/api/user/self", http.StatusNotFound, relayBasePathNotFoundBody},
		{"blocked web asset", "/gateway/assets/index.js", http.StatusNotFound, relayBasePathNotFoundBody},
		{"root path stays fully exposed", "/api/user/self", http.StatusOK, "/api/user/self"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.target, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			require.Equal(t, tc.wantStatus, rec.Code)
			assert.Equal(t, tc.wantBody, rec.Body.String())
		})
	}
}
