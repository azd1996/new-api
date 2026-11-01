package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

const (
	modelsDevAPIURL          = "https://models.dev/api.json"
	modelsDevCatalogTTL      = 10 * time.Minute
	modelsDevFetchTimeout    = 30 * time.Second
	maxModelsDevCatalogBytes = 32 << 20
)

// models.dev prices are USD per 1M tokens. cache_write is the short-lived
// write price (Anthropic 5m, OpenAI 30m); tiers are context-length tiers that
// replace the base prices once the request exceeds tier.size.
type modelsDevPriceSet struct {
	Input      *float64 `json:"input"`
	Output     *float64 `json:"output"`
	CacheRead  *float64 `json:"cache_read"`
	CacheWrite *float64 `json:"cache_write"`
}

type modelsDevContextTier struct {
	modelsDevPriceSet
	Tier struct {
		Type string `json:"type"`
		Size int64  `json:"size"`
	} `json:"tier"`
}

type modelsDevPricedCost struct {
	modelsDevPriceSet
	Tiers []modelsDevContextTier `json:"tiers"`
}

type modelsDevCatalog map[string]struct {
	Models map[string]struct {
		Cost *modelsDevPricedCost `json:"cost"`
	} `json:"models"`
}

type ModelsDevPricingSuggestion struct {
	Expression    string   `json:"expression"`
	Provider      string   `json:"provider"`
	SourceModel   string   `json:"source_model"`
	DerivedFields []string `json:"derived_fields"`
}

var modelsDevCatalogCache struct {
	sync.Mutex
	catalog   modelsDevCatalog
	fetchedAt time.Time
}

// GetModelsDevPricingSuggestion builds a billing expression for one model from
// models.dev. It never writes pricing; the editor applies it to an unsaved draft.
func GetModelsDevPricingSuggestion(c *gin.Context) {
	modelName := strings.TrimSpace(c.Query("model"))
	if modelName == "" {
		common.ApiErrorT(c, "Model name is required")
		return
	}
	catalog, err := loadModelsDevCatalog(c.Request.Context())
	if err != nil {
		logger.LogWarn(c.Request.Context(), common.LogText("models.dev fetch failed: %s", err.Error()))
		common.ApiErrorT(c, "Failed to fetch models.dev pricing")
		return
	}
	suggestion, err := buildModelsDevPricingSuggestion(catalog, modelName)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, suggestion)
}

func loadModelsDevCatalog(ctx context.Context) (modelsDevCatalog, error) {
	modelsDevCatalogCache.Lock()
	defer modelsDevCatalogCache.Unlock()
	if modelsDevCatalogCache.catalog != nil && time.Since(modelsDevCatalogCache.fetchedAt) < modelsDevCatalogTTL {
		return modelsDevCatalogCache.catalog, nil
	}
	ctx, cancel := context.WithTimeout(ctx, modelsDevFetchTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsDevAPIURL, nil)
	if err != nil {
		return nil, err
	}
	response, err := service.GetHttpClient().Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %s", response.Status)
	}
	var catalog modelsDevCatalog
	if err := common.DecodeJson(io.LimitReader(response.Body, maxModelsDevCatalogBytes), &catalog); err != nil {
		return nil, err
	}
	if len(catalog) == 0 {
		return nil, errors.New("empty models.dev catalog")
	}
	modelsDevCatalogCache.catalog = catalog
	modelsDevCatalogCache.fetchedAt = time.Now()
	return catalog, nil
}

var (
	modelsDevRegionPrefix  = regexp.MustCompile(`^(us|eu|apac|global|jp|au)\.`)
	modelsDevVendorPrefix  = regexp.MustCompile(`^(anthropic|openai)\.`)
	modelsDevBedrockSuffix = regexp.MustCompile(`-v\d+(:\d+)?$`)
	modelsDevDateSuffix    = regexp.MustCompile(`-\d{8}$`)
	openAIReasoningModel   = regexp.MustCompile(`^o\d`)
)

