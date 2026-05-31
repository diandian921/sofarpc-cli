# MCP Architecture Blueprint (sofarpc-mcp)

Date: 2026-05-28
Status: Target architecture (long-term vision; not current state)
实施路径: §1–§15 是三层终态愿景；短期实施路径见 §16（codex 审后建议先走单包 20/80，三层推迟）。
Audience: 后续 epic 的施工蓝图。本文只描述目标态，不批评现状。

---

## 1. 定位

`sofarpc-mcp` 是一个 **stdio MCP server**，把 SofaRPC 直接调用能力以 MCP 工具的形式暴露给 LLM agent。
host（Claude Code / Desktop / 其他 MCP client）启动它、用 JSON-RPC over stdio 跟它通话。

```
                ┌──────────────────────────────┐
                │   MCP Host (Claude Code)     │
                │   - 启动 sofarpc-mcp 进程    │
                │   - 通过 stdio JSON-RPC 通话 │
                └──────────────┬───────────────┘
                               │  stdin / stdout (line-delimited JSON)
                               ▼
       ┌────────────────────────────────────────────────┐
       │             sofarpc-mcp process                │
       │  ┌──────────────────────────────────────────┐  │
       │  │           MCP layer (this doc)           │  │
       │  └─────────────────┬────────────────────────┘  │
       │  ┌─────────────────▼────────────────────────┐  │
       │  │     internal/app  (业务编排 / Plan)      │  │
       │  └─────────────────┬────────────────────────┘  │
       │  ┌─────────────────▼────────────────────────┐  │
       │  │  internal/direct (BOLT / Hessian2 codec) │  │
       │  └─────────────────┬────────────────────────┘  │
       └────────────────────┼────────────────────────────┘
                            │ TCP (BOLT)
                            ▼
                    SofaRPC Provider
```

MCP layer **只负责协议侧**，不感知 sofarpc 内部细节。
反过来，`internal/app` 也不知道有 MCP 存在。终态下 **MCP 是唯一的业务入口**，CLI 退化为 binary 包装盒 + maintainer 诊断通道（详见 §12）。

---

## 2. 设计原则

1. **协议层与业务层分离**：wire 格式、JSON-RPC、lifecycle state、cancellation、progress / logging 发射归 `proto`；tool 注册 / 分发归 `server`；业务归 `tools`。业务方完全不感知 JSON-RPC。
2. **Schema 手写、就近放**：每个 tool 的 input / output schema 手写在 tool 旁（不做反射生成——6 工具规模反射是负收益）；`server` 注册时只校验 schema 存在。
3. **失败也是契约**：错误返回的形状、code、`nextTool` 跟成功路径走**同一个** `app.Result`。
4. **Lifecycle 强制**：未经 `initialize / notifications/initialized` 握手的请求一律拒绝。
5. **依赖单向**：`tools → server → proto`，从不反向。

---

## 3. 整体架构

### 3.1 三层包结构

```
internal/mcp/
│
├── proto/        ← Layer 1: pure MCP protocol  (不知道 sofarpc 存在)
│   ├── jsonrpc.go         JSON-RPC 2.0 frame, strict decode, error codes
│   ├── message.go         typed Request / Response / Notification
│   ├── lifecycle.go       initialize 协商 + state machine + capabilities
│   ├── cancellation.go    notifications/cancelled + in-flight registry
│   ├── progress.go        _meta.progressToken 解析 + notifications/progress
│   ├── logging.go         capabilities.logging + notifications/message
│   └── transport.go       stdio reader/writer (line-delimited, size limit)
│
├── server/       ← Layer 2: tool/resource/prompt registry & dispatch (不知道 sofarpc 存在)
│   ├── server.go          Server 持有 Registry + Session
│   ├── registry.go        Tool / Resource / Prompt 注册表
│   ├── tool.go            Tool[A, O] interface + schema 反射
│   ├── annotations.go     Annotations 强制全字段
│   ├── handler.go         typed handler 签名
│   ├── strict_decode.go   不接受未声明字段、不静默吞 bool
│   └── meta.go            _meta 字段统一管理（requestId / elapsedMs）
│
└── tools/        ← Layer 3: sofarpc business tools
    ├── invoke.go          InvokeTool          (destructive)
    ├── invoke_plan.go     InvokePlanTool      (readOnly, dryRun 拆出来)
    ├── resolve.go         ResolveTool         (readOnly)
    ├── probe.go           ProbeTool           (readOnly, openWorld)
    ├── describe.go        DescribeTool        (readOnly)
    ├── doctor.go          DoctorTool          (readOnly)
    ├── config_list.go     ConfigListTool      (readOnly)
    ├── config_save_project.go
    ├── config_save_server.go
    ├── config_remove_project.go (destructive)
    └── config_remove_server.go  (destructive)
```

