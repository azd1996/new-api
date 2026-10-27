package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// Auto-scheduling admin APIs. These manage the VM data source, PromQL rule
// templates, and the rules that bind targets/models to templates. Phase 1 only
// persists configuration and previews results; it does not affect routing.

// scheduleDatasourceResponse hides the basic-auth password; the frontend only
// needs to know whether one is set.
type scheduleDatasourceResponse struct {
	Id             int    `json:"id"`
	Name           string `json:"name"`
	BaseURL        string `json:"base_url"`
	Username       string `json:"username"`
	PasswordSet    bool   `json:"password_set"`
	Enabled        bool   `json:"enabled"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	CreatedTime    int64  `json:"created_time"`
	UpdatedTime    int64  `json:"updated_time"`
}

func toScheduleDatasourceResponse(ds *model.XScheduleDatasource) *scheduleDatasourceResponse {
	return &scheduleDatasourceResponse{
		Id:             ds.Id,
		Name:           ds.Name,
		BaseURL:        ds.BaseURL,
		Username:       ds.Username,
		PasswordSet:    ds.Password != "",
		Enabled:        ds.Enabled,
		TimeoutSeconds: ds.TimeoutSeconds,
		CreatedTime:    ds.CreatedTime,
		UpdatedTime:    ds.UpdatedTime,
	}
}

// ---- Datasource ----

func GetScheduleDatasources(c *gin.Context) {
	list, err := model.GetAllScheduleDatasources()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	resp := make([]*scheduleDatasourceResponse, len(list))
	for i, ds := range list {
		resp[i] = toScheduleDatasourceResponse(ds)
	}
	common.ApiSuccess(c, resp)
}

type scheduleDatasourceRequest struct {
	Name           string `json:"name"`
	BaseURL        string `json:"base_url"`
	Username       string `json:"username"`
	Password       string `json:"password"` // on update, empty means keep existing
	Enabled        bool   `json:"enabled"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

func CreateScheduleDatasource(c *gin.Context) {
	var req scheduleDatasourceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "无效的请求参数: "+err.Error())
		return
	}
	ds := &model.XScheduleDatasource{
		Name:           req.Name,
		BaseURL:        req.BaseURL,
		Username:       req.Username,
		Password:       req.Password,
		Enabled:        req.Enabled,
		TimeoutSeconds: req.TimeoutSeconds,
	}
	if err := model.CreateScheduleDatasource(ds); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "创建成功", "data": toScheduleDatasourceResponse(ds)})
}

func UpdateScheduleDatasource(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "无效的 ID")
		return
	}
	var req scheduleDatasourceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "无效的请求参数: "+err.Error())
		return
	}
	ds, err := model.GetScheduleDatasourceById(id)
	if err != nil {
		common.ApiErrorMsg(c, "未找到该数据源")
		return
	}
	ds.Name = req.Name
	ds.BaseURL = req.BaseURL
	ds.Username = req.Username
	ds.Enabled = req.Enabled
	ds.TimeoutSeconds = req.TimeoutSeconds
	if req.Password != "" {
		ds.Password = req.Password
	}
	if err := model.UpdateScheduleDatasource(ds); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "更新成功", "data": toScheduleDatasourceResponse(ds)})
}

func DeleteScheduleDatasource(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "无效的 ID")
		return
	}
	if err := model.DeleteScheduleDatasourceById(id); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "删除成功"})
}

// ---- Rule templates ----

func GetScheduleRuleTemplates(c *gin.Context) {
	list, err := model.GetAllScheduleRuleTemplates()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, list)
}

type scheduleRuleTemplateRequest struct {
	Name        string `json:"name"`
	PromQL      string `json:"promql"`
	ResultLabel string `json:"result_label"`
	ResultMode  string `json:"result_mode"`
	Description string `json:"description"`
}

func CreateScheduleRuleTemplate(c *gin.Context) {
	var req scheduleRuleTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "无效的请求参数: "+err.Error())
		return
	}
	t := &model.XScheduleRuleTemplate{
		Name:        req.Name,
		PromQL:      req.PromQL,
		ResultLabel: req.ResultLabel,
		ResultMode:  req.ResultMode,
		Description: req.Description,
	}
	if err := model.CreateScheduleRuleTemplate(t); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "创建成功", "data": t})
}

func UpdateScheduleRuleTemplate(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "无效的 ID")
		return
	}
	var req scheduleRuleTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "无效的请求参数: "+err.Error())
		return
	}
	t, err := model.GetScheduleRuleTemplateById(id)
	if err != nil {
		common.ApiErrorMsg(c, "未找到该规则模板")
		return
	}
	t.Name = req.Name
	t.PromQL = req.PromQL
	t.ResultLabel = req.ResultLabel
	t.ResultMode = req.ResultMode
	t.Description = req.Description
	if err := model.UpdateScheduleRuleTemplate(t); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "更新成功", "data": t})
}

func DeleteScheduleRuleTemplate(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "无效的 ID")
		return
	}
	if err := model.DeleteScheduleRuleTemplateById(id); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "删除成功"})
}

// ---- Rules ----

func GetScheduleRules(c *gin.Context) {
	list, err := model.GetAllScheduleRules()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, list)
}

