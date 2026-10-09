# 渠道 45 第三方接入与素材库双协议 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 放开渠道 45 的 base_url 二选一限制以接入第三方上游，并把素材库从单一 BytePlus 官方协议扩展为「官方 + 第三方 cloudwise」双协议，素材组容量耗尽时按上游报错自动建组、重试并回写渠道配置。

**Architecture:** 在 `relay/channel/task/doubao/` 引入 `assetClient` 接口，把现有 `byteplus_asset.go` 里混在一起的「编排」与「官方协议实现」拆开：`asset.go` 负责遍历媒体、缓存、建组重试、回写；`byteplus_asset.go` 只剩官方 AK/SK 签名实现；新增 `cloudwise_asset.go` 为第三方 Bearer 实现。provider 由渠道 settings 里新增的单个字段选择，空值等价官方，存量零迁移。

**Tech Stack:** Go 1.25.1、Gin、GORM v2、`common/json.go` 包装器、`pkg/cachex` HybridCache、React 19 + Base UI（`web/default`）、React 18 + Semi Design（`web/classic`）、Bun。

## Global Constraints

- JSON 序列化必须走 `common.Marshal` / `common.Unmarshal` / `common.UnmarshalJsonStr` / `common.DecodeJson`，禁止在业务代码直接调用 `encoding/json` 的 marshal/unmarshal（`encoding/json` 的类型如 `json.RawMessage` 可以继续作为类型引用）。
- 数据库改动必须同时兼容 SQLite、MySQL >= 5.7.8、PostgreSQL >= 9.6。本计划不新增列、不写迁移——新配置字段存进既有 `settings` TEXT 列的 JSON 里。
- 前端在 `web/default/` 目录用 `bun`，不用 npm/yarn/pnpm。
- 禁止修改或删除 `new-api`、`QuantumNous` 相关的任何署名、品牌、元数据（CLAUDE.md Rule 5）。
- 可选请求参数用指针 + `omitempty`，保证显式零值能透传上游（CLAUDE.md Rule 6）。
- 新增配置字段一律「零值 = 现有行为」，存量渠道不做数据迁移。
- 错误信息里可能含签名 URL 的响应原文，必须过 `common.MaskSensitiveInfo`（`common/str.go:226`）后再返回。
- 每个任务结束时跑 `go build ./...` 确认整仓可编译。

## 参考实现（先读，别重新发明）

`relay/channel/task/seedance3rd/asset.go` 已经实现了本计划要的几乎全部骨架，实现前**必须完整读一遍**：

| 要素 | 位置 | 说明 |
|---|---|---|
| `assetClient` 接口 | `seedance3rd/asset.go:31-33` | 签名与本计划完全一致，照抄 |
| 客户端工厂 | `:272-286` `newAssetClient(model, httpClient)` | 按条件返回不同实现 |
| 编排循环 | `:290-330` `preuploadAssets` | 遍历、跳过 `asset://`、拒绝 base64、缓存、就地替换 |
| 缓存 | `:332-372` | `assetCacheKey` + HybridCache 单例 + get/set |
| `pickMedia` | `:151-164` | 从 `ContentItem` 取出 `*MediaURL` 和 assetType |
| 建组 | `:190-206` `ensureGroup` | **注意：这是反面教材，见下** |

**两个不要照抄的地方**（本计划要做得更好）：

1. `seedance3rd` 的 `groupAssetClient` 每个请求都新建一个客户端（`:272`），`groupID` 零值 → `ensureGroup`（`:191-193`）的早返回永远不命中 → **每个请求都 POST 一次 `/v1/asset-groups`**，且组 id 只活在内存里，请求结束就丢。
2. `CreateAndWait`（`:216-223`）在**任何**错误上都清空组重建重试一次，包括鉴权失败、审核拒绝、URL 不可达——这些换组重试结果一样，只是白建一个组。

doubao 这边必须是：组 id **持久化回渠道 settings**，且**只在 asset 相关错误**上建组。

## File Structure

| 文件 | 责任 | 动作 |
|---|---|---|
| `dto/channel_settings.go` | 渠道 settings DTO + 解析 | 改：加 `AssetProvider` 字段与常量、解析函数返回 provider |
| `relay/channel/task/doubao/asset.go` | **编排**：遍历媒体、缓存、建组重试、回写、客户端工厂 | 新建 |
| `relay/channel/task/doubao/byteplus_asset.go` | **官方协议**：AK/SK 签名、CreateAsset/GetAsset、组相关错误判定 | 改：移出编排与缓存，只留协议 |
| `relay/channel/task/doubao/cloudwise_asset.go` | **第三方协议**：Bearer、建组/上传/查详情、组相关错误判定 | 新建 |
| `relay/channel/task/doubao/asset_test.go` | 编排层测试（provider 无关） | 新建 |
| `relay/channel/task/doubao/cloudwise_asset_test.go` | 第三方协议往返测试 | 新建 |
| `relay/channel/task/doubao/byteplus_asset_test.go` | 官方协议测试 | 改：适配 groupID 提参 |
| `relay/channel/task/doubao/preupload_test.go` | 旧编排测试 | 改：迁入 `asset_test.go` 后删除 |
| `relay/channel/volcengine/adaptor.go` | 同步中继 URL 构造 | 改：TTS 全等判断 → host 归属判断 |
| `web/default/src/features/channels/lib/channel-form.ts` | 前端校验 | 改：AK/SK 按 provider 条件必填 |
| `web/default/src/features/channels/components/channel-mutate-drawer.tsx` | 新前端渠道表单 | 改：Combobox、provider 下拉、删 10 击后门 |
| `web/classic/src/components/table/channels/modals/EditChannelModal.jsx` | 旧前端渠道表单 | 改：同上 |

拆分依据是**协议边界**：编排逻辑对两种 provider 完全一致，协议差异全部收敛在各自的 client 文件里。`byteplus_asset.go` 现在 380 行且同时承担两件事，拆完每个文件单一责任。

---

### Task 1: settings 增加 AssetProvider 字段

**Files:**
- Modify: `dto/channel_settings.go:66-104`
- Test: `dto/channel_settings_test.go`（若不存在则创建）

**Interfaces:**
- Consumes: 无（首个任务）
- Produces:
  - 常量 `dto.AssetProviderBytePlus = "byteplus"`、`dto.AssetProviderCloudwise = "cloudwise"`
  - 字段 `ChannelOtherSettings.AssetProvider string`（json tag `asset_provider`）
  - 方法 `(s *ChannelOtherSettings) ResolveAssetProvider() string` — 空值/未知值返回 `AssetProviderBytePlus`。**指针接收者**，与同类型已有的 `ResolveBytePlusAsset()`（`dto/channel_settings.go:88`）保持一致，不要混用值接收者。

- [ ] **Step 1: 先读现有结构**

读 `dto/channel_settings.go:60-110`，确认 `ChannelOtherSettings` 的字段命名风格、`defaultBytePlusRegion` 常量位置、`ResolveBytePlusAsset()` 的返回形状。新字段要贴着现有 BytePlus 字段放，命名风格保持一致。

- [ ] **Step 2: 写失败测试**

新建 `dto/channel_settings_test.go`：

