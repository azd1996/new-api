package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseMapping 解析渠道当前的 ModelMapping 为 map，便于与期望值比较（忽略键顺序）。
func parseMapping(t *testing.T, channel *Channel) map[string]string {
	t.Helper()
	raw := channel.GetModelMapping()
	if raw == "" || raw == "{}" {
		return map[string]string{}
	}
	result := map[string]string{}
	require.NoError(t, common.UnmarshalJsonStr(raw, &result))
	return result
}

// TestApplyCustomerModelExpansion_Basic 验证基础的 (模型 × 客户) 别名生成，
// 别名既写入 Models（保序：基础模型在前、再按模型分组追加别名），又写入 ModelMapping（别名 -> 真实模型）。
func TestApplyCustomerModelExpansion_Basic(t *testing.T) {
	channel := &Channel{Models: "modelA,modelB"}
	channel.ChannelInfo.Customers = []string{"c1", "c2"}

	require.NoError(t, channel.ApplyCustomerModelExpansion(nil))

	assert.Equal(t,
		[]string{"modelA", "modelB", "modelA_c1", "modelA_c2", "modelB_c1", "modelB_c2"},
		channel.GetModels(),
	)
	assert.Equal(t, map[string]string{
		"modelA_c1": "modelA",
		"modelA_c2": "modelA",
		"modelB_c1": "modelB",
		"modelB_c2": "modelB",
	}, parseMapping(t, channel))
	assert.Equal(t, []string{"c1", "c2"}, channel.ChannelInfo.Customers)
}

// TestApplyCustomerModelExpansion_Idempotent 验证对已展开的渠道以相同客户再次展开结果不变（幂等）。
func TestApplyCustomerModelExpansion_Idempotent(t *testing.T) {
	channel := &Channel{Models: "modelA,modelB"}
	channel.ChannelInfo.Customers = []string{"c1", "c2"}
	require.NoError(t, channel.ApplyCustomerModelExpansion(nil))

	modelsAfterFirst := channel.GetModels()
	mappingAfterFirst := parseMapping(t, channel)

	// 第二次：Models/ModelMapping 已是展开态，客户不变。
	require.NoError(t, channel.ApplyCustomerModelExpansion([]string{"c1", "c2"}))

	assert.Equal(t, modelsAfterFirst, channel.GetModels())
	assert.Equal(t, mappingAfterFirst, parseMapping(t, channel))
}

// TestApplyCustomerModelExpansion_RemovedCustomerCleanup 验证移除某客户后，
// 其遗留别名会被清理（借助 previousCustomers 识别历史别名）。
func TestApplyCustomerModelExpansion_RemovedCustomerCleanup(t *testing.T) {
	channel := &Channel{Models: "modelA,modelB"}
	channel.ChannelInfo.Customers = []string{"c1", "c2"}
	require.NoError(t, channel.ApplyCustomerModelExpansion(nil))

	// 移除 c2，仅保留 c1；传入历史客户以清理 c2 的别名。
	channel.ChannelInfo.Customers = []string{"c1"}
	require.NoError(t, channel.ApplyCustomerModelExpansion([]string{"c1", "c2"}))

	assert.Equal(t, []string{"modelA", "modelB", "modelA_c1", "modelB_c1"}, channel.GetModels())
	assert.Equal(t, map[string]string{
		"modelA_c1": "modelA",
		"modelB_c1": "modelB",
	}, parseMapping(t, channel))
}

// TestApplyCustomerModelExpansion_PreservesManualMapping 验证手动映射条目被保留，别名条目在其基础上叠加。
func TestApplyCustomerModelExpansion_PreservesManualMapping(t *testing.T) {
	manual := `{"foo":"bar"}`
	channel := &Channel{Models: "modelA", ModelMapping: &manual}
	channel.ChannelInfo.Customers = []string{"c1"}

	require.NoError(t, channel.ApplyCustomerModelExpansion(nil))

	assert.Equal(t, []string{"modelA", "modelA_c1"}, channel.GetModels())
	assert.Equal(t, map[string]string{
		"foo":       "bar",
		"modelA_c1": "modelA",
	}, parseMapping(t, channel))
}

// TestApplyCustomerModelExpansion_NoCustomersNoop 验证从无客户且历史也无客户时，不改动 Models / ModelMapping。
func TestApplyCustomerModelExpansion_NoCustomersNoop(t *testing.T) {
	channel := &Channel{Models: "modelA,modelB"}

	require.NoError(t, channel.ApplyCustomerModelExpansion(nil))

	assert.Equal(t, []string{"modelA", "modelB"}, channel.GetModels())
	assert.Nil(t, channel.ModelMapping)
}
