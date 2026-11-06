package model

import (
	"errors"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// Customer maps a downstream customer id (the api_key_id decoded from the
// x-bce-aigw-trace-tag header) to a human-readable name plus per-model-series
// stat discounts and optional Claude cache optimization overrides. Discounts
// are reporting-only and never change actual billing.
type Customer struct {
	Id         int    `json:"id"`
	CustomerId string `json:"customer_id" gorm:"type:varchar(64);uniqueIndex"`
	Name       string `json:"name" gorm:"type:varchar(255);default:''"`
	Remark     string `json:"remark" gorm:"type:text"`
	// DefaultDiscount is the fallback multiplier applied when a model matches no
	// prefix. PrefixDiscounts maps a model-name prefix (a "series", e.g. claude /
	// gpt / gemini / grok) to its discount; a model takes the longest matching
	// prefix. PrefixDiscounts is the serialized JSON persisted to the DB and
	// PrefixDiscountMap its decoded form used by the API and cache.
	DefaultDiscount                 float64                          `json:"default_discount"`
	PrefixDiscounts                 string                           `json:"-" gorm:"type:text"`
	PrefixDiscountMap               map[string]float64               `json:"prefix_discounts" gorm:"-"`
	CreatedTime                     int64                            `json:"created_time" gorm:"bigint"`
	UpdatedTime                     int64                            `json:"updated_time" gorm:"bigint"`
	ClaudeCacheOptimizationJSON     string                           `json:"-" gorm:"column:claude_cache_optimization_json;type:text"`
	ClaudeCacheOptimization         *CustomerClaudeCacheOptimization `json:"claude_cache_optimization" gorm:"-"`
	SessionRoutingModelPrefixesJSON string                           `json:"-" gorm:"column:session_routing_model_prefixes_json;type:text"`
	SessionRoutingModelPrefixes     map[string]bool                  `json:"session_routing_model_prefixes" gorm:"-"`
}

// CustomerClaudeCacheOptimization overrides all five global rules when non-nil.
// A nil configuration inherits legacy global behavior; disabled rules never do.
type CustomerClaudeCacheOptimization struct {
	ForceExtendedTTL         CustomerClaudeCacheOptimizationRule `json:"force_extended_ttl"`
	FixVolatileBreakpoint    CustomerClaudeCacheOptimizationRule `json:"fix_volatile_breakpoint"`
	RetainPreviousBreakpoint CustomerClaudeCacheOptimizationRule `json:"retain_previous_breakpoint"`
	StableBaseBreakpoint     CustomerClaudeCacheOptimizationRule `json:"stable_base_breakpoint"`
	FallbackCacheBreakpoint  CustomerClaudeCacheOptimizationRule `json:"fallback_cache_breakpoint"`
}

type CustomerClaudeCacheOptimizationRule struct {
	Enabled       bool     `json:"enabled"`
	ModelPrefixes []string `json:"model_prefixes"`
}

// Matches applies only within Claude relay paths. Empty prefixes mean every
// model on those paths, including aliases; prefixes are literal, case-insensitive.
func (r CustomerClaudeCacheOptimizationRule) Matches(model string) bool {
	if !r.Enabled {
		return false
	}
	if len(r.ModelPrefixes) == 0 {
		return true
	}
	for _, prefix := range r.ModelPrefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix != "" && strings.HasPrefix(strings.ToLower(model), strings.ToLower(prefix)) {
			return true
		}
	}
	return false
}

