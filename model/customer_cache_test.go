package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// customerBeforeClaudeCachePolicy matches the persisted Customer fields in HEAD,
// before the Claude policy column was introduced.
type customerBeforeClaudeCachePolicy struct {
	Id              int
	CustomerId      string `gorm:"type:varchar(64);uniqueIndex"`
	Name            string `gorm:"type:varchar(255);default:''"`
	Remark          string `gorm:"type:text"`
	DefaultDiscount float64
	PrefixDiscounts string `gorm:"type:text"`
	CreatedTime     int64  `gorm:"bigint"`
	UpdatedTime     int64  `gorm:"bigint"`
}

func (customerBeforeClaudeCachePolicy) TableName() string { return "customers" }

func TestCustomerClaudeCacheOptimizationMigration(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			var driver gorm.Dialector
			switch engine {
			case "sqlite":
				driver = sqlite.Open(filepath.Join(t.TempDir(), "customers.db"))
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_MYSQL_DSN is not configured")
				}
				driver = mysqlMigrationDialector{mysql.Dialector{Config: &mysql.Config{DSN: dsn}}}
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN is not configured")
				}
				driver = postgresMigrationDialector{postgres.Dialector{Config: &postgres.Config{
					DSN: dsn, PreferSimpleProtocol: true,
				}}}
			}
			recorder := &migrationSQLRecorder{}
			db, err := gorm.Open(driver, &gorm.Config{Logger: recorder})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
			var version string
			versionSQL := "SELECT version()"
			if engine == "sqlite" {
				versionSQL = "SELECT sqlite_version()"
			}
			require.NoError(t, db.Raw(versionSQL).Scan(&version).Error)
			t.Logf("%s version: %s", engine, version)
			// Never replace a table belonging to the caller.
			require.False(t, db.Migrator().HasTable(&Customer{}), "use an isolated empty test database")
			previousDB := DB
			customerCacheMu.RLock()
			previousCache := customerCache
			customerCacheMu.RUnlock()
			DB = db
			t.Cleanup(func() {
				DB = previousDB
				customerCacheMu.Lock()
				customerCache = previousCache
				customerCacheMu.Unlock()
			})

			for _, scenario := range []string{"fresh", "upgrade"} {
				t.Run(scenario, func(t *testing.T) {
					require.False(t, db.Migrator().HasTable(&Customer{}))
					t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(&Customer{})) })
					legacy := customerBeforeClaudeCachePolicy{
						CustomerId: "legacy-customer", Name: "Existing 客户", Remark: "preserved remark",
						DefaultDiscount: 0.75, PrefixDiscounts: `{"claude":0.8,"gpt":1}`,
						CreatedTime: 1700000000, UpdatedTime: 1700000010,
					}
					if scenario == "upgrade" {
						require.NoError(t, db.AutoMigrate(&customerBeforeClaudeCachePolicy{}))
						require.False(t, db.Migrator().HasColumn(&Customer{}, "claude_cache_optimization_json"))
						require.NoError(t, db.Create(&legacy).Error)
					}
					require.NoError(t, db.AutoMigrate(&Customer{}))
					if scenario == "fresh" {
						require.NoError(t, db.Create(&legacy).Error)
					}
					columns, err := db.Migrator().ColumnTypes(&Customer{})
					require.NoError(t, err)
					found := false
					for _, column := range columns {
						if column.Name() == "claude_cache_optimization_json" {
							found = true
							assert.Equal(t, "text", strings.ToLower(column.DatabaseTypeName()))
						}
					}
					require.True(t, found, "policy must be stored in a real TEXT column")
					configured := &Customer{
						CustomerId: "configured-customer", Name: "Configured",
						PrefixDiscountMap: map[string]float64{"claude": 0.9},
						ClaudeCacheOptimization: &CustomerClaudeCacheOptimization{
							ForceExtendedTTL: CustomerClaudeCacheOptimizationRule{
								Enabled: true, ModelPrefixes: []string{"claude-sonnet"},
							},
							FallbackCacheBreakpoint: CustomerClaudeCacheOptimizationRule{
								Enabled: true, ModelPrefixes: []string{"claude-opus"},
							},
						},
					}
					require.NoError(t, configured.Insert())
					recorder.reset()
					require.NoError(t, db.AutoMigrate(&Customer{}))
					assert.Empty(t, recorder.schemaMutations(), "second AutoMigrate must emit no DDL")
					var preserved customerBeforeClaudeCachePolicy
					require.NoError(t, db.First(&preserved, legacy.Id).Error)
					assert.Equal(t, legacy, preserved, "all existing persisted fields must survive")
					old, err := GetCustomerById(legacy.Id)
					require.NoError(t, err)
					assert.Nil(t, old.ClaudeCacheOptimization)
					assert.Equal(t, map[string]float64{"claude": 0.8, "gpt": 1}, old.PrefixDiscountMap)
					stored, err := GetCustomerById(configured.Id)
					require.NoError(t, err)
					assert.Equal(t, configured.ClaudeCacheOptimization, stored.ClaudeCacheOptimization)
					assert.Equal(t, configured.PrefixDiscountMap, stored.PrefixDiscountMap)
					assert.Greater(t, configured.Id, legacy.Id, "generated primary keys must remain usable")
					assert.Error(t, db.Create(&Customer{CustomerId: legacy.CustomerId}).Error, "customer ID must stay unique")
					assert.Error(t, db.Create(&Customer{Id: legacy.Id, CustomerId: "duplicate-primary-key"}).Error)
					t.Run("config_crud", TestCustomerClaudeCacheOptimizationPersistence)
					require.NoError(t, DeleteCustomerById(configured.Id))
					var count int64
					require.NoError(t, db.Model(&Customer{}).Where("id = ?", configured.Id).Count(&count).Error)
					assert.Zero(t, count)
					assert.Nil(t, GetCustomerClaudeCacheOptimization(configured.CustomerId))
				})
			}
		})
	}
}

