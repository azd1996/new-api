package controller

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func GetAllCustomers(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	customers, total, err := model.GetAllCustomers(pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(customers)
	common.ApiSuccess(c, pageInfo)
}

func SearchCustomers(c *gin.Context) {
	keyword := c.Query("keyword")
	pageInfo := common.GetPageQuery(c)
	customers, total, err := model.SearchCustomers(keyword, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(customers)
	common.ApiSuccess(c, pageInfo)
}

func GetCustomer(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	customer, err := model.GetCustomerById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": customer})
}

func AddCustomer(c *gin.Context) {
	customer := model.Customer{}
	if err := c.ShouldBindJSON(&customer); err != nil {
		common.ApiError(c, err)
		return
	}
	customer.Id = 0
	if err := customer.Insert(); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "customer.create", map[string]interface{}{
		"customer_id": customer.CustomerId,
		"name":        customer.Name,
	})
	common.ApiSuccess(c, customer)
}

func UpdateCustomer(c *gin.Context) {
	customer := model.Customer{}
	if err := c.ShouldBindJSON(&customer); err != nil {
		common.ApiError(c, err)
		return
	}
	if customer.Id == 0 {
		common.ApiError(c, errors.New("id 为空！"))
		return
	}
	if err := customer.Update(); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "customer.update", map[string]interface{}{
		"customer_id": customer.CustomerId,
		"name":        customer.Name,
	})
	common.ApiSuccess(c, customer)
}

func DeleteCustomer(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.DeleteCustomerById(id); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "customer.delete", map[string]interface{}{"id": id})
	common.ApiSuccess(c, nil)
}

const (
	maxSessionIdentityTestItems    = 20
	maxSessionIdentityTestBodySize = 2 * 1024 * 1024
)

type customerSessionIdentityTestRequest struct {
	CustomerID string                            `json:"customer_id"`
	Config     *common.SessionIdentityConfig     `json:"config"`
	Items      []customerSessionIdentityTestItem `json:"items"`
	ExpectSame bool                              `json:"expect_same"`
}

type customerSessionIdentityTestItem struct {
	Body      string            `json:"body"`
	Headers   map[string]string `json:"headers"`
	APIKey    string            `json:"api_key"`
	ClientIP  string            `json:"client_ip"`
	UserAgent string            `json:"user_agent"`
}

type customerSessionIdentityTestResult struct {
	Index     int    `json:"index"`
	OK        bool   `json:"ok"`
	SessionID string `json:"session_id"`
	RawValue  string `json:"raw_value"`
	Source    string `json:"source"`
	Error     string `json:"error,omitempty"`
}

type customerSessionIdentityTestSummary struct {
	Count              int            `json:"count"`
	OKCount            int            `json:"ok_count"`
	DistinctSessionIDs int            `json:"distinct_session_ids"`
	SessionIDs         []string       `json:"session_ids"`
	Stable             bool           `json:"stable"`
	ExpectSameMet      bool           `json:"expect_same_met"`
	SourcesUsed        map[string]int `json:"sources_used"`
}

