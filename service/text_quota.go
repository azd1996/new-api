package service

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	hosttypes "github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// ToolSurchargeItem is one billable tool-call line for consume logs.
type ToolSurchargeItem struct {
	Name  string  `json:"name"`
	Count int     `json:"count"`
	Price float64 `json:"price"`
}

func appendToolSurchargeLogInfo(other *model.LogOther, items []ToolSurchargeItem) {
	if len(items) == 0 {
		return
	}
	other.SetPublic("tool_surcharges", items)
}

type textQuotaSummary struct {
	PromptTokens           int
	CompletionTokens       int
	TotalTokens            int
	CacheTokens            int
	CacheCreationTokens    int
	CacheCreationTokens5m  int
	CacheCreationTokens1h  int
	CacheCreationTokens12h int
	ImageTokens            int
	AudioTokens            int
	ModelName              string
	TokenName              string
	UseTimeSeconds         int64
	CompletionRatio        float64
	CacheRatio             float64
	ImageRatio             float64
	ModelRatio             float64
	GroupRatio             float64
	ModelPrice             float64
	CacheCreationRatio     float64
	CacheCreationRatio5m   float64
	CacheCreationRatio1h   float64
	CacheCreationRatio12h  float64
	Quota                  int
	// RawQuota, StatQuota and CustomerStatQuota are reporting-only charges
	// derived from the same base as Quota but at a different group ratio: RawQuota
	// uses group ratio 1 (the undiscounted "list" amount), StatQuota uses the
	// channel StatGroupRatio, and CustomerStatQuota uses the resolved customer
	// discount. They NEVER affect actual billing (never feed
	// UpdateUserUsedQuotaAndRequestCount / UpdateChannelUsedQuota / SettleBilling /
	// Log.Quota); they exist so per-channel / per-customer consumption stays
	// visible even when the billing group ratio is 0 (free pass-through channels).
	// See finalizeTextQuotaForGroupRatio.
	RawQuota       int
	StatQuota      int
	StatGroupRatio float64
	// CustomerDiscount is the per-(customer, model) statistics discount resolved
	// from the decoded trace-tag customer id; CustomerStatQuota is the amount
	// scaled by it. HasCustomer is true only when a customer id was decoded, so
	// the amount is recorded only for identified downstream customers.
	// CustomerDiscountFallback / CustomerMatchedPrefix carry resolution
	// provenance. All reporting-only (never billed).
	CustomerDiscount         float64
	CustomerDiscountFallback bool
	CustomerMatchedPrefix    string
	CustomerStatQuota        int
	HasCustomer              bool
	IsClaudeUsageSemantic    bool
	UsageSemantic            string
	AudioInputPrice          float64
	ToolSurchargeItems       []ToolSurchargeItem
	ToolCallSurchargeQuota   decimal.Decimal
	FixedPriceBilling        bool
}

// hasBillableUsage reports whether this request should incur any charge.
// Claude reports cache tokens separately from ordinary input and output.
// A request can carry zero ordinary tokens yet still be billable via cache or a tool-call
// surcharge (e.g. /v1/alpha/search returns no usage but bills one web_search
// call), so token count alone is not sufficient to decide.
func (s *textQuotaSummary) hasBillableUsage() bool {
	return s.FixedPriceBilling || s.TotalTokens > 0 ||
		(s.IsClaudeUsageSemantic && (s.CacheTokens > 0 || s.CacheCreationTokens > 0 ||
			s.CacheCreationTokens5m > 0 || s.CacheCreationTokens1h > 0)) ||
		!s.ToolCallSurchargeQuota.IsZero()
}

func cacheWriteTokensTotal(summary textQuotaSummary) int {
	if summary.CacheCreationTokens5m > 0 || summary.CacheCreationTokens1h > 0 {
		splitCacheWriteTokens := summary.CacheCreationTokens5m + summary.CacheCreationTokens1h
		if summary.CacheCreationTokens > splitCacheWriteTokens {
			return summary.CacheCreationTokens
		}
		return splitCacheWriteTokens
	}
	return summary.CacheCreationTokens
}

func isLegacyClaudeDerivedOpenAIUsage(relayInfo *relaycommon.RelayInfo, usage *dto.Usage) bool {
	if relayInfo == nil || usage == nil {
		return false
	}
	if relayInfo.GetFinalRequestRelayFormat() == types.RelayFormatClaude {
		return false
	}
	if usage.UsageSource != "" || usage.UsageSemantic != "" {
		return false
	}
	return usage.ClaudeCacheCreation5mTokens > 0 || usage.ClaudeCacheCreation1hTokens > 0
}

