package service

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelSelectAutoGroupsTest(t *testing.T) *gorm.DB {
	t.Helper()

	originalDB := model.DB
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalRetryTimes := common.RetryTimes
	originalAutoGroups := setting.AutoGroups2JsonString()
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	originalMaxTokenAutoGroups := setting.GetMaxTokenAutoGroups()
	originalAffinity := *operation_setting.GetChannelAffinitySetting()
	originalRouting := *operation_setting.GetSessionRoutingSetting()
	originalRedisEnabled := common.RedisEnabled

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	model.DB = db
	common.MemoryCacheEnabled = true
	common.RetryTimes = 0
	common.RedisEnabled = false
	ClearChannelAffinityCacheAll()
	*operation_setting.GetChannelAffinitySetting() = operation_setting.ChannelAffinitySetting{
		Enabled: true, SessionMode: "prefer", DefaultTTLSeconds: 60,
		Rules: []operation_setting.ChannelAffinityRule{{
			Name: "session-routing-test", ModelRegex: []string{"^routing-model$"},
			PathRegex: []string{"^/v1/chat/completions$"}, UserAgentInclude: []string{"routing-test"},
			ValueRegex: "^session-", IncludeRuleName: true, IncludeUsingGroup: true,
			KeySources: []operation_setting.ChannelAffinityKeySource{
				{Type: "request_header", Key: "X-Session"},
				{Type: "gjson", Path: "metadata.session"},
			},
		}},
	}
	*operation_setting.GetSessionRoutingSetting() = operation_setting.SessionRoutingSetting{GroupEnabled: map[string]bool{}}

	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`[]`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":2}`))
	require.NoError(t, setting.UpdateMaxTokenAutoGroups("2"))

	t.Cleanup(func() {
		ClearChannelAffinityCacheAll()
		*operation_setting.GetChannelAffinitySetting() = originalAffinity
		*operation_setting.GetSessionRoutingSetting() = originalRouting
		common.RedisEnabled = originalRedisEnabled
		// Clear the test channels even when there was no original database cache.
		require.NoError(t, db.Where("1 = 1").Delete(&model.Ability{}).Error)
		require.NoError(t, db.Where("1 = 1").Delete(&model.Channel{}).Error)
		model.InitChannelCache()
		model.DB = originalDB
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		common.RetryTimes = originalRetryTimes
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(originalAutoGroups))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
		require.NoError(t, setting.UpdateMaxTokenAutoGroups(fmt.Sprintf("%d", originalMaxTokenAutoGroups)))

		if originalMemoryCacheEnabled && originalDB != nil &&
			originalDB.Migrator().HasTable(&model.Channel{}) && originalDB.Migrator().HasTable(&model.Ability{}) {
			model.InitChannelCache()
		}
		sqlDB, err := db.DB()
		if err == nil {
			require.NoError(t, sqlDB.Close())
		}
	})

	return db
}

func sessionRoutingTestRequest(t *testing.T, group, header, body string) (*gin.Context, *RetryParam) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Request.Header.Set("User-Agent", "routing-test")
	if header != "" {
		ctx.Request.Header.Set("X-Session", header)
	}
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, group)
	t.Cleanup(func() { common.CleanupBodyStorage(ctx) })
	return ctx, &RetryParam{Ctx: ctx, TokenGroup: group, ModelName: "routing-model", RequestPath: ctx.Request.URL.Path}
}

func createSessionRoutingTestChannel(t *testing.T, db *gorm.DB, id int, group, modelName string, nonstandard bool, status int) {
	t.Helper()
	createChannelSelectAutoGroupsChannel(t, db, id, group, modelName)
	name := fmt.Sprintf("routing-channel-%d", id)
	if nonstandard {
		name += "-非标"
	}
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", id).Updates(map[string]any{"name": name, "status": status}).Error)
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", id).Update("enabled", status == common.ChannelStatusEnabled).Error)
}

