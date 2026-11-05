package model

import (
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// HasEnabledNonstandardChannel uses the same exact/normalized model lookup as
// channel selection. The category probe is independent of request path filters:
// an unsupported endpoint must not silently bypass a configured routing split.
func HasEnabledNonstandardChannel(group, modelName string) (bool, error) {
	filters := []dto.ChannelFilter{{Kind: dto.FilterSessionRouting, NonstandardSession: true}}
	if !common.MemoryCacheEnabled {
		abilities := DB.Model(&Ability{}).Select("channel_id").
			Where(commonGroupCol+" = ? AND model IN ? AND enabled = ?", group,
				[]string{modelName, ratio_setting.RoutingMatchModelName(modelName)}, true)
		var names []string
		err := DB.Model(&Channel{}).Where("id IN (?) AND status = ?", abilities, common.ChannelStatusEnabled).
			Pluck("name", &names).Error
		if err != nil {
			return false, err
		}
		return slices.ContainsFunc(names, func(name string) bool { return strings.HasSuffix(name, "-非标") }), nil
	}
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()
	for _, name := range []string{modelName, ratio_setting.RoutingMatchModelName(modelName)} {
		for _, id := range group2model2channels[group][name] {
			ch := channelsIDM[id]
			if ch != nil && ch.Status == common.ChannelStatusEnabled {
				if ok, _ := ChannelSatisfiesFilters(ch, modelName, filters); ok {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

func IsChannelEnabledForGroupModel(group string, modelName string, channelID int) bool {
	if group == "" || modelName == "" || channelID <= 0 {
		return false
	}
	if !common.MemoryCacheEnabled {
		return isChannelEnabledForGroupModelDB(group, modelName, channelID)
	}

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	if group2model2channels == nil {
		return false
	}

	if isChannelIDInList(group2model2channels[group][modelName], channelID) {
		return true
	}
	normalized := ratio_setting.RoutingMatchModelName(modelName)
	if normalized != "" && normalized != modelName {
		return isChannelIDInList(group2model2channels[group][normalized], channelID)
	}
	return false
}

func IsChannelEnabledForAnyGroupModel(groups []string, modelName string, channelID int) bool {
	if len(groups) == 0 {
		return false
	}
	for _, g := range groups {
		if IsChannelEnabledForGroupModel(g, modelName, channelID) {
			return true
		}
	}
	return false
}

func isChannelEnabledForGroupModelDB(group string, modelName string, channelID int) bool {
	var count int64
	err := DB.Model(&Ability{}).
		Where(commonGroupCol+" = ? and model = ? and channel_id = ? and enabled = ?", group, modelName, channelID, true).
		Count(&count).Error
	if err == nil && count > 0 {
		return true
	}
	normalized := ratio_setting.RoutingMatchModelName(modelName)
	if normalized == "" || normalized == modelName {
		return false
	}
	count = 0
	err = DB.Model(&Ability{}).
		Where(commonGroupCol+" = ? and model = ? and channel_id = ? and enabled = ?", group, normalized, channelID, true).
		Count(&count).Error
	return err == nil && count > 0
}

func isChannelIDInList(list []int, channelID int) bool {
	return slices.Contains(list, channelID)
}
