package helper

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStreamScannerProcessShutdownClosesIdleUpstream proves a process-shutdown
// force-cancel unblocks a stream idling on upstream reads and ends it with the
// shutdown marker, distinct from the ClientGone path exercised by
// TestStreamScannerHandler_ClientCancelAbortsUpstreamAndReturns.
func TestStreamScannerProcessShutdownClosesIdleUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registry := common.NewRelayRequestRegistry()
	ctx, finish, ok := registry.Begin(context.Background())
	require.True(t, ok)
	defer finish()
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(ctx)
	info := &relaycommon.RelayInfo{DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{}}
	firstChunk := make(chan struct{})
	returned := make(chan struct{})
	go func() {
		StreamScannerHandler(c, &http.Response{Body: reader}, info, func(data string, result *StreamResult) {
			if data == "first" {
				close(firstChunk)
			}
		})
		close(returned)
	}()
	_, err := io.WriteString(writer, "data: first\n")
	require.NoError(t, err)
	select {
	case <-firstChunk:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not process the first chunk")
	}
	shutdown := common.ProcessShutdownInfo{BatchID: "stream-stop", Reason: "shutdown_timeout"}
	registry.ForceCancel(shutdown)
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not unblock the idle stream")
	}
	_, err = io.WriteString(writer, "data: late\n")
	assert.ErrorIs(t, err, io.ErrClosedPipe)
	assert.Equal(t, &shutdown, common.ProcessShutdownFromContext(c.Request.Context()))
	assert.Equal(t, relaycommon.StreamEndReasonScannerErr, info.StreamStatus.EndReason)
}
