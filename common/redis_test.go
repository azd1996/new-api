package common

import (
	"context"
	"testing"

	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeRedisKeyPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{name: "empty", input: "", expect: ""},
		{name: "spaces", input: "   ", expect: ""},
		{name: "trim colons", input: ":shared:", expect: "shared:"},
		{name: "append colon", input: "new-api", expect: "new-api:"},
		{name: "preserve internal colons", input: "cluster:one", expect: "cluster:one:"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expect, normalizeRedisKeyPrefix(tt.input))
		})
	}
}

func TestRedisKeyPrefixHookPrefixesCommands(t *testing.T) {
	t.Parallel()

	hook := redisKeyPrefixHook{prefix: "shared:"}
	ctx := context.Background()

	cmd := redis.NewCmd(ctx, "get", "user:1")
	hook.BeforeProcess(ctx, cmd)
	require.Equal(t, "shared:user:1", cmd.Args()[1])

	mset := redis.NewCmd(ctx, "mset", "user:1", "1", "token:2", "2")
	hook.BeforeProcess(ctx, mset)
	require.Equal(t, "shared:user:1", mset.Args()[1])
	require.Equal(t, "shared:token:2", mset.Args()[3])

	eval := redis.NewCmd(ctx, "evalsha", "sha", 2, "rate:one", "rate:two", "payload")
	hook.BeforeProcess(ctx, eval)
	require.Equal(t, "shared:rate:one", eval.Args()[3])
	require.Equal(t, "shared:rate:two", eval.Args()[4])

	scan := redis.NewCmd(ctx, "scan", 0, "match", "user:*", "count", 10)
	hook.BeforeProcess(ctx, scan)
	require.Equal(t, "shared:user:*", scan.Args()[3])

	hscan := redis.NewCmd(ctx, "hscan", "hash:key", 0, "match", "field:*", "count", 10)
	hook.BeforeProcess(ctx, hscan)
	require.Equal(t, "shared:hash:key", hscan.Args()[1])
	require.Equal(t, "shared:field:*", hscan.Args()[4])
}

func TestRedisKeyPrefixHookSkipsAlreadyPrefixedKeys(t *testing.T) {
	t.Parallel()

	hook := redisKeyPrefixHook{prefix: "shared:"}
	ctx := context.Background()

	cmd := redis.NewCmd(ctx, "get", "shared:user:1")
	hook.BeforeProcess(ctx, cmd)
	require.Equal(t, "shared:user:1", cmd.Args()[1])
}