// TestCustomerSessionIdentity runs the session identity pipeline against one
// or more pasted request bodies. It uses the exact production resolver, so the
// output is what the gateway would inject for each request. Request bodies are
// never persisted or logged.
func TestCustomerSessionIdentity(c *gin.Context) {
	var req customerSessionIdentityTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if len(req.Items) == 0 {
		common.ApiError(c, errors.New("items 不能为空"))
		return
	}
	if len(req.Items) > maxSessionIdentityTestItems {
		common.ApiError(c, fmt.Errorf("items 数量不能超过 %d", maxSessionIdentityTestItems))
		return
	}
	config := req.Config
	if config == nil {
		if req.CustomerID == "" {
			common.ApiError(c, errors.New("customer_id 与 config 至少提供一个"))
			return
		}
		config = model.GetCustomerSessionIdentity(req.CustomerID)
		if config == nil {
			common.ApiError(c, fmt.Errorf("客户 %s 未配置会话身份", req.CustomerID))
			return
		}
	}

	results := make([]customerSessionIdentityTestResult, 0, len(req.Items))
	sessionIDs := []string{}
	sourcesUsed := map[string]int{}
	for i, item := range req.Items {
		result := customerSessionIdentityTestResult{Index: i}
		if len(item.Body) > maxSessionIdentityTestBodySize {
			result.Error = "请求体超过 2MB 限制"
			results = append(results, result)
			continue
		}
		if !gjson.Valid(item.Body) {
			result.Error = "请求体不是合法 JSON"
			results = append(results, result)
			continue
		}
		headers := http.Header{}
		for key, value := range item.Headers {
			headers.Set(key, value)
		}
		if item.UserAgent != "" && headers.Get("User-Agent") == "" {
			headers.Set("User-Agent", item.UserAgent)
		}
		resolution := common.ResolveSessionIdentity(
			[]byte(item.Body),
			headers.Get("x-claude-code-session-id"),
			item.ClientIP,
			item.UserAgent,
			item.APIKey,
			headers,
			config,
		)
		result.SessionID = resolution.SessionID
		result.RawValue = resolution.RawValue
		result.Source = resolution.Source
		result.OK = resolution.Source != "" && resolution.Source != "none"
		if !result.OK {
			result.Error = resolution.Reason
			if result.Error == "" {
				if modelName := gjson.Get(item.Body, "model").String(); config != nil && !config.Matches(modelName) {
					result.Error = "model_not_matched"
				} else {
					result.Error = "custom_source_empty"
				}
			}
		}
		if result.OK {
			sourcesUsed[resolution.Source]++
			if resolution.SessionID != "" && !slices.Contains(sessionIDs, resolution.SessionID) {
				sessionIDs = append(sessionIDs, resolution.SessionID)
			}
		}
		results = append(results, result)
	}

	okCount := 0
	for _, result := range results {
		if result.OK {
			okCount++
		}
	}
	stable := okCount == len(results) && len(sessionIDs) <= 1
	summary := customerSessionIdentityTestSummary{
		Count:              len(results),
		OKCount:            okCount,
		DistinctSessionIDs: len(sessionIDs),
		SessionIDs:         sessionIDs,
		Stable:             stable,
		ExpectSameMet:      !req.ExpectSame || stable,
		SourcesUsed:        sourcesUsed,
	}
	common.ApiSuccess(c, gin.H{"results": results, "summary": summary})
}

const (
	maxCacheOptimizationTestItems    = 20
	maxCacheOptimizationTestBodySize = 2 * 1024 * 1024

	cacheOptimizationTestModeCache     = "cache_optimization"
	cacheOptimizationTestModeFullRelay = "full_relay"
)

type customerCacheOptimizationTestRequest struct {
	CustomerID string                                 `json:"customer_id"`
	Config     *model.CustomerClaudeCacheOptimization `json:"config"`
	Group      string                                 `json:"group"`
	Mode       string                                 `json:"mode"`
	Items      []customerCacheOptimizationTestItem    `json:"items"`
}

type customerCacheOptimizationTestItem struct {
	Body    string            `json:"body"`
	Headers map[string]string `json:"headers,omitempty"`
}

