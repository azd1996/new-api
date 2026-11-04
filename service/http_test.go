package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestShouldCopyUpstreamHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("content-length is filtered", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		assert.False(t, ShouldCopyUpstreamHeader(ctx, "Content-Length", []string{"10"}))
	})

	t.Run("upstream request id header is filtered and never captured", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		assert.False(t, ShouldCopyUpstreamHeader(ctx, common.UpstreamRequestIdKey, []string{"should-not-pass-through"}))
		assert.Empty(t, ctx.GetString(common.UpstreamRequestIdKey))
	})

	t.Run("oneapi request id header is filtered and captured as upstream request id", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		assert.False(t, ShouldCopyUpstreamHeader(ctx, common.RequestIdKey, []string{"upstream-id-1"}))
		assert.Equal(t, "upstream-id-1", ctx.GetString(common.UpstreamRequestIdKey))
	})

	t.Run("oneapi request id header does not overwrite an already captured upstream request id", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Set(common.UpstreamRequestIdKey, "configured-header-id")
		assert.False(t, ShouldCopyUpstreamHeader(ctx, common.RequestIdKey, []string{"upstream-id-1"}))
		assert.Equal(t, "configured-header-id", ctx.GetString(common.UpstreamRequestIdKey))
	})

	t.Run("other headers are copied", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		assert.True(t, ShouldCopyUpstreamHeader(ctx, "X-Custom-Header", []string{"value"}))
	})
}