```go
package dto

import "testing"

func TestResolveAssetProvider(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty defaults to byteplus", "", AssetProviderBytePlus},
		{"explicit byteplus", "byteplus", AssetProviderBytePlus},
		{"explicit cloudwise", "cloudwise", AssetProviderCloudwise},
		{"unknown falls back to byteplus", "wat", AssetProviderBytePlus},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := ChannelOtherSettings{AssetProvider: tc.in}
			if got := s.ResolveAssetProvider(); got != tc.want {
				t.Errorf("ResolveAssetProvider() = %q, want %q", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 3: 跑测试确认失败**

Run: `go test ./dto/ -run TestResolveAssetProvider -v`
Expected: FAIL，编译错误 `undefined: AssetProviderBytePlus`

- [ ] **Step 4: 实现**

在 `dto/channel_settings.go` 的 BytePlus 相关常量旁加：

```go
const (
	// AssetProviderBytePlus 火山官方素材库（AK/SK 签名 + byteplusapi.com）。
	AssetProviderBytePlus = "byteplus"
	// AssetProviderCloudwise 第三方素材库（复用渠道 base_url 与 API Key）。
	AssetProviderCloudwise = "cloudwise"
)
```

在 `ChannelOtherSettings` 里、紧跟现有 `BytePlus*` 字段之后加：

```go
	// AssetProvider 选择素材库协议实现；空值等价 AssetProviderBytePlus，保证存量渠道行为不变。
	AssetProvider string `json:"asset_provider,omitempty"`
```

在 `ResolveBytePlusAsset()` 附近加：

```go
// ResolveAssetProvider 返回素材库协议实现名。空值与未知值都回落到官方实现，
// 保证存量渠道（无此字段）与配置写错时行为不变。
func (s *ChannelOtherSettings) ResolveAssetProvider() string {
	switch s.AssetProvider {
	case AssetProviderCloudwise:
		return AssetProviderCloudwise
	default:
		return AssetProviderBytePlus
	}
}
```

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./dto/ -run TestResolveAssetProvider -v`
Expected: PASS，4 个子测试全绿

- [ ] **Step 6: 确认整仓可编译**

Run: `go build ./...`
Expected: 无输出（成功）

- [ ] **Step 7: 提交**

```bash
git add dto/channel_settings.go dto/channel_settings_test.go
git commit -m "feat(doubao): settings 增加 asset_provider 字段，空值等价官方实现"
```

---

### Task 2: 抽出 assetClient 接口与编排层（纯重构，行为不变）

这一步**不改任何外部行为**，只把 `byteplus_asset.go` 拆成「编排」+「协议」。做完现有测试必须全绿。

**Files:**
- Create: `relay/channel/task/doubao/asset.go`
- Modify: `relay/channel/task/doubao/byteplus_asset.go`（移出 `preuploadAssets`、缓存、`pickMedia`、`truncate`；`CreateAndWait` 提参 groupID）
- Create: `relay/channel/task/doubao/asset_test.go`
- Delete: `relay/channel/task/doubao/preupload_test.go`（用例迁入 `asset_test.go`）
- Modify: `relay/channel/task/doubao/byteplus_asset_test.go`（适配新签名）

**Interfaces:**
- Consumes: `dto.AssetProviderBytePlus`、`dto.ChannelOtherSettings.ResolveAssetProvider()`（Task 1）
- Produces:
  - `type assetClient interface { CreateAndWait(ctx context.Context, groupID, mediaURL, assetType string) (string, error); CreateGroup(ctx context.Context, name string) (string, error); IsGroupExhausted(err error) bool }`
  - `func (a *TaskAdaptor) newAssetClient(httpClient *http.Client) (assetClient, error)`
  - `func (a *TaskAdaptor) preuploadAssets(c *gin.Context, payload *requestPayload) error`（签名不变）
  - `func assetCacheKey(channelId int, url string) string`
  - `func getCachedAssetID(key string) (string, bool)` / `func setCachedAssetID(key, id string)`
  - `func pickMedia(item *ContentItem) (*MediaURL, string)`
  - `func truncate(b []byte, n int) string`

注意 `CreateAndWait` 相比现状**多了 groupID 首参**（现状 `byteplus_asset.go:118` 是从 `cl.groupId` 读的）；组归编排层管，client 不再持有组状态。

- [ ] **Step 1: 读现有代码**

完整读 `relay/channel/task/doubao/byteplus_asset.go`（380 行）和 `relay/channel/task/doubao/preupload_test.go`（114 行），列出哪些函数属于编排（要搬走）、哪些属于官方协议（留下）。搬走的清单应该是：`preuploadAssets`、`assetCacheKey`、`getAssetIDCache`、`getCachedAssetID`、`setCachedAssetID`、`pickMedia`、`truncate`、`assetCacheTTL`。留下的是：`bytePlusAssetClient` 结构体、`CreateAndWait`、`createAsset`、`getAsset`、`doAction`、签名相关、`baseEndpoint`、`intervalOrDefault`、`timeoutOrDefault`。

- [ ] **Step 2: 建 asset.go，定义接口与工厂**

新建 `relay/channel/task/doubao/asset.go`。接口定义：

```go
// assetClient 抽象素材库协议：上传单个媒体 URL → 轮询到就绪 → 返回 assetId。
// 组不由 client 持有，由编排层传入，便于「组满 → 建新组 → 重试」。
type assetClient interface {
	// CreateAndWait 上传单个素材到指定组并轮询到就绪态。
	CreateAndWait(ctx context.Context, groupID, mediaURL, assetType string) (string, error)
	// CreateGroup 新建素材组，返回组 id。
	CreateGroup(ctx context.Context, name string) (string, error)
	// IsGroupExhausted 判定错误是否「组本身不可用」（组满/组不存在/组状态异常）。
	// 只有它返回 true 才会触发建新组；判不准时必须返回 false（宁可报错也不滥建组）。
	IsGroupExhausted(err error) bool
}
```

工厂：

```go
// newAssetClient 按 settings 里的 asset_provider 选协议实现。
func (a *TaskAdaptor) newAssetClient(httpClient *http.Client) (assetClient, error) {
	s := &a.otherSettings
	// ResolveBytePlusAsset 只返回带默认值的 region/project/skipModeration；
	// AK/SK 不在其返回值里，直接读字段。
	region, project, skipModeration := s.ResolveBytePlusAsset()

	switch s.ResolveAssetProvider() {
	case dto.AssetProviderCloudwise:
		base := a.baseURL
		if a.endpointOverride != "" {
			base = a.endpointOverride
		}
		return &cloudwiseAssetClient{
			baseURL:        base,
			apiKey:         a.apiKey,
			skipModeration: skipModeration,
			httpClient:     httpClient,
		}, nil
	default:
		if s.BytePlusAccessKey == "" || s.BytePlusSecretKey == "" {
			return nil, errors.New("byteplus asset library requires both access key and secret key in channel settings")
		}
		return &bytePlusAssetClient{
			ak:             s.BytePlusAccessKey,
			sk:             s.BytePlusSecretKey,
			region:         region,
			projectName:    project,
			skipModeration: skipModeration,
			httpClient:     httpClient,
			endpoint:       a.endpointOverride,
		}, nil
	}
}
```