type customerCacheOptimizationTestResult struct {
	Index        int               `json:"index"`
	OK           bool              `json:"ok"`
	Body         string            `json:"body,omitempty"`
	Model        string            `json:"model,omitempty"`
	Error        string            `json:"error,omitempty"`
	EnabledRules []string          `json:"enabled_rules,omitempty"`
	ChangedSteps []string          `json:"changed_steps,omitempty"`
	SkippedSteps []string          `json:"skipped_steps,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	// Full-relay preview fields.
	ChannelID     int    `json:"channel_id,omitempty"`
	ChannelName   string `json:"channel_name,omitempty"`
	ChannelType   int    `json:"channel_type,omitempty"`
	Group         string `json:"group,omitempty"`
	OriginModel   string `json:"origin_model,omitempty"`
	UpstreamModel string `json:"upstream_model,omitempty"`
	ModelMapped   bool   `json:"model_mapped,omitempty"`
	PassThrough   bool   `json:"pass_through,omitempty"`
	URL           string `json:"url,omitempty"`
	Method        string `json:"method,omitempty"`
}

// TestCustomerCacheOptimization runs the Claude cache-optimization pipeline
// against one or more pasted request bodies. mode selects between the
// cache-optimization-only preview and the full relay preview; neither sends
// anything upstream. Nothing is persisted or logged.
func TestCustomerCacheOptimization(c *gin.Context) {
	var req customerCacheOptimizationTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if len(req.Items) == 0 {
		common.ApiError(c, errors.New("items 不能为空"))
		return
	}
	if len(req.Items) > maxCacheOptimizationTestItems {
		common.ApiError(c, fmt.Errorf("items 数量不能超过 %d", maxCacheOptimizationTestItems))
		return
	}
	mode := cacheOptimizationTestModeCache
	switch req.Mode {
	case "":
	case cacheOptimizationTestModeCache, cacheOptimizationTestModeFullRelay:
		mode = req.Mode
	default:
		common.ApiError(c, fmt.Errorf("mode 仅支持 %s 或 %s", cacheOptimizationTestModeCache, cacheOptimizationTestModeFullRelay))
		return
	}
	group := strings.TrimSpace(req.Group)
	if group == "" {
		group = "default"
	}
	config := req.Config
	if config == nil && req.CustomerID != "" {
		config = model.GetCustomerClaudeCacheOptimization(req.CustomerID)
	}

	results := make([]customerCacheOptimizationTestResult, 0, len(req.Items))
	for i, item := range req.Items {
		result := customerCacheOptimizationTestResult{Index: i}
		if len(item.Body) > maxCacheOptimizationTestBodySize {
			result.Error = "请求体超过 2MB 限制"
			results = append(results, result)
			continue
		}
		if !gjson.Valid(item.Body) {
			result.Error = "请求体不是合法 JSON"
			results = append(results, result)
			continue
		}
		if mode == cacheOptimizationTestModeFullRelay {
			results = append(results, runFullRelayCacheOptimizationTest(item, group, config))
			continue
		}
		modelName := gjson.Get(item.Body, "model").String()
		result.Model = modelName
		policy := relay.ResolveClaudeCacheOptimizationForPreview(config, group, modelName)
		preview, err := relay.PreviewClaudeCacheOptimization([]byte(item.Body), policy)
		if err != nil {
			result.Error = err.Error()
			results = append(results, result)
			continue
		}
		result.OK = true
		result.Body = string(preview.Body)
		result.EnabledRules = preview.EnabledRules
		result.ChangedSteps = preview.ChangedSteps
		result.SkippedSteps = preview.SkippedSteps
		result.Headers = preview.Headers
		results = append(results, result)
	}
	common.ApiSuccess(c, gin.H{"results": results, "mode": mode})
}

// runFullRelayCacheOptimizationTest builds a fresh relay context for the
// pasted body and runs the production Claude relay pipeline in dry-run mode.
func runFullRelayCacheOptimizationTest(item customerCacheOptimizationTestItem, group string, config *model.CustomerClaudeCacheOptimization) customerCacheOptimizationTestResult {
	result := customerCacheOptimizationTestResult{}
	itemCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(item.Body))
	request.Header.Set("Content-Type", "application/json")
	for key, value := range item.Headers {
		request.Header.Set(key, value)
	}
	itemCtx.Request = request
	modelName := gjson.Get(item.Body, "model").String()
	result.Model = modelName
	common.SetContextKey(itemCtx, constant.ContextKeyOriginalModel, modelName)
	common.SetContextKey(itemCtx, constant.ContextKeyUsingGroup, group)
	common.SetContextKey(itemCtx, constant.ContextKeyUserGroup, group)
	common.SetContextKey(itemCtx, constant.ContextKeyTokenGroup, group)
	common.SetContextKey(itemCtx, constant.ContextKeyRequestStartTime, time.Now())
	common.SetContextKey(itemCtx, common.RequestIdKey, common.NewRequestId())
	if config != nil {
		common.SetContextKey(itemCtx, constant.ContextKeyClaudeCacheOptimizationPreviewOverride, config)
	}
	preview, apiErr := relay.PreviewClaudeRelay(itemCtx)
	if apiErr != nil {
		result.Error = apiErr.Error()
		return result
	}
	result.OK = true
	result.Body = preview.Body
	result.ChannelID = preview.ChannelID
	result.ChannelName = preview.ChannelName
	result.ChannelType = preview.ChannelType
	result.Group = preview.Group
	result.OriginModel = preview.OriginModel
	result.UpstreamModel = preview.UpstreamModel
	result.ModelMapped = preview.ModelMapped
	result.PassThrough = preview.PassThrough
	result.URL = preview.URL
	result.Method = preview.Method
	result.Headers = preview.Headers
	result.EnabledRules = preview.EnabledRules
	result.SkippedSteps = preview.SkippedRules
	return result
}