func TestSessionRoutingSelectsByGroupModelAndEnabledChannels(t *testing.T) {
	for _, tc := range []struct {
		name          string
		group         string
		nonstdModel   string
		status        int
		disabled      bool
		explicitAllow bool
		standard      bool
		want          int
	}{
		{name: "automatic nonstandard", group: "behind-cdnaigw_team", nonstdModel: "routing-model", status: common.ChannelStatusEnabled, want: 2202},
		{name: "standard request", group: "behind-cdnaigw_team", nonstdModel: "routing-model", status: common.ChannelStatusEnabled, standard: true, want: 2201},
		{name: "no nonstandard channel", group: "behind-cdnaigw_team", want: 2201},
		{name: "nonstandard only serves another model", group: "behind-cdnaigw_team", nonstdModel: "other-model", status: common.ChannelStatusEnabled, want: 2201},
		{name: "all manually disabled", group: "behind-cdnaigw_team", nonstdModel: "routing-model", status: common.ChannelStatusManuallyDisabled, want: 2201},
		{name: "all automatically disabled", group: "behind-cdnaigw_team", nonstdModel: "routing-model", status: common.ChannelStatusAutoDisabled, want: 2201},
		{name: "explicit off", group: "behind-cdnaigw_team", nonstdModel: "routing-model", status: common.ChannelStatusEnabled, disabled: true, want: 2201},
		{name: "explicit allow without nonstandard", group: "behind-cdnaigw_team", explicitAllow: true, want: 2201},
		{name: "ordinary group ignores off and classification", group: "ordinary", nonstdModel: "routing-model", status: common.ChannelStatusEnabled, disabled: true, standard: true, want: 2202},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupChannelSelectAutoGroupsTest(t)
			createSessionRoutingTestChannel(t, db, 2201, tc.group, "routing-model", false, common.ChannelStatusEnabled)
			if tc.nonstdModel != "" {
				createSessionRoutingTestChannel(t, db, 2202, tc.group, tc.nonstdModel, true, tc.status)
				// The ordinary group's winner is deterministic without routing filters.
				require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 2202).Update("priority", 10).Error)
				require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", 2202).Update("priority", 10).Error)
			}
			if tc.disabled || tc.explicitAllow {
				operation_setting.GetSessionRoutingSetting().GroupEnabled[tc.group] = !tc.disabled
			}
			model.InitChannelCache()
			header := ""
			if tc.standard {
				header = "session-new"
			}
			ctx, param := sessionRoutingTestRequest(t, tc.group, header, `{}`)
			channel, group, selectErr := SelectChannelForRequest(ctx, param.ModelName, param)
			require.Nil(t, selectErr)
			require.NotNil(t, channel)
			assert.Equal(t, tc.want, channel.Id)
			assert.Equal(t, tc.group, group)
			assert.Zero(t, param.GetRetry(), "direct standard selection must not consume a retry")
		})
	}
}

func TestSessionRoutingSharesAffinityRulesWithoutRequiringBinding(t *testing.T) {
	for _, tc := range []struct {
		name, header, body, mode, path, agent string
		bindingOff                            bool
		want                                  int
	}{
		{name: "header cache miss", header: "session-header", body: `{}`, want: 2301},
		{name: "body cache miss", body: `{"metadata":{"session":"session-body"}}`, want: 2301},
		{name: "binding disabled header", header: "session-header", body: `{}`, bindingOff: true, want: 2301},
		{name: "binding disabled body", body: `{"metadata":{"session":"session-body"}}`, bindingOff: true, want: 2301},
		{name: "session mode off", header: "session-header", body: `{}`, mode: "off", want: 2301},
		{name: "invalid value", header: "invalid", body: `{}`, want: 2302},
		{name: "path mismatch", header: "session-header", body: `{}`, path: "/v1/responses", want: 2302},
		{name: "user agent mismatch", header: "session-header", body: `{}`, agent: "other-client", want: 2302},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupChannelSelectAutoGroupsTest(t)
			const group = "behind-cdnaigw_rules"
			createSessionRoutingTestChannel(t, db, 2301, group, "routing-model", false, common.ChannelStatusEnabled)
			createSessionRoutingTestChannel(t, db, 2302, group, "routing-model", true, common.ChannelStatusEnabled)
			model.InitChannelCache()
			affinity := operation_setting.GetChannelAffinitySetting()
			affinity.Enabled = !tc.bindingOff
			if tc.mode != "" {
				affinity.Rules[0].SessionMode = tc.mode
			}
			ctx, param := sessionRoutingTestRequest(t, group, tc.header, tc.body)
			if tc.path != "" {
				ctx.Request.URL.Path = tc.path
				param.RequestPath = tc.path
			}
			if tc.agent != "" {
				ctx.Request.Header.Set("User-Agent", tc.agent)
			}
			_, found := GetPreferredChannelByAffinity(ctx, param.ModelName, group)
			assert.False(t, found, "a fresh session has no binding")
			channel, _, selectErr := SelectChannelForRequest(ctx, param.ModelName, param)
			require.Nil(t, selectErr)
			require.NotNil(t, channel)
			assert.Equal(t, tc.want, channel.Id)
		})
	}
}