字段名已核对（`dto/channel_settings.go:66-72`）：`BytePlusAccessKey`、`BytePlusSecretKey`、`BytePlusAssetGroupId`、`BytePlusProjectName`、`BytePlusRegion`、`BytePlusModerationSkip`（`*bool`，已由 `ResolveBytePlusAsset` 解成 `bool`，别再直接读这个指针）。

搬 `preuploadAssets` 时注意：现状里 AK/SK 为空的检查在编排函数里，现在挪进了工厂，`_MissingCreds` 那个用例的报错路径变成「工厂返回 error」，用例断言仍成立（错误文案保持原样）。

- [ ] **Step 3: 搬编排循环与缓存到 asset.go**

把 `preuploadAssets`、`pickMedia`、`truncate`、缓存那一组（`assetCacheKey` / `getAssetIDCache` / `getCachedAssetID` / `setCachedAssetID` / `assetCacheTTL`）整段从 `byteplus_asset.go` 剪切到 `asset.go`，**逻辑一字不改**，只做两处调整：

1. `preuploadAssets` 里构造 client 的地方改成调 `a.newAssetClient(httpClient)` 并处理它返回的 error。
2. 调用处从 `cl.CreateAndWait(ctx, url, assetType)` 改成 `cl.CreateAndWait(ctx, activeGroup, url, assetType)`，其中 `activeGroup := a.otherSettings.BytePlusAssetGroupId`（本任务先直接用，建组重试留到 Task 3）。

缓存 namespace 保持 `cachex.Namespace("byteplus_asset")` 不变——改了会让存量缓存全部失效。`assetCacheKey` 保持现有入参（确认现状是否含 model，与现有实现一致即可）。

- [ ] **Step 4: 改官方 client 签名**

`byteplus_asset.go` 里 `CreateAndWait` 加 groupID 首参，`createAsset` 同样加，`reqBody.GroupId` 从参数取而不是 `cl.groupId`；删掉结构体上的 `groupId` 字段。

补 `CreateGroup` 与 `IsGroupExhausted` 两个方法占位（Task 3 才写真实逻辑）：

```go
func (cl *bytePlusAssetClient) CreateGroup(ctx context.Context, name string) (string, error) {
	return "", errors.New("not implemented")
}

func (cl *bytePlusAssetClient) IsGroupExhausted(err error) bool { return false }
```

`IsGroupExhausted` 返回 false 意味着本任务不会触发任何建组，行为与现状完全一致——这正是「纯重构」要的。

- [ ] **Step 5: 迁移测试文件**

把 `preupload_test.go` 的 4 个用例（`TestPreuploadAssets_Disabled_Passthrough`、`_Base64Rejected`、`_ReplacesAndIsIdempotent`、`_MissingCreds`）连同 `enabledSettings()`、`ginCtx()` 两个 helper 整段搬到新建的 `asset_test.go`，然后删掉 `preupload_test.go`。用例内容不改。

`byteplus_asset_test.go` 里 `newTestClient` 删掉 `groupId: "group-test"` 一行，`CreateAndWait` 调用处补上组参数：

```go
	id, err := cl.CreateAndWait(context.Background(), "group-test", "https://example.com/i.jpg", "Image")
```

`byteplus_asset_test.go` 里每一处 `CreateAndWait(` 都要改，用 `grep -n "CreateAndWait(" relay/channel/task/doubao/byteplus_asset_test.go` 找全。

- [ ] **Step 6: 跑全包测试确认行为不变**

Run: `go test ./relay/channel/task/doubao/ -v`
Expected: PASS，全部用例通过（含 `adaptor_test.go` 的 477 行既有用例）。这是纯重构的验收标准——**任何一个红都说明搬错了，不要继续往下做**。

- [ ] **Step 7: 确认整仓可编译**

Run: `go build ./...`
Expected: 无输出

- [ ] **Step 8: 提交**

```bash
git add relay/channel/task/doubao/
git commit -m "refactor(doubao): 抽出 assetClient 接口，编排与协议实现分离"
```

---

### Task 3: 组满时建组、重试一次、回写渠道配置

**Files:**
- Modify: `relay/channel/task/doubao/asset.go`（编排循环加建组重试 + 回写）
- Modify: `relay/channel/task/doubao/byteplus_asset.go`（实现 `CreateGroup`、`IsGroupExhausted`，错误带 Code）
- Modify: `relay/channel/task/doubao/asset_test.go`（加建组相关用例）

**Interfaces:**
- Consumes: Task 2 的 `assetClient` 接口三个方法
- Produces:
  - `type assetAPIError struct { Code string; Message string; Raw string }` + `func (e *assetAPIError) Error() string`
  - `func (a *TaskAdaptor) persistAssetGroup(groupID string) error`

- [ ] **Step 1: 定义带 Code 的错误类型**

现状 `byteplus_asset.go:237-241` 把 `ResponseMetadata.Error.Code` 拼进字符串就丢了结构，上层无法判定。在 `asset.go` 加：

```go
// assetAPIError 保留上游错误码，供 IsGroupExhausted 结构化判定，
// 避免上层去 match 错误字符串。
type assetAPIError struct {
	Code    string
	Message string
	Raw     string
}

func (e *assetAPIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("upstream asset error %s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("upstream asset error %s: %s", e.Code, e.Raw)
}
```

改 `byteplus_asset.go` 的 `doAction`：原来返回裸 error 的地方改成返回 `&assetAPIError{Code: env.ResponseMetadata.Error.Code, Message: env.ResponseMetadata.Error.Message, Raw: truncate(body, 512)}`。**注意**：`Raw` 要过 `common.MaskSensitiveInfo` 再存（响应体可能含签名 URL）。

- [ ] **Step 2: 写建组重试的失败测试**

在 `asset_test.go` 加。这三个用例是本任务的核心，缺一不可：

```go
// 组满 → 建组一次 → 重试成功
func TestPreuploadAssets_GroupExhausted_CreatesGroupAndRetries(t *testing.T) {
	var createAssetCalls, createGroupCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("Action") {
		case "CreateAsset":
			if atomic.AddInt32(&createAssetCalls, 1) == 1 {
				w.Write([]byte(`{"ResponseMetadata":{"Error":{"Code":"AssetGroupQuotaExceeded","Message":"group full"}}}`))
				return
			}
			w.Write([]byte(`{"Result":{"Id":"asset-new"}}`))
		case "CreateAssetGroup":
			atomic.AddInt32(&createGroupCalls, 1)
			w.Write([]byte(`{"Result":{"Id":"group-fresh"}}`))
		case "GetAsset":
			w.Write([]byte(`{"Result":{"Status":"Active"}}`))
		}
	}))
	defer srv.Close()

	a := &TaskAdaptor{otherSettings: enabledSettings(), endpointOverride: srv.URL, channelId: 4242}
	payload := &requestPayload{Content: []ContentItem{
		{Type: "image_url", ImageURL: &MediaURL{URL: "https://example.com/retry-1.jpg"}},
	}}
	if err := a.preuploadAssets(ginCtx(), payload); err != nil {
		t.Fatalf("preuploadAssets: %v", err)
	}
	if got := atomic.LoadInt32(&createGroupCalls); got != 1 {
		t.Errorf("CreateAssetGroup calls = %d, want exactly 1", got)
	}
	if got := payload.Content[0].ImageURL.URL; got != "asset://asset-new" {
		t.Errorf("url = %q, want asset://asset-new", got)
	}
}
```

