# D 类平行实现收敛设计（2026-07）

## 1. 背景与基线

- 需求依据：`docs/deep-analysis-2026-07.md` 的 D 类发现，以及 `docs/decisions.md` backlog 3b。
- 代码基线：`fix/deep-analysis-release-blockers@a82ac0a`。
- 实施分支：`refactor/converge-parallel-implementations`，叠加在上一轮安全/正确性修复之上。
- 目标：让一类业务规则只有一个所有者和一个可测试接口，消除“改一处漏一处”的活跃风险；不改变 MCP
  成功响应、RPC wire shape、配置格式或 Java schema 格式。

本设计使用“深模块”原则：在稳定 seam 上保留小接口，把选择顺序、类型解析顺序、错误分类等复杂实现
隐藏在接口之后。所有依赖均为进程内纯计算或本地文件读取，不为测试凭空引入 Adapter。

## 2. 问题定义

当前存在五组已确认的平行实现：

1. `internal/app/resolve.go` 与 `internal/mcp/tools/helpers.go` 各自实现项目/服务端选择，错误类型已漂移。
2. `internal/app` 和 `internal/direct` 各自擦除 Java 泛型/数组并识别 `byte[]`，多维数组和 `final` 行为不同。
3. `internal/app/rpc_types.go` 的 method、field、generic-value 三条类型解析链重复相同优先级规则，
   same-package lookup 和 type-variable fallback 不一致。
4. `internal/javaparser/decls.go` 的顶层/嵌套类型声明解析复制约 75 行，`@interface` 校验已经分叉。
5. 配置损坏在 config、resolve/invoke、describe 等工具中分别变成 `CONFIG_INVALID`、`BAD_REQUEST`、
   `INTERNAL_ERROR`，导致恢复建议不一致。

这些不是为了减少行数而重构。每一项都已有行为分叉，删除重复实现能把修复与验证集中到一个位置，产生
明确的 locality 和 leverage。

## 3. 设计目标与不变量

### 3.1 目标

- 项目/服务端选择规则只由 `internal/app` 所有，MCP tools 不再复制。
- Java 声明类型的基础形态（base type、byte array）只由 `internal/javavalue` 所有。
- RPC 类型解析优先级只实现一次，method/field/value 仅负责组装解析上下文。
- 顶层与嵌套类型声明共用同一声明主体解析实现。
- 所有普通工具遇 `*appconfig.ConfigError` 时保留稳定 code 和 `configPath`。
- 以新 seam 的接口级测试替代重复实现的内部测试，保留 MCP 端到端契约测试。

### 3.2 必须保持的不变量

- `project + profile`、active profile、显式 server、单 server 推断及 required/optional 歧义语义不变。
- `DomainError.Kind`、候选 server details、已有错误 message 的关键内容不变。
- RPC identity type 仍擦除泛型；value type 仍保留并递归解析泛型参数。
- 显式 import 优先于 type variable；declared type parameter 优先于 same-package class。
- wildcard value type 继续走安全的 untyped fallback，不拼出伪 FQN。
- `byte[]`/`java.lang.Byte[]` 继续按 Hessian binary 编码，多维数组不误判为单一 byte array。
- MCP 成功结果 schema、tool 数量、tool name 和参数 schema 不变。

## 4. 改动范围

| 模块 | 主要文件 | 改动 |
| --- | --- | --- |
| 配置选择 | `internal/app/resolve.go`、`internal/mcp/tools/helpers.go` | 建立 app 单一 seam，删除 tools 副本 |
| Java 类型基础语义 | `internal/javavalue`、`internal/app/rpc_*`、`internal/direct/hessian_writer.go` | 统一 base/byte-array 规则 |
| RPC 类型解析 | `internal/app/rpc_types.go`、`rpc_inherited.go`、相关测试 | 参数化单一 resolver |
| Java 声明解析 | `internal/javaparser/decls.go`、parser tests | 顶层委托共享主体 |
| 配置错误路由 | `internal/mcp/tools/*_sdk.go`、tools tests | 统一 ConfigError 路由 |
| 决策与验证 | `docs/decisions.md`、本设计及实施计划 | 记录单一所有者与验证结果 |

不涉及数据库、外部服务、配置表结构或公共依赖升级。

## 5. 模块与接口设计

### 5.1 配置选择模块：`internal/app`

在 app seam 暴露两个纯函数：

```go
type ProjectSelector struct {
    Project string
    Server  string
}

type ProjectSelection struct {
    Name    string
    Project appconfig.Project
}

func SelectProject(cfg appconfig.Config, selector ProjectSelector) (ProjectSelection, error)

type ServerSelector struct {
    Project  string
    Profile  string
    Server   string
    Required bool
}

type ServerSelection struct {
    Name   string
    Server appconfig.Server
    Found  bool
}

func SelectServer(cfg appconfig.Config, selector ServerSelector) (ServerSelection, error)
```

选择顺序与错误构造全部隐藏在这两个函数内。`app.Service`、describe 和 doctor 都跨同一 seam；tools 只把
输入组装为 selector，不再知道候选收集、active profile 或错误 details 的实现。

测试面放到 `internal/app`：表驱动覆盖每条选择路径及 `DomainError`。删除 tools 中对旧私有副本的单元测试，
保留 tools client-server 测试验证渲染后的 code/kind/recovery。

### 5.2 Java 类型基础语义模块：`internal/javavalue`

新增两个纯函数：

```go
func BaseJavaType(javaType string) string
func IsByteArrayType(javaType string) bool
```

