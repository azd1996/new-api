package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/ruliu"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

type BalanceAlertTaskPayload struct {
	SlotStart int64 `json:"slot_start"`
	SlotEnd   int64 `json:"slot_end"`
}

type BalanceAlertRuleResult struct {
	RuleID    string `json:"rule_id"`
	Group     string `json:"group,omitempty"`
	UserCount int    `json:"user_count"`
	Notified  []int  `json:"notified,omitempty"`
	Sent      bool   `json:"sent"`
	Skip      string `json:"skip,omitempty"`
	Error     string `json:"error,omitempty"`
}

type BalanceAlertTaskResult struct {
	SlotStart int64                    `json:"slot_start"`
	SlotEnd   int64                    `json:"slot_end"`
	Items     []BalanceAlertRuleResult `json:"items"`
}

// NaturalHourSlot aligns a run to the wall-clock hour it belongs to.
func NaturalHourSlot(now time.Time) (slotStart, slotEnd time.Time) {
	slotStart = time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location())
	return slotStart, slotStart.Add(time.Hour)
}

func BalanceAlertScheduleKey(slotStart time.Time) string {
	return fmt.Sprintf("balance_alert_%d", slotStart.Unix())
}

// RunBalanceAlertTask reports users whose wallet balance fell below a rule's
// threshold to Ruliu. Each rule sends at most one message per run, listing the
// lowest balances, and a user already reported inside the quiet window is
// skipped so a low balance does not alert every hour.
func RunBalanceAlertTask(ctx context.Context, payload BalanceAlertTaskPayload) (BalanceAlertTaskResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	setting := operation_setting.GetBalanceAlertSetting()
	result := BalanceAlertTaskResult{
		SlotStart: payload.SlotStart,
		SlotEnd:   payload.SlotEnd,
		Items:     make([]BalanceAlertRuleResult, 0, len(setting.Rules)),
	}
	if !setting.Enabled {
		return result, nil
	}

	notifier := ruliu.NewClient(nil)
	quietBefore := common.GetTimestamp() - int64(operation_setting.BalanceAlertQuietWindow().Seconds())
	for _, rule := range setting.Rules {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if !rule.Enabled {
			continue
		}

		item := BalanceAlertRuleResult{RuleID: rule.ID, Group: rule.Group}
		threshold := int(rule.ThresholdAmount * common.QuotaPerUnit)
		if threshold <= 0 {
			item.Skip = "invalid_threshold"
			result.Items = append(result.Items, item)
			continue
		}
		users, err := model.ListUsersBelowBalance(rule.ID, threshold, rule.Group, quietBefore, rule.MaxUsers)
		if err != nil {
			item.Error = err.Error()
			result.Items = append(result.Items, item)
			continue
		}
		item.UserCount = len(users)
		if len(users) == 0 {
			item.Skip = "no_low_balance_users"
			result.Items = append(result.Items, item)
			continue
		}

		notified := make([]int, 0, len(users))
		for _, user := range users {
			notified = append(notified, user.Id)
		}
		item.Notified = notified
		message := buildBalanceAlertMessage(rule, threshold, users)
		if err := notifier.Send(ctx, ruliu.AlertParams{
			AlertToID: rule.AlertToID,
			AlertURL:  rule.AlertURL,
			Msg:       message,
			Link:      rule.Link,
			AtUserIDs: rule.AtUserIDs,
		}); err != nil {
			item.Error = err.Error()
			result.Items = append(result.Items, item)
			continue
		}
		// Mark only after a successful send, so a failed message is retried in
		// the next run instead of being swallowed by the quiet window.
		if err := model.MarkBalanceAlerted(rule.ID, notified); err != nil {
			logger.LogWarn(ctx, common.LogText("failed to mark balance alert state for rule %s: %s", rule.ID, err.Error()))
		}
		item.Sent = true
		result.Items = append(result.Items, item)
	}
	return result, nil
}

// buildBalanceAlertMessage lists each reported user with the balance that
// triggered the alert. User names are gateway-controlled, not client input.
func buildBalanceAlertMessage(rule operation_setting.BalanceAlertRule, threshold int, users []*model.User) string {
	var builder strings.Builder
	builder.WriteString("【余额不足提醒】\n")
	if rule.Group != "" {
		builder.WriteString(fmt.Sprintf("分组: %s\n", rule.Group))
	}
	builder.WriteString(fmt.Sprintf("阈值: %s\n", logger.FormatQuota(threshold)))
	builder.WriteString(fmt.Sprintf("低于阈值的用户数: %d\n", len(users)))
	for _, user := range users {
		builder.WriteString(fmt.Sprintf("- %s (#%d) 余额 %s\n", user.Username, user.Id, logger.FormatQuota(user.Quota)))
	}
	return strings.TrimRight(builder.String(), "\n")
}