```go
// 组满 → 建组 → 仍组满 → 报错，且只建了一个组（每请求上限一次）
func TestPreuploadAssets_GroupExhaustedTwice_OnlyOneGroup(t *testing.T) {
	var createGroupCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("Action") {
		case "CreateAsset":
			w.Write([]byte(`{"ResponseMetadata":{"Error":{"Code":"AssetGroupQuotaExceeded","Message":"group full"}}}`))
		case "CreateAssetGroup":
			atomic.AddInt32(&createGroupCalls, 1)
			w.Write([]byte(`{"Result":{"Id":"group-fresh"}}`))
		}
	}))
	defer srv.Close()

	a := &TaskAdaptor{otherSettings: enabledSettings(), endpointOverride: srv.URL, channelId: 4243}
	payload := &requestPayload{Content: []ContentItem{
		{Type: "image_url", ImageURL: &MediaURL{URL: "https://example.com/twice-1.jpg"}},
	}}
	if err := a.preuploadAssets(ginCtx(), payload); err == nil {
		t.Fatal("expected error when group is still exhausted after retry")
	}
	if got := atomic.LoadInt32(&createGroupCalls); got != 1 {
		t.Errorf("CreateAssetGroup calls = %d, want exactly 1 (no group spam)", got)
	}
}

// 非组相关错误 → 绝不建组（省资源的核心保证）
func TestPreuploadAssets_NonGroupError_NeverCreatesGroup(t *testing.T) {
	for _, code := range []string{"AuthFailure", "ContentModerationRejected", "InvalidParameter"} {
		t.Run(code, func(t *testing.T) {
			var createGroupCalls int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Query().Get("Action") {
				case "CreateAsset":
					w.Write([]byte(`{"ResponseMetadata":{"Error":{"Code":"` + code + `","Message":"nope"}}}`))
				case "CreateAssetGroup":
					atomic.AddInt32(&createGroupCalls, 1)
					w.Write([]byte(`{"Result":{"Id":"group-should-not-exist"}}`))
				}
			}))
			defer srv.Close()

			a := &TaskAdaptor{otherSettings: enabledSettings(), endpointOverride: srv.URL, channelId: 4244}
			payload := &requestPayload{Content: []ContentItem{
				{Type: "image_url", ImageURL: &MediaURL{URL: "https://example.com/" + code + ".jpg"}},
			}}
			if err := a.preuploadAssets(ginCtx(), payload); err == nil {
				t.Fatal("expected error to propagate")
			}
			if got := atomic.LoadInt32(&createGroupCalls); got != 0 {
				t.Errorf("CreateAssetGroup called %d times for %s, want 0", got, code)
			}
		})
	}
}
```

每个用例用不同的 `channelId` 和 URL，避免 HybridCache 单例在用例之间串味。

- [ ] **Step 3: 跑测试确认失败**

Run: `go test ./relay/channel/task/doubao/ -run "TestPreuploadAssets_Group|TestPreuploadAssets_NonGroup" -v`
Expected: FAIL — 前两个因为 `IsGroupExhausted` 恒 false 而不建组，第三个可能意外通过（恒 false 时确实不建组），这是正常的。

- [ ] **Step 4: 实现官方 CreateGroup 与 IsGroupExhausted**

`byteplus_asset.go`：

```go
type createAssetGroupRequest struct {
	Name        string `json:"Name"`
	ProjectName string `json:"ProjectName,omitempty"`
}

// CreateGroup 新建素材组。
func (cl *bytePlusAssetClient) CreateGroup(ctx context.Context, name string) (string, error) {
	env, err := cl.doAction(ctx, "CreateAssetGroup", createAssetGroupRequest{
		Name:        name,
		ProjectName: cl.projectName,
	})
	if err != nil {
		return "", errors.Wrap(err, "byteplus CreateAssetGroup failed")
	}
	if env.Result.Id == "" {
		return "", errors.New("byteplus CreateAssetGroup returned empty group id")
	}
	return env.Result.Id, nil
}

// groupExhaustedCodes 是「组本身不可用」的上游错误码。
// BytePlus 官方文档未提供完整错误码表，这里只收已确认的码；
// 判不准时返回 false —— 宁可把原始错误报给管理员，也不要滥建组。
// 管理员在日志里看到新的组相关错误码后补进本表。
var groupExhaustedCodes = map[string]bool{
	"AssetGroupQuotaExceeded": true,
	"AssetGroupNotFound":      true,
	"InvalidAssetGroupId":     true,
	"AssetGroupNotExist":      true,
}

func (cl *bytePlusAssetClient) IsGroupExhausted(err error) bool {
	var apiErr *assetAPIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return groupExhaustedCodes[apiErr.Code]
}
```

`errors.As` 需要 `github.com/pkg/errors` 支持——本仓库用的是 `pkg/errors`，确认它的 `As` 可用；若不可用改用标准库 `errors.As`（可以同时 import，起别名区分）。

- [ ] **Step 5: 实现编排层的建组重试**

`asset.go` 的 `preuploadAssets` 循环改成：

```go
	activeGroup := a.otherSettings.BytePlusAssetGroupId
	groupCreated := false

	for i := range payload.Content {
		item := &payload.Content[i]
		media, assetType := pickMedia(item)
		if media == nil {
			continue
		}
		url := strings.TrimSpace(media.URL)
		if url == "" || strings.HasPrefix(url, "asset://") {
			continue
		}
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			return errors.Errorf("doubao asset upload requires a public http(s) URL for %s (base64/data URIs unsupported)", item.Type)
		}
		key := assetCacheKey(a.channelId, url)
		if id, ok := getCachedAssetID(key); ok {
			media.URL = "asset://" + id
			continue
		}

		id, err := cl.CreateAndWait(ctx, activeGroup, url, assetType)
		if err != nil && cl.IsGroupExhausted(err) && !groupCreated {
			// 唯一的建组点：上游明确报「组不可用」，且本请求还没建过组。
			newGroup, gErr := cl.CreateGroup(ctx, fmt.Sprintf("newapi-ch%d", a.channelId))
			if gErr != nil {
				return errors.Wrapf(gErr, "create replacement asset group failed (original error: %v)", err)
			}
			activeGroup = newGroup
			groupCreated = true
			if pErr := a.persistAssetGroup(newGroup); pErr != nil {
				// 回写失败不影响本请求；下个请求会再建一次组。
				logger.LogWarn(ctx, fmt.Sprintf("persist doubao asset group for channel %d failed: %s", a.channelId, pErr.Error()))
			}
			id, err = cl.CreateAndWait(ctx, activeGroup, url, assetType)
		}
		if err != nil {
			return errors.Wrapf(err, "preupload %s to doubao asset library failed", item.Type)
		}
		setCachedAssetID(key, id)
		media.URL = "asset://" + id
	}
	return nil
```

