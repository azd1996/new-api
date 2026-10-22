package controller

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
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
}

// UpsertErrorSignatureAlias assigns (or replaces) the short name for a single
// signature. Admin only. Clearing a short name is done via delete, not an empty
// upsert, so the "uncategorized" bucket stays implicit.
func UpsertErrorSignatureAlias(c *gin.Context) {
	var req errorAliasUpsertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	req.SignatureHash = strings.TrimSpace(req.SignatureHash)
	req.ShortName = strings.TrimSpace(req.ShortName)
	if req.SignatureHash == "" {
		common.ApiErrorMsg(c, "signature_hash is required")
		return
	}
	if req.ShortName == "" {
		common.ApiErrorMsg(c, "short_name is required")
		return
	}
	if err := model.UpsertErrorSignatureAlias(req.SignatureHash, req.Signature, req.ShortName); err != nil {
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

type errorStatItem struct {
	Signature     string                  `json:"signature"`
	SignatureHash string                  `json:"signature_hash"`
	ShortName     string                  `json:"short_name"`
	Count         int64                   `json:"count"`
	FirstSeen     int64                   `json:"first_seen"`
	LastSeen      int64                   `json:"last_seen"`
	StatusCode    int                     `json:"status_code"`
	Models        []errorStatModelCount   `json:"models"`
	Channels      []errorStatChannelCount `json:"channels"`
}

// GetErrorStats aggregates error logs in the requested window by normalized
// error signature ("明细错误"), attaching the admin-configured short name plus
// per-model and per-channel breakdowns. Admin only.
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
	}

	stats, err := model.GetErrorSignatureStats(filter, limit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	buildErrorStatsResponse(c, filter, stats)
}

// buildErrorStatsResponse enriches the headline signature rows with short names
// and model/channel breakdowns, then writes the API response.
func buildErrorStatsResponse(c *gin.Context, filter model.ErrorStatFilter, stats []*model.ErrorSignatureStat) {
	hashes := make([]string, 0, len(stats))
	for _, s := range stats {
		hashes = append(hashes, s.SignatureHash)
	}

	modelRows, err := model.GetErrorSignatureModelBreakdown(filter, hashes)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	channelRows, err := model.GetErrorSignatureChannelBreakdown(filter, hashes)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	modelsByHash := make(map[string][]errorStatModelCount)
	for _, r := range modelRows {
		modelsByHash[r.SignatureHash] = append(modelsByHash[r.SignatureHash], errorStatModelCount{Model: r.ModelName, Count: r.Count})
	}
	channelsByHash := make(map[string][]errorStatChannelCount)
	channelIdSet := make(map[int]struct{})
	for _, r := range channelRows {
		channelsByHash[r.SignatureHash] = append(channelsByHash[r.SignatureHash], errorStatChannelCount{ChannelId: r.ChannelId, Count: r.Count})
		channelIdSet[r.ChannelId] = struct{}{}
	}
	channelNames := resolveChannelNames(channelIdSet)

	items := make([]errorStatItem, 0, len(stats))
	for _, s := range stats {
		statusCode := 0
		if m := errorStatusFromSignaturePattern.FindStringSubmatch(s.Signature); m != nil {
			statusCode, _ = strconv.Atoi(m[1])
		}
		channels := channelsByHash[s.SignatureHash]
		for i := range channels {
			channels[i].ChannelName = channelNames[channels[i].ChannelId]
		}
		items = append(items, errorStatItem{
			Signature:     s.Signature,
			SignatureHash: s.SignatureHash,
			ShortName:     model.ResolveErrorShortName(s.SignatureHash),
			Count:         s.Count,
			FirstSeen:     s.FirstSeen,
			LastSeen:      s.LastSeen,
			StatusCode:    statusCode,
			Models:        modelsByHash[s.SignatureHash],
			Channels:      channels,
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
