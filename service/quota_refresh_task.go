package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	quotaRefreshTickInterval = 1 * time.Minute
	quotaRefreshBatchSize    = 300
)

var (
	quotaRefreshOnce    sync.Once
	quotaRefreshRunning atomic.Bool
)

// StartQuotaRefreshTask applies the monthly balance refresh for users whose
// policy is due. Only the master node grants quota, and the grant itself is
// idempotent per calendar month, so a restart or a failover cannot double grant.
func StartQuotaRefreshTask() {
	quotaRefreshOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			logger.LogInfo(context.Background(), common.LogText("monthly quota refresh task started: tick=%s", quotaRefreshTickInterval))
			ticker := time.NewTicker(quotaRefreshTickInterval)
			defer ticker.Stop()

			runQuotaRefreshOnce()
			for range ticker.C {
				runQuotaRefreshOnce()
			}
		})
	})
}

func runQuotaRefreshOnce() {
	if !quotaRefreshRunning.CompareAndSwap(false, true) {
		return
	}
	defer quotaRefreshRunning.Store(false)

	ctx := context.Background()
	period := model.CurrentQuotaPeriod()
	totalGranted := 0
	totalFailed := 0
	for {
		policies, err := model.ListDueMonthlyQuotaRefreshPolicies(quotaRefreshBatchSize)
		if err != nil {
			logger.LogWarn(ctx, common.LogText("monthly quota refresh task failed to list policies: %v", err))
			return
		}
		if len(policies) == 0 {
			break
		}
		settled := 0
		for _, policy := range policies {
			_, err := model.GrantUserQuotaPolicy(policy.UserId, period, nil)
			if err != nil {
				// Already settled, unknown, or no longer eligible: nothing to do,
				// and retrying it every tick would only flood the log.
				if errors.Is(err, model.ErrUserQuotaPolicyAlreadyGranted) ||
					errors.Is(err, model.ErrUserQuotaPolicyNotFound) ||
					errors.Is(err, model.ErrUserQuotaPolicyUserDisabled) ||
					errors.Is(err, model.ErrUserQuotaPolicyInvalidMode) {
					continue
				}
				totalFailed++
				logger.LogWarn(ctx, common.LogText("monthly quota refresh failed: userId=%d, period=%s, err=%v", policy.UserId, period, err))
				continue
			}
			settled++
			totalGranted++
		}
		// A batch that settled nothing would be returned again forever; leave the
		// rest to the next tick instead of looping on the same rows.
		if settled == 0 {
			break
		}
	}
	if common.DebugEnabled && (totalGranted > 0 || totalFailed > 0) {
		logger.LogDebug(ctx, "monthly quota refresh: granted=%d, failed=%d, period=%s", totalGranted, totalFailed, period)
	}
}