func TestCustomerClaudeCacheOptimizationPersistence(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Customer{}))
	customer := &Customer{CustomerId: "claude-cache-optimization-test"}
	require.NoError(t, customer.Insert())
	t.Cleanup(func() { require.NoError(t, DeleteCustomerById(customer.Id)) })
	assert.Nil(t, GetCustomerClaudeCacheOptimization(customer.CustomerId))
	assert.Nil(t, GetCustomerClaudeCacheOptimization("missing-claude-cache-customer"))
	assert.Nil(t, GetCustomerClaudeCacheOptimization(""))
	assert.False(t, CustomerSessionRoutingEnabledForModel(customer.CustomerId, "routing-model"))
	data, err := common.Marshal(customer)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"claude_cache_optimization":null`)
	assert.NotContains(t, string(data), "claude_cache_optimization_json")

	var update Customer
	require.NoError(t, common.UnmarshalJsonStr(`{
		"claude_cache_optimization": {
			"force_extended_ttl": {"enabled":true,"model_prefixes":[" Claude-Sonnet ","claude-sonnet"," ","claude-opus"]},
			"fix_volatile_breakpoint": {"enabled":false,"model_prefixes":["Claude"]},
			"retain_previous_breakpoint": {"enabled":true,"model_prefixes":[]},
			"stable_base_breakpoint": {"enabled":true,"model_prefixes":[" alias/Claude "]},
			"fallback_cache_breakpoint": {"enabled":true,"model_prefixes":[" Claude-Opus ","claude-opus"," ","alias/Claude"]}
		}
	}`, &update))
	update.Id = customer.Id
	update.CustomerId = customer.CustomerId
	require.NoError(t, update.Update())
	stored, err := GetCustomerById(customer.Id)
	require.NoError(t, err)
	require.NotNil(t, stored.ClaudeCacheOptimization)
	assert.Equal(t, []string{"Claude-Sonnet", "claude-opus"}, stored.ClaudeCacheOptimization.ForceExtendedTTL.ModelPrefixes)
	assert.Equal(t, []string{"Claude-Opus", "alias/Claude"}, stored.ClaudeCacheOptimization.FallbackCacheBreakpoint.ModelPrefixes)
	data, err = common.Marshal(stored)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"fallback_cache_breakpoint":{"enabled":true,"model_prefixes":["Claude-Opus","alias/Claude"]}`)
	assert.Empty(t, stored.PrefixDiscountMap, "empty discounts must not skip optimization hydration")
	config := GetCustomerClaudeCacheOptimization(customer.CustomerId)
	require.NotNil(t, config)
	assert.Equal(t, stored.ClaudeCacheOptimization, config)
	assert.True(t, config.ForceExtendedTTL.Matches("cLaUdE-sOnNeT-4"))
	assert.False(t, config.ForceExtendedTTL.Matches("prefix-claude-sonnet"))
	assert.False(t, config.FixVolatileBreakpoint.Matches("claude-sonnet"))
	assert.True(t, config.RetainPreviousBreakpoint.Matches("provider-alias"))
	assert.True(t, config.StableBaseBreakpoint.Matches("ALIAS/claude-4"))
	assert.True(t, config.FallbackCacheBreakpoint.Matches("cLaUdE-oPuS-4"))
	assert.False(t, config.FallbackCacheBreakpoint.Matches("claude-sonnet-4"))

	// Every nested slice and scalar returned from the cache must be private.
	config.ForceExtendedTTL.ModelPrefixes[0] = "changed"
	config.FixVolatileBreakpoint.ModelPrefixes[0] = "changed"
	config.RetainPreviousBreakpoint.ModelPrefixes = append(config.RetainPreviousBreakpoint.ModelPrefixes, "changed")
	config.StableBaseBreakpoint.ModelPrefixes[0] = "changed"
	config.FallbackCacheBreakpoint.ModelPrefixes[0] = "changed"
	config.FallbackCacheBreakpoint.Enabled = false
	config.ForceExtendedTTL.Enabled = false
	assert.Equal(t, stored.ClaudeCacheOptimization, GetCustomerClaudeCacheOptimization(customer.CustomerId))

	update.SessionRoutingModelPrefixes = map[string]bool{"routing-": true}
	require.NoError(t, update.Update())
	assert.True(t, CustomerSessionRoutingEnabledForModel(customer.CustomerId, "routing-model"))

	// An old API payload omits the new field but still updates ordinary fields.
	legacy := &Customer{Id: customer.Id, CustomerId: customer.CustomerId, Name: "legacy update"}
	require.NoError(t, legacy.Update())
	assert.True(t, CustomerSessionRoutingEnabledForModel(customer.CustomerId, "routing-model"), "omitted legacy field must preserve the explicit customer setting")
	preserved, err := GetCustomerById(customer.Id)
	require.NoError(t, err)
	assert.Equal(t, "legacy update", preserved.Name)
	assert.Equal(t, stored.ClaudeCacheOptimization, preserved.ClaudeCacheOptimization)
	assert.Equal(t, stored.ClaudeCacheOptimization, GetCustomerClaudeCacheOptimization(customer.CustomerId))

	legacy.ClaudeCacheOptimization = &CustomerClaudeCacheOptimization{}
	require.NoError(t, legacy.Update())
	disabledCustomer, err := GetCustomerById(customer.Id)
	require.NoError(t, err)
	require.NotNil(t, disabledCustomer.ClaudeCacheOptimization)
	assert.Equal(t, legacy.ClaudeCacheOptimization, disabledCustomer.ClaudeCacheOptimization)
	assert.False(t, GetCustomerClaudeCacheOptimization(customer.CustomerId).ForceExtendedTTL.Matches("claude-sonnet"))
	assert.False(t, GetCustomerClaudeCacheOptimization(customer.CustomerId).FallbackCacheBreakpoint.Matches("claude-opus"))

	// Stored four-rule configurations keep their behavior with the fifth absent.
	require.NoError(t, DB.Model(&Customer{}).Where("id = ?", customer.Id).
		Update("claude_cache_optimization_json", `{
			"force_extended_ttl":{"enabled":true},
			"fix_volatile_breakpoint":{"enabled":true},
			"retain_previous_breakpoint":{"enabled":true},
			"stable_base_breakpoint":{"enabled":true}
		}`).Error)
	require.NoError(t, refreshCustomerCache())
	old, err := GetCustomerById(customer.Id)
	require.NoError(t, err)
	require.NotNil(t, old.ClaudeCacheOptimization)
	oldConfig := GetCustomerClaudeCacheOptimization(customer.CustomerId)
	require.NotNil(t, oldConfig)
	assert.Equal(t, old.ClaudeCacheOptimization, oldConfig)
	assert.True(t, oldConfig.ForceExtendedTTL.Matches("claude-opus"))
	assert.True(t, oldConfig.FixVolatileBreakpoint.Matches("claude-opus"))
	assert.True(t, oldConfig.RetainPreviousBreakpoint.Matches("claude-opus"))
	assert.True(t, oldConfig.StableBaseBreakpoint.Matches("claude-opus"))
	assert.False(t, oldConfig.FallbackCacheBreakpoint.Enabled)
	assert.Empty(t, oldConfig.FallbackCacheBreakpoint.ModelPrefixes)
	assert.False(t, oldConfig.FallbackCacheBreakpoint.Matches("claude-opus"))

	require.NoError(t, DB.Model(&Customer{}).Where("id = ?", customer.Id).
		Update("claude_cache_optimization_json", `{"force_extended_ttl":{"enabled":true},"stable_base_breakpoint":false}`).Error)
	require.NoError(t, refreshCustomerCache())
	corrupt := GetCustomerClaudeCacheOptimization(customer.CustomerId)
	require.NotNil(t, corrupt, "corrupt explicit configuration must not inherit globals")
	assert.False(t, corrupt.ForceExtendedTTL.Matches("claude-sonnet"))
	assert.False(t, corrupt.FallbackCacheBreakpoint.Matches("claude-opus"))
}

