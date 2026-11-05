package controller

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayKeepAliveDuringImagePreparation(t *testing.T) {
	settings := operation_setting.GetGeneralSetting()
	previousEnabled, previousInterval := settings.PingIntervalEnabled, settings.PingIntervalSeconds
	previousCount, previousMedia := constant.CountToken, constant.GetMediaToken
	fetchSettings := system_setting.GetFetchSetting()
	previousFetchSettings := *fetchSettings
	fetchSettings.AllowPrivateIp = true
	fetchSettings.AllowedPorts = []string{"1-65535"}
	settings.PingIntervalEnabled, settings.PingIntervalSeconds = true, 1
	constant.CountToken, constant.GetMediaToken = true, true
	t.Cleanup(func() {
		settings.PingIntervalEnabled, settings.PingIntervalSeconds = previousEnabled, previousInterval
		constant.CountToken, constant.GetMediaToken = previousCount, previousMedia
		*fetchSettings = previousFetchSettings
	})
	service.InitHttpClient()

	imageRequested := make(chan struct{})
	releaseImage := make(chan struct{})
	var releaseOnce sync.Once
	imageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(imageRequested)
		<-releaseImage
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer imageServer.Close()
	engine := gin.New()
	engine.POST("/v1/messages", func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyOriginalModel, "claude-sonnet-4-20250514")
		Relay(c, types.RelayFormatClaude)
	})
	gateway := httptest.NewServer(engine)
	defer gateway.Close()
	defer releaseOnce.Do(func() { close(releaseImage) })

	body := `{"model":"claude-sonnet-4-20250514","max_tokens":16,"stream":true,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"url","url":"` + imageServer.URL + `"}}]}]}`
	client := &http.Client{Timeout: 4 * time.Second}
	response, err := client.Post(gateway.URL+"/v1/messages", "application/json", strings.NewReader(body))
	require.NoError(t, err, "the gateway must send keepalive while its image download is blocked")
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, "text/event-stream", response.Header.Get("Content-Type"))
	reader := bufio.NewReader(response.Body)
	for range 2 {
		line, readErr := reader.ReadString('\n')
		require.NoError(t, readErr)
		require.Equal(t, ": PING\n", line)
		line, readErr = reader.ReadString('\n')
		require.NoError(t, readErr)
		require.Equal(t, "\n", line)
	}
	select {
	case <-imageRequested:
	default:
		t.Fatal("the request never reached image preparation")
	}
	releaseOnce.Do(func() { close(releaseImage) })
	remaining, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Contains(t, string(remaining), "event: error\n")
	require.Contains(t, string(remaining), `"type":"error"`)
	require.Contains(t, string(remaining), "data: ")
}

func TestRelayKeepAliveAcrossUpstreamRetries(t *testing.T) {
	settings := operation_setting.GetGeneralSetting()
	previousEnabled, previousInterval := settings.PingIntervalEnabled, settings.PingIntervalSeconds
	settings.PingIntervalEnabled, settings.PingIntervalSeconds = true, 1
	t.Cleanup(func() {
		settings.PingIntervalEnabled, settings.PingIntervalSeconds = previousEnabled, previousInterval
	})
	service.InitHttpClient()
	var attempts atomic.Int32
	retryStarted, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		close(retryStarted)
		select {
		case <-release:
			_, _ = io.WriteString(w, "data: ok\n\n")
		case <-r.Context().Done():
		}
	}))
	defer upstream.Close()
	engine := gin.New()
	engine.POST("/v1/messages", func(c *gin.Context) {
		stop := helper.StartStreamKeepAlive(c, 10*time.Millisecond)
		defer stop()
		if !assert.NoError(t, http.NewResponseController(c.Writer).SetWriteDeadline(time.Now().Add(5*time.Second))) {
			return
		}
		info := &relaycommon.RelayInfo{IsStream: true, ChannelMeta: &relaycommon.ChannelMeta{}}
		for range 2 {
			request, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, upstream.URL, strings.NewReader("{}"))
			if !assert.NoError(t, err) {
				return
			}
			response, err := channel.DoRequest(c, request, info)
			if !assert.NoError(t, err) {
				return
			}
			if response.StatusCode == http.StatusTooManyRequests {
				response.Body.Close()
				continue
			}
			_, _ = io.Copy(c.Writer, response.Body)
			response.Body.Close()
		}
	})
	gateway := httptest.NewServer(engine)
	defer gateway.Close()
	defer releaseOnce.Do(func() { close(release) })
	client := &http.Client{Timeout: 4 * time.Second}
	response, err := client.Post(gateway.URL+"/v1/messages", "application/json", strings.NewReader("{}"))
	require.NoError(t, err)
	defer response.Body.Close()
	select {
	case <-retryStarted:
	case <-time.After(time.Second):
		t.Fatal("the upstream retry did not start")
	}
	reader := bufio.NewReader(response.Body)
	for range 2 {
		line, readErr := reader.ReadString('\n')
		require.NoError(t, readErr)
		require.Equal(t, ": PING\n", line)
		line, readErr = reader.ReadString('\n')
		require.NoError(t, readErr)
		require.Equal(t, "\n", line)
	}
	releaseOnce.Do(func() { close(release) })
	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Contains(t, string(body), "data: ok\n\n")
	require.Equal(t, int32(2), attempts.Load())
}

