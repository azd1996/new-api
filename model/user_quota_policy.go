package model

import (
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// Balance (wallet) quota policies. A policy is configured per user by an
// administrator and must not live in user.setting, because that blob is writable
// by the user themselves.
const (
	// QuotaPolicyModeMonthlyRefresh overwrites the wallet quota on the first day
	// of every calendar month. Unused quota does not carry over.
	QuotaPolicyModeMonthlyRefresh = "monthly_refresh"
	// QuotaPolicyModeFixed applies the amount once; it never grants again.
	QuotaPolicyModeFixed = "fixed"
)

var (
	ErrUserQuotaPolicyNotFound       = errors.New("user quota policy not found")
	ErrUserQuotaPolicyInvalidMode    = errors.New("invalid user quota policy mode")
	ErrUserQuotaPolicyAlreadyGranted = errors.New("quota already granted for this period")
	ErrUserQuotaPolicyUserDisabled   = errors.New("cannot grant quota for a disabled user")
)

type UserQuotaPolicy struct {
	Id     int `json:"id"`
	UserId int `json:"user_id" gorm:"uniqueIndex;not null"`

	Mode string `json:"mode" gorm:"type:varchar(32);not null;default:''"`
	// Target wallet quota in quota units. Mirrors users.quota so both stay in range.
	Amount  int  `json:"amount" gorm:"type:int;not null;default:0"`
	Enabled bool `json:"enabled"`

	// LastGrantedPeriod is the YYYY-MM calendar month of the last successful
	// monthly grant. It is the idempotency guard for the refresh task.
	LastGrantedPeriod string `json:"last_granted_period" gorm:"type:varchar(7);not null;default:''"`

	UpdatedBy int   `json:"updated_by" gorm:"type:int;not null;default:0"`
	CreatedAt int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt int64 `json:"updated_at" gorm:"bigint"`
}

func (p *UserQuotaPolicy) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	p.CreatedAt = now
	p.UpdatedAt = now
	return nil
}

func (p *UserQuotaPolicy) BeforeUpdate(tx *gorm.DB) error {
	p.UpdatedAt = common.GetTimestamp()
	return nil
}

// CurrentQuotaPeriod is the calendar month used as the grant idempotency key, in
// the server's local timezone so it matches the monthly reporting periods.
func CurrentQuotaPeriod() string {
	return time.Now().Format("2006-01")
}

func GetUserQuotaPolicy(userId int) (*UserQuotaPolicy, error) {
	if userId <= 0 {
		return nil, ErrInvalidUserQuotaAdjustment
	}
	var policy UserQuotaPolicy
	err := DB.First(&policy, "user_id = ?", userId).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserQuotaPolicyNotFound
	}
	if err != nil {
		return nil, err
	}
	return &policy, nil
}

// SaveUserQuotaPolicy creates or updates the policy of one user. Editing the
// amount or the mode keeps LastGrantedPeriod, so an edit can never re-trigger a
// grant inside a month that was already settled.
func SaveUserQuotaPolicy(userId int, mode string, amount int, enabled bool, operatorId int) (*UserQuotaPolicy, error) {
	if userId <= 0 {
		return nil, ErrInvalidUserQuotaAdjustment
	}
	if mode != QuotaPolicyModeMonthlyRefresh && mode != QuotaPolicyModeFixed {
		return nil, ErrUserQuotaPolicyInvalidMode
	}
	if amount <= 0 || amount > common.MaxWalletQuota {
		return nil, ErrInvalidUserQuotaAdjustment
	}

	policy := UserQuotaPolicy{
		UserId:    userId,
		Mode:      mode,
		Amount:    amount,
		Enabled:   enabled,
		UpdatedBy: operatorId,
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		var existing UserQuotaPolicy
		err := tx.First(&existing, "user_id = ?", userId).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tx.Create(&policy).Error
		}
		if err != nil {
			return err
		}
		policy.Id = existing.Id
		policy.CreatedAt = existing.CreatedAt
		policy.LastGrantedPeriod = existing.LastGrantedPeriod
		return tx.Save(&policy).Error
	})
	if err != nil {
		return nil, err
	}
	return &policy, nil
}

