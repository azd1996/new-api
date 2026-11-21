package controller

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// errorSignatureRulesOptionKey is the Option row that persists the admin-edited
// normalization rule set across restarts.
const errorSignatureRulesOptionKey = "ErrorSignatureRules"

// errorStatusFromSignaturePattern pulls the leading `status_code=NNN` that
// RecordErrorLog prefixes onto every error message, so the stats row can expose
// the HTTP status without a second query.
var errorStatusFromSignaturePattern = regexp.MustCompile(`status_code=(\d+)`)

// GetErrorSignatureAliases returns every configured signature → short-name
// mapping. Admin only.
func GetErrorSignatureAliases(c *gin.Context) {
	aliases, err := model.GetAllErrorSignatureAliases()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, aliases)
}

type errorAliasUpsertRequest struct {
	SignatureHash string `json:"signature_hash"`
	Signature     string `json:"signature"`
	ShortName     string `json:"short_name"`
	Attribution   string `json:"attribution"`
	Note          string `json:"note"`
}

// UpsertErrorSignatureAlias assigns (or replaces) the short name, error
// attribution, and free-text note for a single signature. Admin only. Clearing
// all three is done via delete, not an empty upsert, so the "uncategorized"
// bucket stays implicit.
func UpsertErrorSignatureAlias(c *gin.Context) {
	var req errorAliasUpsertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	req.SignatureHash = strings.TrimSpace(req.SignatureHash)
	req.ShortName = strings.TrimSpace(req.ShortName)
	req.Attribution = strings.TrimSpace(req.Attribution)
	req.Note = strings.TrimSpace(req.Note)
	if req.SignatureHash == "" {
		common.ApiErrorMsg(c, "signature_hash is required")
		return
	}
	if req.ShortName == "" && req.Attribution == "" && req.Note == "" {
		common.ApiErrorMsg(c, "short_name, attribution or note is required")
		return
	}
	if err := model.UpsertErrorSignatureAlias(req.SignatureHash, req.Signature, req.ShortName, req.Attribution, req.Note); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// DeleteErrorSignatureAlias removes the mapping for a signature hash, reverting
// it to "uncategorized". Admin only.
func DeleteErrorSignatureAlias(c *gin.Context) {
	hash := strings.TrimSpace(c.Param("hash"))
	if hash == "" {
		common.ApiErrorMsg(c, "signature hash is required")
		return
	}
	if err := model.DeleteErrorSignatureAlias(hash); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

type errorStatModelCount struct {
	Model string `json:"model"`
	Count int64  `json:"count"`
}

type errorStatChannelCount struct {
	ChannelId   int    `json:"channel_id"`
	ChannelName string `json:"channel_name"`
	Count       int64  `json:"count"`
}

type errorStatCustomerCount struct {
	CustomerId   string `json:"customer_id"`
	CustomerName string `json:"customer_name"`
	Count        int64  `json:"count"`
}

type errorStatItem struct {
	GroupKey      string                   `json:"group_key"`
	GroupLabel    string                   `json:"group_label"`
	Signature     string                   `json:"signature"`
	SignatureHash string                   `json:"signature_hash"`
	ShortName     string                   `json:"short_name"`
	Attribution   string                   `json:"attribution"`
	Note          string                   `json:"note"`
	Count         int64                    `json:"count"`
	FirstSeen     int64                    `json:"first_seen"`
	LastSeen      int64                    `json:"last_seen"`
	StatusCode    int                      `json:"status_code"`
	Models        []errorStatModelCount    `json:"models"`
	Channels      []errorStatChannelCount  `json:"channels"`
	Customers     []errorStatCustomerCount `json:"customers"`
}

// GetErrorStats aggregates error logs in the requested window by the chosen
// dimension — 明细错误 (signature), 明细错误类型 (short_name), or 错误归因
// (attribution) — attaching per-model and per-channel breakdowns. Admin only.
func GetErrorStats(c *gin.Context) {
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	statusCode, _ := strconv.Atoi(c.Query("status_code"))
	channel, _ := strconv.Atoi(c.Query("channel"))
	limit, _ := strconv.Atoi(c.Query("limit"))
	filter := model.ErrorStatFilter{
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
		StatusCode:     statusCode,
		ModelName:      c.Query("model_name"),
		Channel:        channel,
		Group:          c.Query("group"),
		CustomerId:     c.Query("customer_id"),
		CustomerName:   c.Query("customer_name"),
		ShortName:      c.Query("short_name"),
		Attribution:    c.Query("attribution"),
	}

	groupBy := c.Query("group_by")
	switch groupBy {
	case model.ErrorStatGroupByShortName, model.ErrorStatGroupByAttribution:
		// use as-is
	default:
		groupBy = model.ErrorStatGroupBySignature
	}

	stats, err := model.GetErrorStatsGrouped(filter, groupBy, limit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	buildErrorStatsResponse(c, stats)
}

// buildErrorStatsResponse enriches the aggregated buckets with sorted
// model/channel breakdowns, then writes the API response.
func buildErrorStatsResponse(c *gin.Context, stats []*model.ErrorGroupStat) {
	channelIdSet := make(map[int]struct{})
	for _, s := range stats {
		for id := range s.Channels {
			channelIdSet[id] = struct{}{}
		}
	}
	channelNames := resolveChannelNames(channelIdSet)

	items := make([]errorStatItem, 0, len(stats))
	for _, s := range stats {
		statusCode := 0
		if m := errorStatusFromSignaturePattern.FindStringSubmatch(s.Signature); m != nil {
			statusCode, _ = strconv.Atoi(m[1])
		}
		models := make([]errorStatModelCount, 0, len(s.Models))
		for name, cnt := range s.Models {
			models = append(models, errorStatModelCount{Model: name, Count: cnt})
		}
		sort.Slice(models, func(i, j int) bool { return models[i].Count > models[j].Count })
		channels := make([]errorStatChannelCount, 0, len(s.Channels))
		for id, cnt := range s.Channels {
			channels = append(channels, errorStatChannelCount{ChannelId: id, ChannelName: channelNames[id], Count: cnt})
		}
		sort.Slice(channels, func(i, j int) bool { return channels[i].Count > channels[j].Count })
		customers := make([]errorStatCustomerCount, 0, len(s.Customers))
		for id, cnt := range s.Customers {
			customers = append(customers, errorStatCustomerCount{CustomerId: id, CustomerName: model.CustomerName(id), Count: cnt})
		}
		sort.Slice(customers, func(i, j int) bool { return customers[i].Count > customers[j].Count })
		items = append(items, errorStatItem{
			GroupKey:      s.GroupKey,
			GroupLabel:    s.GroupLabel,
			Signature:     s.Signature,
			SignatureHash: s.SignatureHash,
			ShortName:     s.ShortName,
			Attribution:   s.Attribution,
			Note:          s.Note,
			Count:         s.Count,
			FirstSeen:     s.FirstSeen,
			LastSeen:      s.LastSeen,
			StatusCode:    statusCode,
			Models:        models,
			Channels:      channels,
			Customers:     customers,
		})
	}
	common.ApiSuccess(c, items)
}

func resolveChannelNames(idSet map[int]struct{}) map[int]string {
	names := make(map[int]string, len(idSet))
	if len(idSet) == 0 {
		return names
	}
	ids := make([]int, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	channels, err := model.GetChannelsByIds(ids)
	if err != nil {
		return names
	}
	for _, ch := range channels {
		if ch.Name != "" {
			names[ch.Id] = ch.Name
		}
	}
	return names
}

// GetErrorSignatureRules returns the normalization rule set currently in effect
// (admin-configured, or the seeded defaults). Admin only.
func GetErrorSignatureRules(c *gin.Context) {
	common.ApiSuccess(c, service.GetEffectiveErrorSignatureRules())
}

type updateErrorSignatureRulesRequest struct {
	Rules []service.SignatureRule `json:"rules"`
}

// UpdateErrorSignatureRules validates, applies, and persists a new normalization
// rule set. Applying first (via SetErrorSignatureRules) rejects any uncompilable
// pattern before anything is written, so a bad edit can never break signature
// generation. Admin only. Takes effect for errors logged after this call.
func UpdateErrorSignatureRules(c *gin.Context) {
	var req updateErrorSignatureRulesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	if err := service.SetErrorSignatureRules(req.Rules); err != nil {
		common.ApiErrorMsg(c, "invalid rule pattern: "+err.Error())
		return
	}
	payload, err := common.Marshal(req.Rules)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.UpdateOption(errorSignatureRulesOptionKey, string(payload)); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, service.GetEffectiveErrorSignatureRules())
}

// GetErrorSignatureRuleSuggestions samples the distinct error signatures in the
// requested window and returns advisory regex rules for residual variable
// tokens still present after normalization, for the admin to copy or tweak.
// Admin only.
func GetErrorSignatureRuleSuggestions(c *gin.Context) {
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	statusCode, _ := strconv.Atoi(c.Query("status_code"))
	channel, _ := strconv.Atoi(c.Query("channel"))
	filter := model.ErrorStatFilter{
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
		StatusCode:     statusCode,
		ModelName:      c.Query("model_name"),
		Channel:        channel,
		Group:          c.Query("group"),
		CustomerId:     c.Query("customer_id"),
		CustomerName:   c.Query("customer_name"),
		ShortName:      c.Query("short_name"),
		Attribution:    c.Query("attribution"),
	}
	stats, err := model.GetErrorSignatureStats(filter, 500)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	signatures := make([]string, 0, len(stats))
	for _, s := range stats {
		if s.Signature != "" {
			signatures = append(signatures, s.Signature)
		}
	}
	common.ApiSuccess(c, service.SuggestErrorSignatureRules(signatures))
}

// aiGeneratedRuleRequest identifies the signature to generate a rule for and
// carries the attribution options offered by the frontend, so the model picks
// from the same values the admin can choose in the rules editor.
type aiGeneratedRuleRequest struct {
	SignatureHash      string   `json:"signature_hash"`
	Signature          string   `json:"signature"`
	AttributionOptions []string `json:"attribution_options"`
}

// aiGeneratedRule is the advisory result returned to the frontend. The rule is
// not persisted until the admin confirms it in the rules editor.
type aiGeneratedRule struct {
	Rule          service.SignatureRule `json:"rule"`
	Samples       []string              `json:"samples"`
	MatchedSample bool                  `json:"matched_sample"`
}

// aiRuleAttributionOptions is the fallback attribution vocabulary when the
// frontend does not send one. It mirrors the standard set seeded in the UI.
var aiRuleAttributionOptions = []string{
	"客户端问题-不可解决",
	"客户端问题-可解决",
	"源站问题",
	"上游网关问题",
}

const aiRuleSystemPrompt = "You are a reliability engineer writing classification rules for an AI API gateway's detailed error statistics. You output only a JSON object, with no markdown fences and no explanations."

// buildAIRulePrompt describes the rule contract and the observed errors to the
// assistant. Names use kebab-case because the rule name doubles as the stable
// detailed-error type key; attribution values are the admin's configured set.
func buildAIRulePrompt(req aiGeneratedRuleRequest, samples, existingRuleNames []string) string {
	attributions := req.AttributionOptions
	if len(attributions) == 0 {
		attributions = aiRuleAttributionOptions
	}

	var b strings.Builder
	b.WriteString("The gateway classifies every upstream error with ordered rules; the first matching enabled rule wins. Generate ONE rule that matches the error class below.\n\n")
	b.WriteString("Rule fields:\n")
	b.WriteString("- name: a short, stable kebab-case class name (also the displayed detailed error type), e.g. bedrock-invalid-thinking-signature.\n")
	b.WriteString("- pattern: a Go RE2 regular expression matched UNANCHORED against the complete raw error string.\n")
	b.WriteString("- signature: the stable normalized display signature. Use fixed text, or a template referencing capture groups like ${1}. It must not retain dynamic IDs, numbers, or timestamps.\n")
	b.WriteString("- attribution: the single best option from this list: ")
	b.WriteString(strings.Join(attributions, ", "))
	b.WriteString(". Use an empty string only if the choice is genuinely ambiguous.\n")
	b.WriteString("- note: a concise English explanation of why this error class exists, or an empty string.\n")
	b.WriteString("- enabled: true.\n\n")
	b.WriteString("Pattern requirements: RE2 only (no lookahead, lookbehind, or backreferences); do not anchor with ^ or $; escape regex metacharacters; match a distinctive stable fragment rather than the whole message; ignore dynamic prefixes such as status_code=..., request ids, timestamps, and account or model names.\n\n")
	b.WriteString("Currently normalized signature:\n")
	b.WriteString(req.Signature)
	b.WriteString("\n\nRaw error samples (newest first):\n")
	for i, sample := range samples {
		fmt.Fprintf(&b, "%d. %s\n", i+1, sample)
	}
	if len(existingRuleNames) > 0 {
		b.WriteString("\nExisting rule names (do not duplicate them):\n")
		b.WriteString(strings.Join(existingRuleNames, ", "))
		b.WriteString("\n")
	}
	b.WriteString("\nReturn exactly this JSON shape and nothing else: {\"name\":\"...\",\"pattern\":\"...\",\"signature\":\"...\",\"attribution\":\"...\",\"note\":\"...\",\"enabled\":true}")
	return b.String()
}

// parseLLMRuleJSON extracts the rule JSON object from an assistant response.
// Models often wrap JSON in markdown fences or prose despite the instructions,
// so locate the first '{' through the last '}' before unmarshalling. The
// pattern is compiled here so an uncompilable suggestion is rejected before it
// can reach the rules editor.
func parseLLMRuleJSON(content string) (service.SignatureRule, error) {
	text := strings.TrimSpace(content)
	if start := strings.Index(text, "{"); start >= 0 {
		if end := strings.LastIndex(text, "}"); end > start {
			text = text[start : end+1]
		}
	}
	var rule service.SignatureRule
	if err := common.Unmarshal([]byte(text), &rule); err != nil {
		return rule, fmt.Errorf("AI assistant returned an unreadable rule: %w", err)
	}
	if strings.TrimSpace(rule.Name) == "" || strings.TrimSpace(rule.Pattern) == "" {
		return rule, errors.New("AI assistant returned a rule without a name or pattern")
	}
	if strings.TrimSpace(rule.Signature) == "" {
		return rule, errors.New("AI assistant returned a rule without a normalized signature")
	}
	if _, err := regexp.Compile(rule.Pattern); err != nil {
		return rule, fmt.Errorf("AI assistant returned an invalid pattern: %w", err)
	}
	rule.Enabled = true
	return rule, nil
}

// GenerateErrorSignatureRule uses the AI assistant to propose one
// normalization rule for a detailed-error signature. It samples the raw error
// contents stored under the signature hash (falling back to the normalized
// signature for logs predating the hash column), then validates the returned
// rule and reports whether its pattern matches one of the samples. Admin only;
// the rule is not saved — the admin reviews it and adds it via the rules
// editor.
func GenerateErrorSignatureRule(c *gin.Context) {
	var req aiGeneratedRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	req.SignatureHash = strings.TrimSpace(req.SignatureHash)
	if req.SignatureHash == "" {
		common.ApiErrorMsg(c, "signature_hash is required")
		return
	}
	req.Signature = strings.TrimSpace(req.Signature)

	samples, err := model.GetErrorSignatureExamples(req.SignatureHash, 3)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if len(samples) == 0 {
		if req.Signature == "" {
			common.ApiErrorMsg(c, "no error samples found for this signature")
			return
		}
		// Logs written before the signature-hash column existed carry the
		// normalized signature only; it is still a usable starting point.
		samples = []string{req.Signature}
	}

	existingRules := service.GetEffectiveErrorSignatureRules()
	existingNames := make([]string, 0, len(existingRules))
	for _, rule := range existingRules {
		if name := strings.TrimSpace(rule.Name); name != "" {
			existingNames = append(existingNames, name)
		}
	}

	response, err := LLMChat(c.Request.Context(), common.GetContextKeyInt(c, constant.ContextKeyUserId), LLMChatRequest{
		Messages: []LLMChatMessage{
			{Role: "system", Content: aiRuleSystemPrompt},
			{Role: "user", Content: buildAIRulePrompt(req, samples, existingNames)},
		},
	})
	if err != nil {
		common.ApiErrorMsg(c, "AI assistant failed: "+err.Error())
		return
	}
	rule, err := parseLLMRuleJSON(response.Content)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	matched := false
	if re, compileErr := regexp.Compile(rule.Pattern); compileErr == nil {
		for _, sample := range samples {
			if re.MatchString(sample) {
				matched = true
				break
			}
		}
	}
	common.ApiSuccess(c, aiGeneratedRule{Rule: rule, Samples: samples, MatchedSample: matched})
}
