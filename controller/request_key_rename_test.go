package controller

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Request body field rename is covered end to end in this one file: rule
// resolution, the body rewrite, the per-attempt request state it installs, and
// the relay retry behavior around it.
// TestResolveRequestKeyRenameRules pins how a channel's request body field
// rename rules are assembled: referenced templates first (selection order),
// the channel's own rules last, and nothing when the channel limits renaming
// to other models.
func TestResolveRequestKeyRenameRules(t *testing.T) {
	templates := operation_setting.GetOverrideTemplateSetting()
	previous := templates.RequestKeyRenameTemplates
	t.Cleanup(func() { templates.RequestKeyRenameTemplates = previous })
	templates.RequestKeyRenameTemplates = []operation_setting.OverrideTemplateItem{
		{Name: "gemini", Content: json.RawMessage(`[{"path":"contents.*.parts.*","keys":{"file_data":"fileData"}}]`)},
		{Name: "broken", Content: json.RawMessage(`{"path":"x"}`)},
	}

	own := dto.RequestKeyRenameRule{Path: "", Keys: map[string]string{"max_tokens": "maxTokens"}}
	fromTemplate := dto.RequestKeyRenameRule{Path: "contents.*.parts.*", Keys: map[string]string{"file_data": "fileData"}}

	tests := []struct {
		name      string
		setting   dto.ChannelSettings
		modelName string
		want      []dto.RequestKeyRenameRule
	}{
		{
			name:      "templates come before the channel's own rules, unknown and broken refs are skipped",
			setting:   dto.ChannelSettings{RequestKeyRename: []dto.RequestKeyRenameRule{own}, RequestKeyRenameTemplateRefs: []string{"missing", "broken", "gemini"}},
			modelName: "gemini-2.5-pro",
			want:      []dto.RequestKeyRenameRule{fromTemplate, own},
		},
		{
			name:      "empty model list applies to every model",
			setting:   dto.ChannelSettings{RequestKeyRename: []dto.RequestKeyRenameRule{own}},
			modelName: "any-model",
			want:      []dto.RequestKeyRenameRule{own},
		},
		{
			name:      "listed model gets channel and template rules",
			setting:   dto.ChannelSettings{RequestKeyRename: []dto.RequestKeyRenameRule{own}, RequestKeyRenameTemplateRefs: []string{"gemini"}, RequestKeyRenameModels: []string{"gemini-2.5-pro"}},
			modelName: "gemini-2.5-pro",
			want:      []dto.RequestKeyRenameRule{fromTemplate, own},
		},
		{
			name:      "model outside the list gets no rules, templates included",
			setting:   dto.ChannelSettings{RequestKeyRename: []dto.RequestKeyRenameRule{own}, RequestKeyRenameTemplateRefs: []string{"gemini"}, RequestKeyRenameModels: []string{"gemini-2.5-pro"}},
			modelName: "gemini-2.5-flash",
			want:      nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, service.ResolveRequestKeyRenameRules(tt.setting, tt.modelName))
		})
	}
}

