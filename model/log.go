package model

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/pkg/logshipper"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"

	"gorm.io/gorm"
)

func applyExplicitLogTextFilter(tx *gorm.DB, column string, value string) (*gorm.DB, error) {
	if value == "" {
		return tx, nil
	}
	if strings.Contains(value, "%") {
		condition, pattern, err := buildLogLikeCondition(column, value)
		if err != nil {
			return nil, err
		}
		return tx.Where(condition, pattern), nil
	}
	return tx.Where(column+" = ?", value), nil
}

// LogFieldFilter selects log rows by a text column using one or more values and
// an include (equals / LIKE) or exclude (not-equals / NOT LIKE) operator. An
// empty Values slice applies no filter, so a single include value behaves
// exactly like the former scalar text filter.
type LogFieldFilter struct {
	Values  []string
	Exclude bool
}

// LogChannelFilter is the numeric counterpart of LogFieldFilter for the
// channel_id column, applied with IN / NOT IN.
type LogChannelFilter struct {
	Values  []int
	Exclude bool
}

// applyLogFieldFilter adds an include/exclude filter over column for every value
// in filter. Include values are OR-combined (equals, or LIKE when the value
// carries a % wildcard and allowLike is set); exclude values are AND-combined
// with the negated operator. An empty filter leaves tx unchanged, so a lone
// include value reproduces the previous scalar behavior.
func applyLogFieldFilter(tx *gorm.DB, column string, filter LogFieldFilter, allowLike bool) (*gorm.DB, error) {
	if len(filter.Values) == 0 {
		return tx, nil
	}

	conditions := make([]string, 0, len(filter.Values))
	args := make([]any, 0, len(filter.Values))
	for _, value := range filter.Values {
		if value == "" {
			continue
		}
		if allowLike && strings.Contains(value, "%") {
			condition, pattern, err := buildLogLikeCondition(column, value)
			if err != nil {
				return nil, err
			}
			if filter.Exclude {
				condition = strings.Replace(condition, " LIKE ", " NOT LIKE ", 1)
			}
			conditions = append(conditions, condition)
			args = append(args, pattern)
			continue
		}
		if filter.Exclude {
			conditions = append(conditions, column+" <> ?")
		} else {
			conditions = append(conditions, column+" = ?")
		}
		args = append(args, value)
	}
	if len(conditions) == 0 {
		return tx, nil
	}

	joiner := " OR "
	if filter.Exclude {
		joiner = " AND "
	}
	return tx.Where("("+strings.Join(conditions, joiner)+")", args...), nil
}

// applyLogChannelFilter adds an IN / NOT IN filter over the channel_id column.
// An empty filter leaves tx unchanged.
func applyLogChannelFilter(tx *gorm.DB, column string, filter LogChannelFilter) *gorm.DB {
	if len(filter.Values) == 0 {
		return tx
	}
	if filter.Exclude {
		return tx.Where(column+" NOT IN ?", filter.Values)
	}
	return tx.Where(column+" IN ?", filter.Values)
}

func buildLogLikeCondition(column string, value string) (string, string, error) {
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		pattern, err := sanitizeClickHouseLikePattern(value)
		if err != nil {
			return "", "", err
		}
		return column + " LIKE ?", pattern, nil
	}

	pattern, err := sanitizeLikePattern(value)
	if err != nil {
		return "", "", err
	}
	return column + " LIKE ? ESCAPE '!'", pattern, nil
}

// statusCodeOtherCondition matches error logs whose Other JSON carries the
// given HTTP status_code (written as `"status_code":NNN`), bounded by the
// trailing `,` or `}` so 400 does not match 4000. Scans the unindexed Other
// blob; pair with a time range. Digits carry no LIKE metacharacters.
func statusCodeOtherCondition(column string, statusCode int) (string, []any) {
	prefix := fmt.Sprintf(`"status_code":%d`, statusCode)
	return "(" + column + " LIKE ? OR " + column + " LIKE ?)",
		[]any{"%" + prefix + ",%", "%" + prefix + "}%"}
}

// errorContentLikeCondition matches logs whose content contains keyword
// (unanchored substring), escaping LIKE metacharacters per dialect.
func errorContentLikeCondition(column string, keyword string) (string, string) {
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(keyword)
		return column + " LIKE ?", "%" + esc + "%"
	}
	esc := strings.NewReplacer(`!`, `!!`, `%`, `!%`, `_`, `!_`).Replace(keyword)
	return column + " LIKE ? ESCAPE '!'", "%" + esc + "%"
}

// otherFieldLikeCondition builds a cross-dialect condition matching logs whose
// Other JSON carries the given string field as `"key":"value"`. It powers the
// downstream trace-tag filters (downstream_trace_tag / customer_id /
// downstream_request_id), which live in the unindexed Other TEXT blob, so this
// scans — callers should pair it with a time range or other narrowing filter.
// The value's LIKE metacharacters are escaped so it cannot alter the pattern.
func otherFieldLikeCondition(column, key, value string) (string, string) {
	literal := fmt.Sprintf(`"%s":"%s"`, key, value)
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(literal)
		return column + " LIKE ?", "%" + esc + "%"
	}
	esc := strings.NewReplacer(`!`, `!!`, `%`, `!%`, `_`, `!_`).Replace(literal)
	return column + " LIKE ? ESCAPE '!'", "%" + esc + "%"
}

// customerNoneFilter is the sentinel value the UI sends for the "其他 / Other
// (no customer info)" option in the customer-id / customer-name filters; it
// selects logs whose Other JSON has no decoded customer.
const customerNoneFilter = "__none__"

