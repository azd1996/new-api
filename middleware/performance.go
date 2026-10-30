package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	prommetrics "github.com/QuantumNous/new-api/pkg/prom_metrics"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// systemStatusProvider sources the sampled system status for the overload check.
// Production uses the live sampler; tests swap it to drive deterministic
// overload cases without touching the sampler goroutine.
var systemStatusProvider = common.GetSystemStatus

// SystemPerformanceCheck 检查系统性能中间件
func SystemPerformanceCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		err := checkSystemPerformance()
		if err == nil {
			c.Next()
			return
		}
		// Gateway-entry overload rejection: record it as an error log before the
		// 503 is written. Rejection happens before authentication, so there is no
		// user/token/channel/model attribution and the request body is never read.
		if constant.ErrorLogEnabled && types.IsRecordErrorLog(err) {
			status := systemStatusProvider()
			config := common.GetPerformanceMonitorConfig()
			// Signature is based on the stable error code only (no live usage or
			// threshold), so usage fluctuations do not split the error group.
			signature := service.NormalizeErrorSignature("status_code=503, " + string(err.GetErrorCode()))
			other := model.NewLogOther()
			other.SetPublic("request_path", c.Request.URL.Path)
			other.SetPublic("error_type", err.GetErrorType())
			other.SetPublic("error_code", err.GetErrorCode())
			other.SetPublic("status_code", err.StatusCode)
			// error_signature / error_signature_hash live in the admin tier so that
			// RecordErrorLog dual-writes the ErrorSignatureHash column, matching the
			// K error-signature convention (see service/relay_error.go).
			other.SetAdmin("error_signature", signature)
			other.SetAdmin("error_signature_hash", service.ErrorSignatureHash(signature))
			other.SetAdmin("node_name", common.NodeName)
			other.SetAdmin("system_overload", map[string]any{
				"cpu_usage":        status.CPUUsage,
				"cpu_threshold":    config.CPUThreshold,
				"memory_usage":     status.MemoryUsage,
				"memory_threshold": config.MemoryThreshold,
				"disk_usage":       status.DiskUsage,
				"disk_threshold":   config.DiskThreshold,
				"authenticated":    false,
			})
			model.RecordErrorLog(c, 0, 0, "", "", err.ErrorWithStatusCode(), 0, 0, false, "", other)
			signatureHash := service.ErrorSignatureHash(signature)
			prommetrics.RecordError(model.ResolveErrorShortName(signatureHash),
				model.ResolveErrorAttribution(signatureHash), "", "", "503", "", "false", "")
		}
		if strings.HasPrefix(c.Request.URL.Path, "/v1/messages") {
			c.JSON(err.StatusCode, gin.H{
				"error": err.ToClaudeError(),
			})
		} else {
			c.JSON(err.StatusCode, gin.H{
				"error": err.ToOpenAIError(),
			})
		}
		c.Abort()
	}
}

// checkSystemPerformance 检查系统性能是否超过阈值
func checkSystemPerformance() *types.NewAPIError {
	// 过载标记在采样侧完成：CPU / 内存按容器 cgroup 配额计算，并要求连续多次
	// 超阈值；判定时再按当前阈值复核，并忽略过期采样。
	overload := common.CheckSystemOverload(systemStatusProvider(), common.GetPerformanceMonitorConfig(), time.Now())
	if overload == nil {
		return nil
	}
	return types.NewErrorWithStatusCode(overload, types.ErrorCode(overload.Code), http.StatusServiceUnavailable)
}
