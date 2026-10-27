package logger

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

const (
	loggerINFO  = "INFO"
	loggerWarn  = "WARN"
	loggerError = "ERR"
	loggerDebug = "DEBUG"
)

const maxLogCount = 1000000

var logCount int
var setupLogLock sync.Mutex
var setupLogWorking bool
var currentLogPath string
var currentLogPathMu sync.RWMutex
var currentLogFile *os.File

// Debug-level logs are routed to a dedicated debug-*.log file (when LOG_DIR is
// set) instead of the shared gin writers, so the high-volume debug dumps do not
// flood stdout/stderr (docker logs). These vars track that separate file.
var debugLogMu sync.Mutex
var debugLogFile *os.File
var debugLogWriter io.Writer
var debugLogPath string
var debugLogCount int
var debugFallbackWarned bool

func GetCurrentLogPath() string {
	currentLogPathMu.RLock()
	defer currentLogPathMu.RUnlock()
	return currentLogPath
}

// GetCurrentDebugLogPath returns the path of the active dedicated debug log
// file, or an empty string if debug output is falling back to stderr.
func GetCurrentDebugLogPath() string {
	debugLogMu.Lock()
	defer debugLogMu.Unlock()
	return debugLogPath
}

func SetupLogger() {
	defer func() {
		setupLogWorking = false
	}()
	if *common.LogDir != "" {
		ok := setupLogLock.TryLock()
		if !ok {
			log.Println("setup log is already working")
			return
		}
		defer func() {
			setupLogLock.Unlock()
		}()
		logPath := filepath.Join(*common.LogDir, fmt.Sprintf("oneapi-%s.log", time.Now().Format("20060102150405")))
		fd, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatal("failed to open log file")
		}
		currentLogPathMu.Lock()
		oldFile := currentLogFile
		currentLogPath = logPath
		currentLogFile = fd
		currentLogPathMu.Unlock()

		common.LogWriterMu.Lock()
		gin.DefaultWriter = io.MultiWriter(os.Stdout, fd)
		gin.DefaultErrorWriter = io.MultiWriter(os.Stderr, fd)
		if oldFile != nil {
			_ = oldFile.Close()
		}
		common.LogWriterMu.Unlock()
	}
}

func LogInfo(ctx context.Context, msg string) {
	logHelper(ctx, loggerINFO, msg)
}

func LogWarn(ctx context.Context, msg string) {
	logHelper(ctx, loggerWarn, msg)
}

func LogError(ctx context.Context, msg string) {
	logHelper(ctx, loggerError, msg)
}

func LogDebug(ctx context.Context, msg string, args ...any) {
	if DebugActive(ctx) {
		if len(args) > 0 {
			msg = fmt.Sprintf(msg, args...)
		}
		logHelper(ctx, loggerDebug, msg)
	}
}

// DebugActive reports whether verbose debug logging should fire for ctx. The
// global DebugEnabled switch forces it on for all traffic; otherwise logging is
// scoped to the api-key (token name) + client-requested model rules configured
// in debug_scope_setting. Callers without a request context (nil or non-gin
// contexts, e.g. background tasks) only honor the global switch.
func DebugActive(ctx context.Context) bool {
	if common.DebugEnabled {
		return true
	}
	if ctx == nil {
		return false
	}
	tokenName, _ := ctx.Value("token_name").(string)
	model, _ := ctx.Value(string(constant.ContextKeyOriginalModel)).(string)
	return operation_setting.DebugScopeMatches(tokenName, model)
}

func logHelper(ctx context.Context, level string, msg string) {
	var id any = "SYSTEM"
	if ctx != nil {
		if requestID := ctx.Value(common.RequestIdKey); requestID != nil {
			id = requestID
		}
	}
	now := time.Now()
	line := fmt.Sprintf("[%s] %v | %s | %s \n", level, now.Format("2006/01/02 - 15:04:05"), id, msg)
	if level == loggerDebug {
		writeDebugLine(line)
		return
	}
	common.LogWriterMu.RLock()
	writer := gin.DefaultErrorWriter
	if level == loggerINFO {
		writer = gin.DefaultWriter
	}
	_, _ = io.WriteString(writer, line)
	common.LogWriterMu.RUnlock()
	logCount++ // we don't need accurate count, so no lock here
	if logCount > maxLogCount && !setupLogWorking {
		logCount = 0
		setupLogWorking = true
		gopool.Go(func() {
			SetupLogger()
		})
	}
}