`BaseJavaType` 统一执行 trim、可选 `final`、泛型擦除和全部数组维度擦除；`IsByteArrayType` 只接受恰好一维
的 `byte[]`/`java.lang.Byte[]`（允许空白和 `final`），不得把 `byte[][]` 当 binary。

app 与 direct 删除本地 `eraseRPCGeneric`/`eraseJavaType`/`isByteArrayType`，统一跨该 seam。该模块是纯
进程内实现，不新增 interface/Adapter；测试直接覆盖公开函数。

### 5.3 RPC 类型解析深模块：`internal/app`

`rpc_types.go` 内建立一个不导出的 `rpcTypeResolver`，其接口只有 identity/value 两种观察结果：

```go
type rpcTypeResolver struct {
    imports            map[string]string
    packageName        string
    declaredTypeParams []string
    knownTypes         map[string]schema.TypeSchema
}

func (r rpcTypeResolver) identity(javaType string) string
func (r rpcTypeResolver) value(javaType string) string
```

两种结果共享唯一 base 解析顺序：

1. Java builtin/primitive；
2. 已限定 FQN；
3. 显式 import；
4. declared type parameter 精确匹配；
5. same-package schema 命中；
6. 仅用于兼容缺失元数据的保守 type-variable 启发式；
7. package fallback。

启发式收窄为 Java 常见单字母类型变量及其数字后缀（`T`、`K`、`T1`），不再把 `ID`、`URL` 当成类型
变量。当前 schema cache 已带 `TypeParams` 且版本已升级，正常路径以声明元数据为准；启发式只是旧/损坏
元数据的安全兜底。

`identity` 擦除 generic/array；`value` 递归保留 generic/array。method 与 field 函数只负责从
`schema.Method`/`schema.TypeSchema` 构造 resolver，不再复制解析链。

### 5.4 Java 类型声明解析：`internal/javaparser`

`parseTypeDecl` 只做 preamble 消费，然后委托 `parseTypeDeclWithPreamble`：

```go
func parseTypeDecl(c *cursor) (TypeDecl, error) {
    pre, err := parsePreamble(c)
    if err != nil { return TypeDecl{}, err }
    return parseTypeDeclWithPreamble(c, pre)
}
```

共享实现必须严格验证 `@` 后为 `interface`，顶层/嵌套 record header 的错误语义统一。该阶段只删除重复，
不实现成员级错误恢复。

### 5.5 配置错误路由：`internal/mcp/tools`

新增一个统一渲染入口：

```go
func failureResult(err error, fallbackCode string) app.Result
```

- `errors.As(err, *appconfig.ConfigError)`：调用 `app.RenderConfigFailure`；
- 其它错误：使用 fallback code，并保留 `app.DomainErrorDetails(err)`。

resolve、invoke/invoke_plan、describe 及其它普通配置读取出口统一调用它。doctor 保留“检查集合”这一特殊
结果模型，避免 `CONFIG_INVALID -> sofarpc_doctor` 自指；其 config check 仍明确报告原错误。配置写工具现有
`configFailureResult` 合并到统一入口。

## 6. 依赖与 seam 纪律

- 既有方向不反转：`mcp/tools -> app`，`app -> appconfig/schema/javavalue/direct`，
  `direct -> javavalue`。
- `direct` 只新增对已依赖的 `javavalue` 函数调用，不得依赖 app/schema。
- 不创建只有一个生产 Adapter 的抽象接口；全部新 seam 都是纯函数或内部 resolver。
- tools 端只做参数/结果适配，不持有业务选择规则。

## 7. 兼容性与迁移

无需配置迁移。允许的可见行为变化仅有：

1. 配置损坏统一返回 `CONFIG_INVALID`/`CONFIG_UNSUPPORTED_VERSION` 与 `configPath`，不再伪装成
   `BAD_REQUEST`/`INTERNAL_ERROR`；
2. 缺失 type-parameter 元数据时，`T` 不再被拼为 same-package 伪 FQN；`ID`/`URL` 默认按类名处理；
3. `final` 和多维数组的 base type 语义在 app/direct 间统一。

其它成功/失败 wire shape 必须由现有 golden、oracle 和 MCP client-server 测试证明无漂移。

## 8. 风险与控制

- **类型语义变化范围大**：先冻结 `BaseJavaType`/resolver 表驱动测试，再替换调用方；最终跑真实 Hessian
  oracle。
- **错误文本被测试依赖**：保留 message 关键文本，只统一 error type/code/details。
- **删除 tools 单测降低覆盖**：先在 app seam 建立等价或更完整测试，再删除旧测试（replace，不叠加）。
- **叠加分支依赖上一轮**：本分支以 `fix/deep-analysis-release-blockers` 为基线；前序分支合入后再调整 PR base。

## 9. 明确不在本 Goal 范围

- Parser 成员级通用错误恢复、错误同步点和 partial AST。
- `maxHessianTotalItems` 调参或配置化。
- describe/doctor 完整迁移到 `app.Service` 的 SourceIndex 注入模型。
- tool registry、read-only annotation 预设、invoke schema 生成、`*_sdk.go` 文件合并。
- javaparser 四份 annotation skip 的进一步统一。
- java.time/BigInteger 下沉、D13 `errorCode` 政策及其它 P2。

## 10. 完成定义

- 五个范围项只有一个实现所有者，旧重复函数被删除；
- 新 seam 的接口级测试覆盖全部旧分支与已知漂移样例；
- MCP 配置错误矩阵返回统一稳定 code/details；
- `go vet ./...`、`go test ./...`、`go test -race ./...`、Windows 交叉构建通过；
- Hessian JVM 与 BOLT oracle gate 通过；
- `docs/decisions.md` 更新 backlog 状态，工作区原有未跟踪文档不被纳入。