### 3.2 依赖方向

```
              ┌──────────────────┐
              │  cmd/sofarpc     │
              │  (binary entry)  │
              └────────┬─────────┘
                       │
           ┌───────────┴───────────┐
           ▼                       ▼
    ┌─────────────┐         ┌─────────────┐
    │ internal/   │         │ internal/   │
    │   cli       │         │  mcp/tools  │   ← Layer 3
    └──────┬──────┘         └──────┬──────┘
           │                       │
           │                       ▼
           │                ┌─────────────┐
           │                │ internal/   │
           │                │ mcp/server  │   ← Layer 2
           │                └──────┬──────┘
           │                       │
           │                       ▼
           │                ┌─────────────┐
           │                │ internal/   │
           │                │ mcp/proto   │   ← Layer 1
           │                └─────────────┘
           │
           ▼
    ┌─────────────────────────────────────┐
    │  internal/app    (业务编排)         │
    │  internal/direct (BOLT/Hessian)     │
    │  internal/schema (Java source 索引) │
    │  internal/appconfig (~/.sofarpc)    │
    └─────────────────────────────────────┘
                   ▲
                   │ 共用
                   │
    Layer 3 (tools) 直接复用 app/schema/appconfig
```

**关键约束**：
- `proto` 包不引用 `server / tools / app`
- `server` 包不引用 `tools / app`
- `tools` 包可以引用 `server / app / schema / appconfig`
- `cli` 跟 `mcp/*` 互不依赖——CLI 直接用 `app`

---

## 4. 三层职责详解

### Layer 1 — `internal/mcp/proto`

**职责**：让上层不再写一行 JSON-RPC。

提供：

| 能力 | 文件 | 关键 type |
|----|----|----|
| 帧读写 | `transport.go` | `Transport.Read() (Message, error)` / `.Write(Message) error` |
| 严格 decode | `jsonrpc.go` | `Decode(line []byte) (Message, error)`，校验 `jsonrpc=="2.0"`、method 非空、单帧单 JSON |
| 错误 code | `jsonrpc.go` | `ParseError / InvalidRequest / MethodNotFound / InvalidParams / InternalError`，统一 `Error{Code, Message, Data}` |
| 生命周期 | `lifecycle.go` | `Session.State`，初始化 / 已初始化 / 关闭中三态 |
| 版本协商 | `lifecycle.go` | `NegotiateVersion(client string) (server string, ok bool)`，命中→返该版本；不命中→返 server 最新或 `-32602` |
| 取消 | `cancellation.go` | `InFlight` 注册表（key = `(id, method)` 防 dup ID 覆盖）；cancel 后**不再发响应** |
| 进度 | `progress.go` | 只在 client 在 `params._meta.progressToken` 传了 token 时发 `notifications/progress` |
| 日志 | `logging.go` | `Session.Log(level, msg)` → `notifications/message` |

**不提供**：tool 概念、resource 概念、prompt 概念——这些是 Layer 2 的事。

### Layer 2 — `internal/mcp/server`

**职责**：把 Layer 1 提供的协议能力组合成 MCP server 的高层语义（tool / resource / prompt 注册与分发）。

核心 type：

```go
type Server struct {
    Info         ServerInfo                 // name / title / version / icons
    Instructions string                     // server 级指引
    Caps         ServerCapabilities         // 声明哪些 capability
    Registry     *Registry
}

type Tool[Args any, Out any] interface {
    Name() string
    Title() string
    Description() string
    Annotations() Annotations
    Execute(ctx context.Context, args Args) (Out, error)
}

type Annotations struct {           // 强制全字段填写，禁止默认值
    ReadOnlyHint    bool
    DestructiveHint bool
    IdempotentHint  bool
    OpenWorldHint   bool
}

// 注册时反射 Args/Out 生成 input/outputSchema
func Register[A, O any](r *Registry, t Tool[A, O])
```