// clone normalizes prefixes without sharing writable slices with the source.
func (o *CustomerClaudeCacheOptimization) clone() *CustomerClaudeCacheOptimization {
	if o == nil {
		return nil
	}
	result := *o
	for _, rule := range []*CustomerClaudeCacheOptimizationRule{
		&result.ForceExtendedTTL, &result.FixVolatileBreakpoint,
		&result.RetainPreviousBreakpoint, &result.StableBaseBreakpoint,
		&result.FallbackCacheBreakpoint,
	} {
		prefixes := make([]string, 0, len(rule.ModelPrefixes))
		seen := make(map[string]bool, len(rule.ModelPrefixes))
		for _, prefix := range rule.ModelPrefixes {
			prefix = strings.TrimSpace(prefix)
			key := strings.ToLower(prefix)
			if prefix != "" && !seen[key] {
				prefixes = append(prefixes, prefix)
				seen[key] = true
			}
		}
		rule.ModelPrefixes = prefixes
	}
	return &result
}

// normalizeDiscount coerces any non-positive, NaN or Inf discount to 1 (no
// discount / discounted-amount equals the original amount).
func normalizeDiscount(d float64) float64 {
	if d <= 0 || math.IsNaN(d) || math.IsInf(d, 0) {
		return 1
	}
	return d
}

// hydrate decodes the independent configuration columns. Invalid discount
// entries are dropped; corrupt cache optimization overrides disable all rules.
func (c *Customer) hydrate() {
	c.ClaudeCacheOptimization = nil
	if strings.TrimSpace(c.ClaudeCacheOptimizationJSON) != "" {
		var config *CustomerClaudeCacheOptimization
		if err := common.UnmarshalJsonStr(c.ClaudeCacheOptimizationJSON, &config); err != nil {
			common.SysError("failed to parse customer Claude cache optimization: " + err.Error())
			// Corrupt explicit configuration must not fall back to global enablement.
			config = &CustomerClaudeCacheOptimization{}
		}
		c.ClaudeCacheOptimization = config.clone()
	}
	c.SessionRoutingModelPrefixes = map[string]bool{}
	if strings.TrimSpace(c.SessionRoutingModelPrefixesJSON) != "" {
		var raw map[string]bool
		if err := common.UnmarshalJsonStr(c.SessionRoutingModelPrefixesJSON, &raw); err != nil {
			common.SysError("failed to parse customer session routing model prefixes: " + err.Error())
		} else {
			for prefix, enabled := range raw {
				prefix = strings.TrimSpace(prefix)
				if prefix != "" {
					c.SessionRoutingModelPrefixes[prefix] = enabled
				}
			}
		}
	}
	c.PrefixDiscountMap = map[string]float64{}
	if strings.TrimSpace(c.PrefixDiscounts) == "" {
		return
	}
	raw := map[string]float64{}
	if err := common.UnmarshalJsonStr(c.PrefixDiscounts, &raw); err != nil {
		common.SysError("failed to parse customer prefix discounts: " + err.Error())
		return
	}
	for prefix, d := range raw {
		if prefix == "" || d <= 0 || math.IsNaN(d) || math.IsInf(d, 0) {
			continue
		}
		c.PrefixDiscountMap[prefix] = d
	}
}

// serialize normalizes the API-facing map and writes it back to the stored
// string column, keeping only valid positive entries.
func (c *Customer) serialize() error {
	c.ClaudeCacheOptimizationJSON = ""
	if c.ClaudeCacheOptimization != nil {
		c.ClaudeCacheOptimization = c.ClaudeCacheOptimization.clone()
		b, err := common.Marshal(c.ClaudeCacheOptimization)
		if err != nil {
			return err
		}
		c.ClaudeCacheOptimizationJSON = string(b)
	}
	clean := map[string]float64{}
	for prefix, d := range c.PrefixDiscountMap {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" || d <= 0 || math.IsNaN(d) || math.IsInf(d, 0) {
			continue
		}
		clean[prefix] = d
	}
	c.PrefixDiscountMap = clean
	if len(clean) == 0 {
		c.PrefixDiscounts = ""
	} else {
		b, err := common.Marshal(clean)
		if err != nil {
			return err
		}
		c.PrefixDiscounts = string(b)
	}
	if c.SessionRoutingModelPrefixes != nil {
		sessionRoutingPrefixes := map[string]bool{}
		for prefix, enabled := range c.SessionRoutingModelPrefixes {
			prefix = strings.TrimSpace(prefix)
			if prefix != "" {
				sessionRoutingPrefixes[prefix] = enabled
			}
		}
		c.SessionRoutingModelPrefixes = sessionRoutingPrefixes
		if len(sessionRoutingPrefixes) == 0 {
			c.SessionRoutingModelPrefixesJSON = ""
		} else {
			b, err := common.Marshal(sessionRoutingPrefixes)
			if err != nil {
				return err
			}
			c.SessionRoutingModelPrefixesJSON = string(b)
		}
	}
	return nil
}

