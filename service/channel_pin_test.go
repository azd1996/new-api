package service

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPinTestContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	return c
}

func TestPinnedChannelContextRoundTrip(t *testing.T) {
	c := newPinTestContext()

	_, ok := GetPinnedChannel(c)
	assert.False(t, ok, "no pin set initially")

	SetPinnedChannel(c, 42)
	id, ok := GetPinnedChannel(c)
	require.True(t, ok)
	assert.Equal(t, 42, id)

	ClearPinnedChannel(c)
	_, ok = GetPinnedChannel(c)
	assert.False(t, ok, "cleared pin must not resolve")

	SetPinnedChannel(c, 0)
	_, ok = GetPinnedChannel(c)
	assert.False(t, ok, "non-positive id must not resolve")
}

func TestResolvePinnedChannel(t *testing.T) {
	original := pinnedChannelLookup
	t.Cleanup(func() { pinnedChannelLookup = original })

	t.Run("no pin falls back", func(t *testing.T) {
		pinnedChannelLookup = func(int) (*model.Channel, error) {
			t.Fatal("lookup must not be called when no pin is set")
			return nil, nil
		}
		_, _, ok := resolvePinnedChannel(&RetryParam{Ctx: newPinTestContext(), TokenGroup: "default"})
		assert.False(t, ok)
	})

	t.Run("pin hit returns enabled channel", func(t *testing.T) {
		pinnedChannelLookup = func(id int) (*model.Channel, error) {
			return &model.Channel{Id: id, Status: common.ChannelStatusEnabled}, nil
		}
		c := newPinTestContext()
		SetPinnedChannel(c, 7)
		channel, group, ok := resolvePinnedChannel(&RetryParam{Ctx: c, TokenGroup: "default"})
		require.True(t, ok)
		assert.Equal(t, 7, channel.Id)
		assert.Equal(t, "default", group)
	})

	t.Run("lookup error falls back", func(t *testing.T) {
		pinnedChannelLookup = func(int) (*model.Channel, error) {
			return nil, errors.New("channel not found")
		}
		c := newPinTestContext()
		SetPinnedChannel(c, 7)
		_, _, ok := resolvePinnedChannel(&RetryParam{Ctx: c, TokenGroup: "default"})
		assert.False(t, ok, "unresolvable pin must fall back to normal selection")
	})

	t.Run("disabled channel falls back", func(t *testing.T) {
		pinnedChannelLookup = func(id int) (*model.Channel, error) {
			return &model.Channel{Id: id, Status: common.ChannelStatusManuallyDisabled}, nil
		}
		c := newPinTestContext()
		SetPinnedChannel(c, 7)
		_, _, ok := resolvePinnedChannel(&RetryParam{Ctx: c, TokenGroup: "default"})
		assert.False(t, ok, "disabled pinned channel must fall back")
	})

	t.Run("auto group uses previously selected group", func(t *testing.T) {
		pinnedChannelLookup = func(id int) (*model.Channel, error) {
			return &model.Channel{Id: id, Status: common.ChannelStatusEnabled}, nil
		}
		c := newPinTestContext()
		common.SetContextKey(c, constant.ContextKeyAutoGroup, "vip")
		SetPinnedChannel(c, 7)
		_, group, ok := resolvePinnedChannel(&RetryParam{Ctx: c, TokenGroup: "auto"})
		require.True(t, ok)
		assert.Equal(t, "vip", group)
	})
}
