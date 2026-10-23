package model

import "sync"

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

// CustomerModelDiscount returns the reporting-only stat discount multiplier for
// (customerID, modelName): a per-model override if present, else the customer's
// default discount, else 1 when the customer is unknown. The result is always a
// positive finite number.
func CustomerModelDiscount(customerID, modelName string) float64 {
	c, ok := getCachedCustomer(customerID)
	if !ok {
		return 1
	}
	if d, ok := c.ModelDiscountMap[modelName]; ok {
		return normalizeDiscount(d)
	}
	return normalizeDiscount(c.DefaultDiscount)
}