func GetAllCustomers(startIdx int, num int) (customers []*Customer, total int64, err error) {
	if err = DB.Model(&Customer{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err = DB.Order("id desc").Limit(num).Offset(startIdx).Find(&customers).Error; err != nil {
		return nil, 0, err
	}
	for _, c := range customers {
		c.hydrate()
	}
	return customers, total, nil
}

func SearchCustomers(keyword string, startIdx int, num int) (customers []*Customer, total int64, err error) {
	like := "%" + keyword + "%"
	query := DB.Model(&Customer{}).Where("customer_id LIKE ? OR name LIKE ? OR remark LIKE ?", like, like, like)
	if err = query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err = query.Order("id desc").Limit(num).Offset(startIdx).Find(&customers).Error; err != nil {
		return nil, 0, err
	}
	for _, c := range customers {
		c.hydrate()
	}
	return customers, total, nil
}

func GetCustomerById(id int) (*Customer, error) {
	if id == 0 {
		return nil, errors.New("id 为空！")
	}
	customer := Customer{Id: id}
	if err := DB.First(&customer, "id = ?", id).Error; err != nil {
		return nil, err
	}
	customer.hydrate()
	return &customer, nil
}

// normalize trims the customer id and coerces the default discount into range;
// it is shared by Insert and Update so validation stays identical.
func (c *Customer) normalize() error {
	c.CustomerId = strings.TrimSpace(c.CustomerId)
	if c.CustomerId == "" {
		return errors.New("客户 ID 不能为空")
	}
	c.Name = strings.TrimSpace(c.Name)
	c.DefaultDiscount = normalizeDiscount(c.DefaultDiscount)
	return c.serialize()
}

func (c *Customer) Insert() error {
	if err := c.normalize(); err != nil {
		return err
	}
	now := common.GetTimestamp()
	c.CreatedTime = now
	c.UpdatedTime = now
	if err := DB.Create(c).Error; err != nil {
		return err
	}
	_ = refreshCustomerCache()
	return nil
}

func (c *Customer) Update() error {
	if err := c.normalize(); err != nil {
		return err
	}
	c.UpdatedTime = common.GetTimestamp()
	// Select the columns explicitly so clearing Name/Remark/discounts persists
	// (GORM's Updates skips zero values otherwise).
	columns := []string{"customer_id", "name", "remark", "default_discount", "prefix_discounts", "updated_time"}
	// Legacy clients omit this field. Only an explicit object replaces the
	// persisted override; an empty object explicitly disables every rule.
	if c.ClaudeCacheOptimization != nil {
		columns = append(columns, "claude_cache_optimization_json")
	}
	if c.SessionRoutingModelPrefixes != nil {
		columns = append(columns, "session_routing_model_prefixes_json")
	}
	err := DB.Model(c).Where("id = ?", c.Id).
		Select(columns).
		Updates(c).Error
	if err != nil {
		return err
	}
	_ = refreshCustomerCache()
	return nil
}

func DeleteCustomerById(id int) error {
	if id == 0 {
		return errors.New("id 为空！")
	}
	if err := DB.Delete(&Customer{}, "id = ?", id).Error; err != nil {
		return err
	}
	_ = refreshCustomerCache()
	return nil
}