func TestRelayKeepAlivePreservesValidationErrors(t *testing.T) {
	settings := operation_setting.GetGeneralSetting()
	previousEnabled := settings.PingIntervalEnabled
	settings.PingIntervalEnabled = true
	t.Cleanup(func() { settings.PingIntervalEnabled = previousEnabled })
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4-20250514","stream":true}`))
	c.Request.Header.Set("Content-Type", "application/json")
	Relay(c, types.RelayFormatClaude)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Header().Get("Content-Type"), "application/json")
	require.NotContains(t, recorder.Body.String(), ": PING")
}

func TestStreamKeepAlivePreservesFastHTTPFailures(t *testing.T) {
	settings := operation_setting.GetGeneralSetting()
	previousEnabled, previousInterval := settings.PingIntervalEnabled, settings.PingIntervalSeconds
	settings.PingIntervalEnabled, settings.PingIntervalSeconds = true, 60
	t.Cleanup(func() {
		settings.PingIntervalEnabled, settings.PingIntervalSeconds = previousEnabled, previousInterval
	})
	service.InitHttpClient()
	for _, status := range []int{http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":"request rejected"}`)
			}))
			defer upstream.Close()
			engine := gin.New()
			engine.POST("/v1/messages", func(c *gin.Context) {
				stop := helper.StartStreamKeepAlive(c, time.Minute)
				defer stop()
				request, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, upstream.URL, strings.NewReader("{}"))
				if !assert.NoError(t, err) {
					return
				}
				info := &relaycommon.RelayInfo{IsStream: true, ChannelMeta: &relaycommon.ChannelMeta{}}
				response, err := channel.DoRequest(c, request, info)
				if !assert.NoError(t, err) {
					return
				}
				defer response.Body.Close()
				body, err := io.ReadAll(response.Body)
				if assert.NoError(t, err) {
					c.Header("Content-Type", "application/json")
					c.Data(response.StatusCode, "application/json", body)
				}
			})
			gateway := httptest.NewServer(engine)
			defer gateway.Close()
			client := &http.Client{Timeout: 4 * time.Second}
			response, err := client.Post(gateway.URL+"/v1/messages", "application/json", strings.NewReader("{}"))
			require.NoError(t, err)
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Equal(t, status, response.StatusCode)
			require.Equal(t, "application/json", response.Header.Get("Content-Type"))
			require.JSONEq(t, `{"error":"request rejected"}`, string(body))
		})
	}
}

func TestRelayKeepAliveHonorsStreamSettings(t *testing.T) {
	settings := operation_setting.GetGeneralSetting()
	previousEnabled := settings.PingIntervalEnabled
	previousCount, previousMedia, previousNonStreamMedia := constant.CountToken, constant.GetMediaToken, constant.GetMediaTokenNotStream
	constant.CountToken, constant.GetMediaToken, constant.GetMediaTokenNotStream = true, true, true
	t.Cleanup(func() {
		settings.PingIntervalEnabled = previousEnabled
		constant.CountToken, constant.GetMediaToken, constant.GetMediaTokenNotStream = previousCount, previousMedia, previousNonStreamMedia
	})
	for _, test := range []struct {
		name    string
		enabled bool
		stream  bool
	}{
		{name: "ping disabled", enabled: false, stream: true},
		{name: "non-streaming", enabled: true, stream: false},
		{name: "fast streaming failure", enabled: true, stream: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			settings.PingIntervalEnabled = test.enabled
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			body := `{"model":"claude-sonnet-4-20250514","max_tokens":16,"stream":` + strconv.FormatBool(test.stream) + `,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"!"}}]}]}`
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			common.SetContextKey(c, constant.ContextKeyOriginalModel, "claude-sonnet-4-20250514")
			Relay(c, types.RelayFormatClaude)
			require.Equal(t, http.StatusInternalServerError, recorder.Code)
			require.Contains(t, recorder.Header().Get("Content-Type"), "application/json")
			require.NotContains(t, recorder.Body.String(), ": PING")
			var response map[string]any
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			require.Equal(t, "error", response["type"])
		})
	}
}

