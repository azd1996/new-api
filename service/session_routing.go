package service

import (
	"maps"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

const behindSessionGroupPrefix = "behind-cdnaigw_"

// SessionRoutingAllowsChannel validates a connection-bound channel for a new
// request without reselecting it or applying affinity-binding side effects.
func SessionRoutingAllowsChannel(c *gin.Context, modelName, group string, ch *model.Channel) (bool, error) {
	filters, err := sessionRoutingFilters(c, modelName, group, false)
	if err != nil {
		return false, err
	}
	ok, _ := model.ChannelSatisfiesFilters(ch, modelName, filters)
	return ok, nil
}

// sessionRoutingGroups resolves a hard pin's scope without letting a synthetic
// auto group bypass its actual groups' routing policies.
func sessionRoutingGroups(c *gin.Context, tokenGroup string) []string {
	if tokenGroup != "auto" {
		return []string{tokenGroup}
	}
	return GetRequestAutoGroups(c, common.GetContextKeyString(c, constant.ContextKeyUserGroup))
}

// sessionRoutingBindingChanged only relaxes strict affinity for a category
// change within a group/model the old channel actually served. It must not
// turn a genuinely unavailable standard binding into a general fallback.
func sessionRoutingBindingChanged(c *gin.Context, group, modelName string, ch *model.Channel) (bool, error) {
	if ch == nil || !strings.HasPrefix(group, behindSessionGroupPrefix) ||
		!slices.Contains(strings.Split(ch.Group, ","), group) {
		return false, nil
	}
	servesModel := false
	for _, name := range ch.GetModels() {
		if name == modelName || name == ratio_setting.RoutingMatchModelName(modelName) {
			servesModel = true
			break
		}
	}
	if !servesModel {
		return false, nil
	}
	filters, err := sessionRoutingFilters(c, modelName, group, false)
	if err != nil {
		return false, err
	}
	_, kind := model.ChannelSatisfiesFilters(ch, modelName, filters)
	return kind == dto.FilterSessionRouting, nil
}

// sessionRoutingFilters resolves policy for the actual candidate group, never
// the synthetic "auto" group. Channel status is checked live; only the group's
// administrative switch and the original request classification are snapshots.
func sessionRoutingFilters(c *gin.Context, modelName, group string, retry bool) ([]dto.ChannelFilter, error) {
	filters := GetChannelConstraints(c).Filters
	if !strings.HasPrefix(group, behindSessionGroupPrefix) {
		return filters, nil
	}
	const settingsKey = "session_routing_group_snapshot"
	var enabled map[string]bool
	if value, ok := c.Get(settingsKey); ok {
		enabled = value.(map[string]bool)
	} else {
		enabled = maps.Clone(operation_setting.GetSessionRoutingSetting().GroupEnabled)
		c.Set(settingsKey, enabled)
	}
	_, _, customerID, decoded := common.TraceTagFromContext(c)
	if decoded && !model.CustomerSessionRoutingEnabled(customerID) {
		RequestPolicy(c).AddEvent(PolicyEvent{
			Group: group,
			Decision: PolicyDecision{
				Action: "standard", Reason: "customer_session_routing_disabled", Source: "session_routing",
			},
		})
		result := make([]dto.ChannelFilter, len(filters), len(filters)+1)
		copy(result, filters)
		return append(result, dto.ChannelFilter{Kind: dto.FilterSessionRouting}), nil
	}
	match := matchRequestChannelAffinity(c, modelName)
	if match.Err != nil {
		return nil, match.Err
	}
	nonstandard := match.Value == ""
	reason := "standard_request"
	useNonstandard := false
	if permitted, configured := enabled[group]; configured && !permitted {
		reason = "session_routing_disabled"
	} else if nonstandard {
		exists, err := model.HasEnabledNonstandardChannel(group, modelName)
		if err != nil {
			return nil, err
		}
		switch {
		case !exists:
			reason = "no_enabled_nonstandard_channel"
		case retry:
			reason = "nonstandard_retry_standard_channel"
		default:
			useNonstandard = true
			reason = "nonstandard_request"
		}
	}
	classification := "standard"
	if nonstandard {
		classification = "nonstandard"
	}
	RequestPolicy(c).AddEvent(PolicyEvent{
		Group: group, Rule: match.Rule.Name,
		Decision: PolicyDecision{Action: classification, Reason: reason, Source: "session_routing"},
	})
	result := make([]dto.ChannelFilter, len(filters), len(filters)+1)
	copy(result, filters)
	return append(result, dto.ChannelFilter{Kind: dto.FilterSessionRouting, NonstandardSession: useNonstandard}), nil
}

func getSessionRoutedChannel(param *RetryParam, group string, retry int) (*model.Channel, error) {
	isRetry := param.GetRetry() > 0 || RequestPolicy(param.Ctx).Attempts > 0
	filters, err := sessionRoutingFilters(param.Ctx, param.ModelName, group, isRetry)
	if err != nil {
		return nil, err
	}
	// A permitted retry enters the standard pool at its highest priority. It
	// consumes the existing attempt budget; this function never starts a retry.
	priorityRetry := retry
	managed := strings.HasPrefix(group, behindSessionGroupPrefix)
	firstFilters := filters
	switchingPool := false
	if managed && isRetry {
		firstFilters, err = sessionRoutingFilters(param.Ctx, param.ModelName, group, false)
		if err != nil {
			return nil, err
		}
		switchingPool = firstFilters[len(firstFilters)-1].NonstandardSession
		if switchingPool {
			priorityRetry = max(0, retry-1)
		}
	}
	channel, err := model.GetRandomSatisfiedChannel(group, param.ModelName, priorityRetry, filters)
	if err != nil || channel != nil || !switchingPool {
		return channel, err
	}
	// If no standard candidate serves this endpoint/model, an allowed retry can
	// still use the nonstandard pool. Explicit opt-out always excludes it.
	return model.GetRandomSatisfiedChannel(group, param.ModelName, retry, firstFilters)
}