func TestCustomerClaudeCacheOptimizationHydration(t *testing.T) {
	for _, tc := range []struct {
		name      string
		discounts string
		config    string
		wantNil   bool
		wantMatch bool
	}{
		{"legacy empty", "", "", true, false},
		{"stored null", "", "null", true, false},
		{"no discounts", "", `{"force_extended_ttl":{"enabled":true,"model_prefixes":[" ",""]}}`, false, true},
		{"invalid discounts", "{", `{"force_extended_ttl":{"enabled":true}}`, false, true},
		{"invalid config", "", "{", false, false},
		{"wrong config type", "", "[]", false, false},
		{"explicit off", `{"claude":0.8}`, "{}", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			customer := &Customer{
				PrefixDiscounts: tc.discounts, ClaudeCacheOptimizationJSON: tc.config,
				ClaudeCacheOptimization: &CustomerClaudeCacheOptimization{
					ForceExtendedTTL: CustomerClaudeCacheOptimizationRule{Enabled: true},
				},
			}
			customer.hydrate()
			if tc.wantNil {
				assert.Nil(t, customer.ClaudeCacheOptimization)
				return
			}
			require.NotNil(t, customer.ClaudeCacheOptimization)
			assert.Equal(t, tc.wantMatch, customer.ClaudeCacheOptimization.ForceExtendedTTL.Matches("claude-sonnet"))
		})
	}
}