func collectToolSurchargeItem(items []ToolSurchargeItem, name string, count int, modelName string) []ToolSurchargeItem {
	if count <= 0 {
		return items
	}
	price := operation_setting.GetToolPriceForModel(name, modelName)
	if price <= 0 || math.IsNaN(price) || math.IsInf(price, 0) {
		return items
	}
	return append(items, ToolSurchargeItem{
		Name:  name,
		Count: count,
		Price: price,
	})
}

func mergeToolSurchargeItems(items []ToolSurchargeItem) []ToolSurchargeItem {
	if len(items) == 0 {
		return nil
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Name == items[j].Name {
			return items[i].Price < items[j].Price
		}
		return items[i].Name < items[j].Name
	})

	merged := items[:0]
	for _, item := range items {
		lastIndex := len(merged) - 1
		if lastIndex >= 0 &&
			merged[lastIndex].Name == item.Name &&
			merged[lastIndex].Price == item.Price {
			if item.Count > math.MaxInt-merged[lastIndex].Count {
				common.SysError(common.LogText("tool surcharge call count overflow for %s", item.Name))
				merged[lastIndex].Count = math.MaxInt
			} else {
				merged[lastIndex].Count += item.Count
			}
			continue
		}
		merged = append(merged, item)
	}
	return merged
}

func calculateTextToolCallSurcharge(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, summary *textQuotaSummary) decimal.Decimal {
	dGroupRatio := decimal.NewFromFloat(summary.GroupRatio)
	dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)

	var items []ToolSurchargeItem

	if relayInfo.ResponsesUsageInfo != nil {
		for name, tool := range relayInfo.ResponsesUsageInfo.BuiltInTools {
			if tool == nil {
				continue
			}
			count := tool.CallCount
			if count > relaycommon.MaxBillableToolCallCount {
				logger.LogWarn(ctx, common.LogText("tool surcharge call count clamped: tool=%s count=%d", name, count))
				count = relaycommon.MaxBillableToolCallCount
			}
			items = collectToolSurchargeItem(items, name, count, summary.ModelName)
		}
	}
	if relayInfo.RelayMode != relayconstant.RelayModeResponses &&
		strings.HasSuffix(summary.ModelName, "search-preview") {
		items = collectToolSurchargeItem(items, dto.BuildInToolWebSearchPreview, 1, summary.ModelName)
	}

	summary.ToolSurchargeItems = mergeToolSurchargeItems(items)
	var surcharge decimal.Decimal
	for _, item := range summary.ToolSurchargeItems {
		surcharge = surcharge.Add(decimal.NewFromFloat(item.Price).
			Mul(decimal.NewFromInt(int64(item.Count))).
			Div(decimal.NewFromInt(1000)).
			Mul(dGroupRatio).
			Mul(dQuotaPerUnit))
	}

	return surcharge
}

// noteQuotaClamp records the first quota saturation event onto relayInfo so it
// can later be attached to the consume/task log for admin auditing. First
// non-nil clamp wins (a single request may hit multiple conversions).
func noteQuotaClamp(relayInfo *relaycommon.RelayInfo, clamp *common.QuotaClamp) {
	if clamp == nil || relayInfo == nil {
		return
	}
	if relayInfo.QuotaClamp == nil {
		relayInfo.QuotaClamp = clamp
	}
}

func composeTieredTextQuota(relayInfo *relaycommon.RelayInfo, summary textQuotaSummary, tieredQuota int, tieredResult *billingexpr.TieredResult) int {
	if summary.ToolCallSurchargeQuota.IsZero() {
		return tieredQuota
	}

	if tieredResult != nil {
		if snap := relayInfo.TieredBillingSnapshot; snap != nil {
			quota, clamp := common.QuotaFromDecimalChecked(decimal.NewFromFloat(tieredResult.ActualQuotaBeforeGroup).
				Mul(decimal.NewFromFloat(snap.GroupRatio)).
				Add(summary.ToolCallSurchargeQuota))
			noteQuotaClamp(relayInfo, clamp)
			return quota
		}
	}

	// Saturate the final sum, not just the surcharge: tieredQuota can be near
	// MaxQuota and adding the surcharge could push the total past the
	// single-request quota policy bound.
	total, clamp := common.QuotaFromDecimalChecked(
		decimal.NewFromInt(int64(tieredQuota)).Add(summary.ToolCallSurchargeQuota),
	)
	noteQuotaClamp(relayInfo, clamp)
	return total
}