Registry 内部：

```go
type wrappedTool struct {
    name         string
    title        string
    description  string
    annotations  Annotations
    inputSchema  json.RawMessage  // 反射自 A
    outputSchema json.RawMessage  // 反射自 O
    invoke       func(ctx context.Context, raw json.RawMessage) (any, error)
}
```

dispatch 过程：

```
proto.Request{method:"tools/call", params:{name, arguments}}
  → server.Server.handleToolsCall
    → registry.lookup(name)
      → strictDecode(raw → A)                  // dryRun:"true" → -32602
        → tool.Execute(ctx, A)
          → wrap as proto.Response{result: {content, structuredContent, _meta}}
```

### Layer 3 — `internal/mcp/tools`

**职责**：把每个 sofarpc 能力包成一个 typed tool。**只写业务**，不碰协议。

每个 tool 一个文件、一个 struct，10–15 行模板 + 业务调用。

```go
type InvokeArgs struct {
    Server           string         `json:"server,omitempty"`
    Project          string         `json:"project,omitempty"`
    Service          string         `json:"service" required:"true"`
    Method           string         `json:"method"  required:"true"`
    ParamTypes       []string       `json:"paramTypes,omitempty"`
    OrderedArguments []any          `json:"orderedArguments,omitempty"`
    Arguments        map[string]any `json:"arguments,omitempty"`
    TimeoutMS        int            `json:"timeoutMs,omitempty"`
    RawResult        bool           `json:"rawResult,omitempty"`
}

type InvokeTool struct{ app *app.Service }

func (t *InvokeTool) Name() string        { return "sofarpc_invoke" }
func (t *InvokeTool) Title() string       { return "SofaRPC Invoke" }
func (t *InvokeTool) Description() string { return "Invoke a SofaRPC method." }
func (t *InvokeTool) Annotations() server.Annotations {
    return server.Annotations{
        ReadOnlyHint: false, DestructiveHint: true,
        IdempotentHint: false, OpenWorldHint: true,
    }
}
func (t *InvokeTool) Execute(ctx context.Context, a InvokeArgs) (app.Result, error) {
    plan, err := t.app.PlanInvocation(ctx, a.toAppInput())
    if err != nil { return app.Result{}, err }
    return app.RenderExecution(t.app.ExecuteInvocation(ctx, plan)), nil
}
```

`dryRun` 拆出成独立 `InvokePlanTool`（readOnlyHint=true），让 host 可以安全地 auto-approve plan。

---

## 5. Lifecycle 状态机

```
                          ┌────────────────┐
                          │  StateNew      │
                          │  (just spawned)│
                          └────────┬───────┘
                                   │ receive: initialize
                                   ▼
                          ┌────────────────┐
              negotiate   │ StateInitializing│
              version &   │  - sent initialize│
              capabilities│    response       │
                          └────────┬───────────┘
                                   │ receive: notifications/initialized
                                   ▼
                          ┌────────────────┐
       ┌──────────────────│  StateReady    │◄────────────┐
       │                  │                │             │
       │                  └────────┬───────┘             │
       │                           │                     │ all
       │ EOF on stdin              │ any request /       │ other
       │                           │ notification        │ requests
       │                           │                     │
       ▼                           ▼                     │
┌────────────────┐         ┌────────────────┐            │
│  StateClosing  │         │   dispatch     │────────────┘
│  drain inflight│         │   (tools/list, │
│  ack cancels   │         │    tools/call, │
└────────────────┘         │    ...)        │
                           └────────────────┘

Gating rules:
  StateNew         → only `initialize` accepted; others → -32002 "server not initialized"
  StateInitializing → only `notifications/initialized` accepted
  StateReady       → all methods accepted
  StateClosing     → respond -32000 "shutting down" to new requests
```

---

## 6. 数据流（一次完整 `sofarpc_invoke`）