`groupCreated` 在循环**外**声明：一个请求里多张图共用同一个新组，不会每张图各建一个。

- [ ] **Step 6: 实现回写**

```go
// persistAssetGroup 把新建的组 id 回写渠道 settings，让后续请求直接复用。
// 不加分布式锁：并发下最坏情况是多建几个空组，代价小于引入锁的复杂度。
func (a *TaskAdaptor) persistAssetGroup(groupID string) error {
	ch, err := model.GetChannelById(a.channelId, true)
	if err != nil {
		return errors.Wrap(err, "load channel failed")
	}
	s := ch.GetOtherSettings()
	s.BytePlusAssetGroupId = groupID
	raw, err := common.Marshal(s)
	if err != nil {
		return errors.Wrap(err, "marshal channel settings failed")
	}
	ch.OtherSettings = string(raw)
	if err := ch.Save(); err != nil {
		return errors.Wrap(err, "save channel failed")
	}
	// 必须传重读得到的完整对象：CacheUpdateChannel 整体替换 channelsIDM[id]，
	// 传裸结构会把该渠道内存缓存里的其余字段清空。
	model.CacheUpdateChannel(ch)
	a.otherSettings.BytePlusAssetGroupId = groupID
	return nil
}
```

**实现前核对三处真实签名**：`model.GetChannelById(id int, selectAll bool) (*Channel, error)`（`model/channel.go:412`）已确认；`ch.GetOtherSettings()` 的返回类型、`ch.Save()` 是否存在、`model.CacheUpdateChannel` 的入参类型都要 grep 确认，按实际调整。

- [ ] **Step 7: 跑测试确认通过**

Run: `go test ./relay/channel/task/doubao/ -v`
Expected: PASS，含 Step 2 的三个新用例和 Task 2 迁移过来的全部旧用例

回写那条路径在单测里走不通（没有 DB），`persistAssetGroup` 会返回 error 并只打 warn，不影响用例断言——这是设计使然。

- [ ] **Step 8: 确认整仓可编译**

Run: `go build ./...`
Expected: 无输出

- [ ] **Step 9: 提交**

```bash
git add relay/channel/task/doubao/
git commit -m "feat(doubao): 组不可用时建新组重试一次并回写渠道配置"
```

---

### Task 4: 第三方 cloudwise 素材库实现

**Files:**
- Create: `relay/channel/task/doubao/cloudwise_asset.go`
- Create: `relay/channel/task/doubao/cloudwise_asset_test.go`

**Interfaces:**
- Consumes: Task 2 的 `assetClient` 接口、`truncate`、`pickMedia`；Task 3 的 `assetAPIError`
- Produces: `type cloudwiseAssetClient struct` 实现完整 `assetClient`

**协议形状**（来自 `接口文档/cloudwise-seedance接入文档/maas seedance2.0调用说明 0710.docx`，实现前**必须自己再读一遍文档**核对，下面是提取结论）：

| 操作 | 方法与路径 | 请求信封 | 响应信封 |
|---|---|---|---|
| 建组 | `POST {base}/api/v1/assets/groups/create` | PascalCase | `{ResponseMetadata, Result:{Id}}` |
| 上传 | `POST {base}/api/v1/assets/create` | snake_case | `{code, message, data:{id}}` |
| 查详情 | `POST {base}/api/v1/assets/get` | PascalCase | `{ResponseMetadata, Result:{Status}}` |

**注意两种信封混用**——上传是小写 `{code,message,data}`，建组和查详情是 PascalCase `Result`。这不是笔误，是上游实际形状。鉴权统一 `Authorization: Bearer {渠道 Key}`，成功码 `code == 10000`。

- [ ] **Step 1: 重读文档核对协议**

打开 `接口文档/cloudwise-seedance接入文档/maas seedance2.0调用说明 0710.docx`，逐个核对上表的路径、字段名大小写、成功码、状态枚举值（就绪态是 `completed` 还是 `Active`）。**文档与上表冲突时以文档为准**，并在实现里加注释记下差异。

- [ ] **Step 2: 写失败测试**

新建 `cloudwise_asset_test.go`：

```go
package doubao

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newCloudwiseTestClient(endpoint string) *cloudwiseAssetClient {
	return &cloudwiseAssetClient{
		baseURL:      endpoint,
		apiKey:       "sk-test",
		httpClient:   http.DefaultClient,
		pollInterval: 5 * time.Millisecond,
		pollTimeout:  2 * time.Second,
	}
}

func TestCloudwise_CreateAndWait_RoundTrip(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/assets/create":
			w.Write([]byte(`{"code":10000,"message":"ok","data":{"id":"cw-asset-1"}}`))
		case "/api/v1/assets/get":
			w.Write([]byte(`{"ResponseMetadata":{"RequestId":"r1"},"Result":{"Id":"cw-asset-1","Status":"completed"}}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	cl := newCloudwiseTestClient(srv.URL)
	id, err := cl.CreateAndWait(context.Background(), "group-1", "https://example.com/i.jpg", "image")
	if err != nil {
		t.Fatalf("CreateAndWait: %v", err)
	}
	if id != "cw-asset-1" {
		t.Errorf("id = %q, want cw-asset-1", id)
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("Authorization = %q, want Bearer sk-test", gotAuth)
	}
}

func TestCloudwise_CreateAsset_ErrorCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":40001,"message":"asset group is full","data":{}}`))
	}))
	defer srv.Close()

	cl := newCloudwiseTestClient(srv.URL)
	_, err := cl.CreateAndWait(context.Background(), "group-1", "https://example.com/i.jpg", "image")
	if err == nil {
		t.Fatal("expected error for code != 10000")
	}
	if !strings.Contains(err.Error(), "asset group is full") {
		t.Errorf("error should carry upstream message, got %v", err)
	}
	if !cl.IsGroupExhausted(err) {
		t.Error("code 40001 with group-full message should be judged group-exhausted")
	}
}

func TestCloudwise_CreateGroup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/assets/groups/create" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ResponseMetadata":{"RequestId":"r1"},"Result":{"Id":"cw-group-9"}}`))
	}))
	defer srv.Close()

	cl := newCloudwiseTestClient(srv.URL)
	id, err := cl.CreateGroup(context.Background(), "newapi-ch1")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if id != "cw-group-9" {
		t.Errorf("group id = %q, want cw-group-9", id)
	}
}
```

`TestCloudwise_CreateAsset_ErrorCode` 里的 `40001` 是占位码——Step 1 读文档时换成真实的组满错误码；文档没给的话保留此码并在实现里注明是推测值。

- [ ] **Step 3: 跑测试确认失败**

Run: `go test ./relay/channel/task/doubao/ -run TestCloudwise -v`
Expected: FAIL，编译错误 `undefined: cloudwiseAssetClient`

- [ ] **Step 4: 实现**

新建 `cloudwise_asset.go`：