func TestSessionRoutingRetryUsesStandardWithoutReclassification(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const group = "behind-cdnaigw_retry"
	createSessionRoutingTestChannel(t, db, 2401, group, "routing-model", false, common.ChannelStatusEnabled)
	createSessionRoutingTestChannel(t, db, 2402, group, "routing-model", true, common.ChannelStatusEnabled)
	model.InitChannelCache()
	ctx, param := sessionRoutingTestRequest(t, group, "", `{}`)
	first, _, selectErr := SelectChannelForRequest(ctx, param.ModelName, param)
	require.Nil(t, selectErr)
	require.NotNil(t, first)
	assert.Equal(t, 2402, first.Id)
	// Simulate a later request rewrite; classification must retain the original.
	ctx.Request.Header.Set("X-Session", "session-added-later")
	param.SetRetry(1)
	second, _, selectErr := SelectChannelForRequest(ctx, param.ModelName, param)
	require.Nil(t, selectErr)
	require.NotNil(t, second)
	assert.Equal(t, 2401, second.Id)
	// RetryParam alone must enable fallback, even without policy attempt counters.
	assert.Zero(t, RequestPolicy(ctx).Attempts)
	param.SetRetry(0)
	again, _, selectErr := SelectChannelForRequest(ctx, param.ModelName, param)
	require.Nil(t, selectErr)
	require.NotNil(t, again)
	assert.Equal(t, 2402, again.Id, "rewritten header must not turn the original nonstandard request into standard")
}

func TestSessionRoutingInactiveSplitPreservesRetryPriority(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("disabled=%t", disabled), func(t *testing.T) {
			db := setupChannelSelectAutoGroupsTest(t)
			const group = "behind-cdnaigw_priority"
			createSessionRoutingTestChannel(t, db, 2411, group, "routing-model", false, common.ChannelStatusEnabled)
			createSessionRoutingTestChannel(t, db, 2412, group, "routing-model", false, common.ChannelStatusEnabled)
			require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 2411).Update("priority", 10).Error)
			require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", 2411).Update("priority", 10).Error)
			if disabled {
				createSessionRoutingTestChannel(t, db, 2413, group, "routing-model", true, common.ChannelStatusEnabled)
				operation_setting.GetSessionRoutingSetting().GroupEnabled[group] = false
			}
			model.InitChannelCache()
			ctx, param := sessionRoutingTestRequest(t, group, "", `{}`)
			first, _, selectErr := SelectChannelForRequest(ctx, param.ModelName, param)
			require.Nil(t, selectErr)
			require.NotNil(t, first)
			assert.Equal(t, 2411, first.Id)
			param.SetRetry(1)
			second, _, selectErr := SelectChannelForRequest(ctx, param.ModelName, param)
			require.Nil(t, selectErr)
			require.NotNil(t, second)
			assert.Equal(t, 2412, second.Id, "inactive splitting must retain normal priority fallback")
		})
	}
}