```
┌─────────┐     {"jsonrpc":"2.0","id":1,"method":"tools/call",
│  Host   │      "params":{"name":"sofarpc_invoke",
│         │       "arguments":{...},
│         │       "_meta":{"progressToken":"p-1"}}}
└────┬────┘
     │ stdin
     ▼
┌────────────────────────────────────────────────────────────┐
│ proto/transport.go                                          │
│   readLine (≤ 16MB)  →  Decode JSON-RPC                     │
│                          ├ jsonrpc=="2.0"? else -32600     │
│                          ├ method 非空? else -32600         │
│                          └ 单帧单 JSON? else -32700         │
└────────────────────────┬───────────────────────────────────┘
                         │
                         ▼
┌────────────────────────────────────────────────────────────┐
│ proto/lifecycle.go                                          │
│   session.state == Ready ?  else -32002                    │
└────────────────────────┬───────────────────────────────────┘
                         │
                         ▼
┌────────────────────────────────────────────────────────────┐
│ proto/cancellation.go                                       │
│   inflight.register(id, method, cancelFn)                  │
│   ctx, _ = context.WithCancel(parent)                       │
└────────────────────────┬───────────────────────────────────┘
                         │
                         ▼ (async dispatch via goroutine pool)
┌────────────────────────────────────────────────────────────┐
│ proto/progress.go                                           │
│   if params._meta.progressToken → enable broker            │
└────────────────────────┬───────────────────────────────────┘
                         │
                         ▼
┌────────────────────────────────────────────────────────────┐
│ server/registry.go: lookup("sofarpc_invoke") → wrappedTool │
│ server/strict_decode.go:                                    │
│   raw arguments → InvokeArgs                               │
│     ├ unknown field? → -32602                              │
│     └ dryRun:"true"? → -32602 (bool 严格)                  │
└────────────────────────┬───────────────────────────────────┘
                         │
                         ▼
┌────────────────────────────────────────────────────────────┐
│ tools/invoke.go: InvokeTool.Execute(ctx, args)              │
│   app.PlanInvocation(...)                                  │
│     ↓ progress.send("plan resolved")                       │
│   app.ExecuteInvocation(...)                               │
│     ↓ progress.send("TCP connected")                       │
│     ↓ progress.send("hessian decoded")                     │
│   return app.Result                                         │
└────────────────────────┬───────────────────────────────────┘
                         │
                         ▼
┌────────────────────────────────────────────────────────────┐
│ server/meta.go: 把 requestId / elapsedMs 挪到 _meta         │
│ wrap as toolResult:                                         │
│   { content: [{type:text, text: summary}],                 │
│     structuredContent: app.Result,                         │
│     _meta: {requestId, elapsedMs},                         │
│     isError: !result.ok }                                  │
└────────────────────────┬───────────────────────────────────┘
                         │
                         ▼
┌────────────────────────────────────────────────────────────┐
│ proto/cancellation.go:                                      │
│   if ctx already cancelled → DROP, do not send response    │
│   else inflight.complete(id)                                │
└────────────────────────┬───────────────────────────────────┘
                         │
                         ▼
┌────────────────────────────────────────────────────────────┐
│ proto/transport.go: write line (mutex 仅保护 Encode→Write) │
│   for long writes use buffered channel + writer goroutine  │
└────────────────────────┬───────────────────────────────────┘
                         │ stdout
                         ▼
                    ┌─────────┐
                    │  Host   │
                    └─────────┘
```

---

## 7. 协议合规面（capabilities）

server 在 `initialize` 响应中**显式声明**支持的 capability：

```jsonc
{
  "protocolVersion": "<negotiated>",
  "capabilities": {
    "tools":   { "listChanged": false },   // 工具列表静态，不会运行时变
    "logging": {}                           // 支持 notifications/message
  },
  "serverInfo": {
    "name":    "sofarpc-mcp",
    "title":   "SofaRPC Direct Invoker",
    "version": "<build>",
    "description": "Invoke SofaRPC services directly over BOLT/Hessian2.",
    "icons":   [{ "src": "...", "mimeType": "image/svg+xml" }]
  },
  "instructions": "Run sofarpc_resolve before sofarpc_invoke. When multiple servers exist, always pass `server`. Use sofarpc_describe query=... to find a service FQN before invoking."
}
```