```go
package doubao

// cloudwiseAssetClient 第三方素材库：Bearer 鉴权，复用渠道 base_url 与 API Key。
// 注意上游两种响应信封混用：上传是 {code,message,data}，建组/查详情是 PascalCase Result。
type cloudwiseAssetClient struct {
	baseURL        string
	apiKey         string
	skipModeration bool
	httpClient     *http.Client
	pollInterval   time.Duration
	pollTimeout    time.Duration
}

const cloudwiseSuccessCode = 10000

// cloudwiseDataEnvelope 上传接口的小写信封。
type cloudwiseDataEnvelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Id     string `json:"id"`
		Status string `json:"status"`
	} `json:"data"`
}

// cloudwiseResultEnvelope 建组/查详情的 PascalCase 信封。
type cloudwiseResultEnvelope struct {
	ResponseMetadata struct {
		RequestId string `json:"RequestId"`
		Error     *struct {
			Code    string `json:"Code"`
			Message string `json:"Message"`
		} `json:"Error"`
	} `json:"ResponseMetadata"`
	Result struct {
		Id     string `json:"Id"`
		Status string `json:"Status"`
	} `json:"Result"`
}

func (cl *cloudwiseAssetClient) CreateAndWait(ctx context.Context, groupID, mediaURL, assetType string) (string, error) {
	id, err := cl.createAsset(ctx, groupID, mediaURL, assetType)
	if err != nil {
		return "", err
	}
	deadline := time.Now().Add(cl.timeout())
	for {
		status, raw, err := cl.getAsset(ctx, id)
		if err != nil {
			return "", err
		}
		switch strings.ToLower(status) {
		case "completed", "active", "success":
			return id, nil
		case "failed", "error":
			return "", errors.Errorf("cloudwise asset %s failed: %s", id, common.MaskSensitiveInfo(raw))
		}
		if time.Now().After(deadline) {
			return "", errors.Errorf("cloudwise asset %s not ready within %s (last status: %s)", id, cl.timeout(), status)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(cl.interval()):
		}
	}
}
```

`createAsset`（小写信封，snake_case 请求体）：

```go
func (cl *cloudwiseAssetClient) createAsset(ctx context.Context, groupID, mediaURL, assetType string) (string, error) {
	reqBody := map[string]any{
		"group_id":   groupID,
		"url":        mediaURL,
		"asset_type": assetType,
		"name":       "newapi",
	}
	if cl.skipModeration {
		reqBody["moderation"] = map[string]string{"strategy": "Skip"}
	}
	var env cloudwiseDataEnvelope
	raw, err := cl.post(ctx, "/api/v1/assets/create", reqBody, &env)
	if err != nil {
		return "", errors.Wrap(err, "cloudwise create asset failed")
	}
	if env.Code != cloudwiseSuccessCode {
		return "", &assetAPIError{
			Code:    strconv.Itoa(env.Code),
			Message: env.Message,
			Raw:     common.MaskSensitiveInfo(raw),
		}
	}
	if env.Data.Id == "" {
		return "", errors.New("cloudwise create asset returned empty id")
	}
	return env.Data.Id, nil
}
```

`CreateGroup`、`getAsset`（PascalCase 信封）、`post`（统一发请求：`common.Marshal` 序列化、设 `Authorization: Bearer`、`Content-Type: application/json`、读全响应体、`common.Unmarshal` 到 out、返回原文供报错）、`interval()`/`timeout()`（照 `seedance3rd/asset.go:45-57` 的默认值模式）逐个实现。

`IsGroupExhausted`：

```go
// cloudwiseGroupExhaustedCodes 第三方上游的组不可用错误码。
// 文档未提供完整错误码表，判不准时返回 false（不建组、直接报错）。
var cloudwiseGroupExhaustedCodes = map[string]bool{
	"40001": true,
}

func (cl *cloudwiseAssetClient) IsGroupExhausted(err error) bool {
	var apiErr *assetAPIError
	if !errors.As(err, &apiErr) {
		return false
	}
	if cloudwiseGroupExhaustedCodes[apiErr.Code] {
		return true
	}
	// 兜底：错误码表不全时按消息关键词判定组相关错误。
	msg := strings.ToLower(apiErr.Message)
	return strings.Contains(msg, "group") &&
		(strings.Contains(msg, "full") || strings.Contains(msg, "not found") ||
			strings.Contains(msg, "quota") || strings.Contains(msg, "exceed"))
}
```

