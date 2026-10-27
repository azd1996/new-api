package model

import (
	"errors"
	"strings"
	"time"
)

// The auto-scheduling feature lets admins describe, per user group / level /
// token, how upstream traffic for a model should be preferred or excluded based
// on VictoriaMetrics (PromQL) metrics. The three tables below store its
// configuration. Their names carry an `x_` prefix so they never collide with a
// table that upstream new-api might add under the same concept later.
//
// Phase 1 only persists configuration and previews PromQL results; it does not
// yet influence real request routing.

// Scheduling rule-template result interpretation modes.
const (
	// ScheduleResultModeRank means the PromQL result rows form a priority order
	// (first = most preferred). Covers both "prefer" and "deprefer" — the
	// direction lives in the PromQL (sort_desc vs sort_asc).
	ScheduleResultModeRank = "rank"
	// ScheduleResultModeExclude means every entity in the result set should be
	// removed from scheduling candidates (threshold-style templates).
	ScheduleResultModeExclude = "exclude"
)

// Scheduling rule target types, in increasing specificity. More specific
// targets win when several rules match the same request (resolved at schedule
// time in a later phase).
const (
	ScheduleTargetGroup = "group"
	ScheduleTargetUser  = "user"
	ScheduleTargetToken = "token"
)

// XScheduleDatasource stores a VictoriaMetrics / Prometheus-compatible data
// source. BaseURL is the service root (e.g. ".../select/11/prometheus"); the
// query API path (/api/v1/query) is appended by the query service. Password is
// a basic-auth credential and is never serialized back to the frontend.
type XScheduleDatasource struct {
	Id             int    `json:"id" gorm:"primaryKey"`
	Name           string `json:"name" gorm:"type:varchar(128);not null"`
	BaseURL        string `json:"base_url" gorm:"type:varchar(512);not null"`
	Username       string `json:"username" gorm:"type:varchar(128)"`
	Password       string `json:"-" gorm:"type:varchar(512)"`
	Enabled        bool   `json:"enabled"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	CreatedTime    int64  `json:"created_time"`
	UpdatedTime    int64  `json:"updated_time"`
}

func (XScheduleDatasource) TableName() string { return "x_schedule_datasource" }

// XScheduleRuleTemplate is a named PromQL template with `__PLACEHOLDER__`
// tokens. ResultLabel is the metric label whose value identifies the scheduled
// entity in the query result; ResultMode says how to treat that result.
type XScheduleRuleTemplate struct {
	Id          int    `json:"id" gorm:"primaryKey"`
	Name        string `json:"name" gorm:"type:varchar(128);uniqueIndex;not null"`
	PromQL      string `json:"promql" gorm:"type:text;not null"`
	ResultLabel string `json:"result_label" gorm:"type:varchar(128);not null"`
	ResultMode  string `json:"result_mode" gorm:"type:varchar(16);not null"`
	Description string `json:"description" gorm:"type:varchar(512)"`
	CreatedTime int64  `json:"created_time"`
	UpdatedTime int64  `json:"updated_time"`
}

func (XScheduleRuleTemplate) TableName() string { return "x_schedule_rule_template" }

// XScheduleRule binds a target (group/user/token) and model to a rule template,
// supplying the placeholder values. Params is a JSON object mapping placeholder
// name (without the surrounding underscores, e.g. "MODEL") to its value. The
// fetched PromQL result is not persisted; it is queried live for preview.
type XScheduleRule struct {
	Id          int    `json:"id" gorm:"primaryKey"`
	TargetType  string `json:"target_type" gorm:"type:varchar(16);not null"`
	TargetValue string `json:"target_value" gorm:"type:varchar(255);not null"`
	Model       string `json:"model" gorm:"type:varchar(255);not null"`
	TemplateId  int    `json:"template_id" gorm:"index;not null"`
	Params      string `json:"params" gorm:"type:text"`
	Enabled     bool   `json:"enabled"`
	CreatedTime int64  `json:"created_time"`
	UpdatedTime int64  `json:"updated_time"`
}

func (XScheduleRule) TableName() string { return "x_schedule_rule" }

// ---- Datasource ----

func validateScheduleDatasource(ds *XScheduleDatasource) error {
	ds.Name = strings.TrimSpace(ds.Name)
	ds.BaseURL = strings.TrimSpace(ds.BaseURL)
	if ds.Name == "" {
		return errors.New("datasource name is required")
	}
	if ds.BaseURL == "" {
		return errors.New("datasource base_url is required")
	}
	if !strings.HasPrefix(ds.BaseURL, "http://") && !strings.HasPrefix(ds.BaseURL, "https://") {
		return errors.New("datasource base_url must start with http:// or https://")
	}
	if ds.TimeoutSeconds <= 0 {
		ds.TimeoutSeconds = 10
	}
	return nil
}

func GetAllScheduleDatasources() ([]*XScheduleDatasource, error) {
	var list []*XScheduleDatasource
	err := DB.Order("id asc").Find(&list).Error
	return list, err
}

func GetScheduleDatasourceById(id int) (*XScheduleDatasource, error) {
	var ds XScheduleDatasource
	if err := DB.First(&ds, id).Error; err != nil {
		return nil, err
	}
	return &ds, nil
}

func CreateScheduleDatasource(ds *XScheduleDatasource) error {
	if err := validateScheduleDatasource(ds); err != nil {
		return err
	}
	now := time.Now().Unix()
	ds.CreatedTime = now
	ds.UpdatedTime = now
	return DB.Create(ds).Error
}

func UpdateScheduleDatasource(ds *XScheduleDatasource) error {
	if err := validateScheduleDatasource(ds); err != nil {
		return err
	}
	ds.UpdatedTime = time.Now().Unix()
	return DB.Save(ds).Error
}

func DeleteScheduleDatasourceById(id int) error {
	return DB.Delete(&XScheduleDatasource{}, id).Error
}

// ---- Rule templates ----

func validateScheduleRuleTemplate(t *XScheduleRuleTemplate) error {
	t.Name = strings.TrimSpace(t.Name)
	t.PromQL = strings.TrimSpace(t.PromQL)
	t.ResultLabel = strings.TrimSpace(t.ResultLabel)
	t.ResultMode = strings.TrimSpace(t.ResultMode)
	if t.Name == "" {
		return errors.New("template name is required")
	}
	if t.PromQL == "" {
		return errors.New("template promql is required")
	}
	if t.ResultLabel == "" {
		return errors.New("template result_label is required")
	}
	if t.ResultMode != ScheduleResultModeRank && t.ResultMode != ScheduleResultModeExclude {
		return errors.New("template result_mode must be 'rank' or 'exclude'")
	}
	return nil
}

func GetAllScheduleRuleTemplates() ([]*XScheduleRuleTemplate, error) {
	var list []*XScheduleRuleTemplate
	err := DB.Order("id asc").Find(&list).Error
	return list, err
}

func GetScheduleRuleTemplateById(id int) (*XScheduleRuleTemplate, error) {
	var t XScheduleRuleTemplate
	if err := DB.First(&t, id).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

func CreateScheduleRuleTemplate(t *XScheduleRuleTemplate) error {
	if err := validateScheduleRuleTemplate(t); err != nil {
		return err
	}
	now := time.Now().Unix()
	t.CreatedTime = now
	t.UpdatedTime = now
	return DB.Create(t).Error
}

func UpdateScheduleRuleTemplate(t *XScheduleRuleTemplate) error {
	if err := validateScheduleRuleTemplate(t); err != nil {
		return err
	}
	t.UpdatedTime = time.Now().Unix()
	return DB.Save(t).Error
}

func DeleteScheduleRuleTemplateById(id int) error {
	return DB.Delete(&XScheduleRuleTemplate{}, id).Error
}

// ---- Rules ----

func validateScheduleRule(r *XScheduleRule) error {
	r.TargetType = strings.TrimSpace(r.TargetType)
	r.TargetValue = strings.TrimSpace(r.TargetValue)
	r.Model = strings.TrimSpace(r.Model)
	switch r.TargetType {
	case ScheduleTargetGroup, ScheduleTargetUser, ScheduleTargetToken:
	default:
		return errors.New("rule target_type must be 'group', 'user' or 'token'")
	}
	if r.TargetValue == "" {
		return errors.New("rule target_value is required")
	}
	if r.Model == "" {
		return errors.New("rule model is required")
	}
	if r.TemplateId <= 0 {
		return errors.New("rule template_id is required")
	}
	if _, err := GetScheduleRuleTemplateById(r.TemplateId); err != nil {
		return errors.New("rule references a non-existent template")
	}
	if strings.TrimSpace(r.Params) == "" {
		r.Params = "{}"
	}
	return nil
}

func GetAllScheduleRules() ([]*XScheduleRule, error) {
	var list []*XScheduleRule
	err := DB.Order("id asc").Find(&list).Error
	return list, err
}

func GetScheduleRuleById(id int) (*XScheduleRule, error) {
	var r XScheduleRule
	if err := DB.First(&r, id).Error; err != nil {
		return nil, err
	}
	return &r, nil
}

func CreateScheduleRule(r *XScheduleRule) error {
	if err := validateScheduleRule(r); err != nil {
		return err
	}
	now := time.Now().Unix()
	r.CreatedTime = now
	r.UpdatedTime = now
	return DB.Create(r).Error
}

func UpdateScheduleRule(r *XScheduleRule) error {
	if err := validateScheduleRule(r); err != nil {
		return err
	}
	r.UpdatedTime = time.Now().Unix()
	return DB.Save(r).Error
}

func DeleteScheduleRuleById(id int) error {
	return DB.Delete(&XScheduleRule{}, id).Error
}