// writeDebugLine writes a preformatted DEBUG line to the dedicated debug log
// file, rotating it like the main log and lazily (re)opening it so that
// toggling DebugEnabled at runtime works without a restart.
func writeDebugLine(line string) {
	debugLogMu.Lock()
	defer debugLogMu.Unlock()
	if debugLogWriter == nil || (debugLogFile != nil && debugLogCount > maxLogCount) {
		openDebugLogLocked()
	}
	_, _ = io.WriteString(debugLogWriter, line)
	debugLogCount++
}

// openDebugLogLocked opens (or rotates) the dedicated debug log file. It must be
// called with debugLogMu held. When LOG_DIR is unset or the file cannot be
// opened, debug output falls back to stderr (docker logs) and warns once.
func openDebugLogLocked() {
	if *common.LogDir == "" {
		if !debugFallbackWarned {
			debugFallbackWarned = true
			log.Println("debug log: log directory not configured, debug logs fall back to stderr")
		}
		debugLogWriter = os.Stderr
		return
	}
	logPath := filepath.Join(*common.LogDir, fmt.Sprintf("debug-%s.log", time.Now().Format("20060102150405")))
	fd, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		if !debugFallbackWarned {
			debugFallbackWarned = true
			log.Printf("debug log: failed to open %s, fall back to stderr: %v", logPath, err)
		}
		debugLogWriter = os.Stderr
		return
	}
	oldFile := debugLogFile
	debugLogFile = fd
	debugLogWriter = fd
	debugLogPath = logPath
	debugLogCount = 0
	if oldFile != nil {
		_ = oldFile.Close()
	}
}

func LogQuota(quota int) string {
	// 新逻辑：根据额度展示类型输出
	q := float64(quota)
	switch operation_setting.GetQuotaDisplayType() {
	case operation_setting.QuotaDisplayTypeCNY:
		usd := q / common.QuotaPerUnit
		cny := usd * operation_setting.USDExchangeRate
		return fmt.Sprintf("¥%.6f 额度", cny)
	case operation_setting.QuotaDisplayTypeCustom:
		usd := q / common.QuotaPerUnit
		rate := operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
		symbol := operation_setting.GetGeneralSetting().CustomCurrencySymbol
		if symbol == "" {
			symbol = "¤"
		}
		if rate <= 0 {
			rate = 1
		}
		v := usd * rate
		return fmt.Sprintf("%s%.6f 额度", symbol, v)
	case operation_setting.QuotaDisplayTypeTokens:
		return fmt.Sprintf("%d 点额度", quota)
	default: // USD
		return fmt.Sprintf("＄%.6f 额度", q/common.QuotaPerUnit)
	}
}

func FormatQuota(quota int) string {
	q := float64(quota)
	switch operation_setting.GetQuotaDisplayType() {
	case operation_setting.QuotaDisplayTypeCNY:
		usd := q / common.QuotaPerUnit
		cny := usd * operation_setting.USDExchangeRate
		return fmt.Sprintf("¥%.6f", cny)
	case operation_setting.QuotaDisplayTypeCustom:
		usd := q / common.QuotaPerUnit
		rate := operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
		symbol := operation_setting.GetGeneralSetting().CustomCurrencySymbol
		if symbol == "" {
			symbol = "¤"
		}
		if rate <= 0 {
			rate = 1
		}
		v := usd * rate
		return fmt.Sprintf("%s%.6f", symbol, v)
	case operation_setting.QuotaDisplayTypeTokens:
		return fmt.Sprintf("%d", quota)
	default:
		return fmt.Sprintf("＄%.6f", q/common.QuotaPerUnit)
	}
}

// LogJson 仅供测试使用 only for test
func LogJson(ctx context.Context, msg string, obj any) {
	if !DebugActive(ctx) {
		return
	}
	jsonStr, err := common.Marshal(obj)
	if err != nil {
		LogError(ctx, fmt.Sprintf("json marshal failed: %s", err.Error()))
		return
	}
	LogDebug(ctx, "%s | %s", msg, jsonStr)
}
