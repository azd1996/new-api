# 渠道密钥掩码显示（Channel Key Masking）

## 背景

渠道（Channel）密钥保存后在编辑抽屉里完全不可见：列表/详情接口统一用 GORM 的 `Omit("key")` 把 `key` 列排除，前端拿到的 `key` 恒为空。要看完整密钥必须走 `POST /api/channel/:id/key`，该接口受 `SecureVerificationRequired`（Passkey/2FA）保护，运维日常核对密钥是否填对时较为繁琐。

## 目标

保存后在编辑渠道时展示密钥的**掩码**（首尾可见、中间星号），用于比对是否填错，无需 2FA。完整密钥仍只通过原有 2FA 接口下发。

## 关键决策

- **不新增数据库字段**。密钥本就存在 `channel.key` 列，掩码在后端读取完整 key 后即时计算，不落库。
- **掩码规则：前 10 位 + 中间 `*` + 后 6 位**；长度 ≤ 16 的短密钥整体打码（避免泄露）；多密钥（按行分隔）逐行打码。
- **完整密钥绝不随详情接口下发**：`GetChannel` 读出完整 key、算出掩码后立即把 `Key` 清空再返回。
- **可见范围**：掩码对所有能进入编辑抽屉的管理员可见；完整密钥的 `Reveal key` / `Copy`（2FA）按钮仍仅超级管理员可见。

## 实现

### 后端

- `model/channel.go`
  - `Channel` 新增展示专用字段 `KeyMasked string` —— `json:"key_masked,omitempty" gorm:"-"`，不持久化。
  - 新增 `MaskKey(key string) string`：前 `maskKeyRevealPrefix=10`、后 `maskKeyRevealSuffix=6` 保留，中间以 `*` 补齐；短密钥全掩码；多密钥逐行处理。
- `controller/channel.go` 的 `GetChannel`
  - 改为 `model.GetChannelById(id, true)`（读出完整 key）。
  - 设置 `channel.KeyMasked = model.MaskKey(channel.Key)`，随后 `channel.Key = ""` 再返回。
- `model/channel_mask_key_test.go`：表驱动测试覆盖空值、短密钥全掩码、边界长度、长密钥首尾、多密钥逐行。

### 前端（仅编辑抽屉，渠道列表不变）

- `web/default/src/features/channels/types.ts`：`channelSchema` 增加 `key_masked: z.string().nullish()`。
- `web/default/src/features/channels/components/drawers/channel-mutate-drawer.tsx`
  - 读取 `channelData.data.key_masked` 得到 `maskedChannelKey`。
  - 「Current key」区块对所有管理员展示（原先仅超级管理员）；输入框显示 `channelKey ?? maskedChannelKey`。
  - `Reveal key` / `Copy`（完整密钥、走 2FA）仅超级管理员可见；key 输入框仍留空，“留空保持原密钥”逻辑不变。
- i18n：新增 3 条文案（`Showing a masked key. Reveal to view the full key.` / `Showing a masked key for verification.` / `No key saved yet`），en/zh/zh-TW/fr/ja/ru/vi 均已补齐，并经 `bun run i18n:sync`。

## 验证

- `go vet` + `go test ./model -run TestMaskKey` 通过；`gofmt` 无改动。
- 前端 `bun run typecheck` 通过；改动文件 `oxlint` 0 error；各 locale JSON 校验可解析。

## 效果与注意

- 编辑渠道时可见类似 `sk-proj-ab**********WXYZ6` 的掩码用于核对；完整密钥仍需 2FA。
- 若账号未配置 2FA，`Reveal key` 会报错，但不影响掩码显示。