**明确不支持**（不声明，避免 host 错误启用）：
- `resources`（曾考虑暴露 `~/.sofarpc/config.json`，因含 attachments / 凭证有泄密风险已否决，见 spec review 第二节 N20）
- `prompts`
- `roots` / `sampling` / `elicitation`（client capability，server 不主动发起）

---

## 8. Tool 全量清单（目标态）

| Name | Annotations | input 概要 | output |
|----|----|----|----|
| `sofarpc_resolve` | readOnly, idempotent | server? project? | endpoint + diagnostics |
| `sofarpc_probe` | readOnly, idempotent, openWorld | server\|address, service? | TCP probe result |
| `sofarpc_describe` | readOnly, idempotent | service\|query, method?, limit? | methods + DTO schemas |
| `sofarpc_doctor` | readOnly, idempotent | project?, server?, service?, method? | checks[] |
| `sofarpc_invoke_plan` | readOnly, idempotent | service, method, args... | plan (no network) |
| `sofarpc_invoke` | destructive, openWorld | service, method, args..., timeoutMs? | app.Result |
| `sofarpc_config_list` | readOnly, idempotent | project? | projects + servers |
| `sofarpc_config_save_project` | not-readOnly | name, workspaceRoot, servicePrefixes? | saved |
| `sofarpc_config_save_server` | not-readOnly | name, address, project, ... | saved |
| `sofarpc_config_remove_project` | destructive | name, confirm:true, cascade? | removed |
| `sofarpc_config_remove_server` | destructive | name, confirm:true | removed |

注：`--disable-config-write` 启动时，4 个写工具不注册（`tools/list` 自然只剩只读集）。

---

## 9. 错误模型（单一形状）

无论是 协议错误 / bad-arguments / business error，**最终给到 agent 的 structuredContent 形状一致**：

```jsonc
// 成功
{
  "ok": true,
  "result": { ... },        // business payload
  "_meta": { "requestId": "...", "elapsedMs": 123 }
}

// 失败
{
  "ok": false,
  "error": {
    "code":    "ARGUMENT_TYPE_MISMATCH",   // 业务 code (kebab/snake)
    "kind":    "bad_arguments",            // 大类
    "message": "...",
    "details": { ... },
    "nextTool": {                          // 可选，agent 自恢复用
      "name": "sofarpc_describe",
      "arguments": { "service": "..." }
    }
  },
  "_meta": { "requestId": "...", "elapsedMs": 12 }
}
```

JSON-RPC 层错误（`-32xxx`）只在**协议层**用，比如 method not found、parse error、参数 schema 校验失败。
业务层错误**永远**走 `result.structuredContent.ok=false`，不抛 JSON-RPC error。

---

## 10. 并发与取消模型

```
                   ┌──────────────────┐
                   │  read loop       │
                   │  (single thread) │
                   └────────┬─────────┘
                            │ readLine + decode
                            ▼
                  ┌─────────────────────┐
                  │  classify request   │
                  └────┬─────────┬──────┘
                       │         │
              async-safe         sync-safe
                 (e.g.            (e.g.
              tools/call         tools/list,
              invoke/probe/      initialize)
              describe)              │
                  │                  ▼
                  ▼            handle inline
        ┌──────────────────┐   → write to outbox
        │ goroutine pool   │
        │ (1 per request)  │
        │ register inflight│
        │ ctx with cancel  │
        └────────┬─────────┘
                 │ tool.Execute(ctx, args)
                 ▼
        ┌──────────────────┐
        │  result / error  │
        │  → check ctx     │
        │    if cancelled, │
        │       drop       │
        │    else write    │
        └────────┬─────────┘
                 │
                 ▼
       ┌──────────────────────┐
       │ writer goroutine     │ ← outbox channel
       │ (serializes writes,  │
       │  never blocks read   │
       │  loop)               │
       └──────────────────────┘
```

关键设计：
- **read loop 永不阻塞**：所有 handler 走 goroutine pool 或直接走 inline 但极快（initialize / tools/list 这种 < 1ms 的）
- **写出走单独 writer**：handler 把响应丢 channel，writer goroutine 串行写 stdout。一个 slow write 不会卡其它响应
- **inflight registry 用复合 key**：`(id, method)`，杜绝 duplicate ID 覆盖 cancel 函数
- **cancel 后不发响应**：spec 要求；in-flight registry 在响应即将发出时检查 ctx 状态

