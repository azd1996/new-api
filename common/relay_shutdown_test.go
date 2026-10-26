package common

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayShutdownCancelsUpstreamAndWaitsForLogCleanup(t *testing.T) {
	registry := NewRelayRequestRegistry()
	parent, disconnect := context.WithCancel(context.Background())
	ctx, done, ok := registry.Begin(parent)
	require.True(t, ok)
	defer done()
	canceledWait, cancelWait := context.WithCancel(context.Background())
	cancelWait()
	disconnect()
	assert.NoError(t, RelayUpstreamContext(ctx).Err(), "client disconnect must not cancel usage collection")
	assert.Nil(t, ProcessShutdownFromContext(ctx))

	registry.Drain()
	_, _, accepted := registry.Begin(context.Background())
	assert.False(t, accepted)
	assert.ErrorIs(t, registry.Wait(canceledWait), context.Canceled, "handler has not finished logging")
	assert.NoError(t, RelayUpstreamContext(ctx).Err(), "graceful drain must allow completion")

	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	stop := TrackRelayCloser(ctx, writer)
	defer stop()
	info := ProcessShutdownInfo{Instance: "node-a", BatchID: "stop-1", At: 1792886400, Reason: "shutdown_timeout"}
	assert.Equal(t, 1, registry.ForceCancel(info))
	assert.ErrorIs(t, RelayUpstreamContext(ctx).Err(), context.Canceled)
	assert.Equal(t, &info, ProcessShutdownFromContext(ctx))
	readDone := make(chan error, 1)
	go func() {
		_, err := reader.Read(make([]byte, 1))
		readDone <- err
	}()
	select {
	case err := <-readDone:
		assert.ErrorIs(t, err, io.EOF, "forced shutdown must unblock registered sockets")
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown closer did not run")
	}
	assert.ErrorIs(t, registry.Wait(canceledWait), context.Canceled, "canceling I/O must not mark logging complete")
	done()
	require.NoError(t, registry.Wait(context.Background()))
}

func TestRelayShutdownDoesNotMarkCompletedRequests(t *testing.T) {
	registry := NewRelayRequestRegistry()
	finishedCtx, done, ok := registry.Begin(context.Background())
	require.True(t, ok)
	done()
	assert.Zero(t, registry.ForceCancel(ProcessShutdownInfo{BatchID: "stop-2"}))
	assert.Nil(t, ProcessShutdownFromContext(finishedCtx))
	assert.Nil(t, AttachProcessShutdownInfo(finishedCtx, nil))
	require.NoError(t, registry.Wait(context.Background()))
}