func TestRenameRequestKeys(t *testing.T) {
	geminiFileDataRules := []dto.RequestKeyRenameRule{
		{Path: "contents.*.parts.*", Keys: map[string]string{"file_data": "fileData"}},
		{Path: "contents.*.parts.*.file_data", Keys: map[string]string{"mime_type": "mimeType", "file_uri": "fileUri"}},
	}

	tests := []struct {
		name      string
		body      string
		rules     []dto.RequestKeyRenameRule
		want      string
		wantAudit []string
	}{
		{
			name:      "gemini file_data renamed at exact levels, values untouched",
			body:      `{"contents":[{"role":"user","parts":[{"text":"file_data mime_type"},{"file_data":{"mime_type":"image/jpeg","file_uri":"https://x/file_uri.jpg"}}]}],"generationConfig":{"thinking_config":{"thinking_level":"HIGH"}}}`,
			rules:     geminiFileDataRules,
			want:      `{"contents":[{"role":"user","parts":[{"text":"file_data mime_type"},{"fileData":{"mimeType":"image/jpeg","fileUri":"https://x/file_uri.jpg"}}]}],"generationConfig":{"thinking_config":{"thinking_level":"HIGH"}}}`,
			wantAudit: []string{"rename contents.0.parts.1.file_data -> fileData", "rename contents.0.parts.1.file_data.mime_type -> mimeType", "rename contents.0.parts.1.file_data.file_uri -> fileUri"},
		},
		{
			name:      "same key outside the configured path is kept",
			body:      `{"tools":[{"functionDeclarations":[{"parameters":{"properties":{"file_data":{"type":"string"}}}}]}],"contents":[{"parts":[{"file_data":{}}]}]}`,
			rules:     geminiFileDataRules[:1],
			want:      `{"tools":[{"functionDeclarations":[{"parameters":{"properties":{"file_data":{"type":"string"}}}}]}],"contents":[{"parts":[{"fileData":{}}]}]}`,
			wantAudit: []string{"rename contents.0.parts.0.file_data -> fileData"},
		},
		{
			name:      "double star matches any depth including root",
			body:      `{"thinking_config":1,"generationConfig":{"thinking_config":{"thinking_level":"HIGH"}}}`,
			rules:     []dto.RequestKeyRenameRule{{Path: "**", Keys: map[string]string{"thinking_config": "thinkingConfig"}}},
			want:      `{"thinkingConfig":1,"generationConfig":{"thinkingConfig":{"thinking_level":"HIGH"}}}`,
			wantAudit: []string{"rename thinking_config -> thinkingConfig", "rename generationConfig.thinking_config -> thinkingConfig"},
		},
		{
			name:      "root path renames only top-level keys",
			body:      `{"max_tokens":0,"a":{"max_tokens":1.50}}`,
			rules:     []dto.RequestKeyRenameRule{{Path: "$", Keys: map[string]string{"max_tokens": "maxTokens"}}},
			want:      `{"maxTokens":0,"a":{"max_tokens":1.50}}`,
			wantAudit: []string{"rename max_tokens -> maxTokens"},
		},
		{
			name: "later rule wins for the same key at the same path",
			body: `{"file_data":1}`,
			rules: []dto.RequestKeyRenameRule{
				{Path: "", Keys: map[string]string{"file_data": "fromTemplate"}},
				{Path: "$", Keys: map[string]string{"file_data": "fileData"}},
			},
			want:      `{"fileData":1}`,
			wantAudit: []string{"rename file_data -> fileData"},
		},
		{
			name:      "existing target key wins and source is left as is",
			body:      `{"file_data":1,"fileData":2}`,
			rules:     []dto.RequestKeyRenameRule{{Path: "", Keys: map[string]string{"file_data": "fileData"}}},
			want:      `{"file_data":1,"fileData":2}`,
			wantAudit: nil,
		},
		{
			name:      "escaped source key matches its decoded name",
			body:      `{"file` + "\\" + `u005fdata":{"value":"untouched"}}`,
			rules:     []dto.RequestKeyRenameRule{{Path: "", Keys: map[string]string{"file_data": "fileData"}}},
			want:      `{"fileData":{"value":"untouched"}}`,
			wantAudit: []string{"rename file_data -> fileData"},
		},
		{
			name:      "escaped parent and child keys match decoded paths",
			body:      `{"file` + "\\" + `u005fdata":{"mime` + "\\" + `u005ftype":"image/jpeg"}}`,
			rules:     []dto.RequestKeyRenameRule{{Path: "file_data", Keys: map[string]string{"mime_type": "mimeType"}}},
			want:      `{"file` + "\\" + `u005fdata":{"mimeType":"image/jpeg"}}`,
			wantAudit: []string{"rename file_data.mime_type -> mimeType"},
		},
		{
			name:      "escaped existing target still prevents a collision",
			body:      `{"file` + "\\" + `u005fdata":1,"file` + "\\" + `u0044ata":2}`,
			rules:     []dto.RequestKeyRenameRule{{Path: "", Keys: map[string]string{"file_data": "fileData"}}},
			want:      `{"file` + "\\" + `u005fdata":1,"file` + "\\" + `u0044ata":2}`,
			wantAudit: nil,
		},
		{
			name:      "formatting and escaped strings are preserved",
			body:      "{\n  \"parts\": [ { \"file_data\" : { \"x\": \"a\\\"}b\" } } ]\n}",
			rules:     []dto.RequestKeyRenameRule{{Path: "parts.*", Keys: map[string]string{"file_data": "fileData"}}},
			want:      "{\n  \"parts\": [ { \"fileData\" : { \"x\": \"a\\\"}b\" } } ]\n}",
			wantAudit: []string{"rename parts.0.file_data -> fileData"},
		},
		{
			name:      "numeric index segment targets one array element",
			body:      `{"parts":[{"file_data":1},{"file_data":2}]}`,
			rules:     []dto.RequestKeyRenameRule{{Path: "parts.1", Keys: map[string]string{"file_data": "fileData"}}},
			want:      `{"parts":[{"file_data":1},{"fileData":2}]}`,
			wantAudit: []string{"rename parts.1.file_data -> fileData"},
		},
		{
			name:      "audit lines fold array indices into ranges",
			body:      `{"parts":[{"file_data":1},{"file_data":2}]}`,
			rules:     []dto.RequestKeyRenameRule{{Path: "parts.*", Keys: map[string]string{"file_data": "fileData"}}},
			want:      `{"parts":[{"fileData":1},{"fileData":2}]}`,
			wantAudit: []string{"rename parts.[0-1].file_data -> fileData"},
		},
		{
			name:      "invalid json is returned unchanged",
			body:      `{"file_data":`,
			rules:     []dto.RequestKeyRenameRule{{Path: "", Keys: map[string]string{"file_data": "fileData"}}},
			want:      `{"file_data":`,
			wantAudit: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, audit, err := relaycommon.RenameRequestKeys([]byte(tt.body), tt.rules)
			require.NoError(t, err)
			assert.Equal(t, tt.wantAudit, audit)
			assert.Equal(t, tt.want, string(got))
		})
	}
}

