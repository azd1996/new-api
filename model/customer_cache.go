package model

import (
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// Customer config is admin-managed and tiny, so it is kept fully in memory and
// rebuilt on every mutation. The map is keyed by CustomerId. Lookups run on the
// log-write hot path (name resolution + per-model stat discount), so they must
// not hit the DB per request.
var (
	customerCacheMu   sync.RWMutex
	customerCacheOnce sync.Once
	customerCache     map[string]*Customer
)

func ensureCustomerCache() {
	customerCacheOnce.Do(func() { _ = refreshCustomerCache() })
}

// refreshCustomerCache rebuilds the in-memory customer map from the DB. Called
// at first use and after every create/update/delete.
func refreshCustomerCache() error {
	var customers []*Customer
	if err := DB.Find(&customers).Error; err != nil {
		return err
	}
	m := make(map[string]*Customer, len(customers))
	for _, c := range customers {
		c.hydrate()
		m[c.CustomerId] = c
	}
	customerCacheMu.Lock()
	customerCache = m
	customerCacheMu.Unlock()
	return nil
}

// SyncCustomerCache periodically rebuilds the customer cache from the DB so
// customer config created/edited on another instance propagates to this one
// without a restart, mirroring SyncChannelCache / SyncOptions. frequency is the
// SYNC_FREQUENCY interval in seconds; a non-positive value disables polling.
func SyncCustomerCache(frequency int) {
	if frequency <= 0 {
		return
	}
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		if err := refreshCustomerCache(); err != nil {
			common.SysError("failed to sync customer cache: " + err.Error())
		}
	}
}

func getCachedCustomer(customerID string) (*Customer, bool) {
	if customerID == "" {
		return nil, false
	}
	ensureCustomerCache()
	customerCacheMu.RLock()
	defer customerCacheMu.RUnlock()
	c, ok := customerCache[customerID]
	return c, ok
}

// CustomerName returns the configured display name for a customer id, or "" when
// the customer has no mapping configured.
func CustomerName(customerID string) string {
	if c, ok := getCachedCustomer(customerID); ok {
		return c.Name
	}
	return ""
}

// CustomerConfigured reports whether a customer id has an admin-managed mapping
// (name + discounts). The customer-statistics dashboard uses this to decide
// whether the per-customer discounted amount is meaningful or should fall back
// to the channel-discounted amount.
func CustomerConfigured(customerID string) bool {
	_, ok := getCachedCustomer(customerID)
	return ok
}

// CustomerModelDiscount returns the reporting-only stat discount multiplier for
// (customerID, modelName): the longest matching model-name prefix ("series")
// wins, else the customer's default discount, else 1 when the customer is
// unknown. Matching is case-insensitive. The result is always positive/finite.
func CustomerModelDiscount(customerID, modelName string) float64 {
	c, ok := getCachedCustomer(customerID)
	if !ok {
		return 1
	}
	lowerModel := strings.ToLower(modelName)
	best := ""
	for prefix := range c.PrefixDiscountMap {
		if len(prefix) > len(best) && strings.HasPrefix(lowerModel, strings.ToLower(prefix)) {
			best = prefix
		}
	}
	if best != "" {
		return normalizeDiscount(c.PrefixDiscountMap[best])
	}
	return normalizeDiscount(c.DefaultDiscount)
}
