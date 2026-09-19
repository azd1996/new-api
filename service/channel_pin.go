package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// pinnedChannelContextKey stores a soft-pinned channel id on the request context.
//
// "Soft pin" means channel selection prefers this channel but transparently
// falls back to normal selection when the channel is unavailable. This is
// distinct from the token-bound specific_channel_id, which is a hard pin that
// also disables retry (see controller.shouldRetry). The error-triggered
// original-channel retry uses this to resend a rewritten request to the same
// channel that just failed, instead of demoting to a lower-priority channel.
const pinnedChannelContextKey constant.ContextKey = "retry_pinned_channel_id"

// pinnedChannelLookup resolves a channel by id. It is a package variable so
// tests can substitute a deterministic lookup without a database/cache.
var pinnedChannelLookup = model.CacheGetChannel

// SetPinnedChannel marks channelId as the preferred channel for the next
// selection. A non-positive id clears the pin.
func SetPinnedChannel(c *gin.Context, channelId int) {
	if c == nil {
		return
	}
	common.SetContextKey(c, pinnedChannelContextKey, channelId)
}

// ClearPinnedChannel removes any soft pin so subsequent selections are normal.
func ClearPinnedChannel(c *gin.Context) {
	if c == nil {
		return
	}
	common.SetContextKey(c, pinnedChannelContextKey, 0)
}

// GetPinnedChannel returns the soft-pinned channel id, if a positive one is set.
func GetPinnedChannel(c *gin.Context) (int, bool) {
	if c == nil {
		return 0, false
	}
	value, ok := common.GetContextKey(c, pinnedChannelContextKey)
	if !ok {
		return 0, false
	}
	id, ok := value.(int)
	if !ok || id <= 0 {
		return 0, false
	}
	return id, true
}

// resolvePinnedChannel returns the soft-pinned channel and its effective group
// when a pin is set and the channel is currently usable (exists + enabled).
// Otherwise it returns ok=false so the caller falls back to normal selection.
//
// Scope note: this is used by the original-channel retry, where the pinned
// channel just served the same request and therefore already serves the
// requested group+model. A general external pin (e.g. a future
// X-SPECIFIC_CHANNEL header) should additionally verify group+model serving.
func resolvePinnedChannel(param *RetryParam) (*model.Channel, string, bool) {
	if param == nil {
		return nil, "", false
	}
	channelId, ok := GetPinnedChannel(param.Ctx)
	if !ok {
		return nil, "", false
	}
	channel, err := pinnedChannelLookup(channelId)
	if err != nil || channel == nil || channel.Status != common.ChannelStatusEnabled {
		return nil, "", false
	}
	return channel, pinnedSelectGroup(param), true
}

// pinnedSelectGroup resolves the group to report for a pinned selection: the
// token group normally, or the previously auto-selected group when using "auto".
func pinnedSelectGroup(param *RetryParam) string {
	if param.TokenGroup != "auto" {
		return param.TokenGroup
	}
	if value, ok := common.GetContextKey(param.Ctx, constant.ContextKeyAutoGroup); ok {
		if group, ok := value.(string); ok && group != "" {
			return group
		}
	}
	return param.TokenGroup
}