---

## 11. 测试金字塔

```
                    ▲
                    │
              ┌─────┴─────┐
              │  e2e (10) │   全 stdin→stdout，3 个核心场景：
              │           │     - 初始化 + tools/list
              └───────────┘     - invoke 成功
                                - cancel during invoke
            ┌──────────────────┐
            │  tools 层 (40)   │   每个 tool 直接 Execute()，
            │                  │   不走 JSON-RPC
            └──────────────────┘
        ┌────────────────────────┐
        │  server 层 (30)        │   注册 mock tool 验证：
        │                        │     - schema 反射
        │                        │     - annotations 注入
        │                        │     - dispatch
        │                        │     - strict decode 拒绝 unknown field
        └────────────────────────┘
    ┌──────────────────────────────┐
    │  proto 层 (60)               │   纯 JSON 输入输出：
    │                              │     - lifecycle gating
    │                              │     - version negotiation (3 case)
    │                              │     - JSON-RPC 严格校验 (10 case)
    │                              │     - cancel semantics (4 case)
    │                              │     - duplicate ID
    │                              │     - progress with/without token
    └──────────────────────────────┘
```

外加：
- **contract test**：把 `tools/list` 输出过官方 MCP JSON Schema (ajv) 验
- **selftest**：启动时跑 `initialize → notifications/initialized → tools/list → tools/call sofarpc_config_list`，4 步全过才算 ok

---

## 12. 与 CLI / app 的边界（终态愿景）

终态下 **MCP 是唯一的业务入口**。CLI 不再是 MCP 的对等物，而是退化成两件事：**binary 包装盒** + **maintainer 诊断通道**。所有业务能力（invoke / resolve / describe / probe / 配置管理）只通过 MCP 工具暴露。

```
                  ┌────────────────┐
                  │  internal/app  │  ← 唯一业务真值源
                  │  Service/Plan/ │
                  │  Result        │
                  └───┬─────────┬──┘
   consume(仅诊断)    │         │ consume(全部业务)
                      │         │
       ┌──────────────▼┐      ┌─▼─────────────┐
       │ internal/cli  │      │ internal/mcp  │
       │ 包装盒 + 诊断 │      │ 唯一业务入口  │
       └───────────────┘      └───────────────┘
             ▲                        ▲
       install/setup/mcp/        所有业务调用
       version/doctor/debug      (agent via stdio)
```

### 终态 CLI 表面（三档）

| 档 | 命令 | 稳定性 | 说明 |
|----|----|----|----|
| ✓ Lifecycle | install / self-install / setup / mcp / version / help | 完整承诺 | binary 包装盒，不可删 |
| ✓ Diagnostics | doctor / debug ping / debug resolve / debug dump-config | 稳定，maintainer 向 | 顶层 ping / resolve 降级为 debug 子命令 |
| ✗ 本次删除 | invoke | — | 能力转 `sofarpc_invoke`（已锁定） |
| ⏳ 未来缩减 | project / server 的 add\|list\|remove | 需单独决定 | 终态转 `sofarpc_config`，**不在本次 invoke 删除范围** |

本次删除（已锁定）：

| 删除的 CLI | 替代的 MCP 工具 |
|----|----|
| `sofarpc invoke` | `sofarpc_invoke` |

未来缩减（终态愿景，需单独决定，**不在本次范围**）：

| 未来可能删 | 替代的 MCP 工具 |
|----|----|
| `sofarpc project add/list/remove` | `sofarpc_config`（save_project / remove_project / list） |
| `sofarpc server add/list/remove` | `sofarpc_config`（save_server / remove_server / list） |

### 决策标准（以后加任何 CLI verb 都套这三条）

保留 / 新增一个 CLI verb，**当且仅当**满足以下之一：

1. 它是装 / 注册 / 启动 / 检查 / 修复 **MCP server 本身**所必需
2. 它支持一个**不能合理依赖 agent host** 的工作流（CI / scripting / outage / policy-restricted），**且有实际使用证据**
3. 它是**有测量数据 + deprecation 责任人 + deprecation 日期**的兼容 shim

否则不加；能力只暴露为 MCP 工具。

