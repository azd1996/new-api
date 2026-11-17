package controller

import (
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

type quotaPolicyRequest struct {
	UserId  int    `json:"user_id"`
	Mode    string `json:"mode"`
	Amount  int    `json:"amount"`
	Enabled *bool  `json:"enabled"`
}

// loadQuotaPolicyTargetUser resolves the target user and rejects targets the
// caller may not manage, mirroring the rule used by the manual quota dialog.
func loadQuotaPolicyTargetUser(c *gin.Context, userId int) (*model.User, bool) {
	if userId <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return nil, false
	}
	user, err := model.GetUserById(userId, false)
	if err != nil {
		common.ApiErrorT(c, "User does not exist")
		return nil, false
	}
	if !canManageTargetRole(c.GetInt("role"), user.Role) {
		common.ApiErrorT(c, "No permission to update users of same or higher permission level")
		return nil, false
	}
	return user, true
}

func GetUserQuotaPolicy(c *gin.Context) {
	userId, ok := parseQuotaPolicyUserId(c)
	if !ok {
		return
	}
	if _, allowed := loadQuotaPolicyTargetUser(c, userId); !allowed {
		return
	}
	policy, err := model.GetUserQuotaPolicy(userId)
	if errors.Is(err, model.ErrUserQuotaPolicyNotFound) {
		common.ApiSuccess(c, gin.H{"policy": nil})
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"policy": policy})
}

func PutUserQuotaPolicy(c *gin.Context) {
	var req quotaPolicyRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if _, allowed := loadQuotaPolicyTargetUser(c, req.UserId); !allowed {
		return
	}
	// An empty mode clears the policy instead of storing an unusable row.
	if req.Mode == "" {
		deleteUserQuotaPolicy(c, req.UserId)
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	policy, err := model.SaveUserQuotaPolicy(req.UserId, req.Mode, req.Amount, enabled, c.GetInt("id"))
	if errors.Is(err, model.ErrInvalidUserQuotaAdjustment) {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if errors.Is(err, model.ErrUserQuotaPolicyInvalidMode) {
		common.ApiErrorT(c, "Invalid quota policy mode")
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordQuotaPolicyAudit(c, req.UserId, "user.quota_policy_update", "User quota policy updated", model.AuditFields{
		"target_user_id": req.UserId,
		"mode":           policy.Mode,
		"amount":         policy.Amount,
		"enabled":        policy.Enabled,
	})
	common.ApiSuccess(c, gin.H{"policy": policy})
}

func DeleteUserQuotaPolicy(c *gin.Context) {
	userId, ok := parseQuotaPolicyUserId(c)
	if !ok {
		return
	}
	if _, allowed := loadQuotaPolicyTargetUser(c, userId); !allowed {
		return
	}
	deleteUserQuotaPolicy(c, userId)
}

func deleteUserQuotaPolicy(c *gin.Context, userId int) {
	if err := model.DeleteUserQuotaPolicy(userId); err != nil {
		// Clearing a policy that was never stored is already the requested state.
		if errors.Is(err, model.ErrUserQuotaPolicyNotFound) {
			common.ApiSuccess(c, nil)
			return
		}
		common.ApiError(c, err)
		return
	}
	recordQuotaPolicyAudit(c, userId, "user.quota_policy_delete", "User quota policy removed", model.AuditFields{
		"target_user_id": userId,
	})
	common.ApiSuccess(c, nil)
}

// ApplyUserQuotaPolicy grants the monthly refresh right now instead of waiting
// for the next calendar month. A grant already made for this month is reported
// as success, because the caller's intent (this month is settled) is satisfied.
func ApplyUserQuotaPolicy(c *gin.Context) {
	var req quotaPolicyRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if _, allowed := loadQuotaPolicyTargetUser(c, req.UserId); !allowed {
		return
	}
	adjustment, err := model.GrantUserQuotaPolicy(req.UserId, model.CurrentQuotaPeriod(), auditOperatorInfo(c))
	switch {
	case errors.Is(err, model.ErrUserQuotaPolicyAlreadyGranted):
		common.ApiSuccessT(c, "Quota already granted for this period", nil)
	case errors.Is(err, model.ErrUserQuotaPolicyNotFound):
		common.ApiErrorT(c, "User quota policy does not exist")
	case errors.Is(err, model.ErrUserQuotaPolicyInvalidMode):
		common.ApiErrorT(c, "This quota policy does not refresh monthly")
	case errors.Is(err, model.ErrUserQuotaPolicyUserDisabled):
		common.ApiErrorT(c, "User is disabled")
	case err != nil:
		common.ApiError(c, err)
	default:
		markAuditLogged(c)
		common.ApiSuccess(c, gin.H{"from": adjustment.Before, "to": adjustment.After})
	}
}

func recordQuotaPolicyAudit(c *gin.Context, userId int, action string, content string, params model.AuditFields) {
	model.RecordLogWithAdminInfo(userId, model.LogTypeManage, content, auditOperatorInfo(c),
		&model.AuditOperation{Action: action, Params: params}, c)
	markAuditLogged(c)
}

func parseQuotaPolicyUserId(c *gin.Context) (int, bool) {
	userId, err := strconv.Atoi(c.Query("user_id"))
	if err != nil || userId <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return 0, false
	}
	return userId, true
}
