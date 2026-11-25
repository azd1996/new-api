package constant

type ContextKey string

const (
	ContextKeyTokenCountMeta  ContextKey = "token_count_meta"
	ContextKeyPromptTokens    ContextKey = "prompt_tokens"
	ContextKeyEstimatedTokens ContextKey = "estimated_tokens"

	ContextKeyOriginalModel        ContextKey = "original_model"
	ContextKeyRequestStartTime     ContextKey = "request_start_time"
	ContextKeyClaudeMetadataUserID ContextKey = "claude_metadata_user_id"
	// ContextKeyClaudeMetadataUserIDSource labels which session identity
	// source tier produced the pre-computed metadata.user_id candidate.
	ContextKeyClaudeMetadataUserIDSource ContextKey = "claude_metadata_user_id_source"
	// ContextKeyExtractedSessionID holds a custom-extracted session identity
	// computed before affinity matching; channel affinity uses it as a key.
	ContextKeyExtractedSessionID ContextKey = "extracted_session_id"
	// ContextKeyExtractedSessionIDSource labels where the extracted identity
	// came from (custom:<source> or header).
	ContextKeyExtractedSessionIDSource ContextKey = "extracted_session_id_source"

	/* token related keys */
	ContextKeyTokenUnlimited         ContextKey = "token_unlimited_quota"
	ContextKeyTokenKey               ContextKey = "token_key"
	ContextKeyTokenId                ContextKey = "token_id"
	ContextKeyTokenGroup             ContextKey = "token_group"
	ContextKeyOriginTasks            ContextKey = "origin_tasks"
	ContextKeyChannelConstraints     ContextKey = "channel_constraints"
	ContextKeyTokenModelLimitEnabled ContextKey = "token_model_limit_enabled"
	ContextKeyTokenModelLimit        ContextKey = "token_model_limit"
	ContextKeyTokenCrossGroupRetry   ContextKey = "token_cross_group_retry"
	ContextKeyTokenAutoGroups        ContextKey = "token_auto_groups"
	// ContextKeyTokenCustomerID holds the customer id bound to the API token
	// by an administrator. It is the fallback when no trace-tag customer can
	// be resolved (e.g. this gateway runs in front of the unified gateway).
	ContextKeyTokenCustomerID ContextKey = "token_customer_id"

	/* channel related keys */
	ContextKeyChannelId                ContextKey = "channel_id"
	ContextKeyChannelName              ContextKey = "channel_name"
	ContextKeyChannelCreateTime        ContextKey = "channel_create_time"
	ContextKeyChannelBaseUrl           ContextKey = "base_url"
	ContextKeyChannelType              ContextKey = "channel_type"
	ContextKeyChannelSetting           ContextKey = "channel_setting"
	ContextKeyChannelOtherSetting      ContextKey = "channel_other_setting"
	ContextKeyChannelParamOverride     ContextKey = "param_override"
	ContextKeyChannelHeaderOverride    ContextKey = "header_override"
	ContextKeyChannelOrganization      ContextKey = "channel_organization"
	ContextKeyChannelAutoBan           ContextKey = "auto_ban"
	ContextKeyChannelModelMapping      ContextKey = "model_mapping"
	ContextKeyChannelStatusCodeMapping ContextKey = "status_code_mapping"
	ContextKeyChannelIsMultiKey        ContextKey = "channel_is_multi_key"
	ContextKeyChannelMultiKeyIndex     ContextKey = "channel_multi_key_index"
	ContextKeyChannelKey               ContextKey = "channel_key"

	// ContextKeyUpstreamRequestOrigin is the scheme and host of the last upstream
	// request, which differs from the base URL for absolute Advanced Custom routes
	// and for adaptors that build official endpoints themselves.
	ContextKeyUpstreamRequestOrigin ContextKey = "upstream_request_origin"

	ContextKeyAutoGroup           ContextKey = "auto_group"
	ContextKeyAutoGroupIndex      ContextKey = "auto_group_index"
	ContextKeyAutoGroupRetryIndex ContextKey = "auto_group_retry_index"

	/* user related keys */
	ContextKeyUserId      ContextKey = "id"
	ContextKeyUserSetting ContextKey = "user_setting"
	ContextKeyUserQuota   ContextKey = "user_quota"
	ContextKeyUserStatus  ContextKey = "user_status"
	ContextKeyUserEmail   ContextKey = "user_email"
	ContextKeyUserGroup   ContextKey = "user_group"
	ContextKeyUsingGroup  ContextKey = "group"
	// ContextKeySessionID stores the request's resolved session identifier for
	// the consume/error log (dual-written to the real session_id column).
	ContextKeySessionID ContextKey = "session_id"
	// ContextKeySessionIDSource labels where the session id came from.
	ContextKeySessionIDSource ContextKey = "session_id_source"
	ContextKeyUserName        ContextKey = "username"

	ContextKeyLocalCountTokens ContextKey = "local_count_tokens"

	ContextKeySystemPromptOverride ContextKey = "system_prompt_override"

	// ContextKeyFileSourcesToCleanup stores file sources that need cleanup when request ends
	ContextKeyFileSourcesToCleanup ContextKey = "file_sources_to_cleanup"

	// ContextKeyAdminRejectReason stores an admin-only reject/block reason extracted from upstream responses.
	// It is not returned to end users, but can be persisted into consume/error logs for debugging.
	ContextKeyAdminRejectReason ContextKey = "admin_reject_reason"

	// ContextKeyLanguage stores the user's language preference for i18n
	ContextKeyLanguage             ContextKey = "language"
	ContextKeyIsStream             ContextKey = "is_stream"
	ContextKeyResponseStreamStatus ContextKey = "response_stream_status"

	// ContextKeyAuditLogged marks that the current request has already recorded
	// a manage/operation audit log inside the handler. When set, the admin-audit
	// fallback in authHelper (finishAdminAudit) skips its record to avoid
	// duplicate entries.
	ContextKeyAuditLogged ContextKey = "audit_logged"

	// ContextKeyTokenAuditParams contains only the API token operation's safe metadata.
	ContextKeyTokenAuditParams ContextKey = "token_audit_params"
	// ContextKeyTokenAuditSucceeded disambiguates token responses that exceed the audit buffer.
	ContextKeyTokenAuditSucceeded ContextKey = "token_audit_succeeded"

	// ContextKeyClaudeCacheOptimizationPreviewOverride carries an explicit
	// Claude cache-optimization configuration for the admin full-link preview,
	// which cannot derive it from the customer cache the way production does.
	ContextKeyClaudeCacheOptimizationPreviewOverride ContextKey = "claude_cache_optimization_preview_override"
)