// Request bodies can reach tens of megabytes, where the rewrite buffer is the
// dominant allocation of the whole relay. A body that no rule rewrites must
// therefore come back as the caller's own slice, never as a copy.
func TestRenameRequestKeysReturnsOriginalBodyWhenNothingRenamed(t *testing.T) {
	rules := []dto.RequestKeyRenameRule{{Path: "contents.*.parts.*", Keys: map[string]string{"file_data": "fileData"}}}

	tests := []struct {
		name string
		body string
	}{
		{name: "no rule key anywhere in the body", body: `{"contents":[{"parts":[{"fileData":{"mimeType":"image/png"}}]}]}`},
		{name: "rule key only present as a value", body: `{"contents":[{"parts":[{"text":"file_data"}]}]}`},
		{name: "rule key present outside the configured path", body: `{"tools":[{"file_data":1}],"contents":[{"parts":[{"text":"x"}]}]}`},
		{name: "target key already set on the same object", body: `{"contents":[{"parts":[{"file_data":1,"fileData":2}]}]}`},
		{name: "escaped key the literal scan cannot resolve", body: `{"contents":[{"parts":[{"mime` + "\\" + `u005ftype":"image/png"}]}]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := []byte(tt.body)
			got, audit, err := relaycommon.RenameRequestKeys(data, rules)
			require.NoError(t, err)
			require.NotEmpty(t, got)
			assert.Empty(t, audit)
			assert.Equal(t, tt.body, string(got))
			assert.Same(t, &data[0], &got[0], "body must be returned without a rewrite copy")
		})
	}
}

func TestRenameRequestKeysRecoversGeminiFileData(t *testing.T) {
	body := `{"contents":[{"role":"user","parts":[{"file_data":{"mime_type":"image/jpeg","file_uri":"https://img/a.jpg"}}]}]}`
	rules := []dto.RequestKeyRenameRule{
		{Path: "contents.*.parts.*", Keys: map[string]string{"file_data": "fileData"}},
		{Path: "contents.*.parts.*.file_data", Keys: map[string]string{"mime_type": "mimeType", "file_uri": "fileUri"}},
	}

	renamed, audit, err := relaycommon.RenameRequestKeys([]byte(body), rules)
	require.NoError(t, err)
	require.NotEmpty(t, audit)

	var req dto.GeminiChatRequest
	require.NoError(t, req.UnmarshalJSON(renamed))
	require.Len(t, req.Contents, 1)
	require.Len(t, req.Contents[0].Parts, 1)
	require.NotNil(t, req.Contents[0].Parts[0].FileData)
	assert.Equal(t, "image/jpeg", req.Contents[0].Parts[0].FileData.MimeType)
	assert.Equal(t, "https://img/a.jpg", req.Contents[0].Parts[0].FileData.FileUri)
}

func TestRequestKeyRenameRetryRestoresUncommittedStreamMode(t *testing.T) {
	require.NoError(t, i18n.Init())
	db := modelManagementDB(t, "sqlite", "")
	require.NoError(t, db.AutoMigrate(&model.Token{}, &model.UserSubscription{}, &model.SubscriptionPlan{}))
	previousRetries, previousLogs := common.RetryTimes, common.LogConsumeEnabled
	previousErrors, previousCount := constant.ErrorLogEnabled, constant.CountToken
	common.RetryTimes, common.LogConsumeEnabled = 1, false
	constant.ErrorLogEnabled, constant.CountToken = false, false
	t.Cleanup(func() {
		common.RetryTimes, common.LogConsumeEnabled = previousRetries, previousLogs
		constant.ErrorLogEnabled, constant.CountToken = previousErrors, previousCount
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"rename-retry":0}`))
	service.InitHttpClient()
	user := model.User{Id: 7001, Username: "rename-retry", Status: common.UserStatusEnabled, Group: "default", Quota: 100000}
	require.NoError(t, db.Create(&user).Error)
	token := model.Token{UserId: user.Id, Key: "renameretrytoken", Status: common.TokenStatusEnabled, ExpiredTime: -1, UnlimitedQuota: true}
	require.NoError(t, db.Create(&token).Error)

	type attempt struct {
		path string
		body map[string]any
	}
	attempts := make(chan attempt, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if !assert.NoError(t, common.DecodeJson(r.Body, &body)) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		attempts <- attempt{path: r.URL.Path, body: body}
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/first/") {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":{"message":"retry this attempt","type":"server_error","code":"server_error"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"retry-success","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"restored"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	t.Cleanup(upstream.Close)
	channels := []model.Channel{
		{Name: "rename-first", Type: constant.ChannelTypeOpenAI, Key: "test-only", Status: common.ChannelStatusEnabled, Models: "rename-retry", Group: "default", Priority: common.GetPointer(int64(20)), BaseURL: common.GetPointer(upstream.URL + "/first"), AutoBan: common.GetPointer(0)},
		{Name: "original-second", Type: constant.ChannelTypeOpenAI, Key: "test-only", Status: common.ChannelStatusEnabled, Models: "rename-retry", Group: "default", Priority: common.GetPointer(int64(10)), BaseURL: common.GetPointer(upstream.URL + "/second"), AutoBan: common.GetPointer(0)},
	}
	channels[0].SetSetting(dto.ChannelSettings{PassThroughBodyEnabled: true, RequestKeyRename: []dto.RequestKeyRenameRule{{Keys: map[string]string{"streaming": "stream", "metadata_alias": "metadata"}}}})
	channels[1].SetSetting(dto.ChannelSettings{PassThroughBodyEnabled: true})
	for i := range channels {
		require.NoError(t, db.Create(&channels[i]).Error)
		require.NoError(t, channels[i].AddAbilities(db))
	}

	committed := false
	engine := gin.New()
	engine.POST("/v1/chat/completions", middleware.TokenAuth(), middleware.Distribute(), func(c *gin.Context) {
		defer common.CleanupBodyStorage(c)
		if committed {
			helper.SetEventStreamHeaders(c)
			_, _ = c.Writer.WriteString(": PING\n\n")
			c.Writer.Flush()
		}
		Relay(c, types.RelayFormatOpenAI)
	})
	const body = `{"model":"rename-retry","messages":[{"role":"user","content":"hello"}],"streaming":true,"metadata_alias":{"tag":"original"}}`
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer sk-"+token.Key)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	assert.Contains(t, recorder.Body.String(), `"content":"restored"`)
	require.Len(t, attempts, 2, "a prepared SSE header must not prevent the uncommitted retry")
	first, second := <-attempts, <-attempts
	assert.Equal(t, "/first/v1/chat/completions", first.path)
	assert.Equal(t, true, first.body["stream"])
	assert.Equal(t, map[string]any{"tag": "original"}, first.body["metadata"])
	assert.NotContains(t, first.body, "streaming")
	assert.NotContains(t, first.body, "metadata_alias")
	assert.Equal(t, "/second/v1/chat/completions", second.path)
	var original map[string]any
	require.NoError(t, common.UnmarshalJsonStr(body, &original))
	assert.Equal(t, original, second.body, "the next channel must receive the client's original body")

	committed = true
	request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer sk-"+token.Key)
	recorder = httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusOK, recorder.Code, "the already committed SSE status must be preserved")
	assert.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	assert.Contains(t, recorder.Body.String(), ": PING\n\n")
	assert.Contains(t, recorder.Body.String(), "cannot disable streaming after SSE has started")
	require.Len(t, attempts, 1, "a committed SSE response must not retry through a non-stream channel")
	assert.Equal(t, "/first/v1/chat/completions", (<-attempts).path)

	for _, tc := range []struct {
		name      string
		rules     []dto.RequestKeyRenameRule
		firstBody string
	}{
		{name: "no rename rules"},
		{name: "rename rules do not match", rules: []dto.RequestKeyRenameRule{{Keys: map[string]string{"absent": "renamed"}}}},
		{
			name:      "rename leaves stream mode unchanged",
			rules:     []dto.RequestKeyRenameRule{{Keys: map[string]string{"metadata_alias": "metadata"}}},
			firstBody: `{"model":"rename-retry","messages":[{"role":"user","content":"hello"}],"stream":false,"metadata":{"tag":"original"}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			channels[0].SetSetting(dto.ChannelSettings{PassThroughBodyEnabled: true, RequestKeyRename: tc.rules})
			require.NoError(t, db.Model(&channels[0]).Update("setting", channels[0].Setting).Error)
			const originalBody = `{"model":"rename-retry","messages":[{"role":"user","content":"hello"}],"stream":false,"metadata_alias":{"tag":"original"}}`
			request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(originalBody))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer sk-"+token.Key)
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)
			assert.Equal(t, http.StatusOK, recorder.Code)
			assert.Contains(t, recorder.Body.String(), `"content":"restored"`)
			assert.NotContains(t, recorder.Body.String(), "request key rename cannot disable streaming")
			require.Len(t, attempts, 2, "unchanged stream mode must preserve the existing retry behavior")
			first, second := <-attempts, <-attempts
			assert.Equal(t, "/first/v1/chat/completions", first.path)
			assert.Equal(t, "/second/v1/chat/completions", second.path)
			if tc.firstBody == "" {
				tc.firstBody = originalBody
			}
			var expectedFirst, expectedSecond map[string]any
			require.NoError(t, common.UnmarshalJsonStr(tc.firstBody, &expectedFirst))
			require.NoError(t, common.UnmarshalJsonStr(originalBody, &expectedSecond))
			assert.Equal(t, expectedFirst, first.body)
			assert.Equal(t, expectedSecond, second.body)
		})
	}
}

func TestRequestKeyRenameSynchronizesRequestState(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		format types.RelayFormat
		body   string
		rules  []dto.RequestKeyRenameRule
		check  func(*testing.T, *relaycommon.RelayInfo)
	}{
		{
			name: "renaming stream away selects non-stream response handling",
			path: "/v1/chat/completions", format: types.RelayFormatOpenAI,
			body:  `{"model":"review-model","messages":[{"role":"user","content":"hello"}],"stream":true}`,
			rules: []dto.RequestKeyRenameRule{{Keys: map[string]string{"stream": "streaming"}}},
		},
		{
			name: "stream alias selects stream response handling",
			path: "/v1/chat/completions", format: types.RelayFormatOpenAI,
			body:  `{"model":"review-model","messages":[{"role":"user","content":"hello"}],"streaming":true}`,
			rules: []dto.RequestKeyRenameRule{{Keys: map[string]string{"streaming": "stream"}}},
		},
		{
			name: "rerank response uses renamed document fields and return option",
			path: "/v1/rerank", format: types.RelayFormatRerank,
			body: `{"model":"review-model","query":"question","documents":[{"legacy":"document"}],"returnDocs":true}`,
			rules: []dto.RequestKeyRenameRule{
				{Keys: map[string]string{"returnDocs": "return_documents"}},
				{Path: "documents.*", Keys: map[string]string{"legacy": "text"}},
			},
			check: func(t *testing.T, info *relaycommon.RelayInfo) {
				require.NotNil(t, info.RerankerInfo)
				assert.True(t, info.ReturnDocuments)
				assert.Equal(t, []any{map[string]any{"text": "document"}}, info.Documents)
			},
		},
		{
			name: "Responses tool aliases initialize current request tools",
			path: "/v1/responses", format: types.RelayFormatOpenAIResponses,
			body:  `{"model":"review-model","input":"hello","tool_list":[{"type":"web_search_preview"}]}`,
			rules: []dto.RequestKeyRenameRule{{Keys: map[string]string{"tool_list": "tools"}}},
			check: func(t *testing.T, info *relaycommon.RelayInfo) {
				require.NotNil(t, info.ResponsesUsageInfo)
				require.Contains(t, info.BuiltInTools, dto.BuildInToolWebSearchPreview)
				assert.Zero(t, info.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", tt.path, strings.NewReader(tt.body))
			ctx.Request.Header.Set("Content-Type", "application/json")
			common.SetContextKey(ctx, constant.ContextKeyOriginalModel, "review-model")
			common.SetContextKey(ctx, constant.ContextKeyChannelSetting, dto.ChannelSettings{RequestKeyRename: tt.rules})
			defer common.CleanupBodyStorage(ctx)
			request, err := helper.GetAndValidateRequest(ctx, tt.format)
			require.NoError(t, err)
			info, err := relaycommon.GenRelayInfo(ctx, tt.format, request, nil)
			require.NoError(t, err)
			originalStream := info.IsStream
			originalRerank, originalTools := info.RerankerInfo, info.ResponsesUsageInfo
			state := &requestKeyRenameState{}
			defer state.restore(ctx, info)

			require.Nil(t, state.apply(ctx, info, tt.format))
			require.NotEmpty(t, info.RequestKeyRenameAudit)
			assert.Equal(t, info.Request.IsStream(ctx.Request), info.IsStream)
			assert.Equal(t, info.IsStream, common.GetContextKeyBool(ctx, constant.ContextKeyIsStream))
			if tt.check != nil {
				tt.check(t, info)
			}

			state.restore(ctx, info)
			common.SetContextKey(ctx, constant.ContextKeyChannelSetting, dto.ChannelSettings{})
			require.Nil(t, state.apply(ctx, info, tt.format))
			assert.Same(t, request, info.Request)
			assert.Equal(t, originalStream, info.IsStream)
			assert.Equal(t, originalStream, common.GetContextKeyBool(ctx, constant.ContextKeyIsStream))
			assert.Equal(t, originalRerank, info.RerankerInfo)
			assert.Equal(t, originalTools, info.ResponsesUsageInfo)
			assert.Empty(t, info.RequestKeyRenameAudit)
			storage, err := common.GetBodyStorage(ctx)
			require.NoError(t, err)
			body, err := storage.Bytes()
			require.NoError(t, err)
			assert.Equal(t, tt.body, string(body))
		})
	}
}
