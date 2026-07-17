# 延后健壮性工作补全设计（2026-07）

## 1. 背景与纠偏

上一 Goal `refactor/converge-parallel-implementations` 完成了 D 类平行实现收敛，但错误地把以下三项排除：

1. describe/doctor 完整迁移到 `app.Service` 的 ConfigStore/SourceIndex 注入模型；
2. 调整 Hessian container item budget，降低合法超大批量响应的误伤；
3. javaparser 成员级通用错误恢复，让单个不支持成员不再导致整文件丢失。

本设计用于补齐这三项。代码基线为
`refactor/converge-parallel-implementations@07f1f6e`，实施分支为
`fix/complete-deferred-resilience-work`。

## 2. 目标与不变量

### 2.1 目标

- 同一个 `app.Service` 实例下，resolve/invoke/describe/doctor 使用同一 ConfigStore 和 SourceIndex adapter。
- 允许超过旧累计 100 万项阈值的合法 Hessian 响应，同时保留确定的单容器与累计内存放大上限。
- lexer 成功后，单个不支持的 class/interface/annotation 成员可被跳过，文件其余声明继续进入 schema。
- 恢复行为必须可观测：parser warning 进入 `schema.Index.Warnings` 和 describe 输出。

### 2.2 必须保持的不变量

- MCP tool name、input/output schema 和成功 envelope 不变。
- doctor 继续返回检查集合，且失败恢复不得指回 doctor 自己。
- SourceIndex 默认实现仍使用本地 `schema.LoadOrBuildIndex`，缓存语义不变。
- Hessian 仍拒绝负长度、长度超过剩余输入、过深嵌套和异常放大；不得改成无限制或用户可关闭的保护。
- Parser 只在 type body 的成员 seam 恢复；package/import/type header、未闭合字符串/注释、未闭合结构仍为文件级致命错误。
- 被跳过成员不得产生半成品 Method/Field/Type；恢复点不得吞掉后续合法成员或 owner 的 `}`。

## 3. SourceIndex 注入模型补全

### 3.1 当前问题

`app.Service` 已持有可注入的 `ConfigStore` 和 `SourceIndex`，但现有 `SourceIndex` 只暴露 `Describe`。
invoke 经过该 seam，describe/doctor 却直接调用 tools 私有 `loadConfig()` 和
`schema.LoadOrBuildIndex()`。因此测试或宿主传入自定义 store/source 时，同一个 MCP server 内的数据源会分叉。

### 3.2 seam 设计

将 SourceIndex 的 interface 改为加载一个项目索引：

```go
type SourceIndex interface {
    Load(ctx context.Context, projectName string, project appconfig.Project) (*schema.Index, error)
}
```

`LocalSourceIndex.Load` 隐藏 project 到 schema project 的适配、缓存加载和取消检查。`app.Service` 暴露两个
供同进程调用方使用的受控入口：

```go
func (s *Service) LoadConfig(ctx context.Context) (appconfig.Config, error)
func (s *Service) LoadSourceIndex(ctx context.Context, projectName string, project appconfig.Project) (*schema.Index, error)
```

invoke 在加载后调用 `schema.Describe`；describe/doctor 接收构建 server 时使用的同一个 `appSvc`，配置、选择、
索引均经过该实例。tools 仍负责 MCP 参数和展示，app seam 负责数据源一致性。

这是一个真实 seam：生产 adapter 为本地缓存索引，测试 adapter 为内存索引/错误 adapter。测试从
`app.Service` 和 MCP tool interface 观察结果，不直接断言 adapter 内部状态以外的实现细节。

### 3.3 注册与测试迁移

- `AddDescribe`、`AddDoctor` 增加 `appSvc *app.Service` 参数；`newSDKServer` 传入共享实例。
- 原 tools 测试改用默认 app service；新增注入测试，使用与磁盘配置相冲突的 fake store/source，证明工具只读注入数据。
- config error routing 矩阵继续通过共享 app service，稳定 code/path 不变。

## 4. Hessian item budget 调整

### 4.1 风险模型

BOLT response body 仍受 16 MiB 上限保护，但 Hessian 的 null、compact int 等值可用极少 wire bytes 产生大量
Go `interface{}` slot。旧策略把单容器和累计阈值都设为 `1<<20`，会让多个合理大小容器的累计解码过早失败，
也无法支持略超 100 万元素的单批结果。

### 4.2 新预算

采用耦合但分层的固定预算：

```go
maxHessianContainerItems = 2 << 20 // 单次 make/单容器最多约 200 万 slot
maxHessianTotalItems     = 4 << 20 // 单次解码累计最多约 400 万 slot
```

按 64 位 Go interface slot 16 bytes 粗算，单 slice backing array 上限约 32 MiB、累计 slot 下限约 64 MiB；
复杂 map/object 会有额外开销，但仍从原始漏洞可触发的数十 GiB 降为确定上限。该策略将旧阈值扩大 2x/4x，
同时保留 response byte limit、remaining-input 校验、depth limit 和 ref 校验。

