package operation_setting

import (
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

const (
	// DefaultBalanceAlertMaxUsers caps how many users one alert message lists.
	DefaultBalanceAlertMaxUsers = 20
	MaxBalanceAlertMaxUsers     = 100
	// balanceAlertQuietWindow is the per-user cooldown: a user already reported
	// inside the window is left out of the next message.
	balanceAlertQuietWindow = 24 * time.Hour
)

type BalanceAlertRule struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
	// Empty group matches every user group.
	Group string `json:"group"`
	// ThresholdAmount is the balance floor in the site's display currency unit
	// (USD by default); a user at or above it is never reported.
	ThresholdAmount float64  `json:"threshold_amount"`
	MaxUsers        int      `json:"max_users"`
	AlertToID       []int    `json:"alert_toid"`
	AlertURL        string   `json:"alert_url"`
	Link            string   `json:"link,omitempty"`
	AtUserIDs       []string `json:"at_user_ids,omitempty"`
}

type BalanceAlertSetting struct {
	Enabled bool               `json:"enabled"`
	Rules   []BalanceAlertRule `json:"rules"`
}

var balanceAlertSetting = BalanceAlertSetting{}

func init() {
	config.GlobalConfig.Register("balance_alert_setting", &balanceAlertSetting)
}

func GetBalanceAlertSetting() BalanceAlertSetting {
	setting := balanceAlertSetting
	setting.Rules = append([]BalanceAlertRule(nil), balanceAlertSetting.Rules...)
	for i := range setting.Rules {
		setting.Rules[i].AlertToID = append([]int(nil), setting.Rules[i].AlertToID...)
		setting.Rules[i].AtUserIDs = append([]string(nil), setting.Rules[i].AtUserIDs...)
		normalizeBalanceAlertRule(&setting.Rules[i])
	}
	return setting
}

func BalanceAlertEnabled() bool {
	setting := GetBalanceAlertSetting()
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

// BalanceAlertQuietWindow is the cooldown applied per user and rule.
func BalanceAlertQuietWindow() time.Duration {
	return balanceAlertQuietWindow
}

// ValidateBalanceAlertRulesJSON checks the persisted rules array before it is
// stored, so an unusable rule cannot reach the scheduled task.
func ValidateBalanceAlertRulesJSON(value string) error {
	var rules []BalanceAlertRule
	if err := common.UnmarshalJsonStr(value, &rules); err != nil {
		return fmt.Errorf("balance alert rules must be a JSON array: %w", err)
	}
	seen := make(map[string]struct{}, len(rules))
	for i := range rules {
		rule := &rules[i]
		normalizeBalanceAlertRule(rule)
		if rule.ID == "" {
			return fmt.Errorf("balance alert rule #%d is missing id", i+1)
		}
		if _, ok := seen[rule.ID]; ok {
			return fmt.Errorf("duplicate balance alert rule id: %s", rule.ID)
		}
		seen[rule.ID] = struct{}{}
		if rule.Enabled {
			if rule.ThresholdAmount <= 0 {
				return fmt.Errorf("balance alert rule %s needs a positive threshold", rule.ID)
			}
			if len(rule.AlertToID) == 0 {
				return fmt.Errorf("balance alert rule %s has no alert_toid", rule.ID)
			}
			if err := validateAlertURL(rule.AlertURL); err != nil {
				return fmt.Errorf("balance alert rule %s: %w", rule.ID, err)
			}
		}
	}
	return nil
}

func normalizeBalanceAlertRule(rule *BalanceAlertRule) {
	rule.ID = strings.TrimSpace(rule.ID)
	rule.Group = strings.TrimSpace(rule.Group)
	rule.AlertURL = strings.TrimSpace(rule.AlertURL)
	rule.Link = strings.TrimSpace(rule.Link)
	if rule.ThresholdAmount < 0 {
		rule.ThresholdAmount = 0
	}
	if rule.MaxUsers <= 0 {
		rule.MaxUsers = DefaultBalanceAlertMaxUsers
	}
	if rule.MaxUsers > MaxBalanceAlertMaxUsers {
		rule.MaxUsers = MaxBalanceAlertMaxUsers
	}
}