func TestSessionRoutingCustomerSwitchForcesStandardPool(t *testing.T) {
	t.Setenv(common.TraceTagKeyEnv, "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
	db := setupChannelSelectAutoGroupsTest(t)
	require.NoError(t, db.AutoMigrate(&model.Customer{}))
	customer := &model.Customer{
		CustomerId: "api-key-004", Name: "Routing customer",
		SessionRoutingModelPrefixes: map[string]bool{"routing-": false},
	}
	require.NoError(t, customer.Insert())
	t.Cleanup(func() {
		require.NoError(t, model.DeleteCustomerById(customer.Id))
	})
	const group = "behind-cdnaigw_customer"
	createSessionRoutingTestChannel(t, db, 2441, group, "routing-model", false, common.ChannelStatusEnabled)
	createSessionRoutingTestChannel(t, db, 2442, group, "routing-model", true, common.ChannelStatusEnabled)
	model.InitChannelCache()

	const traceTag = "C7J42H35CV56G2VKWLMPH72GEKF24S72RCDZOG6KAXXIJ5Q5D2K7LNKIH3R7D5KGLDOMTOX3XGH7HBRLCWQMCZKGHY7EXQBCLVWUATI"
	ctx, param := sessionRoutingTestRequest(t, group, "", `{}`)
	ctx.Request.Header.Set(common.TraceTagHeader, traceTag)
	require.NoError(t, customer.Update())

	channel, _, selectErr := SelectChannelForRequest(ctx, param.ModelName, param)
	require.Nil(t, selectErr)
	require.NotNil(t, channel)
	assert.Equal(t, 2441, channel.Id)
}

func TestSessionRoutingAutoUsesActualGroupSwitch(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const offGroup, onGroup = "behind-cdnaigw_off", "behind-cdnaigw_on"
	createSessionRoutingTestChannel(t, db, 2501, offGroup, "routing-model", true, common.ChannelStatusEnabled)
	createSessionRoutingTestChannel(t, db, 2502, onGroup, "routing-model", false, common.ChannelStatusEnabled)
	createSessionRoutingTestChannel(t, db, 2503, onGroup, "routing-model", true, common.ChannelStatusEnabled)
	operation_setting.GetSessionRoutingSetting().GroupEnabled = map[string]bool{offGroup: false, onGroup: true, "auto": false}
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","behind-cdnaigw_off":"Off","behind-cdnaigw_on":"On"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"behind-cdnaigw_off":1,"behind-cdnaigw_on":1}`))
	model.InitChannelCache()
	ctx, param := sessionRoutingTestRequest(t, "auto", "", `{}`)
	common.SetContextKey(ctx, constant.ContextKeyTokenAutoGroups, []string{offGroup, onGroup})
	channel, group, selectErr := SelectChannelForRequest(ctx, param.ModelName, param)
	require.Nil(t, selectErr)
	require.NotNil(t, channel)
	assert.Equal(t, 2503, channel.Id)
	assert.Equal(t, onGroup, group, "off group must not use its only nonstandard channel")
	assert.Equal(t, onGroup, common.GetContextKeyString(ctx, constant.ContextKeyAutoGroup))
}

func TestSessionRoutingRejectsConflictingHardPin(t *testing.T) {
	for _, standard := range []bool{false, true} {
		t.Run(fmt.Sprintf("standard=%t", standard), func(t *testing.T) {
			db := setupChannelSelectAutoGroupsTest(t)
			const group = "behind-cdnaigw_pin"
			createSessionRoutingTestChannel(t, db, 2601, group, "routing-model", false, common.ChannelStatusEnabled)
			createSessionRoutingTestChannel(t, db, 2602, group, "routing-model", true, common.ChannelStatusEnabled)
			model.InitChannelCache()
			header, pinned := "", 2601
			if standard {
				header, pinned = "session-pin", 2602
			}
			ctx, param := sessionRoutingTestRequest(t, group, header, `{}`)
			GetChannelConstraints(ctx).AddPin(dto.ChannelPin{ChannelId: pinned, Source: dto.PinSourceToken, Rank: dto.PinRankToken})
			channel, _, selectErr := SelectChannelForRequest(ctx, param.ModelName, param)
			assert.Nil(t, channel)
			require.NotNil(t, selectErr)
			assert.Equal(t, 400, selectErr.StatusCode)
			assert.Equal(t, dto.FilterSessionRouting, selectErr.FilterKind)
		})
	}
}

func TestSessionRoutingStrictHistoricalBinding(t *testing.T) {
	for _, tc := range []struct {
		name     string
		oldID    int
		disabled bool
		want     int
	}{
		{name: "wrong category rebinds", oldID: 2702, want: 2701},
		{name: "disabled nonstandard rebinds to standard", oldID: 2702, disabled: true, want: 2701},
		{name: "disabled standard preserves strict", oldID: 2701, disabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupChannelSelectAutoGroupsTest(t)
			const group = "behind-cdnaigw_strict"
			createSessionRoutingTestChannel(t, db, 2701, group, "routing-model", false, common.ChannelStatusEnabled)
			createSessionRoutingTestChannel(t, db, 2702, group, "routing-model", true, common.ChannelStatusEnabled)
			// A replacement standard channel proves strict failure is not a lack of candidates.
			createSessionRoutingTestChannel(t, db, 2703, group, "routing-model", false, common.ChannelStatusEnabled)
			require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 2701).Update("priority", 10).Error)
			require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", 2701).Update("priority", 10).Error)
			operation_setting.GetChannelAffinitySetting().Rules[0].SessionMode = "strict"
			seed, seedParam := sessionRoutingTestRequest(t, group, "session-history", `{}`)
			_, found := GetPreferredChannelByAffinity(seed, seedParam.ModelName, group)
			require.False(t, found)
			RecordChannelAffinity(seed, tc.oldID)
			if tc.disabled {
				require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", tc.oldID).Update("status", common.ChannelStatusManuallyDisabled).Error)
				require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", tc.oldID).Update("enabled", false).Error)
			}
			model.InitChannelCache()
			ctx, param := sessionRoutingTestRequest(t, group, "session-history", `{}`)
			oldID, found := GetPreferredChannelByAffinity(ctx, param.ModelName, group)
			require.True(t, found)
			require.Equal(t, tc.oldID, oldID)
			channel, _, selectErr := SelectChannelForRequest(ctx, param.ModelName, param)
			if tc.want == 0 {
				assert.Nil(t, channel)
				require.NotNil(t, selectErr)
				assert.Equal(t, "strict_session_binding_unavailable", selectErr.Message)
				assert.Equal(t, 503, selectErr.StatusCode)
				return
			}
			require.Nil(t, selectErr)
			require.NotNil(t, channel)
			assert.Equal(t, tc.want, channel.Id)
			RecordChannelAffinity(ctx, channel.Id)
			next, nextParam := sessionRoutingTestRequest(t, group, "session-history", `{}`)
			rebound, found := GetPreferredChannelByAffinity(next, nextParam.ModelName, group)
			require.True(t, found)
			assert.Equal(t, tc.want, rebound, "the historical binding must be replaced")
		})
	}
}