type scheduleRuleRequest struct {
	TargetType  string `json:"target_type"`
	TargetValue string `json:"target_value"`
	Model       string `json:"model"`
	TemplateId  int    `json:"template_id"`
	Params      string `json:"params"`
	Enabled     bool   `json:"enabled"`
}

func CreateScheduleRule(c *gin.Context) {
	var req scheduleRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "无效的请求参数: "+err.Error())
		return
	}
	r := &model.XScheduleRule{
		TargetType:  req.TargetType,
		TargetValue: req.TargetValue,
		Model:       req.Model,
		TemplateId:  req.TemplateId,
		Params:      req.Params,
		Enabled:     req.Enabled,
	}
	if err := model.CreateScheduleRule(r); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "创建成功", "data": r})
}

func UpdateScheduleRule(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "无效的 ID")
		return
	}
	var req scheduleRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "无效的请求参数: "+err.Error())
		return
	}
	r, err := model.GetScheduleRuleById(id)
	if err != nil {
		common.ApiErrorMsg(c, "未找到该规则")
		return
	}
	r.TargetType = req.TargetType
	r.TargetValue = req.TargetValue
	r.Model = req.Model
	r.TemplateId = req.TemplateId
	r.Params = req.Params
	r.Enabled = req.Enabled
	if err := model.UpdateScheduleRule(r); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "更新成功", "data": r})
}

func DeleteScheduleRule(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "无效的 ID")
		return
	}
	if err := model.DeleteScheduleRuleById(id); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "删除成功"})
}

// ---- VM query: test & preview ----

type scheduleDatasourceTestRequest struct {
	Id             int    `json:"id"`
	BaseURL        string `json:"base_url"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

// TestScheduleDatasource verifies connectivity/auth for a data source. It can
// test an unsaved config from the request body; when an id is given and no
// password is supplied, the stored password is reused.
func TestScheduleDatasource(c *gin.Context) {
	var req scheduleDatasourceTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "无效的请求参数: "+err.Error())
		return
	}
	ds := &model.XScheduleDatasource{
		BaseURL:        req.BaseURL,
		Username:       req.Username,
		Password:       req.Password,
		TimeoutSeconds: req.TimeoutSeconds,
	}
	if req.Id > 0 {
		stored, err := model.GetScheduleDatasourceById(req.Id)
		if err != nil {
			common.ApiErrorMsg(c, "未找到该数据源")
			return
		}
		if ds.BaseURL == "" {
			ds.BaseURL = stored.BaseURL
		}
		if ds.Username == "" {
			ds.Username = stored.Username
		}
		if ds.Password == "" {
			ds.Password = stored.Password
		}
	}
	if err := service.TestScheduleDatasource(ds); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "连接成功"})
}

type schedulePreviewRequest struct {
	DatasourceId int               `json:"datasource_id"`
	RuleId       int               `json:"rule_id"`
	TemplateId   int               `json:"template_id"`
	PromQL       string            `json:"promql"`
	ResultLabel  string            `json:"result_label"`
	Params       map[string]string `json:"params"`
}

// PreviewSchedule resolves a template/rule's PromQL against a data source and
// returns the reduced (label, value) rows for the admin UI.
func PreviewSchedule(c *gin.Context) {
	var req schedulePreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "无效的请求参数: "+err.Error())
		return
	}

	ds, err := resolvePreviewDatasource(req.DatasourceId)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}

	promql := req.PromQL
	resultLabel := req.ResultLabel
	params := req.Params
	if params == nil {
		params = map[string]string{}
	}

	if req.RuleId > 0 {
		rule, err := model.GetScheduleRuleById(req.RuleId)
		if err != nil {
			common.ApiErrorMsg(c, "未找到该规则")
			return
		}
		tpl, err := model.GetScheduleRuleTemplateById(rule.TemplateId)
		if err != nil {
			common.ApiErrorMsg(c, "规则引用的模板不存在")
			return
		}
		promql = tpl.PromQL
		resultLabel = tpl.ResultLabel
		ruleParams := map[string]string{}
		if rule.Params != "" {
			_ = common.UnmarshalJsonStr(rule.Params, &ruleParams)
		}
		params = ruleParams
	} else if req.TemplateId > 0 {
		tpl, err := model.GetScheduleRuleTemplateById(req.TemplateId)
		if err != nil {
			common.ApiErrorMsg(c, "未找到该规则模板")
			return
		}
		promql = tpl.PromQL
		if resultLabel == "" {
			resultLabel = tpl.ResultLabel
		}
	}

	if promql == "" {
		common.ApiErrorMsg(c, "缺少 PromQL，请指定 rule_id、template_id 或 promql")
		return
	}

	result, err := service.PreviewSchedulePromQL(ds, promql, resultLabel, params)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

// resolvePreviewDatasource picks the requested data source, or falls back to the
// first enabled one when none is specified.
func resolvePreviewDatasource(id int) (*model.XScheduleDatasource, error) {
	if id > 0 {
		ds, err := model.GetScheduleDatasourceById(id)
		if err != nil {
			return nil, errors.New("未找到指定的数据源")
		}
		return ds, nil
	}
	list, err := model.GetAllScheduleDatasources()
	if err != nil {
		return nil, err
	}
	for _, ds := range list {
		if ds.Enabled {
			return ds, nil
		}
	}
	return nil, errors.New("请先配置并启用一个 VM 数据源")
}