var modelsDevCloudProviders = []string{"amazon-bedrock", "google-vertex-anthropic", "google-vertex", "azure"}

// modelsDevModelKey reduces gateway and provider model IDs to one comparable
// form: Bedrock region/vendor prefixes, version suffixes, provider paths and
// Vertex "@date" separators are removed or normalized.
func modelsDevModelKey(name string, dropDate bool) string {
	key := strings.ToLower(strings.TrimSpace(name))
	if index := strings.LastIndex(key, "/"); index >= 0 {
		key = key[index+1:]
	}
	key = strings.ReplaceAll(key, "@", "-")
	key = modelsDevRegionPrefix.ReplaceAllString(key, "")
	key = modelsDevVendorPrefix.ReplaceAllString(key, "")
	key = modelsDevBedrockSuffix.ReplaceAllString(key, "")
	if dropDate {
		key = modelsDevDateSuffix.ReplaceAllString(key, "")
	}
	return key
}

func buildModelsDevPricingSuggestion(catalog modelsDevCatalog, modelName string) (*ModelsDevPricingSuggestion, error) {
	lowerName := strings.ToLower(modelName)
	isClaude := strings.Contains(lowerName, "claude")
	isGPT := strings.Contains(lowerName, "gpt")

	officialProvider := ""
	if isClaude {
		officialProvider = "anthropic"
	} else if isGPT || openAIReasoningModel.MatchString(modelsDevModelKey(modelName, false)) {
		officialProvider = "openai"
	}
	// Cloud providers republish official list prices, so they rank before
	// resellers, which often omit cache prices or apply their own discounts.
	providers := make([]string, 0, len(catalog))
	for _, preferred := range append([]string{officialProvider}, modelsDevCloudProviders...) {
		if _, ok := catalog[preferred]; ok && !slices.Contains(providers, preferred) {
			providers = append(providers, preferred)
		}
	}
	resellers := make([]string, 0, len(catalog))
	for provider := range catalog {
		if !slices.Contains(providers, provider) {
			resellers = append(resellers, provider)
		}
	}
	slices.Sort(resellers)
	providers = append(providers, resellers...)

	// Exact IDs win over date-less aliases. Within a pass the official
	// provider wins outright; otherwise the most complete price wins and
	// provider order breaks ties.
	var provider, sourceModel string
	var cost *modelsDevPricedCost
	bestScore := -1
	for _, dropDate := range []bool{false, true} {
		target := modelsDevModelKey(modelName, dropDate)
		for _, candidateProvider := range providers {
			keys := make([]string, 0, len(catalog[candidateProvider].Models))
			for key := range catalog[candidateProvider].Models {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			// The caller's exact ID beats other region variants of it.
			if index := slices.Index(keys, modelName); index > 0 {
				keys[0], keys[index] = keys[index], keys[0]
			}
			for _, key := range keys {
				candidate := catalog[candidateProvider].Models[key].Cost
				if candidate == nil || candidate.Input == nil || candidate.Output == nil || modelsDevModelKey(key, dropDate) != target {
					continue
				}
				score := len(candidate.Tiers)
				if candidate.CacheRead != nil {
					score++
				}
				if candidate.CacheWrite != nil {
					score++
				}
				if candidateProvider == officialProvider {
					score = math.MaxInt
				}
				if score > bestScore {
					provider, sourceModel, cost, bestScore = candidateProvider, key, candidate, score
				}
				break
			}
		}
		if cost != nil {
			break
		}
	}
	if cost == nil {
		return nil, common.NewMessage("Model not found on models.dev")
	}
	if !cost.valid() {
		return nil, common.NewMessage("models.dev pricing for this model is invalid")
	}

	derived := make(map[string]bool)
	tiers := make([]modelsDevContextTier, 0, len(cost.Tiers))
	for _, tier := range cost.Tiers {
		if tier.Tier.Type != "context" || tier.Tier.Size <= 0 || !tier.valid() {
			continue
		}
		// Missing tier prices keep the base price so every tier prices the
		// same token categories as the base tier.
		if tier.Input == nil {
			tier.Input = cost.Input
		}
		if tier.Output == nil {
			tier.Output = cost.Output
		}
		if tier.CacheRead == nil {
			tier.CacheRead = cost.CacheRead
		}
		if tier.CacheWrite == nil {
			tier.CacheWrite = cost.CacheWrite
		}
		// models.dev writes the same boundary as 272000 or 272001.
		if tier.Tier.Size%1000 == 1 {
			tier.Tier.Size--
		}
		tiers = append(tiers, tier)
	}
	sort.Slice(tiers, func(i, j int) bool { return tiers[i].Tier.Size < tiers[j].Tier.Size })

	priceSets := []modelsDevPriceSet{cost.modelsDevPriceSet}
	for _, tier := range tiers {
		priceSets = append(priceSets, tier.modelsDevPriceSet)
	}
	bodies := make([]string, len(priceSets))
	for index, prices := range priceSets {
		input := decimal.NewFromFloat(*prices.Input)
		var body strings.Builder
		body.WriteString("p * " + input.String())
		body.WriteString(" + c * " + decimal.NewFromFloat(*prices.Output).String())
		if prices.CacheRead != nil {
			body.WriteString(" + cr * " + decimal.NewFromFloat(*prices.CacheRead).String())
		} else if isClaude {
			// Anthropic's cache read is 0.1x input.
			body.WriteString(" + cr * " + input.Mul(decimal.NewFromFloat(0.1)).String())
			derived["cr"] = true
		}
		cacheWrite := prices.CacheWrite
		if cacheWrite == nil && isClaude {
			// Anthropic's 5-minute cache write is 1.25x input.
			value, _ := input.Mul(decimal.NewFromFloat(1.25)).Float64()
			cacheWrite = &value
			derived["cc"] = true
		}
		if cacheWrite != nil {
			body.WriteString(" + cc * " + decimal.NewFromFloat(*cacheWrite).String())
		}
		if isClaude {
			// models.dev has no 1-hour price; Anthropic bills it at 2x input.
			body.WriteString(" + cc1h * " + input.Mul(decimal.NewFromInt(2)).String())
			derived["cc1h"] = true
		} else if isGPT && cacheWrite != nil {
			// models.dev has no 12-hour price; OpenAI bills it at 2x the 30m write.
			body.WriteString(" + cc12h * " + decimal.NewFromFloat(*cacheWrite).Mul(decimal.NewFromInt(2)).String())
			derived["cc12h"] = true
		}
		bodies[index] = body.String()
	}

	// len <= t1 ? base : len <= t2 ? tier1 : tier2, the chain form the
	// pricing editor parses back into tiers.
	tierNames := make([]string, len(bodies))
	for index := range tierNames {
		switch index {
		case 0:
			tierNames[index] = "standard"
		case 1:
			tierNames[index] = "long_context"
		default:
			tierNames[index] = fmt.Sprintf("long_context_%d", index)
		}
	}
	last := len(bodies) - 1
	expression := fmt.Sprintf(`tier("%s", %s)`, tierNames[last], bodies[last])
	for index := len(tiers) - 1; index >= 0; index-- {
		expression = fmt.Sprintf(`len <= %d ? tier("%s", %s) : %s`, tiers[index].Tier.Size, tierNames[index], bodies[index], expression)
	}
	if _, err := billingexpr.CompileFromCache(expression); err != nil {
		return nil, fmt.Errorf("generated expression is invalid: %w", err)
	}

	derivedFields := make([]string, 0, len(derived))
	for field := range derived {
		derivedFields = append(derivedFields, field)
	}
	slices.Sort(derivedFields)
	return &ModelsDevPricingSuggestion{
		Expression:    expression,
		Provider:      provider,
		SourceModel:   sourceModel,
		DerivedFields: derivedFields,
	}, nil
}

func (prices modelsDevPriceSet) valid() bool {
	for _, value := range []*float64{prices.Input, prices.Output, prices.CacheRead, prices.CacheWrite} {
		if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0) {
			return false
		}
	}
	return true
}
