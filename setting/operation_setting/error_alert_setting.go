package operation_setting

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

const (
	DefaultErrorAlertTopN = 15
	MaxErrorAlertTopN     = 100
)

type ErrorAlertRule struct {
	ID         string   `json:"id"`
	Enabled    bool     `json:"enabled"`
	CustomerID string   `json:"customer_id"`
	TopN       int      `json:"top_n"`
	AlertToID  []int    `json:"alert_toid"`
	AlertURL   string   `json:"alert_url"`
	Link       string   `json:"link,omitempty"`
	AtUserIDs  []string `json:"at_user_ids,omitempty"`
}

type ErrorAlertSetting struct {
	Enabled bool             `json:"enabled"`
	Rules   []ErrorAlertRule `json:"rules"`
}

var errorAlertSetting = ErrorAlertSetting{}

func init() {
	config.GlobalConfig.Register("error_alert_setting", &errorAlertSetting)
}

func GetErrorAlertSetting() ErrorAlertSetting {
	setting := errorAlertSetting
	setting.Rules = append([]ErrorAlertRule(nil), errorAlertSetting.Rules...)
	for i := range setting.Rules {
		setting.Rules[i].AlertToID = append([]int(nil), setting.Rules[i].AlertToID...)
		setting.Rules[i].AtUserIDs = append([]string(nil), setting.Rules[i].AtUserIDs...)
		normalizeErrorAlertRule(&setting.Rules[i])
	}
	return setting
}

func ErrorAlertEnabled() bool {
	setting := GetErrorAlertSetting()
	if !setting.Enabled {
		return false
	}
	for _, rule := range setting.Rules {
		if rule.Enabled {
			return true
		}
	}
	return false
}

func ValidateErrorAlertRulesJSON(value string) error {
	var rules []ErrorAlertRule
	if err := common.UnmarshalJsonStr(value, &rules); err != nil {
		return fmt.Errorf("error alert rules must be a JSON array: %w", err)
	}
	seen := make(map[string]struct{}, len(rules))
	for i := range rules {
		rule := &rules[i]
		normalizeErrorAlertRule(rule)
		if rule.ID == "" {
			return fmt.Errorf("error alert rule #%d is missing id", i+1)
		}
		if _, ok := seen[rule.ID]; ok {
			return fmt.Errorf("duplicate error alert rule id: %s", rule.ID)
		}
		seen[rule.ID] = struct{}{}
		if rule.CustomerID == "" {
			return fmt.Errorf("error alert rule %s is missing customer_id", rule.ID)
		}
		if rule.Enabled {
			if len(rule.AlertToID) == 0 {
				return fmt.Errorf("error alert rule %s has no alert_toid", rule.ID)
			}
			if err := validateAlertURL(rule.AlertURL); err != nil {
				return fmt.Errorf("error alert rule %s: %w", rule.ID, err)
			}
		}
	}
	return nil
}

func normalizeErrorAlertRule(rule *ErrorAlertRule) {
	rule.ID = strings.TrimSpace(rule.ID)
	rule.CustomerID = strings.TrimSpace(rule.CustomerID)
	rule.AlertURL = strings.TrimSpace(rule.AlertURL)
	rule.Link = strings.TrimSpace(rule.Link)
	if rule.TopN <= 0 {
		rule.TopN = DefaultErrorAlertTopN
	}
	if rule.TopN > MaxErrorAlertTopN {
		rule.TopN = MaxErrorAlertTopN
	}
}

func validateAlertURL(rawURL string) error {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Host == "" {
		return errors.New("alert_url must be an absolute HTTP or HTTPS URL")
	}
	return nil
}
