package channel

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoRequestReturnsUpstreamRedirectWithoutFollowing(t *testing.T) {
	service.InitHttpClient()
	gin.SetMode(gin.TestMode)
	sharedClient := service.GetHttpClient()
	require.NotNil(t, sharedClient)
	require.NotNil(t, sharedClient.CheckRedirect)
	originalRedirectPolicy := reflect.ValueOf(sharedClient.CheckRedirect).Pointer()

	var targetRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetRequests.Add(1)
		w.WriteHeader(http.StatusTeapot)
	}))
	defer target.Close()

	const responseBody = "redirect response"
	tests := []int{
		http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusSeeOther,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect,
	}

	for _, statusCode := range tests {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			targetRequests.Store(0)
			var sourceRequests atomic.Int32
			type sourceResult struct {
				body []byte
				err  error
			}
			sourceResultCh := make(chan sourceResult, 1)
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sourceRequests.Add(1)
				body, err := io.ReadAll(r.Body)
				sourceResultCh <- sourceResult{body: body, err: err}
				w.Header().Set("Location", target.URL+"/redirect-target")
				w.WriteHeader(statusCode)
				_, _ = io.WriteString(w, responseBody)
			}))
			defer source.Close()

			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/relay", nil)

			req, err := http.NewRequest(http.MethodPost, source.URL, bytes.NewReader([]byte("request body")))
			require.NoError(t, err)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

			resp, err := doRequest(ctx, req, info)
			require.NoError(t, err)
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			gotSource := <-sourceResultCh
			require.NoError(t, gotSource.err)

			assert.Equal(t, statusCode, resp.StatusCode)
			assert.Equal(t, target.URL+"/redirect-target", resp.Header.Get("Location"))
			assert.Equal(t, responseBody, string(body))
			assert.Equal(t, []byte("request body"), gotSource.body)
			assert.EqualValues(t, 1, sourceRequests.Load())
			assert.Zero(t, targetRequests.Load())
		})
	}

	assert.Equal(t, originalRedirectPolicy, reflect.ValueOf(sharedClient.CheckRedirect).Pointer(), "the cached client must not be mutated")
}

func TestDoRequestSetsUpstreamRequestIdResponseHeader(t *testing.T) {
	service.InitHttpClient()
	gin.SetMode(gin.TestMode)

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(common.RequestIdKey, "upstream-id-123")
		w.WriteHeader(http.StatusOK)
	}))
	defer source.Close()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/relay", nil)

	req, err := http.NewRequest(http.MethodPost, source.URL, bytes.NewReader(nil))
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

	resp, err := doRequest(ctx, req, info)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, "upstream-id-123", ctx.GetString(common.UpstreamRequestIdKey))
	assert.Equal(t, "upstream-id-123", ctx.Writer.Header().Get(common.UpstreamRequestIdKey))
}

func TestDoRequestUsesConfiguredUpstreamRequestIdHeader(t *testing.T) {
	service.InitHttpClient()
	gin.SetMode(gin.TestMode)

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Custom-Request-Id", "custom-id-456")
		w.WriteHeader(http.StatusOK)
	}))
	defer source.Close()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/relay", nil)

	req, err := http.NewRequest(http.MethodPost, source.URL, bytes.NewReader(nil))
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	info.ChannelSetting.UpstreamRequestIdHeader = "X-Custom-Request-Id"

	resp, err := doRequest(ctx, req, info)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, "custom-id-456", ctx.GetString(common.UpstreamRequestIdKey))
	assert.Equal(t, "custom-id-456", ctx.Writer.Header().Get(common.UpstreamRequestIdKey))
}