> **关于 `invoke`（最终：直接删）**：它表面符合标准 #2（CI / script 场景），但目前无使用证据，按「无数据不留」删除。⚠️ codex 反对盲删，提示 discovery asymmetry——最可能用它的人（CI / cron / 受限机器）撞墙不会反馈，只会默默绕过，删错了也收不到信号。最终仍决定删除；若日后出现真实需求，按标准 #2 低成本加回（现有 `invoke` 已是 `app.Service` 薄消费者，加回成本低）。

### app 作为单一真值源

- `app.Service` 是唯一业务真值源；CLI 的诊断命令和 MCP 的业务工具都 consume 它
- CLI 和 MCP 之间**没有任何代码共享**——共享只发生在 `app` 层
- `app.Result` 是统一输出契约（CLI `--json` 和 MCP `structuredContent` 是同一个结构体）

---

## 13. Server 进程边界

| 维度 | 行为 |
|----|----|
| 启动 | host 用 stdio 启动 `sofarpc-mcp`；进程 1:1 对应一个 host session |
| stdin EOF | 立刻进入 `StateClosing`，drain in-flight，正常退出 0 |
| panic | 在 handler 内 recover，response 给 client 固定 `"internal error"`，详情 + stack 写 stderr + 自增 errorId |
| stderr | 仅写脱敏后的 server 自身日志，绝不写用户数据 / 文件路径 / IP；host 可选择捕获 |
| log capability | 启用后通过 `notifications/message` 把结构化日志推给 host，分级 debug/info/warn/error |
| concurrency | 单 read loop + N worker goroutine + 单 writer goroutine；context 携带 cancel |
| 最大帧 | stdio 单 JSON 帧 ≤ 16MB；超出 → `-32600` 并继续 |

---

## 14. 演进路径（从现状到目标态）

不在本文档详述，但目标蓝图就是上面这套。
落地分两个阶段：

1. **阶段 A（补丁）**：在现有 `internal/mcp/` 单包内修 P0/P1 合规点（约 3-4 天）
2. **阶段 B（重构）**：抽 `proto` / `server` / `tools` 三层，搬现有 handler 进 `tools/`（约 3-5 天）

阶段 A 的修复在阶段 B 里多数会自然消化，所以两阶段可视为「先止血、再换器官」。

---

## 15. 一句话总结

```
proto  = 协议本身（不知道 sofarpc）
server = tool 注册与分发（不知道 sofarpc）
tools  = sofarpc 业务工具（不知道 JSON-RPC）

依赖单向、schema 即代码、错误一形、lifecycle 强制。
```

---

## 16. Codex 蓝图审查与实施路径决策

§1–§15 是**目标态设计**。本节记录 codex 审查的可执行结论 + 最终裁决。**历史**：codex 两轮推荐单包路径 B、判三层对 6 工具过度设计；最终仍裁决走路径 A（理由见文末「已裁决」）。下面只保留**对落地 A 有用**的部分。

### 落地 A 的技术前提（codex 历轮，仍适用）

- **schema 手写、不反射**：6 工具 ~40 字段，反射生成是 tarpit（conditional-required 的 `confirm`、enum、alias、oneOf、动态值类型都表达不了）。保留 `tool_schema.go` builder。
- **pinned tests 是 tripwire**：`TestInitializeEchoes...` 等钉死当前 wire 行为的测试，先改成断言**目标**契约、看它失败、再实现。
- **两业务面 drift 是头号风险**：重构期 app / CLI 仍出货，若 `app.Result` / `InvocationPlan` 中途变，半迁移的 MCP 在打移动靶 → 见「已裁决」的两条护栏。

### 落地 A 的执行序（codex 第三轮，每步 `go test ./...` 绿）

**自底向上，先建 proto，最后才挪文件**（不要「单包改完再搬」——那会把导出 API、测试重写、循环依赖都推到最后才暴露）：

