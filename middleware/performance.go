package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	prommetrics "github.com/QuantumNous/new-api/pkg/prom_metrics"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// SystemPerformanceCheck 检查系统性能中间件
func SystemPerformanceCheck() gin.HandlerFunc {
	return systemPerformanceCheck(common.GetSystemStatus)
}

func systemPerformanceCheck(getStatus func() common.SystemStatus) gin.HandlerFunc {
	return func(c *gin.Context) {
		config := common.GetPerformanceMonitorConfig()
		status := getStatus()
		if err := checkSystemPerformance(config, status); err != nil {
			if constant.ErrorLogEnabled && types.IsRecordErrorLog(err) {
				// Reject before authentication: do not invent user/model/channel
				// attribution or inspect credentials/body just to enrich this log.
				signature := service.NormalizeErrorSignature("status_code=503, " + string(err.GetErrorCode()))
				signatureHash := service.ErrorSignatureHash(signature)
				other := map[string]interface{}{
					"request_path":         c.Request.URL.Path,
					"error_type":           err.GetErrorType(),
					"error_code":           err.GetErrorCode(),
					"status_code":          err.StatusCode,
					"error_signature":      signature,
					"error_signature_hash": signatureHash,
					"admin_info": map[string]interface{}{
						"node_name": common.NodeName,
						"system_overload": map[string]interface{}{
							"cpu_usage":        status.CPUUsage,
							"cpu_threshold":    config.CPUThreshold,
							"memory_usage":     status.MemoryUsage,
							"memory_threshold": config.MemoryThreshold,
							"disk_usage":       status.DiskUsage,
							"disk_threshold":   config.DiskThreshold,
							"authenticated":    false,
						},
					},
				}
				model.RecordErrorLog(c, 0, 0, "", "", err.ErrorWithStatusCode(), 0, 0, false, "", other)
				if prommetrics.Enabled() {
					prommetrics.RecordError(model.ResolveErrorShortName(signatureHash),
						model.ResolveErrorAttribution(signatureHash), "", "", "503", "", "false", "")
				}
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
			return
		}
		c.Next()
	}
}

// checkSystemPerformance 检查系统性能是否超过阈值
func checkSystemPerformance(config common.PerformanceMonitorConfig, status common.SystemStatus) *types.NewAPIError {
	if config.OverloadConsecutiveSamples <= 0 {
		return nil
	}

	// 检查 CPU
	if status.CPUOverloaded {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("system cpu overloaded (current: %.1f%%, threshold: %d%%)", status.CPUUsage, config.CPUThreshold),
			"system_cpu_overloaded", http.StatusServiceUnavailable)
	}

	// 检查内存
	if status.MemoryOverloaded {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("system memory overloaded (current: %.1f%%, threshold: %d%%)", status.MemoryUsage, config.MemoryThreshold),
			"system_memory_overloaded", http.StatusServiceUnavailable)
	}

	// 检查磁盘
	if status.DiskOverloaded {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("system disk overloaded (current: %.1f%%, threshold: %d%%)", status.DiskUsage, config.DiskThreshold),
			"system_disk_overloaded", http.StatusServiceUnavailable)
	}

	return nil
}
