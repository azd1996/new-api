package model

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/QuantumNous/new-api/pkg/logshipper"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestShipLogMapsEveryLogField pins the Log -> logshipper.Row mapping. Without
// it, a transposed assignment (prompt/completion tokens are the obvious pair)
// or a Log field added later but never mapped would ship wrong or default
// values into the billing table with nothing failing.
//
// Log.Id and Log.ChannelName are set below but intentionally not shipped: the
// cluster table has no column for either. Log.CreatedAt maps to Row.Ts.
func TestShipLogMapsEveryLogField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "newapi_logs.log")
	require.NoError(t, logshipper.Init(logshipper.Config{
		Filename:     path,
		InstanceName: "log_test3",
	}))
	t.Cleanup(func() { require.NoError(t, logshipper.Close()) })

	// Every value is distinct and non-zero so a swapped pair cannot pass.
	log := &Log{
		Id:                101,
		UserId:            102,
		CreatedAt:         1785477600,
		Type:              LogTypeConsume,
		Content:           "content-value",
		Username:          "username-value",
		TokenName:         "token-name-value",
		ModelName:         "model-name-value",
		Quota:             103,
		PromptTokens:      104,
		CompletionTokens:  105,
		UseTime:           106,
		IsStream:          true,
		ChannelId:         107,
		ChannelName:       "channel-name-not-persisted",
		TokenId:           108,
		Group:             "group-value",
		Ip:                "10.0.0.1",
		RequestId:         "request-id-value",
		UpstreamRequestId: "upstream-request-id-value",
		Other:             `{"matched_tier":"standard"}`,
	}
	shipLog(log)

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	require.True(t, scanner.Scan(), "shipLog wrote nothing")
	line := scanner.Bytes()

	var envelope struct {
		Data logshipper.Row `json:"data"`
	}
	require.NoError(t, json.Unmarshal(line, &envelope))

	assert.Equal(t, logshipper.Row{
		UserId:            102,
		Ts:                1785477600,
		Type:              LogTypeConsume,
		Content:           "content-value",
		Username:          "username-value",
		TokenName:         "token-name-value",
		ModelName:         "model-name-value",
		Quota:             103,
		PromptTokens:      104,
		CompletionTokens:  105,
		UseTime:           106,
		IsStream:          true,
		ChannelId:         107,
		TokenId:           108,
		GroupName:         "group-value",
		Ip:                "10.0.0.1",
		RequestId:         "request-id-value",
		UpstreamRequestId: "upstream-request-id-value",
		Other:             `{"matched_tier":"standard"}`,
		XInstanceName:     "log_test3",
	}, envelope.Data)

	// Catches a Row field added without a corresponding mapping: every field
	// above was given a non-zero value, so any zero remaining is unmapped.
	v := reflect.ValueOf(envelope.Data)
	for i := 0; i < v.NumField(); i++ {
		assert.Falsef(t, v.Field(i).IsZero(),
			"Row.%s is zero: shipLog is not mapping it", v.Type().Field(i).Name)
	}
}

// TestShipLogWhenDisabled protects createLog's contract: it calls shipLog
// unconditionally, so a disabled shipper must be a silent no-op.
func TestShipLogWhenDisabled(t *testing.T) {
	require.False(t, logshipper.Enabled())
	shipLog(&Log{Id: 1, UserId: 2})
}
