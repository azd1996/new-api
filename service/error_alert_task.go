package service

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/ruliu"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

const errorAlertSlotDuration = 15 * time.Minute

type ErrorAlertTaskPayload struct {
	SlotStart int64 `json:"slot_start"`
	SlotEnd   int64 `json:"slot_end"`
}

type ErrorAlertRuleResult struct {
	RuleID      string `json:"rule_id"`
	CustomerID  string `json:"customer_id"`
	ErrorCount  int64  `json:"error_count"`
	MessageSent bool   `json:"message_sent"`
	SkipReason  string `json:"skip_reason,omitempty"`
	Error       string `json:"error,omitempty"`
}

type ErrorAlertTaskResult struct {
	SlotStart int64                  `json:"slot_start"`
	SlotEnd   int64                  `json:"slot_end"`
	Items     []ErrorAlertRuleResult `json:"items"`
}

func NaturalQuarterHourSlot(now time.Time) (slotStart, slotEnd time.Time) {
	slotEnd = now.Truncate(errorAlertSlotDuration)
	slotStart = slotEnd.Add(-errorAlertSlotDuration)
	return slotStart, slotEnd
}

func ErrorAlertScheduleKey(slotEnd time.Time) string {
	return fmt.Sprintf("%s:%d", model.SystemTaskTypeErrorAlert, slotEnd.Unix())
}

func RunErrorAlertTask(ctx context.Context, payload ErrorAlertTaskPayload) (ErrorAlertTaskResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if payload.SlotEnd <= payload.SlotStart {
		return ErrorAlertTaskResult{}, fmt.Errorf("invalid error alert time slot")
	}

	setting := operation_setting.GetErrorAlertSetting()
	result := ErrorAlertTaskResult{
		SlotStart: payload.SlotStart,
		SlotEnd:   payload.SlotEnd,
		Items:     make([]ErrorAlertRuleResult, 0, len(setting.Rules)),
	}
	if !setting.Enabled {
		return result, nil
	}

	notifier := ruliu.NewClient(nil)
	for _, rule := range setting.Rules {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if !rule.Enabled {
			continue
		}

		item := ErrorAlertRuleResult{
			RuleID:     rule.ID,
			CustomerID: rule.CustomerID,
		}
		filter := model.ErrorStatFilter{
			StartTimestamp: payload.SlotStart,
			EndTimestamp:   payload.SlotEnd - 1,
			CustomerId:     rule.CustomerID,
		}
		total, err := model.CountErrorLogs(filter)
		if err != nil {
			item.Error = err.Error()
			result.Items = append(result.Items, item)
			continue
		}
		item.ErrorCount = total
		if total == 0 {
			item.SkipReason = "no_errors"
			result.Items = append(result.Items, item)
			continue
		}

		stats, err := model.GetErrorSignatureStats(filter, rule.TopN)
		if err != nil {
			item.Error = err.Error()
			result.Items = append(result.Items, item)
			continue
		}
		if len(stats) == 0 {
			item.SkipReason = "no_signed_errors"
			result.Items = append(result.Items, item)
			continue
		}

		message := buildErrorAlertMessage(rule.CustomerID, payload, total, stats)
		err = notifier.Send(ctx, ruliu.AlertParams{
			AlertToID: rule.AlertToID,
			AlertURL:  rule.AlertURL,
			Msg:       message,
			Link:      rule.Link,
			AtUserIDs: rule.AtUserIDs,
		})
		if err != nil {
			item.Error = err.Error()
		} else {
			item.MessageSent = true
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func buildErrorAlertMessage(customerID string, payload ErrorAlertTaskPayload, total int64, stats []*model.ErrorSignatureStat) string {
	var builder strings.Builder
	builder.WriteString("明细错误告警\n")
	fmt.Fprintf(&builder, "客户：%s\n", customerID)
	fmt.Fprintf(&builder, "统计窗口：%s - %s\n",
		time.Unix(payload.SlotStart, 0).Format("2006-01-02 15:04:05"),
		time.Unix(payload.SlotEnd, 0).Format("2006-01-02 15:04:05"),
	)
	fmt.Fprintf(&builder, "错误总数：%d\n\n", total)
	builder.WriteString("排名\t明细错误\t次数\t占比\n")
	for i, stat := range stats {
		label := model.ResolveErrorShortName(stat.SignatureHash)
		if label == "" {
			label = stat.Signature
		}
		if len(label) > 160 {
			label = label[:160] + "..."
		}
		share := float64(stat.Count) / float64(total) * 100
		if math.IsNaN(share) || math.IsInf(share, 0) {
			share = 0
		}
		fmt.Fprintf(&builder, "%d\t%s\t%d\t%.2f%%\n", i+1, label, stat.Count, share)
	}
	return builder.String()
}
