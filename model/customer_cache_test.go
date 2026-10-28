package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCustomerDiscountResolution(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Customer{}))
	customer := &Customer{
		CustomerId:      "discount-resolution-test",
		DefaultDiscount: 0.25,
		PrefixDiscountMap: map[string]float64{
			"gpt":   0.8,
			"GPT-4": 1,
		},
	}
	require.NoError(t, customer.Insert())
	t.Cleanup(func() { require.NoError(t, DeleteCustomerById(customer.Id)) })
	emptyCustomer := &Customer{CustomerId: "discount-resolution-empty", DefaultDiscount: 0.5}
	require.NoError(t, emptyCustomer.Insert())
	t.Cleanup(func() { require.NoError(t, DeleteCustomerById(emptyCustomer.Id)) })

	for _, tc := range []struct {
		name       string
		customerID string
		modelName  string
		want       CustomerDiscountResolution
	}{
		{"explicit one longest prefix", customer.CustomerId, "gPt-4o", CustomerDiscountResolution{Discount: 1, MatchedPrefix: "GPT-4"}},
		{"configured discount", customer.CustomerId, "gpt-3.5", CustomerDiscountResolution{Discount: 0.8, MatchedPrefix: "gpt"}},
		{"no matching prefix ignores default", customer.CustomerId, "claude-sonnet", CustomerDiscountResolution{Discount: 1, Fallback: true}},
		{"no configured prefixes", emptyCustomer.CustomerId, "gpt-4o", CustomerDiscountResolution{Discount: 1, Fallback: true}},
		{"missing customer", "unconfigured-discount-customer", "gpt-4o", CustomerDiscountResolution{Discount: 1, Fallback: true}},
		{"no customer id", "", "gpt-4o", CustomerDiscountResolution{Discount: 1, Fallback: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ResolveCustomerModelDiscount(tc.customerID, tc.modelName))
			assert.Equal(t, tc.want.Discount, CustomerModelDiscount(tc.customerID, tc.modelName))
		})
	}
}