func TestCustomerClaudeCacheOptimizationRuleMatches(t *testing.T) {
	for _, tc := range []struct {
		name     string
		rule     CustomerClaudeCacheOptimizationRule
		model    string
		expected bool
	}{
		{"off with empty prefixes", CustomerClaudeCacheOptimizationRule{}, "claude-sonnet", false},
		{"all Claude path aliases", CustomerClaudeCacheOptimizationRule{Enabled: true}, "custom-alias", true},
		{"literal prefix", CustomerClaudeCacheOptimizationRule{Enabled: true, ModelPrefixes: []string{"claude-*"}}, "claude-sonnet", false},
		{"case insensitive trimmed prefix", CustomerClaudeCacheOptimizationRule{Enabled: true, ModelPrefixes: []string{" Claude-SONNET "}}, "CLAUDE-sonnet-4", true},
		{"prefix not substring", CustomerClaudeCacheOptimizationRule{Enabled: true, ModelPrefixes: []string{"claude"}}, "other/claude", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.rule.Matches(tc.model))
		})
	}
}

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

func TestCustomerSessionRoutingModelPrefixResolution(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Customer{}))
	customer := &Customer{
		CustomerId: "session-routing-prefix-test",
		SessionRoutingModelPrefixes: map[string]bool{
			"claude-":        true,
			"claude-sonnet-": false,
			"gpt-":           false,
		},
	}
	require.NoError(t, customer.Insert())
	t.Cleanup(func() { require.NoError(t, DeleteCustomerById(customer.Id)) })

	assert.True(t, CustomerSessionRoutingEnabledForModel(customer.CustomerId, "claude-opus-4"))
	assert.False(t, CustomerSessionRoutingEnabledForModel(customer.CustomerId, "claude-sonnet-4"))
	assert.False(t, CustomerSessionRoutingEnabledForModel(customer.CustomerId, "gpt-4o"))
	assert.False(t, CustomerSessionRoutingEnabledForModel(customer.CustomerId, "gemini-2.5"))
}