func createChannelSelectAutoGroupsChannel(t *testing.T, db *gorm.DB, id int, group, modelName string) {
	t.Helper()
	priority := int64(0)
	weight := uint(100)
	require.NoError(t, db.Create(&model.Channel{
		Id:       id,
		Type:     constant.ChannelTypeOpenAI,
		Key:      fmt.Sprintf("key-%d", id),
		Status:   common.ChannelStatusEnabled,
		Name:     fmt.Sprintf("channel-%d", id),
		Weight:   &weight,
		Models:   modelName,
		Group:    group,
		Priority: &priority,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     group,
		Model:     modelName,
		ChannelId: id,
		Enabled:   true,
		Priority:  &priority,
		Weight:    weight,
	}).Error)
}

func TestCacheGetRandomSatisfiedChannelUsesTokenAutoGroupsWhenGlobalAutoIsEmpty(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const modelName = "auto-groups-runtime-model"
	createChannelSelectAutoGroupsChannel(t, db, 2101, "vip", modelName)
	createChannelSelectAutoGroupsChannel(t, db, 2102, "default", modelName)
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenAutoGroups, []string{"vip", "default"})
	common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, true)

	retry := 0
	param := &RetryParam{
		Ctx:         ctx,
		TokenGroup:  "auto",
		ModelName:   modelName,
		RequestPath: "/v1/chat/completions",
		Retry:       &retry,
	}

	first, selectedGroup, err := CacheGetRandomSatisfiedChannel(param)
	require.NoError(t, err)
	require.NotNil(t, first)
	assert.Equal(t, 2101, first.Id)
	assert.Equal(t, "vip", selectedGroup)
	assert.Equal(t, "vip", common.GetContextKeyString(ctx, constant.ContextKeyAutoGroup))
	assert.Empty(t, setting.GetAutoGroups(), "the selection must not depend on the global Auto list")

	param.IncreaseRetry()
	second, selectedGroup, err := CacheGetRandomSatisfiedChannel(param)
	require.NoError(t, err)
	require.NotNil(t, second)
	assert.Equal(t, 2102, second.Id)
	assert.Equal(t, "default", selectedGroup)
	assert.Equal(t, "default", common.GetContextKeyString(ctx, constant.ContextKeyAutoGroup))
}