// otherFieldAbsentCondition builds a cross-dialect condition matching logs whose
// Other JSON does NOT contain the given key (e.g. requests without a decoded
// customer). The key's LIKE metacharacters (notably `_`) are escaped.
func otherFieldAbsentCondition(column, key string) (string, string) {
	literal := fmt.Sprintf(`"%s":`, key)
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(literal)
		return column + " NOT LIKE ?", "%" + esc + "%"
	}
	esc := strings.NewReplacer(`!`, `!!`, `%`, `!%`, `_`, `!_`).Replace(literal)
	return column + " NOT LIKE ? ESCAPE '!'", "%" + esc + "%"
}

func sanitizeClickHouseLikePattern(input string) (string, error) {
	input = strings.ReplaceAll(input, `\`, `\\`)
	input = strings.ReplaceAll(input, `_`, `\_`)

	if err := validateLikePattern(input); err != nil {
		return "", err
	}
	return input, nil
}

type Log struct {
	Id                int    `json:"id" gorm:"index:idx_created_at_id,priority:2;index:idx_user_id_id,priority:2"`
	UserId            int    `json:"user_id" gorm:"index;index:idx_user_id_id,priority:1"`
	CreatedAt         int64  `json:"created_at" gorm:"bigint;index:idx_created_at_id,priority:1;index:idx_created_at_type"`
	Type              int    `json:"type" gorm:"index:idx_created_at_type"`
	Content           string `json:"content"`
	Username          string `json:"username" gorm:"index;index:index_username_model_name,priority:2;default:''"`
	TokenName         string `json:"token_name" gorm:"index;default:''"`
	ModelName         string `json:"model_name" gorm:"index;index:index_username_model_name,priority:1;default:''"`
	Quota             int    `json:"quota" gorm:"default:0"`
	PromptTokens      int    `json:"prompt_tokens" gorm:"default:0"`
	CompletionTokens  int    `json:"completion_tokens" gorm:"default:0"`
	UseTime           int    `json:"use_time" gorm:"default:0"`
	IsStream          bool   `json:"is_stream"`
	ChannelId         int    `json:"channel" gorm:"index"`
	ChannelName       string `json:"channel_name" gorm:"->"`
	TokenId           int    `json:"token_id" gorm:"default:0;index"`
	Group             string `json:"group" gorm:"index"`
	Ip                string `json:"ip" gorm:"index;default:''"`
	RequestId         string `json:"request_id,omitempty" gorm:"type:varchar(64);index:idx_logs_request_id;default:''"`
	UpstreamRequestId string `json:"upstream_request_id,omitempty" gorm:"type:varchar(128);index:idx_logs_upstream_request_id;default:''"`
	SessionID         string `json:"session_id,omitempty" gorm:"type:varchar(128);index:idx_logs_session_id;default:''"`
	// ErrorSignatureHash duplicates other.error_signature_hash into a real indexed
	// column so the detailed-error stats drill-down is a fast exact match.
	ErrorSignatureHash string `json:"error_signature_hash,omitempty" gorm:"type:varchar(64);index:idx_logs_error_signature_hash;default:''"`
	// DownstreamRequestId / CustomerId are decoded from the downstream gateway
	// trace-tag header and duplicated into real indexed columns for fast search.
	DownstreamRequestId string `json:"downstream_request_id,omitempty" gorm:"type:varchar(128);index:idx_logs_downstream_request_id;default:''"`
	CustomerId          string `json:"customer_id,omitempty" gorm:"type:varchar(128);index:idx_logs_customer_id;default:''"`
	// RetryTriggered duplicates other.admin_info.retry_triggered into a real
	// indexed column (error logs) so the admin log list can filter attempts that
	// fired an error-triggered retry-override. Reporting-only; never billed.
	RetryTriggered bool `json:"retry_triggered" gorm:"not null;default:false;index:idx_logs_retry_triggered"`
	// RawQuota/StatQuota/StatGroupRatio/CustomerStatQuota/CustomerDiscount
	// duplicate the reporting-only raw/discounted amounts from the Other JSON
	// (admin_info) into real columns (consume logs only), written alongside the
	// Other copy so they can reach ClickHouse and be SUM-aggregated. They never
	// affect billing; historical rows predating the columns read as 0.
	RawQuota          int     `json:"raw_quota,omitempty" gorm:"default:0"`
	StatQuota         int     `json:"stat_quota,omitempty" gorm:"default:0"`
	StatGroupRatio    float64 `json:"stat_group_ratio,omitempty" gorm:"default:0"`
	CustomerStatQuota int     `json:"customer_stat_quota,omitempty" gorm:"default:0"`
	CustomerDiscount  float64 `json:"customer_discount,omitempty" gorm:"default:0"`
	Other             string  `json:"other"`
}

// don't use iota, avoid change log type value
const (
	LogTypeUnknown = 0
	LogTypeTopup   = 1
	LogTypeConsume = 2
	LogTypeManage  = 3
	LogTypeSystem  = 4
	LogTypeError   = 5
	LogTypeRefund  = 6
	LogTypeLogin   = 7
	// LogTypeRetryOverride records how the channel's retry-override rules
	// processed a request. Admin-only: details live under Other.admin_info and
	// this type is filtered out of non-admin (user) log queries.
	LogTypeRetryOverride = 8
)

// adminOnlyLogTypes lists log types that must never appear in non-admin (user)
// log queries; their detail lives under Other.admin_info.
var adminOnlyLogTypes = []int{LogTypeRetryOverride}

func ensureLogRequestId(log *Log) {
	if log != nil && log.RequestId == "" {
		log.RequestId = common.NewRequestId()
	}
}

func createLog(log *Log) error {
	ensureLogRequestId(log)
	if err := LOG_DB.Create(log).Error; err != nil {
		return err
	}
	shipLog(log)
	return nil
}

// shipLog dual-writes the row to the local log-shipper file so LoongCollector
// can forward it to the ClickHouse cluster. It runs after the LOG_DB insert
// succeeded and never propagates failures: log recording sits on the relay hot
// path, and losing a shipped copy must not fail the request or mask the row
// that already landed in LOG_DB.
func shipLog(log *Log) {
	if !logshipper.Enabled() {
		return
	}
	err := logshipper.Ship(logshipper.Row{
		UserId:            log.UserId,
		Ts:                log.CreatedAt,
		Type:              log.Type,
		Content:           log.Content,
		Username:          log.Username,
		TokenName:         log.TokenName,
		ModelName:         log.ModelName,
		Quota:             log.Quota,
		PromptTokens:      log.PromptTokens,
		CompletionTokens:  log.CompletionTokens,
		UseTime:           log.UseTime,
		IsStream:          log.IsStream,
		ChannelId:         log.ChannelId,
		TokenId:           log.TokenId,
		GroupName:         log.Group,
		Ip:                log.Ip,
		RequestId:         log.RequestId,
		UpstreamRequestId: log.UpstreamRequestId,
		RawQuota:          log.RawQuota,
		StatQuota:         log.StatQuota,
		StatGroupRatio:    log.StatGroupRatio,
		CustomerStatQuota: log.CustomerStatQuota,
		CustomerDiscount:  log.CustomerDiscount,
		Other:             log.Other,
	})
	if err != nil {
		common.SysError("failed to ship log: " + err.Error())
	}
}

func clickHouseLogOrder(prefix string) string {
	return prefix + "created_at desc, " + prefix + "request_id desc"
}

func assignDisplayLogIds(logs []*Log, startIdx int) {
	for i := range logs {
		logs[i].Id = startIdx + i + 1
	}
}

func formatUserLogs(logs []*Log, startIdx int) {
	for i := range logs {
		logs[i].ChannelName = ""
		logs[i].Other = formatLogOtherJSON(logs[i].Other, logOtherVisibilityUser)
	}
	assignDisplayLogIds(logs, startIdx)
}

// FormatAdminLogs removes root-only diagnostics while retaining operational
// admin_info. Root callers must not pass their results through this formatter.
func FormatAdminLogs(logs []*Log) {
	for i := range logs {
		logs[i].Other = formatLogOtherJSON(logs[i].Other, logOtherVisibilityAdmin)
	}
}

// FormatRootLogs normalizes legacy metadata into the current scoped shape
// without removing root-only diagnostics.
func FormatRootLogs(logs []*Log) {
	for i := range logs {
		logs[i].Other = formatLogOtherJSON(logs[i].Other, logOtherVisibilityRoot)
	}
}

func GetLogByTokenId(tokenId int) (logs []*Log, err error) {
	order := "id desc"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = clickHouseLogOrder("")
	}
	err = LOG_DB.Model(&Log{}).Where("token_id = ?", tokenId).Where("type NOT IN ?", adminOnlyLogTypes).Order(order).Limit(common.MaxRecentItems).Find(&logs).Error
	formatUserLogs(logs, 0)
	return logs, err
}

func RecordLog(userId int, logType int, content *common.Message) {
	if logType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(userId, false)
	other := NewLogOther()
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      logType,
		Content:   other.setContent([]*common.Message{content}),
		Other:     other.JSONString(),
	}
	err := createLog(log)
	if err != nil {
		common.SysLog(common.LogText("failed to record log: %s", err.Error()))
	}
}

// RecordLogWithAdminInfo stores operator metadata under other.admin_info and
// an optional, user-visible operation descriptor under other.op for localization.
func RecordLogWithAdminInfo(userId int, logType int, content string, adminInfo *AuditAdminInfo, operation *AuditOperation, request ...*gin.Context) {
	if logType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(userId, false)
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      logType,
		Content:   content,
	}
	if logType == LogTypeManage {
		var c *gin.Context
		if len(request) > 0 {
			c = request[0]
		}
		actorRole := 0
		if c != nil {
			actorRole = c.GetInt("role")
		}
		RecordAuditLog(c, AuditLog{UserId: userId, Username: username, ActorRole: actorRole, Category: AuditCategoryOperation, Content: content, Other: AuditOther{AdminInfo: adminInfo, Op: operation}, Success: true})
		return
	}
	if len(request) > 0 && request[0] != nil {
		log.RequestId = request[0].GetString(common.RequestIdKey)
	}
	if adminInfo != nil || operation != nil {
		data, err := common.Marshal(AuditOther{AdminInfo: adminInfo, Op: operation})
		if err != nil {
			common.SysError(common.LogText("failed to encode log admin info: %s", err.Error()))
			return
		}
		log.Other = string(data)
	}
	if err := createLog(log); err != nil {
		common.SysLog(common.LogText("failed to record log: %s", err.Error()))
	}
}

// OverrideAuditLogParams carries the request identity for a retry-override audit
// log. The processing detail is nested under Other.admin_info.retry_override, so
// it is admin-only; the log type is additionally filtered out of user log
// queries. These logs never carry quota/token counts.
type OverrideAuditLogParams struct {
	UserId            int
	ChannelId         int
	ModelName         string
	TokenName         string
	TokenId           int
	Group             string
	RequestId         string
	UpstreamRequestId string
	IsStream          bool
}

// RecordRetryOverrideLog records a retry-override trigger (LogTypeRetryOverride).
// detail is nested under Other.admin_info.retry_override.
func RecordRetryOverrideLog(params OverrideAuditLogParams, content string, detail map[string]any) {
	username, _ := GetUsernameById(params.UserId, false)
	other := NewLogOther()
	if len(detail) > 0 {
		other.SetAdmin("retry_override", detail)
	}
	log := &Log{
		UserId:            params.UserId,
		Username:          username,
		CreatedAt:         common.GetTimestamp(),
		Type:              LogTypeRetryOverride,
		Content:           content,
		ModelName:         params.ModelName,
		TokenName:         params.TokenName,
		TokenId:           params.TokenId,
		ChannelId:         params.ChannelId,
		Group:             params.Group,
		IsStream:          params.IsStream,
		RequestId:         params.RequestId,
		UpstreamRequestId: params.UpstreamRequestId,
		Other:             other.JSONString(),
	}
	if err := createLog(log); err != nil {
		common.SysLog(common.LogText("failed to record retry override log: %s", err.Error()))
	}
}

// RecordLoginLog writes new login events to the independent audit table.
// username 由调用方传入（登录流程已持有用户对象），避免额外的数据库查询。
// content 为英文兜底文本（用于导出）；action+params 供前端本地化渲染。
// other 包含 login_method、user_agent 等结构化信息。
func RecordLoginLog(userId, actorRole int, username string, content string, ip string, action string, params map[string]any, other AuditOther, request ...*gin.Context) {
	other.Op = &AuditOperation{Action: action, Params: params}
	var c *gin.Context
	if len(request) > 0 {
		c = request[0]
	}
	RecordAuditLog(c, AuditLog{UserId: userId, Username: username, ActorRole: actorRole, Category: AuditCategoryLogin, Action: action, Content: content, Ip: ip, Other: other, Success: true})
}

// RecordOperationAuditLog writes new operation/security events to the audit table.
// logUserId 为日志归属者，管理审计日志应归属实际操作者；目标资源/用户放入
// action params。username 内部按 logUserId 查询。content 为英文兜底文本（供导出使用）。
// action+params 写入 Other.op，供前端本地化渲染（普通用户可见，不含敏感信息）。
// adminInfo 存放操作者身份（写入 Other.admin_info，普通用户查询时剥离）；
// auditInfo 存放路由/方法/结果等中间件兜底信息（写入 Other.audit_info，普通用户查询时剥离）。
func RecordOperationAuditLog(logUserId, actorRole int, content string, ip string, action string, params map[string]any, adminInfo *AuditAdminInfo, auditInfo *AuditRequestInfo, request ...*gin.Context) {
	username, _ := GetUsernameById(logUserId, false)
	other := AuditOther{
		Op:        &AuditOperation{Action: action, Params: params},
		AdminInfo: adminInfo,
		AuditInfo: auditInfo,
	}
	var c *gin.Context
	if len(request) > 0 {
		c = request[0]
	}
	category := AuditCategoryOperation
	if adminInfo == nil {
		category = AuditCategorySecurity
	}
	status, success := 200, true
	if auditInfo != nil {
		status = auditInfo.Status
		success = auditInfo.Success
	}
	RecordAuditLog(c, AuditLog{UserId: logUserId, Username: username, ActorRole: actorRole, Category: category, Action: action, Content: content, Ip: ip, Status: status, Success: success, Other: other})
}

func RecordTopupLog(userId int, content *common.Message, callerIp string, paymentMethod string, callbackPaymentMethod string) {
	username, _ := GetUsernameById(userId, false)
	other := NewLogOther()
	other.MergeAdmin(map[string]any{
		"server_ip":               common.GetIp(),
		"node_name":               common.NodeName,
		"caller_ip":               callerIp,
		"payment_method":          paymentMethod,
		"callback_payment_method": callbackPaymentMethod,
		"version":                 common.Version,
	})
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeTopup,
		Content:   other.setContent([]*common.Message{content}),
		Ip:        callerIp,
		Other:     other.JSONString(),
	}
	err := createLog(log)
	if err != nil {
		common.SysLog(common.LogText("failed to record topup log: %s", err.Error()))
	}
}

// attachProcessShutdownOther marks a log whose request was force-cancelled by a
// process shutdown. The marker is admin-tier only and never touches billing.
func attachProcessShutdownOther(c *gin.Context, other *LogOther) {
	if c == nil || c.Request == nil || other == nil {
		return
	}
	if info := common.ProcessShutdownFromContext(c.Request.Context()); info != nil {
		other.SetAdmin("process_shutdown", info)
	}
}

func RecordErrorLog(c *gin.Context, userId int, channelId int, modelName string, tokenName string, content string, tokenId int, useTimeSeconds int,
	isStream bool, group string, other *LogOther) {
	logger.LogInfo(c, common.LogText("record error log: userId=%d, channelId=%d, modelName=%s, tokenName=%s, content=%s", userId, channelId, modelName, tokenName, common.LocalLogPreview(content)))
	username := c.GetString("username")
	requestId := c.GetString(common.RequestIdKey)
	upstreamRequestId := c.GetString(common.UpstreamRequestIdKey)
	sessionID := common.GetContextKeyString(c, constant.ContextKeySessionID)
	if sessionID != "" && other != nil {
		other.SetPublic("session_id", sessionID)
	}
	downstreamRequestID, customerID := "", ""
	if raw, reqID, custID, decoded := common.TraceTagFromContext(c); raw != "" && other != nil {
		other.SetAdmin("downstream_trace_tag", raw)
		if decoded {
			downstreamRequestID, customerID = reqID, custID
			other.SetAdmin("downstream_request_id", reqID)
			other.SetAdmin("customer_id", custID)
			if name := CustomerName(custID); name != "" {
				other.SetAdmin("customer_name", name)
			}
		}
	}
	attachProcessShutdownOther(c, other)
	errorSignatureHash := ""
	retryTriggered := false
	if other != nil {
		errorSignatureHash, _ = other.adminInfo["error_signature_hash"].(string)
		retryTriggered, _ = other.adminInfo["retry_triggered"].(bool)
	}
	otherStr := other.JSONString()
	// 判断是否需要记录 IP
	needRecordIp := false
	// userId == 0 is a pre-authentication rejection (e.g. system overload): skip
	// the user-setting lookup so it does not spuriously query a missing user.
	if userId > 0 {
		if settingMap, err := GetUserSetting(userId, false); err == nil {
			if settingMap.RecordIpLog {
				needRecordIp = true
			}
		}
	}
	log := &Log{
		UserId:              userId,
		Username:            username,
		CreatedAt:           common.GetTimestamp(),
		Type:                LogTypeError,
		SessionID:           sessionID,
		DownstreamRequestId: downstreamRequestID,
		CustomerId:          customerID,
		ErrorSignatureHash:  errorSignatureHash,
		RetryTriggered:      retryTriggered,
		Content:             content,
		PromptTokens:        0,
		CompletionTokens:    0,
		TokenName:           tokenName,
		ModelName:           modelName,
		Quota:               0,
		ChannelId:           channelId,
		TokenId:             tokenId,
		UseTime:             useTimeSeconds,
		IsStream:            isStream,
		Group:               group,
		Ip: func() string {
			if needRecordIp {
				return c.ClientIP()
			}
			return ""
		}(),
		RequestId:         requestId,
		UpstreamRequestId: upstreamRequestId,
		Other:             otherStr,
	}
	err := createLog(log)
	if err != nil {
		logger.LogError(c, common.LogText("failed to record log: %s", err.Error()))
	}
}

type RecordConsumeLogParams struct {
	ChannelId        int               `json:"channel_id"`
	PromptTokens     int               `json:"prompt_tokens"`
	CompletionTokens int               `json:"completion_tokens"`
	ModelName        string            `json:"model_name"`
	TokenName        string            `json:"token_name"`
	Quota            int               `json:"quota"`
	Content          []*common.Message `json:"content"`
	TokenId          int               `json:"token_id"`
	UseTimeSeconds   int               `json:"use_time_seconds"`
	IsStream         bool              `json:"is_stream"`
	Group            string            `json:"group"`
	Other            *LogOther         `json:"other"`
}

func RecordConsumeLog(c *gin.Context, userId int, params RecordConsumeLogParams) {
	if !common.LogConsumeEnabled {
		return
	}
	logger.LogInfo(c, common.LogText("record consume log: userId=%d, params=%s", userId, common.GetJsonString(params)))
	username := c.GetString("username")
	requestId := c.GetString(common.RequestIdKey)
	upstreamRequestId := c.GetString(common.UpstreamRequestIdKey)
	createdAt := common.GetTimestamp()
	other := params.Other
	sessionID := common.GetContextKeyString(c, constant.ContextKeySessionID)
	if sessionID != "" && other != nil {
		other.SetPublic("session_id", sessionID)
	}
	downstreamRequestID, customerID := "", ""
	if raw, reqID, custID, decoded := common.TraceTagFromContext(c); raw != "" && other != nil {
		other.SetAdmin("downstream_trace_tag", raw)
		if decoded {
			downstreamRequestID, customerID = reqID, custID
			other.SetAdmin("downstream_request_id", reqID)
			other.SetAdmin("customer_id", custID)
			if name := CustomerName(custID); name != "" {
				other.SetAdmin("customer_name", name)
			}
		}
	}
	attachProcessShutdownOther(c, other)
	content := other.setContent(params.Content)
	otherStr := other.JSONString()
	// Dual-write the reporting-only raw/discounted amounts (set via SetAdmin in
	// PostTextConsumeQuota) into their own columns while keeping the Other JSON
	// copy. nil/absent reads yield zero values; customer_* are only present when
	// the request carried a decoded customer id.
	var rawQuota, statQuota, customerStatQuota int
	var statGroupRatio, customerDiscount float64
	if other != nil {
		rawQuota, _ = other.adminInfo["raw_quota"].(int)
		statQuota, _ = other.adminInfo["stat_quota"].(int)
		statGroupRatio, _ = other.adminInfo["stat_group_ratio"].(float64)
		customerStatQuota, _ = other.adminInfo["customer_stat_quota"].(int)
		customerDiscount, _ = other.adminInfo["customer_discount"].(float64)
	}
	// 判断是否需要记录 IP
	needRecordIp := false
	if settingMap, err := GetUserSetting(userId, false); err == nil {
		if settingMap.RecordIpLog {
			needRecordIp = true
		}
	}
	log := &Log{
		UserId:              userId,
		Username:            username,
		CreatedAt:           createdAt,
		Type:                LogTypeConsume,
		SessionID:           sessionID,
		DownstreamRequestId: downstreamRequestID,
		CustomerId:          customerID,
		Content:             content,
		PromptTokens:        params.PromptTokens,
		CompletionTokens:    params.CompletionTokens,
		TokenName:           params.TokenName,
		ModelName:           params.ModelName,
		Quota:               params.Quota,
		ChannelId:           params.ChannelId,
		TokenId:             params.TokenId,
		UseTime:             params.UseTimeSeconds,
		IsStream:            params.IsStream,
		Group:               params.Group,
		Ip: func() string {
			if needRecordIp {
				return c.ClientIP()
			}
			return ""
		}(),
		RequestId:         requestId,
		UpstreamRequestId: upstreamRequestId,
		RawQuota:          rawQuota,
		StatQuota:         statQuota,
		StatGroupRatio:    statGroupRatio,
		CustomerStatQuota: customerStatQuota,
		CustomerDiscount:  customerDiscount,
		Other:             otherStr,
	}
	err := createLog(log)
	if err != nil {
		logger.LogError(c, common.LogText("failed to record log: %s", err.Error()))
	}
	if common.DataExportEnabled {
		var cacheReadTokens, cacheCreationTokens int
		if other != nil {
			cacheReadTokens, _ = other.public["cache_tokens"].(int)
			// The settlement path already normalizes the cache creation total.
			// Do not add the 5m/1h breakdown again or infer tokens from prices.
			cacheCreationTokens, _ = other.public["cache_write_tokens"].(int)
		}
		LogQuotaData(QuotaDataLogParams{
			UserID:              userId,
			Username:            username,
			ModelName:           params.ModelName,
			Quota:               params.Quota,
			CreatedAt:           createdAt,
			TokenUsed:           params.PromptTokens + params.CompletionTokens,
			CacheReadTokens:     int64(cacheReadTokens),
			CacheCreationTokens: int64(cacheCreationTokens),
			UseGroup:            params.Group,
			TokenID:             params.TokenId,
			ChannelID:           params.ChannelId,
			NodeName:            common.NodeName,
			CustomerId:          customerID,
			ChannelStatQuota:    statQuota,
			CustomerStatQuota:   customerStatQuota,
		})
	}
}

type RecordTaskBillingLogParams struct {
	UserId    int
	LogType   int
	Content   []*common.Message
	ChannelId int
	ModelName string
	Quota     int
	TokenId   int
	Group     string
	Other     *LogOther
	NodeName  string // 任务发起节点；为空时回退当前节点
}

func RecordTaskBillingLog(params RecordTaskBillingLogParams) {
	if params.LogType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(params.UserId, false)
	tokenName := ""
	if params.TokenId > 0 {
		if token, err := GetTokenById(params.TokenId); err == nil {
			tokenName = token.Name
		}
	}
	createdAt := common.GetTimestamp()
	other := params.Other
	log := &Log{
		UserId:    params.UserId,
		Username:  username,
		CreatedAt: createdAt,
		Type:      params.LogType,
		Content:   other.setContent(params.Content),
		TokenName: tokenName,
		ModelName: params.ModelName,
		Quota:     params.Quota,
		ChannelId: params.ChannelId,
		TokenId:   params.TokenId,
		Group:     params.Group,
		Other:     other.JSONString(),
	}
	err := createLog(log)
	if err != nil {
		common.SysLog(common.LogText("failed to record task billing log: %s", err.Error()))
	}
	if params.LogType == LogTypeConsume && common.DataExportEnabled {
		nodeName := params.NodeName
		if nodeName == "" {
			nodeName = common.NodeName
		}
		LogQuotaData(QuotaDataLogParams{
			UserID:    params.UserId,
			Username:  username,
			ModelName: params.ModelName,
			Quota:     params.Quota,
			CreatedAt: createdAt,
			UseGroup:  params.Group,
			TokenID:   params.TokenId,
			ChannelID: params.ChannelId,
			NodeName:  nodeName,
		})
	}
}

// LogFilter is the usage-log search shared by the log list and the log export.
// Username and Channel are administrator filters. ModelName/Username/TokenName/
// Channel/Group each accept multiple values with an include/exclude operator; a
// single include value reproduces the former scalar behavior.
type LogFilter struct {
	Type                int
	StartTimestamp      int64
	EndTimestamp        int64
	ModelName           LogFieldFilter
	Username            LogFieldFilter
	TokenName           LogFieldFilter
	Channel             LogChannelFilter
	Group               LogFieldFilter
	RequestId           string
	UpstreamRequestId   string
	SessionId           string
	DownstreamRequestId string
	CustomerId          string
	// CustomerName and DownstreamTraceTag match the admin-only trace-tag
	// fields recorded in Other (unindexed; pair with a time range).
	CustomerName       string
	DownstreamTraceTag string
	StatusCode         int
	ErrorKeyword       string
	ErrorSignatureHash string
	// RetryTriggered, when set, restricts to error logs whose attempt did (true)
	// or did not (false) fire an error-triggered retry-override. Admin-only.
	RetryTriggered *bool
}

func (f LogFilter) apply(tx *gorm.DB) (*gorm.DB, error) {
	var err error
	if f.Type != LogTypeUnknown {
		tx = tx.Where("logs.type = ?", f.Type)
	}
	if tx, err = applyLogFieldFilter(tx, "logs.model_name", f.ModelName, true); err != nil {
		return nil, err
	}
	if tx, err = applyLogFieldFilter(tx, "logs.username", f.Username, true); err != nil {
		return nil, err
	}
	if tx, err = applyLogFieldFilter(tx, "logs.token_name", f.TokenName, false); err != nil {
		return nil, err
	}
	if f.RequestId != "" {
		tx = tx.Where("logs.request_id = ?", f.RequestId)
	}
	if f.UpstreamRequestId != "" {
		tx = tx.Where("logs.upstream_request_id = ?", f.UpstreamRequestId)
	}
	if f.SessionId != "" {
		tx = tx.Where("logs.session_id = ?", f.SessionId)
	}
	if f.DownstreamRequestId != "" {
		tx = tx.Where("logs.downstream_request_id = ?", f.DownstreamRequestId)
	}
	if f.CustomerId != "" {
		tx = tx.Where("logs.customer_id = ?", f.CustomerId)
	}
	if f.CustomerName != "" {
		cond, pat := otherFieldLikeCondition("logs.other", "customer_name", f.CustomerName)
		tx = tx.Where(cond, pat)
	}
	if f.DownstreamTraceTag != "" {
		cond, pat := otherFieldLikeCondition("logs.other", "downstream_trace_tag", f.DownstreamTraceTag)
		tx = tx.Where(cond, pat)
	}
	if f.StatusCode != 0 {
		cond, args := statusCodeOtherCondition("logs.other", f.StatusCode)
		tx = tx.Where(cond, args...)
	}
	if f.Type == LogTypeError && f.ErrorKeyword != "" {
		cond, pat := errorContentLikeCondition("logs.content", f.ErrorKeyword)
		tx = tx.Where(cond, pat)
	}
	if f.ErrorSignatureHash != "" {
		tx = tx.Where("logs.error_signature_hash = ?", f.ErrorSignatureHash)
	}
	if f.RetryTriggered != nil {
		tx = tx.Where("logs.type = ? AND logs.retry_triggered = ?", LogTypeError, *f.RetryTriggered)
	}
	if f.StartTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", f.StartTimestamp)
	}
	if f.EndTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", f.EndTimestamp)
	}
	tx = applyLogChannelFilter(tx, "logs.channel_id", f.Channel)
	if tx, err = applyLogFieldFilter(tx, "logs."+logGroupCol, f.Group, false); err != nil {
		return nil, err
	}
	return tx, nil
}

func GetAllLogs(filter LogFilter, startIdx int, num int) (logs []*Log, total int64, err error) {
	tx, err := filter.apply(LOG_DB)
	if err != nil {
		return nil, 0, err
	}
	err = tx.Model(&Log{}).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}
	order := "logs.created_at desc, logs.id desc"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = clickHouseLogOrder("logs.")
	}
	err = tx.Order(order).Limit(num).Offset(startIdx).Find(&logs).Error
	if err != nil {
		return nil, 0, err
	}
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		assignDisplayLogIds(logs, startIdx)
	}

	channelIds := types.NewSet[int]()
	for _, log := range logs {
		if log.ChannelId != 0 {
			channelIds.Add(log.ChannelId)
		}
	}

	if channelIds.Len() > 0 {
		var channels []struct {
			Id   int    `gorm:"column:id"`
			Name string `gorm:"column:name"`
		}
		if common.MemoryCacheEnabled {
			// Cache get channel
			for _, channelId := range channelIds.Items() {
				if cacheChannel, err := CacheGetChannel(channelId); err == nil {
					channels = append(channels, struct {
						Id   int    `gorm:"column:id"`
						Name string `gorm:"column:name"`
					}{
						Id:   channelId,
						Name: cacheChannel.Name,
					})
				}
			}
		} else {
			// Bulk query channels from DB
			if err = DB.Table("channels").Select("id, name").Where("id IN ?", channelIds.Items()).Find(&channels).Error; err != nil {
				return logs, total, err
			}
		}
		channelMap := make(map[int]string, len(channels))
		for _, channel := range channels {
			channelMap[channel.Id] = channel.Name
		}
		for i := range logs {
			logs[i].ChannelName = channelMap[logs[i].ChannelId]
		}
	}

	return logs, total, err
}

const logSearchCountLimit = 10000

func GetUserLogs(userId int, filter LogFilter, startIdx int, num int) (logs []*Log, total int64, err error) {
	tx, err := filter.apply(LOG_DB.Where("logs.user_id = ?", userId).Where("logs.type NOT IN ?", adminOnlyLogTypes))
	if err != nil {
		return nil, 0, err
	}
	err = tx.Model(&Log{}).Limit(logSearchCountLimit).Count(&total).Error
	if err != nil {
		common.SysError(common.LogText("failed to count user logs: %s", err.Error()))
		return nil, 0, common.NewMessage("Failed to query logs")
	}
	order := "logs.id desc"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = clickHouseLogOrder("logs.")
	}
	err = tx.Order(order).Limit(num).Offset(startIdx).Find(&logs).Error
	if err != nil {
		common.SysError(common.LogText("failed to search user logs: %s", err.Error()))
		return nil, 0, common.NewMessage("Failed to query logs")
	}

	formatUserLogs(logs, startIdx)
	return logs, total, err
}

// StreamAllLogs calls fn with each log matching filter, newest first, as an
// administrator with role sees it. Rows are read one at a time, so an export
// never holds the whole result in memory.
func StreamAllLogs(ctx context.Context, filter LogFilter, role int, fn func(*Log) error) error {
	tx, err := filter.apply(LOG_DB.WithContext(ctx))
	if err != nil {
		return err
	}
	visibility := logOtherVisibilityAdmin
	if role >= common.RoleRootUser {
		visibility = logOtherVisibilityRoot
	}
	channelNames := make(map[int]string)
	return streamLogs(tx, "logs.created_at desc, logs.id desc", func(log *Log) error {
		if log.ChannelId != 0 {
			name, ok := channelNames[log.ChannelId]
			if !ok {
				if channel, err := CacheGetChannel(log.ChannelId); err == nil {
					name = channel.Name
				}
				channelNames[log.ChannelId] = name
			}
			log.ChannelName = name
		}
		log.Other = formatLogOtherJSON(log.Other, visibility)
		return fn(log)
	})
}

// StreamUserLogs is StreamAllLogs for one user's own logs, shaped like the
// user log list.
func StreamUserLogs(ctx context.Context, userId int, filter LogFilter, fn func(*Log) error) error {
	tx, err := filter.apply(LOG_DB.WithContext(ctx).Where("logs.user_id = ?", userId).Where("logs.type NOT IN ?", adminOnlyLogTypes))
	if err != nil {
		return err
	}
	count := 0
	return streamLogs(tx, "logs.id desc", func(log *Log) error {
		count++
		log.Id = count
		log.ChannelName = ""
		log.Other = formatLogOtherJSON(log.Other, logOtherVisibilityUser)
		return fn(log)
	})
}

func streamLogs(tx *gorm.DB, order string, fn func(*Log) error) error {
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = clickHouseLogOrder("logs.")
	}
	rows, err := tx.Model(&Log{}).Order(order).Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var log Log
		if err := LOG_DB.ScanRows(rows, &log); err != nil {
			return err
		}
		if err := fn(&log); err != nil {
			return err
		}
	}
	return rows.Err()
}

type Stat struct {
	Quota int `json:"quota"`
	Rpm   int `json:"rpm"`
	Tpm   int `json:"tpm"`
}

func SumUsedQuota(logType int, startTimestamp int64, endTimestamp int64, modelName LogFieldFilter, username LogFieldFilter, tokenName LogFieldFilter, channel LogChannelFilter, group LogFieldFilter) (stat Stat, err error) {
	tx := LOG_DB.Table("logs").Select("COALESCE(sum(quota), 0) quota")

	// 为rpm和tpm创建单独的查询
	rpmTpmQuery := LOG_DB.Table("logs").Select("count(*) rpm, COALESCE(sum(prompt_tokens), 0) + COALESCE(sum(completion_tokens), 0) tpm")

	if tx, err = applyLogFieldFilter(tx, "username", username, true); err != nil {
		return stat, err
	}
	if rpmTpmQuery, err = applyLogFieldFilter(rpmTpmQuery, "username", username, true); err != nil {
		return stat, err
	}
	if tx, err = applyLogFieldFilter(tx, "token_name", tokenName, false); err != nil {
		return stat, err
	}
	if rpmTpmQuery, err = applyLogFieldFilter(rpmTpmQuery, "token_name", tokenName, false); err != nil {
		return stat, err
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if tx, err = applyLogFieldFilter(tx, "model_name", modelName, true); err != nil {
		return stat, err
	}
	if rpmTpmQuery, err = applyLogFieldFilter(rpmTpmQuery, "model_name", modelName, true); err != nil {
		return stat, err
	}
	tx = applyLogChannelFilter(tx, "channel_id", channel)
	rpmTpmQuery = applyLogChannelFilter(rpmTpmQuery, "channel_id", channel)
	if tx, err = applyLogFieldFilter(tx, logGroupCol, group, false); err != nil {
		return stat, err
	}
	if rpmTpmQuery, err = applyLogFieldFilter(rpmTpmQuery, logGroupCol, group, false); err != nil {
		return stat, err
	}

	tx = tx.Where("type = ?", LogTypeConsume)
	rpmTpmQuery = rpmTpmQuery.Where("type = ?", LogTypeConsume)

	// 只统计最近60秒的rpm和tpm
	rpmTpmQuery = rpmTpmQuery.Where("created_at >= ?", time.Now().Add(-60*time.Second).Unix())

	// 执行查询
	if err := tx.Scan(&stat).Error; err != nil {
		common.SysError(common.LogText("failed to query log stat: %s", err.Error()))
		return stat, common.NewMessage("Failed to query statistics")
	}
	var rateStat struct {
		Rpm int
		Tpm int
	}
	if err := rpmTpmQuery.Scan(&rateStat).Error; err != nil {
		common.SysError(common.LogText("failed to query rpm/tpm stat: %s", err.Error()))
		return stat, common.NewMessage("Failed to query statistics")
	}
	stat.Rpm = rateStat.Rpm
	stat.Tpm = rateStat.Tpm

	return stat, nil
}

func SumUsedToken(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string) (token int) {
	tx := LOG_DB.Table("logs").Select("COALESCE(sum(prompt_tokens), 0) + COALESCE(sum(completion_tokens), 0)")
	if username != "" {
		tx = tx.Where("username = ?", username)
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if modelName != "" {
		tx = tx.Where("model_name = ?", modelName)
	}
	tx.Where("type = ?", LogTypeConsume).Scan(&token)
	return token
}

func CountOldLog(ctx context.Context, targetTimestamp int64) (int64, error) {
	var total int64
	if err := LOG_DB.WithContext(ctx).Model(&Log{}).Where("created_at < ?", targetTimestamp).Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func DeleteOldLogBatch(ctx context.Context, targetTimestamp int64, limit int) (int64, error) {
	if limit <= 0 {
		limit = 100
	}
	if nil != ctx.Err() {
		return 0, ctx.Err()
	}

	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		// ClickHouse DELETE is a heavy mutation that rewrites data parts, so
		// per-batch mutations would be pathologically slow. Remove all matching
		// rows in a single synchronous mutation regardless of limit; the reported
		// count lets the caller's progress loop complete in one pass.
		total, err := CountOldLog(ctx, targetTimestamp)
		if err != nil {
			return 0, err
		}
		if total == 0 {
			return 0, nil
		}
		if err := LOG_DB.WithContext(ctx).Exec(
			"ALTER TABLE logs DELETE WHERE created_at < ? SETTINGS mutations_sync = 1",
			targetTimestamp,
		).Error; err != nil {
			return 0, err
		}
		return total, nil
	}

	result := LOG_DB.WithContext(ctx).Where("created_at < ?", targetTimestamp).Limit(limit).Delete(&Log{})
	if nil != result.Error {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}
