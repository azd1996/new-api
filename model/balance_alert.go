package model

import (
	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// BalanceAlertState is the per-user cooldown of a balance alert rule. A user is
// reported at most once per quiet window, so the scan does not resend the same
// low balance every hour.
type BalanceAlertState struct {
	Id          int    `json:"id"`
	UserId      int    `json:"user_id" gorm:"uniqueIndex:idx_balance_alert_user_rule,priority:1;not null"`
	RuleId      string `json:"rule_id" gorm:"uniqueIndex:idx_balance_alert_user_rule,priority:2;type:varchar(64);not null"`
	LastAlertAt int64  `json:"last_alert_at" gorm:"bigint;not null;default:0"`

	CreatedAt int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt int64 `json:"updated_at" gorm:"bigint"`
}

func (s *BalanceAlertState) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	s.CreatedAt = now
	s.UpdatedAt = now
	return nil
}

func (s *BalanceAlertState) BeforeUpdate(tx *gorm.DB) error {
	s.UpdatedAt = common.GetTimestamp()
	return nil
}

// ListUsersBelowBalance returns enabled users whose wallet balance is below the
// threshold, optionally narrowed to one user group, skipping users already
// alerted by the same rule inside the quiet window. Users are ordered by the
// lowest balance first so one alert message covers the worst cases.
func ListUsersBelowBalance(ruleId string, threshold int, group string, quietBefore int64, limit int) ([]*User, error) {
	users := make([]*User, 0, limit)
	query := DB.Model(&User{}).
		Joins("LEFT JOIN balance_alert_states ON balance_alert_states.user_id = users.id AND balance_alert_states.rule_id = ?", ruleId).
		Where("users.status = ? AND users.quota < ?", common.UserStatusEnabled, threshold).
		Where("(balance_alert_states.last_alert_at IS NULL OR balance_alert_states.last_alert_at < ?)", quietBefore)
	if group != "" {
		query = query.Where("users."+commonGroupCol+" = ?", group)
	}
	err := query.Order("users.quota ASC").Limit(limit).Find(&users).Error
	return users, err
}

// MarkBalanceAlerted records that a rule reported a user now, so the quiet
// window applies to the next scan.
func MarkBalanceAlerted(ruleId string, userIds []int) error {
	if len(userIds) == 0 {
		return nil
	}
	now := common.GetTimestamp()
	rows := make([]BalanceAlertState, 0, len(userIds))
	for _, userId := range userIds {
		rows = append(rows, BalanceAlertState{UserId: userId, RuleId: ruleId, LastAlertAt: now})
	}
	return DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "rule_id"}},
		DoUpdates: clause.Assignments(map[string]any{"last_alert_at": now}),
	}).Create(&rows).Error
}