func TestRelayStreamErrorAfterKeepAlive(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAI} {
		t.Run(string(format), func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/stream", nil)
			helper.SetEventStreamHeaders(c)
			require.NoError(t, helper.PingData(c))
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://provider.example")
			common.SetContextKey(c, constant.ContextKeyUpstreamRequestOrigin, "https://provider.example")
			apiError := types.NewErrorWithStatusCode(errors.New("upstream rejected input at https://provider.example/v1/messages"), types.ErrorCodeBadResponseStatusCode, http.StatusBadRequest)
			writeRelayStreamError(c, format, apiError)
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
			require.Contains(t, recorder.Body.String(), "upstream rejected input")
			require.NotContains(t, recorder.Body.String(), "provider.example")
			status, ok := common.GetContextKeyType[*relaycommon.StreamStatus](c, constant.ContextKeyResponseStreamStatus)
			require.True(t, ok)
			require.True(t, status.ResponseFailed(), "stream errors must not consume the successful-request rate limit")
			require.Equal(t, http.StatusBadRequest, status.OutcomeSnapshot().ErrorStatus)
			if format == types.RelayFormatClaude {
				require.Contains(t, recorder.Body.String(), "event: error\n")
			} else {
				require.Contains(t, recorder.Body.String(), "data: [DONE]")
			}
		})
	}
}

func TestParamOverrideResponseHeaders(t *testing.T) {
	const rule = `{"operations":[{
		"mode":"return_error",
		"conditions":[
			{"path":"tools.#(type==\"web_search_20250305\").type","mode":"full","value":"web_search_20250305"},
			{"path":"tools.#(type==\"web_search\").type","mode":"full","value":"web_search"},
			{"path":"tools.#(type==\"web_search_preview\").type","mode":"full","value":"web_search_preview"}
		],
		"logic":"OR",
		"value":{
			"status_code":403,
			"message":"Web search tools are not permitted on this channel.",
			"code":"web_search_not_allowed",
			"type":"invalid_request_error",
			"response_headers":{"x-web-search-blocked":"true","X-Block-Reason":"web_search_not_allowed","Retry-After":"60"}
		}
	}]}`
	var override map[string]any
	require.NoError(t, common.UnmarshalJsonStr(rule, &override))
	for _, tool := range []string{"web_search_20250305", "web_search", "web_search_preview"} {
		for _, format := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAI, types.RelayFormatGemini} {
			t.Run(tool+"/"+string(format), func(t *testing.T) {
				input := []byte(`{"tools":[{"type":"function","name":"other"},{"type":"` + tool + `","name":"web_search"}]}`)
				_, err := relaycommon.ApplyParamOverride(input, override, nil)
				require.Error(t, err)
				returnErr, ok := relaycommon.AsParamOverrideReturnError(err)
				require.True(t, ok)
				apiError := relaycommon.NewAPIErrorFromParamOverride(returnErr)
				assert.True(t, types.IsSkipRetryError(apiError))
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/relay", nil)
				writeRelayError(c, format, apiError, nil)
				assert.Equal(t, http.StatusForbidden, recorder.Code)
				assert.Equal(t, "true", recorder.Result().Header.Get("X-Web-Search-Blocked"))
				assert.Equal(t, "web_search_not_allowed", recorder.Result().Header.Get("X-Block-Reason"))
				assert.Equal(t, "60", recorder.Result().Header.Get("Retry-After"))
				assert.Equal(t, "application/json; charset=utf-8", recorder.Result().Header.Get("Content-Type"))
				if format == types.RelayFormatClaude {
					assert.JSONEq(t, `{"type":"error","error":{"type":"web_search_not_allowed","message":"Web search tools are not permitted on this channel."}}`, recorder.Body.String())
				} else {
					assert.JSONEq(t, `{"error":{"type":"invalid_request_error","code":"web_search_not_allowed","message":"Web search tools are not permitted on this channel.","param":""}}`, recorder.Body.String())
				}
			})
		}
	}
	t.Run("unmatched tools pass through", func(t *testing.T) {
		for _, input := range []string{`{}`, `{"tools":[]}`, `{"tools":[{"type":"function","name":"other"}]}`} {
			output, err := relaycommon.ApplyParamOverride([]byte(input), override, nil)
			require.NoError(t, err)
			assert.JSONEq(t, input, string(output))
		}
	})
	t.Run("only terminal error headers are applied", func(t *testing.T) {
		_, err := relaycommon.ApplyParamOverride([]byte(`{"tools":[{"type":"web_search"}]}`), override, nil)
		returnErr, ok := relaycommon.AsParamOverrideReturnError(err)
		require.True(t, ok)
		returnErr.SkipRetry = false
		firstError := relaycommon.NewAPIErrorFromParamOverride(returnErr)
		assert.False(t, types.IsSkipRetryError(firstError))
		// Converting an intermediate error must not write headers to the response.
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/relay", nil)
		assert.Empty(t, recorder.Header())
		finalError := types.NewErrorWithStatusCode(errors.New("later failure"), types.ErrorCodeBadResponse, http.StatusBadGateway)
		writeRelayError(c, types.RelayFormatOpenAI, finalError, nil)
		assert.Empty(t, recorder.Result().Header.Get("X-Web-Search-Blocked"))
		assert.Equal(t, http.StatusBadGateway, recorder.Code)
	})
	t.Run("committed stream retains its headers", func(t *testing.T) {
		_, err := relaycommon.ApplyParamOverride([]byte(`{"tools":[{"type":"web_search"}]}`), override, nil)
		returnErr, ok := relaycommon.AsParamOverrideReturnError(err)
		require.True(t, ok)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/relay", nil)
		helper.SetEventStreamHeaders(c)
		require.NoError(t, helper.PingData(c))
		writeRelayError(c, types.RelayFormatClaude, relaycommon.NewAPIErrorFromParamOverride(returnErr), nil)
		assert.Equal(t, http.StatusOK, recorder.Code)
		assert.Empty(t, recorder.Header().Get("X-Web-Search-Blocked"))
		assert.Equal(t, "text/event-stream", recorder.Result().Header.Get("Content-Type"))
		assert.Contains(t, recorder.Body.String(), "event: error\n")
	})
}