预算不做配置项：远端输入的安全边界不应被普通配置无意关闭。若生产证据显示仍需提高，应同时记录进程内存
目标和代表性 payload，再修改这两个耦合常量。

### 4.3 验证

- 直接测试 `reserveContainerItems`：旧 100 万累计边界以上可通过，新 400 万累计边界仍拒绝。
- 构造超过 100 万但低于新单容器上限的紧凑列表，证明实际 reader 可解码；测试规模控制在 CI 可接受范围。
- 既有畸形长度/fuzz/oracle 全部继续通过。

## 5. Parser 成员级通用错误恢复

### 5.1 恢复 seam

恢复只放在 `parseClassBodyMembers`：每次解析成员前记录 cursor checkpoint；`parseMember` 返回错误时回滚到
checkpoint，由 `recoverTypeMember` 从成员起点同步。这样嵌套 type 已经消费部分 body 时也不会丢失 brace 层级。

同步规则：

1. 在顶层 delimiter 深度遇 `;`：消费并结束当前坏成员；
2. 在顶层遇成员 body `{...}`：平衡跳过完整 body；紧随 `;` 时一并消费；
3. 在未进入成员 body 前遇 owner `}`：不消费，交还 owner；
4. 到 EOF 或结构无法平衡：恢复失败，返回原始错误，整文件继续按现有逻辑标记为 skip warning。

不以 `<`/`>` 作为同步深度，因为 Java 表达式比较符会造成与旧 initializer bug 相同的误判。

### 5.2 warning interface

`CompilationUnit` 增加仅在非空时序列化的 warning：

```go
type ParseWarning struct {
    Pos     Position
    Message string
}

type CompilationUnit struct {
    // existing fields...
    Warnings []ParseWarning `json:"Warnings,omitempty"`
}
```

warning 记录坏成员起点与原始 parse error。schema 在成功接收 partial compilation unit 后把 warning 加入
`Index.Warnings`，措辞使用 `recover <path>: ...`，与文件完全失败的 `skip <path>: ...` 区分。

### 5.3 缓存与兼容

Parser 恢复会让过去被整个跳过的文件产生新 schema，因此 `indexCacheVersion` 必须从 6 bump 到 7，强制旧缓存
重建。正常文件 AST golden 因 `omitempty` 不发生变化。

测试至少覆盖：

- 坏字段/坏方法后仍解析后续字段和方法；
- 坏嵌套 type 不污染 outer，后续成员仍保留；
- annotation/interface body 走同一恢复；
- 未闭合 method/type body 仍致命；
- schema 收到 partial type 和 recover warning，describe 回显 warning；
- 正常 fixture/golden 无变化。

## 6. 实施顺序与依赖纪律

1. 先补 app SourceIndex seam 和注入测试，再迁移 describe/doctor。
2. 独立调整 Hessian budget 与负面测试，随后跑 JVM/BOLT oracle。
3. 最后实现 parser warning/recovery并 bump schema cache；该项影响面最大，单独提交。
4. 回填 decisions、本设计和计划，执行全量门禁。

依赖方向保持 `mcp/tools -> app -> schema/appconfig`、`direct` 自包含、`schema -> javaparser`，不新增反向依赖。

## 7. 明确不在本 Goal 范围

- lexer 级错误恢复；未闭合字符串、注释或 text block 的 partial token stream。
- 表达式 AST、方法体 AST 或完整 Java 编译器语义。
- 按调用参数动态关闭 Hessian 安全预算。
- SourceIndex 远端实现、跨项目搜索或 schema cache 存储格式重写。

## 8. 完成定义

- describe/doctor 不再直接调用 tools `loadConfig` 或 `schema.LoadOrBuildIndex`；共享 app service 注入测试通过。
- 合法累计/单容器超过旧 100 万阈值的 Hessian 测试通过，新硬上限仍被拒绝。
- 单个可同步的坏成员不再丢失文件，warning 经 parser→schema→describe 可观测。
- cache version 已 bump，正常 AST/schema/MCP wire 契约无漂移。
- `go vet ./...`、全量、race、Windows build、BOLT oracle、Hessian JVM oracle 全部通过。
- 原有六份未跟踪历史文档不被修改或提交。

## 9. 实施结果

| 范围 | 提交 | 结果 |
| --- | --- | --- |
| app.Service 数据源注入 | `51b587a` | describe/doctor 与 invoke 共用 ConfigStore/SourceIndex；损坏磁盘配置 + 内存 adapter 的 MCP 测试通过 |
| Hessian item budget | `b073493` | 单容器/累计上限调整为 2M/4M；1,048,577 项实际 compact list 解码通过，新累计上限继续拒绝 |
| Parser member recovery | `31cfeec` | class/interface/annotation/nested type 可安全同步；warning 贯通 schema/MCP describe；cache v7 重建 |

全部停止条件均未触发。Parser 恢复保持在 type-body member seam；lexer 错误和结构不闭合仍 fatal。

最终门禁全部通过：`git diff --check`、`go vet ./...`、`go test ./...`、`go test -race ./...`、Windows
amd64 交叉构建、BOLT race oracle，以及 Hessian JVM/BOLT real oracle gate。