关键词兜底只对**同时**含 "group" 和容量/存在性词的消息生效，不会把鉴权失败误判成组问题。

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./relay/channel/task/doubao/ -run TestCloudwise -v`
Expected: PASS，3 个用例全绿

- [ ] **Step 6: 跑全包测试**

Run: `go test ./relay/channel/task/doubao/ -v`
Expected: PASS，新旧用例全绿

- [ ] **Step 7: 确认整仓可编译**

Run: `go build ./...`
Expected: 无输出

- [ ] **Step 8: 提交**

```bash
git add relay/channel/task/doubao/cloudwise_asset.go relay/channel/task/doubao/cloudwise_asset_test.go
git commit -m "feat(doubao): 新增第三方 cloudwise 素材库实现"
```

---

### Task 5: TTS 路由改为按 host 归属判断

**Files:**
- Modify: `relay/channel/volcengine/adaptor.go:276-280`（`GetRequestURL` 的 TTS 分支）
- Modify: `relay/channel/volcengine/adaptor.go:332-346`（`DoRequest` 的同一判断）
- Test: `relay/channel/volcengine/adaptor_test.go`（若不存在则创建）

**Interfaces:**
- Consumes: 无（独立于素材库任务，可与 Task 1-4 并行）
- Produces: `func isOfficialVolcHost(baseURL string) bool`

**为什么要改**：现状是 base_url 与 cn-beijing 默认值**字符串全等**才走 WebSocket TTS，否则拼 `{base}/v1/audio/speech`——方舟不提供这个路径。选官方 bytepluses 那一项时同样中招。Task 6-8 放开自由填写会让这个坑从「少见」变「常见」。

- [ ] **Step 1: 写失败测试**

```go
func TestIsOfficialVolcHost(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"https://ark.cn-beijing.volces.com", true},
		{"https://ark.ap-southeast.bytepluses.com", true},
		{"https://ark.cn-beijing.volces.com/api/coding", true},
		{"https://ark.cn-shanghai.volces.com", true},
		{"https://api.cloudwise.ai", false},
		{"https://my-proxy.example.com", false},
		{"", false},
		{"not-a-url", false},
		{"https://evil-volces.com.attacker.net", false},
	}
	for _, tc := range cases {
		if got := isOfficialVolcHost(tc.in); got != tc.want {
			t.Errorf("isOfficialVolcHost(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
```

最后一个用例是关键：必须按 host **后缀边界**判断，不能用 `strings.Contains`，否则 `evil-volces.com.attacker.net` 会被误判成官方域。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./relay/channel/volcengine/ -run TestIsOfficialVolcHost -v`
Expected: FAIL，`undefined: isOfficialVolcHost`

- [ ] **Step 3: 实现**

```go
// officialVolcHostSuffixes 火山/BytePlus 官方域后缀。
var officialVolcHostSuffixes = []string{".volces.com", ".bytepluses.com"}

// isOfficialVolcHost 判断 base_url 是否指向火山官方域。
// TTS 只有官方域才提供 WebSocket 接口；第三方地址走标准 HTTP 路径。
// 按 host 后缀边界匹配，避免 evil-volces.com.attacker.net 之类的误判。
func isOfficialVolcHost(baseURL string) bool {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, suffix := range officialVolcHostSuffixes {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}
```

改 `GetRequestURL` 的 TTS 分支（`:276-280`）：

```go
	case constant.RelayModeAudioSpeech:
		if isOfficialVolcHost(baseUrl) {
			return "wss://openspeech.bytedance.com/api/v1/tts/ws_binary", nil
		}
		return fmt.Sprintf("%s/v1/audio/speech", baseUrl), nil
```

改 `DoRequest`（`:339`）：

```go
		if isOfficialVolcHost(baseUrl) {
			if info.IsStream {
				return nil, nil
			}
		}
```

两处判断必须一致——不一致会导致 URL 走 WebSocket 但请求走 HTTP（或反之）。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./relay/channel/volcengine/ -v`
Expected: PASS

- [ ] **Step 5: 确认整仓可编译**

Run: `go build ./...`
Expected: 无输出

- [ ] **Step 6: 提交**

```bash
git add relay/channel/volcengine/
git commit -m "fix(volcengine): TTS 按 host 归属判断官方域，替代 base_url 全等比较"
```

---

### Task 6: 前端校验按 provider 分支

先做校验再做 UI——UI 可以直接依赖校验已就位。

**Files:**
- Modify: `web/default/src/features/channels/lib/channel-form.ts:288-312`（BytePlus 校验块）

**Interfaces:**
- Consumes: Task 1 的 `asset_provider` 字段名（前端 settings JSON 的 key）
- Produces: 校验规则——`asset_provider === 'byteplus'`（含空值）时 AK/SK 必填；`'cloudwise'` 时不要求 AK/SK 但要求 `base_url` 非空；两种 provider 下 `group_id` 均选填

- [ ] **Step 1: 读现有校验块**

读 `web/default/src/features/channels/lib/channel-form.ts:280-320`，确认现有 `superRefine` 的写法、`addIssue` 的 path 格式、settings 字段怎么取值（是 `data.settings` 字符串还是已解析对象）。**照现有风格改，不要引入新模式。**

- [ ] **Step 2: 改校验逻辑**

把现有「`byteplus_asset_enabled` 且 type ∈ [45,54] 时 AK/SK/GroupId 三者必填」改成：

```
若 byteplus_asset_enabled 且 type ∈ [45, 54]:
    provider = settings.asset_provider || 'byteplus'
    若 provider === 'byteplus':
        access_key 为空 → addIssue(path: byteplus_access_key, '官方素材库需要 Access Key')
        secret_key 为空 → addIssue(path: byteplus_secret_key, '官方素材库需要 Secret Key')
    若 provider === 'cloudwise':
        base_url 为空 → addIssue(path: base_url, '第三方素材库复用渠道 API 地址，请先填写')
    group_id 不再校验（两种 provider 下都选填）
```

错误文案走 i18n：新增的 key 加进 `web/default/src/i18n/locales/zh.json` 与 `en.json`，然后跑 `bun run i18n:sync`（在 `web/default/` 目录下）补齐其余语种。

注意这个校验块**同时覆盖 type 54**，改动对 54 生效——这是有意的，两个渠道类型共用同一套素材库配置。

- [ ] **Step 3: 确认前端能构建**

在 `web/default/` 目录下 Run: `bun run build`
Expected: 构建成功。若仓库配了 typecheck 脚本（查 `web/default/package.json` 的 scripts），也跑一遍。

- [ ] **Step 4: 加校验单测（若有测试框架）**

查 `web/default/package.json` 是否有 `test` 脚本。**有**则给上述三条分支各写一个用例（provider 空值要求 AK/SK、cloudwise 不要求 AK/SK、cloudwise 要求 base_url）；**没有**则跳过本步，在提交信息里注明校验逻辑靠 Task 7 的手工走查覆盖。不要为此新引入测试框架。

- [ ] **Step 5: 提交**

```bash
git add web/default/src/features/channels/lib/channel-form.ts web/default/src/i18n/locales/
git commit -m "feat(channel): 素材库校验按 provider 分支，第三方不再强制 AK/SK"
```

---

### Task 7: 新前端 UI（base_url 可编辑 + provider 下拉 + 删后门）

**Files:**
- Modify: `web/default/src/features/channels/components/channel-mutate-drawer.tsx`
  - `:415-431` 删 `useHiddenClickUnlock` 调用与 `useEffect` reset
  - `:1935-1994` 锁定态 Select → Combobox
  - `:1997-2019` 删解锁态分支
  - `:1467-1626` 素材库配置区加 provider 下拉与字段条件显示
- Delete: `web/default/src/hooks/use-hidden-click-unlock.ts`（确认无其他调用方后）

**Interfaces:**
- Consumes: Task 6 的校验规则、Task 1 的 `asset_provider` 字段名
- Produces: 无（UI 末端）

- [ ] **Step 1: 确认后门 hook 的调用方**

Run: `grep -rn "useHiddenClickUnlock" web/`
Expected: 只有 `web/default/src/hooks/use-hidden-click-unlock.ts`（定义）和 `channel-mutate-drawer.tsx`（唯一调用方）两处。

**若出现第三处调用**，只删 drawer 里的调用、保留 hook 文件。

- [ ] **Step 2: base_url 换成可编辑下拉**

删掉 `:415-431` 的 hook 调用和随后的 `useEffect(... resetDoubaoApiUnlock)`。

把 `:1935-1994` 的锁定态 `Select` 换成 Base UI 的 Combobox（可输入 + 预设项）。两个官方地址作为预设项：

```
https://ark.cn-beijing.volces.com
https://ark.ap-southeast.bytepluses.com
```

删掉 `:1997-2019` 的解锁态 `Input` 分支——限制没了它就是死代码。

**先找参照**：`grep -rn "Combobox" web/default/src/` 找仓库里现有的 Combobox 用法，照它的 props 风格写；仓库没有 Combobox 组件的话，用 `Input` + `datalist`，或 `Input` 配一个「填入官方地址」的下拉按钮。**不要为此引入新依赖。**

保留 `:673-680` 那个把 `doubao-coding-plan` 改写成 cn-beijing 的 `useEffect` 和 `:1962-1967` 的显示层兜底——本次不碰两个前端在这块的既有分歧。

- [ ] **Step 3: 素材库配置区加 provider 下拉**

在 `:1467-1626` 素材库区块顶部加 provider 选择（默认「火山官方」），按选择切换字段可见性：

| provider | 显示 | 隐藏 |
|---|---|---|
| 火山官方（byteplus） | Access Key、Secret Key、Region、Project、Group Id、跳过审核 | — |
| 第三方（cloudwise） | Group Id、跳过审核 | Access Key、Secret Key、Region、Project |

第三方分支下加一行说明文字：素材库复用本渠道的 API 地址与密钥。文案走 `t()`，key 加进 zh/en 后跑 `bun run i18n:sync`。

隐藏字段时**不要清空已有值**——管理员切回官方 provider 应该还能看到原来填的 AK/SK。

- [ ] **Step 4: 构建**

在 `web/default/` 目录下 Run: `bun run build`
Expected: 构建成功

- [ ] **Step 5: 手工走查**

起前端 dev server（`bun run dev`），打开渠道编辑抽屉，逐项确认：

1. type 45 的 API 地址是可输入的，两个官方地址能一键选中
2. 手填 `https://api.cloudwise.ai` 能保存成功
3. 点 10 次「API Base URL」标签**没有任何反应**（后门已删）
4. provider 选「第三方」后 AK/SK 字段消失，且不再报必填错误
5. provider 切回「火山官方」，原先填的 AK/SK 还在
6. 打开一个存量 type 45 渠道，provider 显示「火山官方」，其余字段与改动前一致

- [ ] **Step 6: 提交**

```bash
git add web/default/src/ && git rm web/default/src/hooks/use-hidden-click-unlock.ts
git commit -m "feat(channel): 新前端放开渠道45地址输入，素材库加 provider 选择"
```

若 Step 1 发现 hook 有其他调用方，去掉 `git rm` 那半句。

---

### Task 8: 旧前端 UI（同 Task 7，Semi Design 版）

**Files:**
- Modify: `web/classic/src/components/table/channels/modals/EditChannelModal.jsx`
  - `:520-532` 删手写的 `handleApiConfigSecretClick`
  - `:1369-1374`、`:1512-1513` 删 reset 逻辑
  - `:3546` 删卡片上的 `onClick`
  - `:3667` 删 `doubaoApiEditUnlocked` 条件，base_url 输入常开
  - `:3742-3747` 保留 `doubao-coding-plan` 的 grandfathering
  - 素材库区块加 provider 下拉

**Interfaces:**
- Consumes: Task 6 的校验语义（旧前端校验独立，需按同样规则手写）
- Produces: 无

- [ ] **Step 1: 删后门**

`:520-532` 整段 `handleApiConfigSecretClick` 与配套 state 删掉；`:1369-1374`（类型切换时 reset）、`:1512-1513`（弹窗 reset）删掉；`:3546` 的 `<div onClick={handleApiConfigSecretClick}>` 去掉 onClick，div 本身留着（有布局作用）。

- [ ] **Step 2: base_url 常开可输入**

`:3667` 的 `(inputs.type !== 45 || doubaoApiEditUnlocked)` 改成常量 true 分支——即 type 45 与其他类型一样直接渲染通用 base_url 输入框，配一个官方地址预设下拉（Semi 的 `AutoComplete` 或 `Select` + `filter`，照本文件里现有 Semi 组件用法写）。

`:3742-3747` 的 `doubao-coding-plan` 选项及 `canKeepDeprecatedDoubaoCodingPlan` 的 disabled 逻辑**保持不动**——旧前端的 grandfathering 是有意设计，本次不改。

- [ ] **Step 3: 素材库区块加 provider 下拉**

同 Task 7 Step 3 的字段可见性表，用 Semi 组件实现。旧前端文案走 `t()`（确认本文件的 i18n 用法后照做）。

- [ ] **Step 4: 校验同步**

旧前端若有独立的 AK/SK 必填校验（grep `byteplus_access_key` 在本文件内的出现位置），按 Task 6 的规则同步改：provider 为 cloudwise 时不校验 AK/SK。

- [ ] **Step 5: 构建**

在 `web/classic/` 目录下 Run: `bun run build`
Expected: 构建成功。先确认 `web/classic/` 有 `package.json` 与 build 脚本；它用 Vite + React 18，与 default 是独立工程。

- [ ] **Step 6: 手工走查**

同 Task 7 Step 5 的 6 项，外加第 7 项：打开一个 base_url 为 `doubao-coding-plan` 的存量渠道，确认该选项仍可见、可保存（grandfathering 未被破坏）。

- [ ] **Step 7: 提交**

```bash
git add web/classic/src/
git commit -m "feat(channel): 旧前端放开渠道45地址输入，素材库加 provider 选择"
```

---

### Task 9: 端到端验证与收尾

**Files:** 无新增改动（只跑验证；发现问题回到对应 Task 修）

- [ ] **Step 1: 全量后端测试**

Run: `go test ./...`
Expected: PASS。**先记录基线**——改动前跑一次存下结果，对比确认没有新增失败。仓库里可能本来就有失败用例，不要把既有失败算到本次改动头上。

- [ ] **Step 2: 后端静态检查**

Run: `go vet ./...`
Expected: 无新增告警

若仓库配了 golangci-lint（查根目录 `.golangci.yml`），也跑一遍。

- [ ] **Step 3: 两个前端构建**

在 `web/default/` 与 `web/classic/` 各 Run: `bun run build`
Expected: 均成功

- [ ] **Step 4: 存量渠道回归（关键）**

这一步验证「零迁移」这个核心承诺。用一个**现有的 type 54 官方素材库渠道**（不改任何配置）发一次带 URL 参考图的视频请求：

1. 请求成功，图片被替换成 `asset://`
2. 素材落在原来配置的 group 里，**没有新建组**
3. 渠道 settings 里 `asset_provider` 字段仍为空

任何一条不符，说明默认值回落有问题，回到 Task 1/2 修。

- [ ] **Step 5: 第三方渠道端到端**

新建一个 type 45 渠道：base_url 填 `https://api.cloudwise.ai`、Key 填第三方 key、素材库开启、provider 选「第三方」、group_id 留空。发一次带 URL 参考图的视频请求，确认：

1. 组自动创建，渠道 settings 里 `byteplus_asset_group_id` 被回写
2. 图片替换成 `asset://`
3. 视频任务提交成功、能轮询到完成
4. **再发一次同样的请求**：不重复建组（复用回写的 group）、命中素材缓存

第 4 条是回写逻辑的真实验收——单测覆盖不到。

- [ ] **Step 6: 日志检查**

翻这次验证产生的日志，确认没有 AK/SK、Bearer token、签名 URL 泄漏（`common.MaskSensitiveInfo` 是否真的生效）。发现泄漏立刻回到对应 Task 补 mask。

- [ ] **Step 7: 提交任何收尾修复**

```bash
git add -A
git commit -m "test(doubao): 端到端验证收尾"
```

无修复则跳过本步，不要造空提交。

---

## 验收清单

对着 spec 逐条核对：

- [ ] type 45 渠道 base_url 可自由填写，两个官方地址仍是一键预设（Task 7、8）
- [ ] 10 击后门在两个前端均已删除（Task 7 Step 1、Task 8 Step 1）
- [ ] `asset_provider` 空值 = 官方实现，存量渠道零改动（Task 1、Task 9 Step 4）
- [ ] 第三方 provider 复用渠道 base_url 与 Key，不需要 AK/SK（Task 4、Task 6）
- [ ] group_id 留空时自动建组并回写（Task 3、Task 9 Step 5）
- [ ] 只有上游明确报组不可用才建组，每请求最多一个（Task 3 Step 2 三个用例）
- [ ] 素材缓存 namespace 未变，存量缓存不失效（Task 2 Step 3）
- [ ] TTS 按 host 归属判断，第三方地址不再走错路径（Task 5）
- [ ] 素材库校验按 provider 分支，两个前端一致（Task 6、8 Step 4）
- [ ] 后端 `go test ./...` 与两个前端构建均通过（Task 9）
- [ ] 日志无密钥泄漏（Task 9 Step 6）