1. **先加边界测试**：`proto` 不 import 任何 repo 包；`server → proto`；`tools → server + app / schema / appconfig`；`server` 不得 import `tools` / `app`。
2. **建 `internal/mcp/proto`**：JSON-RPC 类型、strict decode、错误、transport、lifecycle state、in-flight cancellation、progress / logging 发射。先把协议测试搬过来。
3. **`internal/mcp` 留作 facade**：包住 `proto.Session`，让 `internal/cli/mcp.go` 不 churn。
4. **建 `internal/mcp/server`**：registry、annotations、strict typed decode、result 包装、`_meta`。只用 mock tool，不碰 SofaRPC。
5. **建 `internal/mcp/tools`**：逐个搬真工具——resolve、probe、describe、doctor、config 拆分、invoke_plan、invoke。
6. 全绿后清理旧 handler helper，或留薄 facade。

### 三层边界与依赖（避开 cycle 的关键）

依赖方向：`cli/mcp facade → tools → server → proto`；`proto` 只依赖 stdlib；`tools → app / schema / appconfig`。

| 包 | 职责 |
|----|----|
| `proto` | JSON-RPC 帧编解码、stdio transport、lifecycle state、版本协商、in-flight registry、cancellation 语义、progress token 解析、progress / logging 发射、client capabilities。**有状态的会话协议，不只是 struct。** |
| `server` | MCP 语义分发（tools/list、tools/call）、registry、annotations、strict 参数 decode、result 包装、`_meta`、outputSchema 存在性。**不含任何 SofaRPC 业务。** |
| `tools` | SofaRPC 工具实现 + 手写 schema。调 `app.Service` / `schema` / `appconfig`。 |

**避 cycle 的关键**：progress / logging 不让 `tools` 直接碰 `proto`，而是 `server` 暴露一个 `Runtime` 接口：

```go
// server 包
type Runtime interface {
    Progress(ctx context.Context, msg string, percent *float64)
    Log(ctx context.Context, level, msg string)
}
type Tool[A any, O any] struct {
    Spec ToolSpec
    Run  func(context.Context, Runtime, A) (O, error)
}
```

`server` 用 `proto.Session` 实现 `Runtime`；`tools` 只 import `server.Runtime`，**永不 import `proto`**。

### invoke 删除清单（codex 第三轮，精确）

- `internal/cli/cli.go`：删 `case "invoke"` 分发 + help 行 `sofarpc invoke [flags]`
- 删整个 `internal/cli/invoke.go`
- `internal/cli/subcommands_test.go`：删 `TestInvokeSubcommandReturnsDirectFailureResult` / `TestInvokeRejectsArgTypeMismatch` / `TestBuildInvokeInputPreservesLargeJSONNumbers`
- 清理 import（尤其 `strconv`）
- 更新 README / docs 里 `sofarpc invoke`、`--assertions-json` 引用

抓残留：

```sh
rg -n 'runInvoke|buildInvokeInput|executeInvoke|applyAssertions|assertions-json|sofarpc invoke' internal cmd README.md docs
go list -deps ./internal/app ./internal/mcp | rg '/internal/cli'   # 应为空
```

已确认：`app` / `mcp` 不依赖 CLI invoke（MCP 的 invoke 在 `server.go` 直连 `app.Service`）。

### 三个弃坑陷阱（Path A 专属）

1. **边改行为边挪文件** → 自底向上、一次一层、`internal/mcp` facade 留到最后。
2. **progress / logging / cancel 引发 import cycle** → 状态放 `proto`；`server.Runtime` 接口给 `tools`；`tools` 永不 import `proto`。
3. **schema / registry 框架膨胀超过 6 工具实际需要** → schema 手写就近放；一个 typed 注册 helper；本次不做反射生成器。

### 已裁决（2026-05-31）

1. **路径 A：三层分包** —— 抽 `proto` / `server` / `tools`。⚠️ codex 两轮均推荐单包路径 B（判三层对 6 工具过度设计、收益与单包相同、弃坑风险高）；最终仍选 A，取其长期架构整洁。落地纪律：守「< 1 周完成，否则回退」，先增量改、最后才挪包（见上「迁移序」）。
2. **invoke：直接删** —— 从 CLI 移除，能力转 `sofarpc_invoke` MCP 工具。接受 codex 提的 discovery asymmetry 风险（CI / script 用户撞墙不反馈）。
3. **重构护栏：两个都做** —— (a) 重构期冻结 `app` 公共类型；(b) 加跨 render contract test（同一 InvocationPlan 过 CLI / MCP 断言 `app.Result` 相同）。
