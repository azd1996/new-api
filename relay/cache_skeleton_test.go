package relay

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const skeletonTurn1 = `{
  "model": "claude-sonnet-5-5", "max_tokens": 1000,
  "tools": [{"name":"Bash","input_schema":{"type":"object"}},{"name":"Read","input_schema":{"type":"object"}}],
  "system": [{"type":"text","text":"You are Claude Code"},{"type":"text","text":"rules","cache_control":{"type":"ephemeral"}}],
  "messages": [
    {"role":"user","content":"fix the bug"},
    {"role":"assistant","content":[{"type":"thinking","thinking":"","signature":"s1"},{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]},
    {"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"a.go"}]},
    {"role":"system","content":[{"type":"text","text":"<total_tokens>900 tokens left</total_tokens>","cache_control":{"type":"ephemeral","ttl":"1h"}}]}
  ]
}`

// Turn 2: the model called another tool and the reminder moved to the end
// with a new value; the breakpoint stays on the reminder.
const skeletonTurn2 = `{
  "model": "claude-sonnet-5-5", "max_tokens": 2000,
  "tools": [{"name":"Bash","input_schema":{"type":"object"}},{"name":"Read","input_schema":{"type":"object"}}],
  "system": [{"type":"text","text":"You are Claude Code"},{"type":"text","text":"rules","cache_control":{"type":"ephemeral"}}],
  "messages": [
    {"role":"user","content":"fix the bug"},
    {"role":"assistant","content":[{"type":"thinking","thinking":"","signature":"s1"},{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]},
    {"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"a.go","cache_control":{"type":"ephemeral"}}]},
    {"role":"assistant","content":[{"type":"tool_use","id":"t2","name":"Read","input":{"path":"a.go"}}]},
    {"role":"user","content":[{"type":"tool_result","tool_use_id":"t2","content":"package a"}]},
    {"role":"system","content":[{"type":"text","text":"<total_tokens>800 tokens left</total_tokens>","cache_control":{"type":"ephemeral","ttl":"1h"}}]}
  ]
}`

func TestBuildClaudeCacheSkeletonStructure(t *testing.T) {
	row := buildClaudeCacheSkeleton([]byte(skeletonTurn1))

	assert.Equal(t, "claude-sonnet-5-5", row.Model)
	assert.Equal(t, []string{
		"tool", "tool", "system.text", "system.text",
		"user.text", "assistant.thinking", "assistant.tool_use", "user.tool_result", "system.text",
	}, row.BlkKind)
	assert.Equal(t, []string{"Bash", "Read", "", "", "", "", "Bash", "", ""}, row.BlkName)
	assert.Equal(t, []uint16{4, 5, 7, 8}, row.MsgStart)
	assert.Equal(t, []uint16{3, 8}, row.BpIdx)
	assert.Equal(t, []string{"5m", "1h"}, row.BpTtl)
	require.Len(t, row.BlkLen, len(row.BlkHash))
	assert.NotZero(t, row.BlkLen[0])
}

func TestBuildClaudeCacheSkeletonPrefixComparison(t *testing.T) {
	turn1 := buildClaudeCacheSkeleton([]byte(skeletonTurn1))
	turn2 := buildClaudeCacheSkeleton([]byte(skeletonTurn2))

	// Blocks #0-#7 are identical even though turn 2 added a breakpoint on the
	// tool_result (#7): cache_control is excluded from the content hash.
	assert.Equal(t, turn1.BlkHash[:8], turn2.BlkHash[:8])
	// The first divergence is turn 1's tail breakpoint (#8, the reminder).
	assert.NotEqual(t, turn1.BlkHash[8], turn2.BlkHash[8])
	assert.Equal(t, uint16(8), turn1.BpIdx[len(turn1.BpIdx)-1])
	assert.Equal(t, []uint16{3, 7, 10}, turn2.BpIdx)

	// Same model/tools/leading system block keeps one lane; max_tokens is not
	// rendered into the prompt so the param fingerprint is unchanged.
	assert.Equal(t, turn1.Lane, turn2.Lane)
	assert.Equal(t, turn1.ParamFp, turn2.ParamFp)
}

func TestBuildClaudeCacheSkeletonParamFingerprint(t *testing.T) {
	base := buildClaudeCacheSkeleton([]byte(`{"model":"m","output_config":{"effort":"high"},"messages":[{"role":"user","content":"hi"}]}`))
	effort := buildClaudeCacheSkeleton([]byte(`{"model":"m","output_config":{"effort":"xhigh"},"messages":[{"role":"user","content":"hi"}]}`))
	thinking := buildClaudeCacheSkeleton([]byte(`{"model":"m","output_config":{"effort":"high"},"thinking":{"type":"adaptive"},"messages":[{"role":"user","content":"hi"}]}`))

	assert.NotEqual(t, base.ParamFp, effort.ParamFp)
	assert.NotEqual(t, base.ParamFp, thinking.ParamFp)
	assert.Equal(t, base.BlkHash, effort.BlkHash)
	assert.Equal(t, "xhigh", effort.Effort)
	assert.Equal(t, "adaptive", thinking.Thinking)
}

func TestBuildClaudeCacheSkeletonTopLevelAutomaticCaching(t *testing.T) {
	row := buildClaudeCacheSkeleton([]byte(`{"model":"m","cache_control":{"type":"ephemeral"},"messages":[{"role":"user","content":"a"},{"role":"assistant","content":"b"}]}`))

	assert.Equal(t, []uint16{1}, row.BpIdx)
	assert.Equal(t, []string{"5m"}, row.BpTtl)
}