// finalizeTextQuotaForGroupRatio reproduces the billed text-quota math for an
// arbitrary group ratio from the already-assembled, pre-group-ratio decimals:
// scaledBase is the per-request charge before the group ratio and the configured
// other-ratios, and surchargeUnit is the tool-call surcharge before the group
// ratio. Called with the real group ratio it returns the billed quota
// byte-for-byte; called with 1 / StatGroupRatio / CustomerDiscount it returns
// the report-only calibers. The other-ratios application, the ratio-branch
// minimum-one-point clamp and the zero-usage / minimum-one guards all match the
// billed path exactly, so no caliber can change the deducted quota.
func finalizeTextQuotaForGroupRatio(summary *textQuotaSummary, priceData *hosttypes.PriceData, scaledBase, surchargeUnit decimal.Decimal, groupRatio float64, isRatioBranch bool) (int, *common.QuotaClamp) {
	dGroupRatio := decimal.NewFromFloat(groupRatio)
	calc := priceData.ApplyOtherRatiosToDecimal(scaledBase.Mul(dGroupRatio))
	calc = calc.Add(surchargeUnit.Mul(dGroupRatio))
	ratio := decimal.NewFromFloat(summary.ModelRatio).Mul(dGroupRatio)
	if isRatioBranch && !ratio.IsZero() && calc.LessThanOrEqual(decimal.Zero) {
		calc = decimal.NewFromInt(1)
	}
	quota, clamp := common.QuotaFromDecimalChecked(calc)
	if !summary.hasBillableUsage() {
		quota = 0
	} else if !ratio.IsZero() && quota == 0 {
		quota = 1
	}
	return quota, clamp
}

// tieredCaliberQuota mirrors composeTieredTextQuota for a report-only caliber:
// the tiered pre-group-ratio quota scaled by the given ratio plus the billed
// tool-call surcharge. Reporting-only; it never feeds the deducted quota.
func tieredCaliberQuota(summary textQuotaSummary, tieredResult *billingexpr.TieredResult, groupRatio float64) int {
	q := decimal.NewFromFloat(tieredResult.ActualQuotaBeforeGroup).
		Mul(decimal.NewFromFloat(groupRatio)).
		Add(summary.ToolCallSurchargeQuota)
	return common.QuotaFromDecimal(q)
}

