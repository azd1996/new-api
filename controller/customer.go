package controller

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

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