func TestParamOverrideResponseHeaderValidation(t *testing.T) {
	for _, test := range []struct {
		name    string
		value   string
		invalid bool
	}{
		{name: "legacy string", value: `"blocked"`},
		{name: "legacy object", value: `{"message":"blocked"}`},
		{name: "empty headers", value: `{"message":"blocked","response_headers":{}}`},
		{name: "empty header value", value: `{"message":"blocked","response_headers":{"X-Empty":""}}`},
		{name: "non object", value: `{"message":"blocked","response_headers":[]}`, invalid: true},
		{name: "null", value: `{"message":"blocked","response_headers":null}`, invalid: true},
		{name: "numeric value", value: `{"message":"blocked","response_headers":{"Retry-After":60}}`, invalid: true},
		{name: "newline in name", value: `{"message":"blocked","response_headers":{"X-Bad\r\nInjected":"x"}}`, invalid: true},
		{name: "newline in value", value: `{"message":"blocked","response_headers":{"X-Bad":"x\r\nInjected: yes"}}`, invalid: true},
		{name: "duplicate names", value: `{"message":"blocked","response_headers":{"X-Flag":"a","x-flag":"b"}}`, invalid: true},
		{name: "content length", value: `{"message":"blocked","response_headers":{"content-length":"0"}}`, invalid: true},
		{name: "transfer encoding", value: `{"message":"blocked","response_headers":{"Transfer-Encoding":"chunked"}}`, invalid: true},
		{name: "connection", value: `{"message":"blocked","response_headers":{"Connection":"X-Flag"}}`, invalid: true},
		{name: "content type", value: `{"message":"blocked","response_headers":{"Content-Type":"text/event-stream"}}`, invalid: true},
		{name: "content encoding", value: `{"message":"blocked","response_headers":{"Content-Encoding":"gzip"}}`, invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var override map[string]any
			require.NoError(t, common.UnmarshalJsonStr(`{"operations":[{"mode":"return_error","value":`+test.value+`}]}`, &override))
			output, err := relaycommon.ApplyParamOverride([]byte(`{}`), override, nil)
			if test.invalid {
				// Preserve the rule engine's existing behavior: invalid operations
				// are skipped, without exposing any configured response headers.
				require.NoError(t, err)
				assert.JSONEq(t, `{}`, string(output))
				return
			}
			require.Error(t, err)
			returnErr, ok := relaycommon.AsParamOverrideReturnError(err)
			require.True(t, ok)
			assert.Equal(t, http.StatusBadRequest, returnErr.StatusCode)
			assert.True(t, returnErr.SkipRetry)
		})
	}
}

func TestStreamKeepAliveCancellation(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	requestContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Request = httptest.NewRequest(http.MethodPost, "/stream", nil).WithContext(requestContext)
	stop := helper.StartStreamKeepAlive(c, time.Hour)
	cancel()
	stopped := make(chan struct{})
	go func() {
		stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("keepalive did not stop after client cancellation")
	}
	_, err := c.Writer.WriteString("data: done\n\n")
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(recorder.Body.String(), "data: done\n\n"))
}