// calculateTextQuotaSummary expects a usage already remapped by
// effectiveBillingUsage; PostTextConsumeQuota performs that remap once and shares
// the result with tiered billing, affinity observation and logging.
func calculateTextQuotaSummary(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.Usage) textQuotaSummary {
	summary := textQuotaSummary{
		ModelName:             relayInfo.GetBillingModelName(),
		TokenName:             ctx.GetString("token_name"),
		UseTimeSeconds:        time.Now().Unix() - relayInfo.StartTime.Unix(),
		CompletionRatio:       relayInfo.PriceData.CompletionRatio,
		CacheRatio:            relayInfo.PriceData.CacheRatio,
		ImageRatio:            relayInfo.PriceData.ImageRatio,
		ModelRatio:            relayInfo.PriceData.ModelRatio,
		GroupRatio:            relayInfo.PriceData.GroupRatioInfo.GroupRatio,
		ModelPrice:            relayInfo.PriceData.ModelPrice,
		CacheCreationRatio:    relayInfo.PriceData.CacheCreationRatio,
		CacheCreationRatio5m:  relayInfo.PriceData.CacheCreation5mRatio,
		CacheCreationRatio1h:  relayInfo.PriceData.CacheCreation1hRatio,
		CacheCreationRatio12h: relayInfo.PriceData.CacheCreation12hRatio,
		UsageSemantic:         usageSemanticFromUsage(relayInfo, usage),
	}
	summary.IsClaudeUsageSemantic = summary.UsageSemantic == "anthropic"

	// StatGroupRatio is the channel-level statistics/discount ratio used only for
	// the reporting StatQuota. Unset or invalid falls back to 1 (StatQuota equals
	// the undiscounted RawQuota). Reporting-only; never billed.
	summary.StatGroupRatio = 1
	if relayInfo.ChannelMeta != nil {
		if r := relayInfo.ChannelSetting.StatGroupRatio; r != nil && *r > 0 && !math.IsInf(*r, 1) && !math.IsNaN(*r) {
			summary.StatGroupRatio = *r
		}
	}

	// CustomerDiscount is the per-(customer, model) statistics discount resolved
	// from the decoded trace-tag customer id (longest series prefix, else 1).
	// Reporting-only; recorded only when a customer id was actually decoded from
	// the inbound trace-tag header.
	summary.CustomerDiscount = 1
	if _, _, customerID, decoded := common.TraceTagFromContext(ctx); decoded && customerID != "" {
		summary.HasCustomer = true
		resolution := model.ResolveCustomerModelDiscount(customerID, summary.ModelName)
		summary.CustomerDiscount = resolution.Discount
		summary.CustomerDiscountFallback = resolution.Fallback
		summary.CustomerMatchedPrefix = resolution.MatchedPrefix
	}

	if usage == nil {
		usage = &dto.Usage{
			PromptTokens:     relayInfo.GetEstimatePromptTokens(),
			CompletionTokens: 0,
			TotalTokens:      relayInfo.GetEstimatePromptTokens(),
		}
	}

	summary.PromptTokens = usage.PromptTokens
	summary.CompletionTokens = usage.CompletionTokens
	summary.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	summary.CacheTokens = usage.PromptTokensDetails.CachedTokens
	summary.CacheCreationTokens = usage.PromptTokensDetails.CacheCreationTokensTotal()
	summary.CacheCreationTokens5m = usage.ClaudeCacheCreation5mTokens
	summary.CacheCreationTokens1h = usage.ClaudeCacheCreation1hTokens
	summary.ImageTokens = usage.PromptTokensDetails.ImageTokens
	summary.AudioTokens = usage.PromptTokensDetails.AudioTokens
	legacyClaudeDerived := isLegacyClaudeDerivedOpenAIUsage(relayInfo, usage)
	if !summary.IsClaudeUsageSemantic && !legacyClaudeDerived {
		summary.CacheCreationTokens12h = usage.PromptTokensDetails.CacheWrite12hTokens()
	}
	isOpenRouterClaudeBilling := relayInfo.ChannelMeta != nil &&
		relayInfo.ChannelType == constant.ChannelTypeOpenRouter &&
		summary.IsClaudeUsageSemantic

	if isOpenRouterClaudeBilling {
		summary.PromptTokens -= summary.CacheTokens
		isUsingCustomSettings := relayInfo.PriceData.UsePrice || hasCustomModelRatio(summary.ModelName, relayInfo.PriceData.ModelRatio)
		if summary.CacheCreationTokens == 0 && relayInfo.PriceData.CacheCreationRatio != 1 && usage.Cost != 0 && !isUsingCustomSettings {
			maybeCacheCreationTokens := CalcOpenRouterCacheCreateTokens(*usage, relayInfo.PriceData)
			if maybeCacheCreationTokens >= 0 && summary.PromptTokens >= maybeCacheCreationTokens {
				summary.CacheCreationTokens = maybeCacheCreationTokens
			}
		}
		summary.PromptTokens -= summary.CacheCreationTokens
	}

	dPromptTokens := decimal.NewFromInt(int64(summary.PromptTokens))
	dCacheTokens := decimal.NewFromInt(int64(summary.CacheTokens))
	dImageTokens := decimal.NewFromInt(int64(summary.ImageTokens))
	dAudioTokens := decimal.NewFromInt(int64(summary.AudioTokens))
	dCompletionTokens := decimal.NewFromInt(int64(summary.CompletionTokens))
	dCachedCreationTokens := decimal.NewFromInt(int64(summary.CacheCreationTokens))
	dCompletionRatio := decimal.NewFromFloat(summary.CompletionRatio)
	dCacheRatio := decimal.NewFromFloat(summary.CacheRatio)
	dImageRatio := decimal.NewFromFloat(summary.ImageRatio)
	dModelRatio := decimal.NewFromFloat(summary.ModelRatio)
	dModelPrice := decimal.NewFromFloat(summary.ModelPrice)
	dCacheCreationRatio := decimal.NewFromFloat(summary.CacheCreationRatio)
	dCacheCreationRatio5m := decimal.NewFromFloat(summary.CacheCreationRatio5m)
	dCacheCreationRatio1h := decimal.NewFromFloat(summary.CacheCreationRatio1h)
	dCacheCreationRatio12h := decimal.NewFromFloat(summary.CacheCreationRatio12h)
	dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)

	summary.ToolCallSurchargeQuota = calculateTextToolCallSurcharge(ctx, relayInfo, &summary)
	// surchargeUnit is the same tool-call surcharge BEFORE the group ratio, so the
	// billed quota and the report-only calibers can each scale it by their own
	// group ratio. surchargeUnit.Mul(dGroupRatio) is byte-identical to
	// summary.ToolCallSurchargeQuota (distributive over the per-item sum).
	var surchargeUnit decimal.Decimal
	for _, item := range summary.ToolSurchargeItems {
		surchargeUnit = surchargeUnit.Add(decimal.NewFromFloat(item.Price).
			Mul(decimal.NewFromInt(int64(item.Count))).
			Div(decimal.NewFromInt(1000)).
			Mul(dQuotaPerUnit))
	}

	// scaledBase is the per-request charge evaluated at group ratio = 1 and before
	// the configured other-ratios. The final charge is linear in the group ratio,
	// so the billed quota and the three reporting quotas (raw at group ratio 1,
	// stat at the channel statistics ratio, customer at the customer discount) are
	// all derived from this single base via finalizeTextQuotaForGroupRatio.
	isRatioBranch := !relayInfo.PriceData.UsePrice
	var scaledBase decimal.Decimal
	if isRatioBranch {
		baseTokens := dPromptTokens

		var cachedTokensWithRatio decimal.Decimal
		if !dCacheTokens.IsZero() {
			if !summary.IsClaudeUsageSemantic && !legacyClaudeDerived {
				baseTokens = baseTokens.Sub(dCacheTokens)
			}
			cachedTokensWithRatio = dCacheTokens.Mul(dCacheRatio)
		}

		var cachedCreationTokensWithRatio decimal.Decimal
		hasSplitCacheCreationTokens := summary.CacheCreationTokens5m > 0 || summary.CacheCreationTokens1h > 0
		if !dCachedCreationTokens.IsZero() || hasSplitCacheCreationTokens {
			if !summary.IsClaudeUsageSemantic && !legacyClaudeDerived {
				baseTokens = baseTokens.Sub(dCachedCreationTokens)
				// The total includes the 12h writes, which have their own ratio.
				dCachedCreationTokens12h := decimal.NewFromInt(int64(summary.CacheCreationTokens12h))
				cachedCreationTokensWithRatio = dCachedCreationTokens.Sub(dCachedCreationTokens12h).Mul(dCacheCreationRatio)
				cachedCreationTokensWithRatio = cachedCreationTokensWithRatio.Add(dCachedCreationTokens12h.Mul(dCacheCreationRatio12h))
			} else {
				remaining := max(summary.CacheCreationTokens-summary.CacheCreationTokens5m-summary.CacheCreationTokens1h, 0)
				cachedCreationTokensWithRatio = decimal.NewFromInt(int64(remaining)).Mul(dCacheCreationRatio)
				cachedCreationTokensWithRatio = cachedCreationTokensWithRatio.Add(decimal.NewFromInt(int64(summary.CacheCreationTokens5m)).Mul(dCacheCreationRatio5m))
				cachedCreationTokensWithRatio = cachedCreationTokensWithRatio.Add(decimal.NewFromInt(int64(summary.CacheCreationTokens1h)).Mul(dCacheCreationRatio1h))
			}
		}

		var imageTokensWithRatio decimal.Decimal
		if !dImageTokens.IsZero() {
			baseTokens = baseTokens.Sub(dImageTokens)
			imageTokensWithRatio = dImageTokens.Mul(dImageRatio)
		}

		var audioInputUnit decimal.Decimal
		if !dAudioTokens.IsZero() {
			summary.AudioInputPrice = operation_setting.GetGeminiInputAudioPricePerMillionTokens(summary.ModelName)
			if summary.AudioInputPrice > 0 {
				baseTokens = baseTokens.Sub(dAudioTokens)
				audioInputUnit = decimal.NewFromFloat(summary.AudioInputPrice).
					Div(decimal.NewFromInt(1000000)).Mul(dAudioTokens).Mul(dQuotaPerUnit)
			}
		}

		// OpenAI cache-write usage reports unadjusted prefix counts, so
		// cached_tokens + cache_write_tokens can exceed prompt_tokens and the
		// remainder can go negative. Clamp at zero so overlap never turns into
		// a negative base charge.
		if baseTokens.IsNegative() {
			baseTokens = decimal.Zero
		}

		promptQuota := baseTokens.Add(cachedTokensWithRatio).Add(imageTokensWithRatio).Add(cachedCreationTokensWithRatio)
		completionQuota := dCompletionTokens.Mul(dCompletionRatio)
		// (promptQuota+completionQuota).Mul(dModelRatio) is the group-ratio-1 base;
		// audioInputUnit is already at group ratio 1. The old code multiplied the
		// first term by ratio (=dModelRatio*dGroupRatio) and audio by dGroupRatio,
		// then applied other-ratios; scaling this base by the group ratio inside
		// the helper is the exact same decimal product.
		scaledBase = promptQuota.Add(completionQuota).Mul(dModelRatio).Add(audioInputUnit)
	} else {
		scaledBase = dModelPrice.Mul(dQuotaPerUnit)
	}

	var billingClamp *common.QuotaClamp
	summary.Quota, billingClamp = finalizeTextQuotaForGroupRatio(&summary, &relayInfo.PriceData, scaledBase, surchargeUnit, summary.GroupRatio, isRatioBranch)
	noteQuotaClamp(relayInfo, billingClamp)
	summary.RawQuota, _ = finalizeTextQuotaForGroupRatio(&summary, &relayInfo.PriceData, scaledBase, surchargeUnit, 1, isRatioBranch)
	summary.StatQuota, _ = finalizeTextQuotaForGroupRatio(&summary, &relayInfo.PriceData, scaledBase, surchargeUnit, summary.StatGroupRatio, isRatioBranch)
	summary.CustomerStatQuota, _ = finalizeTextQuotaForGroupRatio(&summary, &relayInfo.PriceData, scaledBase, surchargeUnit, summary.CustomerDiscount, isRatioBranch)

	return summary
}