func DeleteUserQuotaPolicy(userId int) error {
	if userId <= 0 {
		return ErrInvalidUserQuotaAdjustment
	}
	result := DB.Where("user_id = ?", userId).Delete(&UserQuotaPolicy{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrUserQuotaPolicyNotFound
	}
	return nil
}

// ListDueMonthlyQuotaRefreshPolicies returns the policies still owed a grant for
// the current calendar month. Failures keep their old LastGrantedPeriod, so they
// are picked up again on a later tick.
func ListDueMonthlyQuotaRefreshPolicies(limit int) ([]*UserQuotaPolicy, error) {
	policies := make([]*UserQuotaPolicy, 0, limit)
	err := DB.Where(
		"enabled = ? AND mode = ? AND (last_granted_period = '' OR last_granted_period <> ?)",
		true, QuotaPolicyModeMonthlyRefresh, CurrentQuotaPeriod(),
	).Order("user_id").Limit(limit).Find(&policies).Error
	return policies, err
}

// GrantUserQuotaPolicy applies one monthly refresh for the given calendar period.
// The wallet write, the idempotency mark and the policy read share a transaction,
// so a retry can neither grant twice nor lose a grant. The wallet delta is
// mirrored into the quota cache after commit, matching AdjustUserQuota.
func GrantUserQuotaPolicy(userId int, period string, adminInfo *AuditAdminInfo) (*UserQuotaAdjustment, error) {
	if userId <= 0 || period == "" {
		return nil, ErrInvalidUserQuotaAdjustment
	}
	var adjustment UserQuotaAdjustment
	grantedAmount := 0
	err := DB.Transaction(func(tx *gorm.DB) error {
		var policy UserQuotaPolicy
		err := lockForUpdate(tx).First(&policy, "user_id = ?", userId).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrUserQuotaPolicyNotFound
		}
		if err != nil {
			return err
		}
		if policy.Mode != QuotaPolicyModeMonthlyRefresh {
			return ErrUserQuotaPolicyInvalidMode
		}
		if policy.LastGrantedPeriod == period {
			return ErrUserQuotaPolicyAlreadyGranted
		}
		var user User
		if err := lockForUpdate(tx).First(&user, userId).Error; err != nil {
			return err
		}
		if user.Status != common.UserStatusEnabled {
			return ErrUserQuotaPolicyUserDisabled
		}
		after, err := common.WalletQuotaFromDecimalStrict(decimal.NewFromInt(int64(policy.Amount)))
		if err != nil {
			return ErrWalletQuotaLimitExceeded
		}
		// An unchanged grant is still a successful settlement, including on MySQL
		// configurations that count only changed rows in RowsAffected.
		if after != user.Quota {
			result := tx.Model(&User{}).Where("id = ?", userId).Update("quota", after)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return gorm.ErrRecordNotFound
			}
		}
		if err := tx.Model(&UserQuotaPolicy{}).Where("user_id = ?", userId).
			Update("last_granted_period", period).Error; err != nil {
			return err
		}
		adjustment = UserQuotaAdjustment{UserID: user.Id, Username: user.Username, Before: user.Quota, After: after}
		grantedAmount = policy.Amount
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Apply only the committed difference, preserving outstanding reservations.
	// Both balances are bounded above, so their difference fits in int64.
	delta := int64(adjustment.After) - int64(adjustment.Before)
	if delta != 0 {
		if err := cacheIncrUserQuota(userId, delta); err != nil {
			common.SysError(common.LogText("failed to sync monthly quota grant for user %d: %s", userId, err))
		}
	}

	operation := &AuditOperation{Action: "user.quota_monthly_refresh", Params: AuditFields{
		"period":       period,
		"target_quota": grantedAmount,
		"from":         adjustment.Before,
		"to":           adjustment.After,
	}}
	if data, err := common.Marshal(AuditOther{AdminInfo: adminInfo, Op: operation}); err == nil {
		log := &Log{
			UserId:    userId,
			Username:  adjustment.Username,
			CreatedAt: common.GetTimestamp(),
			Type:      LogTypeTopup,
			Content:   "Monthly quota refresh",
			Quota:     int(delta),
			Other:     string(data),
		}
		if err := createLog(log); err != nil {
			common.SysLog(common.LogText("failed to record monthly quota grant log: %s", err.Error()))
		}
	} else {
		common.SysError(common.LogText("failed to encode monthly quota grant audit: %s", err.Error()))
	}
	return &adjustment, nil
}