func usageSemanticFromUsage(relayInfo *relaycommon.RelayInfo, usage *dto.Usage) string {
	if usage != nil && usage.UsageSemantic != "" {
		return usage.UsageSemantic
	}
	if relayInfo != nil && relayInfo.GetFinalRequestRelayFormat() == types.RelayFormatClaude {
		return "anthropic"
	}
	return "openai"
}

func PostTextConsumeQuota(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.Usage, extraContent []*common.Message) {
	originUsage := usage
	billingUsage := effectiveBillingUsage(usage)
	if usage == nil {
		extraContent = append(extraContent, common.NewMessage("Upstream returned no usage"))
	}
	if originUsage != nil {
		ObserveChannelAffinityUsageCacheByRelayFormat(ctx, billingUsage, relayInfo.GetFinalRequestRelayFormat())
	}

	adminRejectReason := common.GetContextKeyString(ctx, constant.ContextKeyAdminRejectReason)
	summary := calculateTextQuotaSummary(ctx, relayInfo, billingUsage)

	var tieredResult *billingexpr.TieredResult
	var tieredTokens billingexpr.TokenParams
	tieredBillingApplied := false
	snap := relayInfo.TieredBillingSnapshot
	// Providers normally estimate missing usage before settlement. Preserve the
	// same prompt estimate when a fixed-price expression reaches us without it;
	// its conditions must still run and may select a token-priced fallback.
	if billingUsage == nil && snap != nil && billingexpr.UsesFixedPricingByHash(snap.ExprString, snap.ExprHash) {
		billingUsage = &dto.Usage{PromptTokens: summary.PromptTokens, CompletionTokens: summary.CompletionTokens, TotalTokens: summary.TotalTokens}
	}
	if billingUsage != nil {
		var tieredUsedVars map[string]bool
		if snap := relayInfo.TieredBillingSnapshot; snap != nil {
			tieredUsedVars = billingexpr.UsedVarsByHash(snap.ExprString, snap.ExprHash)
		}
		tieredTokens = BuildTieredTokenParams(billingUsage, summary.IsClaudeUsageSemantic, tieredUsedVars)
		tieredOk, tieredQuota, tieredRes := TryTieredSettle(relayInfo, tieredTokens)
		if tieredOk {
			tieredBillingApplied = true
			tieredResult = tieredRes
			summary.Quota = composeTieredTextQuota(relayInfo, summary, tieredQuota, tieredRes)
			// Recompute the report-only calibers off the tiered billed base so they
			// track tiered billing too (ActualQuotaBeforeGroup scaled by 1 /
			// StatGroupRatio / CustomerDiscount, plus the billed surcharge). Never
			// feeds the deducted quota.
			if tieredRes != nil {
				summary.RawQuota = tieredCaliberQuota(summary, tieredRes, 1)
				summary.StatQuota = tieredCaliberQuota(summary, tieredRes, summary.StatGroupRatio)
				summary.CustomerStatQuota = tieredCaliberQuota(summary, tieredRes, summary.CustomerDiscount)
			}
			summary.FixedPriceBilling = isFixedPriceSettlement(relayInfo, tieredRes)
			if summary.FixedPriceBilling {
				summary.AudioInputPrice = 0
			}
		}
	}

	for _, item := range summary.ToolSurchargeItems {
		q := decimal.NewFromFloat(item.Price).
			Mul(decimal.NewFromInt(int64(item.Count))).
			Div(decimal.NewFromInt(1000)).
			Mul(decimal.NewFromFloat(summary.GroupRatio)).
			Mul(decimal.NewFromFloat(common.QuotaPerUnit))
		extraContent = append(extraContent, common.NewMessage("{{name}} called {{count}} times, cost {{quota}}", map[string]any{
			"name":  item.Name,
			"count": item.Count,
			"quota": logger.FormatQuota(common.QuotaFromDecimal(q)),
		}))
	}
	if summary.AudioInputPrice > 0 && summary.AudioTokens > 0 {
		q := decimal.NewFromFloat(summary.AudioInputPrice).Div(decimal.NewFromInt(1000000)).Mul(decimal.NewFromInt(int64(summary.AudioTokens))).Mul(decimal.NewFromFloat(summary.GroupRatio)).Mul(decimal.NewFromFloat(common.QuotaPerUnit))
		extraContent = append(extraContent, common.NewMessage("Audio input cost {{quota}}", map[string]any{"quota": logger.FormatQuota(common.QuotaFromDecimal(q))}))
	}

	if !summary.hasBillableUsage() {
		extraContent = append(extraContent, common.NewMessage("Upstream returned no usage, so nothing was charged (possibly an upstream timeout)"))
		logger.LogError(ctx, common.LogText("total tokens is 0, cannot consume quota, userId %d, channelId %d, tokenId %d, model %s， pre-consumed quota %d", relayInfo.UserId, relayInfo.ChannelId, relayInfo.TokenId, summary.ModelName, relayInfo.FinalPreConsumedQuota))
	} else {
		model.UpdateUserUsedQuotaAndRequestCount(relayInfo.UserId, summary.Quota)
		model.UpdateChannelUsedQuota(relayInfo.ChannelId, summary.Quota)
	}

	if err := SettleBilling(ctx, relayInfo, summary.Quota); err != nil {
		logger.LogError(ctx, common.LogText("error settling billing: %s", err.Error()))
	}

	logModel := summary.ModelName
	if strings.HasPrefix(logModel, "gpt-4-gizmo") {
		logModel = "gpt-4-gizmo-*"
		extraContent = append(extraContent, common.NewMessage("Model {{model}}", map[string]any{"model": summary.ModelName}))
	}
	if strings.HasPrefix(logModel, "gpt-4o-gizmo") {
		logModel = "gpt-4o-gizmo-*"
		extraContent = append(extraContent, common.NewMessage("Model {{model}}", map[string]any{"model": summary.ModelName}))
	}

	var other *model.LogOther
	if summary.IsClaudeUsageSemantic {
		other = GenerateClaudeOtherInfo(ctx, relayInfo,
			summary.ModelRatio, summary.GroupRatio, summary.CompletionRatio,
			summary.CacheTokens, summary.CacheRatio,
			summary.CacheCreationTokens, summary.CacheCreationRatio,
			summary.CacheCreationTokens5m, summary.CacheCreationRatio5m,
			summary.CacheCreationTokens1h, summary.CacheCreationRatio1h,
			summary.ModelPrice, relayInfo.PriceData.GroupRatioInfo.GroupSpecialRatio)
		other.SetPublic("usage_semantic", "anthropic")
	} else {
		other = GenerateTextOtherInfo(ctx, relayInfo, summary.ModelRatio, summary.GroupRatio, summary.CompletionRatio, summary.CacheTokens, summary.CacheRatio, summary.ModelPrice, relayInfo.PriceData.GroupRatioInfo.GroupSpecialRatio)
	}
	// Reporting-only charges (never billed): raw_quota is the undiscounted amount
	// (group ratio 1), stat_quota applies the channel statistics ratio. They keep
	// per-channel consumption visible even when the billing group ratio is 0.
	// Written at the admin tier (root-only internal deployment): non-admin log
	// views strip admin_info for free.
	other.SetAdmin("raw_quota", summary.RawQuota)
	other.SetAdmin("stat_quota", summary.StatQuota)
	other.SetAdmin("stat_group_ratio", summary.StatGroupRatio)
	// Customer discounted amount: the raw amount scaled by the per-customer stat
	// discount, recorded only for requests whose trace-tag decoded a customer id
	// (customer_id itself is written by RecordConsumeLog's trace-tag path).
	if summary.HasCustomer {
		other.SetAdmin("customer_stat_quota", summary.CustomerStatQuota)
		other.SetAdmin("customer_discount", summary.CustomerDiscount)
		other.SetAdmin("customer_discount_fallback", summary.CustomerDiscountFallback)
		if summary.CustomerMatchedPrefix != "" {
			other.SetAdmin("customer_discount_matched_prefix", summary.CustomerMatchedPrefix)
		}
	}
	appendUsageBillingPathForLog(other, common.GetContextKeyBool(ctx, constant.ContextKeyLocalCountTokens), originUsage)
	if adminRejectReason != "" {
		other.SetAdmin("reject_reason", adminRejectReason)
	}
	if summary.ImageTokens != 0 {
		other.SetPublic("image", true)
		other.SetPublic("image_ratio", summary.ImageRatio)
		other.SetPublic("image_output", summary.ImageTokens)
	}
	appendToolSurchargeLogInfo(other, summary.ToolSurchargeItems)
	if summary.AudioInputPrice > 0 && summary.AudioTokens > 0 {
		other.SetPublic("audio_input_seperate_price", true)
		other.SetPublic("audio_input_token_count", summary.AudioTokens)
		other.SetPublic("audio_input_price", summary.AudioInputPrice)
	}
	if summary.CacheCreationTokens > 0 {
		other.SetPublic("cache_creation_tokens", summary.CacheCreationTokens)
		other.SetPublic("cache_creation_ratio", summary.CacheCreationRatio)
	}
	if summary.CacheCreationTokens5m > 0 {
		other.SetPublic("cache_creation_tokens_5m", summary.CacheCreationTokens5m)
		other.SetPublic("cache_creation_ratio_5m", summary.CacheCreationRatio5m)
	}
	if summary.CacheCreationTokens1h > 0 {
		other.SetPublic("cache_creation_tokens_1h", summary.CacheCreationTokens1h)
		other.SetPublic("cache_creation_ratio_1h", summary.CacheCreationRatio1h)
	}
	if summary.CacheCreationTokens12h > 0 {
		other.SetPublic("cache_creation_tokens_12h", summary.CacheCreationTokens12h)
		other.SetPublic("cache_creation_ratio_12h", summary.CacheCreationRatio12h)
	}
	cacheWriteTokens := cacheWriteTokensTotal(summary)
	if cacheWriteTokens > 0 {
		// cache_write_tokens: normalized cache creation total for UI display.
		// If split 5m/1h values are present, this is their sum; otherwise it falls back
		// to cache_creation_tokens.
		other.SetPublic("cache_write_tokens", cacheWriteTokens)
	}
	if relayInfo.GetFinalRequestRelayFormat() != types.RelayFormatClaude && billingUsage != nil && billingUsage.UsageSource != "" && billingUsage.InputTokens > 0 {
		// input_tokens_total: explicit normalized total input used by the usage log UI.
		// Only write this field when upstream/current conversion has already provided a
		// reliable total input value and tagged the usage source. Do not infer it from
		// prompt/cache fields here, otherwise old upstream payloads may be double-counted.
		other.SetPublic("input_tokens_total", billingUsage.InputTokens)
	}
	if tieredBillingApplied {
		InjectTieredBillingInfo(other, relayInfo, tieredResult)

	}

	attachQuotaSaturation(ctx, relayInfo, other)

	model.RecordConsumeLog(ctx, relayInfo.UserId, model.RecordConsumeLogParams{
		ChannelId:        relayInfo.ChannelId,
		PromptTokens:     summary.PromptTokens,
		CompletionTokens: summary.CompletionTokens,
		ModelName:        logModel,
		TokenName:        summary.TokenName,
		Quota:            summary.Quota,
		Content:          extraContent,
		TokenId:          relayInfo.TokenId,
		UseTimeSeconds:   int(summary.UseTimeSeconds),
		IsStream:         relayInfo.IsStream,
		Group:            relayInfo.UsingGroup,
		Other:            other,
	})
	relayInfo.PerformanceOutputTokens = int64(summary.CompletionTokens)
}
