# Agent-First Improvement Plan

本文把当前最值得做的 Agent-first 改进整理成可执行路线。重点不是再迁移 MCP 协议层；`main` 已经使用官方 `modelcontextprotocol/go-sdk`。重点是让 Agent 在多步 SOFARPC 泛化调用中少丢上下文、少猜参数、少误触危险调用。

## 背景

当前服务器暴露的核心流程是：

```text
sofarpc_resolve
  -> sofarpc_describe
  -> sofarpc_invoke_plan
  -> sofarpc_invoke
  -> on failure: error.nextTool / sofarpc_probe / sofarpc_doctor
```

这个流程是对的，但它对 Agent 的要求较高：Agent 需要跨多个 tool call 记住 `project`、`server`、`service`、`method`、`paramTypes`、`orderedArguments` 或 `arguments`。一旦上下文变长，就容易漏字段、误用 overload、把上一轮服务名带到下一轮调用，或者重复拼接已经验证过的调用计划。

本文更新后的原则是：**第一阶段不新增工具**。先把现有工具的 schema、instructions、候选返回和隐藏契约收紧；只有在真实 Codex / Claude host 使用中仍明显出现上下文丢失或重复传参问题时，再考虑状态型 session/replay。

注意：**新增 0 个工具不等于 0 风险**。例如后文的 `planId` 方案虽然不增加 tool，但仍引入服务端状态、TTL/LRU、过期语义、脱敏边界和 Java long 精度测试；它的成本主要来自状态管理，而不是工具数量。因此它仍放在后续验证项里。

> **状态（2026-06-06，分支 `feat/agent-first-contract`）**：下表 **P0 三项已落地（Sprint 1）** —— per-tool outputSchema、instructions 失败恢复路径、invoke 别名收敛，并补了结构性 + 语义性（jsonschema-go）+ okResult 不变式 guard 测试。**下面对应的 P0 小节保留为「改动前」的设计记录**（其「当前问题」描述的是 Sprint 1 之前的状态），勿据此重复规划。P1/P2 仍待做。

## MCP 依据

这些改进遵循 MCP 的现有 primitives，不依赖私有协议：

- Tools：模型可调用的动作入口，支持 `inputSchema`、`outputSchema`、`structuredContent`、`isError` 和 tool annotations。参考 <https://modelcontextprotocol.io/specification/2025-06-18/server/tools>
- Prompts：用户可选择的可复用工作流模板，适合暴露“标准 SOFARPC 调用流程”。参考 <https://modelcontextprotocol.io/specification/2025-06-18/server/prompts>
- Resources：应用提供给模型的上下文，适合暴露安全、只读、脱敏的 schema/compatibility 信息。参考 <https://modelcontextprotocol.io/specification/2025-06-18/server/resources>
- Completion：官方 completion 主要面向 prompt/resource template 参数，不是普通 tool 参数；因此当前阶段先增强 `resolve` / `describe` 的候选输出，而不是新增 `suggest_*` tools。参考 <https://modelcontextprotocol.io/specification/2025-11-25/server/utilities/completion>
- Progress：只在 client 提供 progress token 时发送阶段进度。参考 <https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/progress>

## 优先级总览

| 优先级 | 改进 | 目标 |
|---|---|---|
| P0 | Per-tool outputSchema / guard tests | 把 `app.Result.data` 的真实形状变成机器可读契约，补上 raw SDK 路径不会自动校验的测试护栏 |
| P0 | Server instructions failure path | 在初始化指引里明确 `error.nextTool` / `error.recovery`、`doctor` / `probe` 的恢复路径 |
| P0 | Invoke schema/contract 收敛 | 消除“能解但不展示”的别名；保留 `required` 作为 host/LLM 填参提示 |
| P1 | Enhance resolve/describe candidates | 不新增 suggest 工具，把 server/service/method 候选并进现有工具输出 |
| P1 | Prompt template | 把推荐调用流程暴露为用户可选工作流 |
| P1/P2 | Session / Replay | 真实 host 验证仍有上下文丢失时再做，避免过早引入状态和工具膨胀 |
| P2 | Safe resources | 提供安全只读上下文，不暴露 config secrets |
| P2 | Safety annotations / write defaults | 降低配置写入、真实 invoke、显式地址 probe 的误用风险 |
| P2 | Large result text mirror policy | 对大结果避免 `structuredContent` 与 text 镜像无条件双份占用上下文 |
| P2 | Progress granularity | 让长耗时步骤更可观察 |

## P0: Per-Tool Output Schema / Guard Tests

> ✅ **已在 Sprint 1 落地**(`render.go` 的 `resultOutputSchemaWithData` + 每工具 data schema)。本节为改动前设计记录:下面「当前问题」里「所有工具共享一份 `resultOutputSchema`」描述的是 Sprint 1 之前的状态。

### 当前问题

当前所有 MCP 工具共享一份 `resultOutputSchema`。这份 schema 只描述了统一的 `app.Result` 信封：

```json
{
  "ok": true,
  "code": "SUCCESS",
  "data": {}
}
```

真正对 Agent 有价值的业务载荷被写成了裸 `object`。因此 Agent 从 `tools/list` 里看不出：

- `sofarpc_invoke` 的 `data` 里有 `result`、`assertions`、`warnings` 等字段。
- `sofarpc_invoke_plan` 的 `data` 里有 `dryRun`、`plan`。
- `sofarpc_describe` 的 `data` 里有 `service`、`methods`、`types`、`candidates`。
- `sofarpc_resolve` / `probe` / `doctor` 各自返回的诊断字段。

这会削弱 MCP `outputSchema` 的核心收益：给 client / LLM 提供类型信息、指导解析 structured result、支撑更严格的集成测试。

### 为什么这里尤其要补测试

项目目前有意使用 go-sdk 的 raw `Server.AddTool` 路径，而不是泛型 `mcp.AddTool`。这是合理的：raw adapter 能保留 `UseNumber`，避免 Java long 被转成 `float64`，也能把参数错误包装成友好的 `app.Result`。

代价是 raw 路径不会替我们做这些事：

- 不按 input schema 自动校验参数。
- 不按 output schema 自动校验 `structuredContent`。
- 不自动填充 `StructuredContent` / text mirror / `IsError`。

这些责任已经集中在 `adaptTool` / `manualResult` 里，所以 output contract 也必须由本项目自己守住。

### 推荐实现

保持统一 `app.Result` 信封，但把每个工具的 `data` 子结构描述清楚。

对 `sofarpc_resolve` / `sofarpc_describe`，第一批拆 schema 时就把候选字段预留进去。Sprint 2 会增强这两个工具的候选输出，如果 Sprint 1 的 output schema 只按当前字段写死，后面会立刻返工。建议从一开始就允许：

- `data.candidates[]`
- `score`
- `reason`
- `method`
- `paramTypes`
- `parameterNames`

可以先做一个 helper，避免每个 tool 重写完整信封：

```go
func resultOutputSchemaWithData(dataSchema json.RawMessage) json.RawMessage
```

然后每个工具提供自己的 `dataSchema`：

```go
var invokePlanDataSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "dryRun": {"type": "boolean"},
    "plan": {"type": "object"}
  },
  "required": ["dryRun", "plan"]
}`)
```

不要把 `outputSchema` 改成只描述业务 `data`；MCP 实际返回的 `structuredContent` 是整个 `app.Result`，schema 必须覆盖完整信封。

`sofarpc_invoke` 有一个特殊点：`data.result` 是远端 Java 返回值解码后的任意对象树，静态形状不可知。invoke 的 output schema 只能描述外层字段，例如 `result`、`assertions`、`warnings`、`diagnostics`；`result` 本身必须保持开放 schema，不要给深层字段设置 `required`，否则真实 RPC 返回稍有变化就会让 guard test 失败。

### 测试

- `tools/list` 中每个 tool 的 `OutputSchema` 都不是共享的裸 `data: object`。
- 每个 tool 的 fixture 成功结果都能按自己的 `OutputSchema` 验证通过。
- 失败结果仍符合统一 error schema，且包含 `nextTool` / `recovery`。
- 增加守护测试：`okResult` 的 `data` 必须序列化为 JSON object，禁止未来误传数组或标量。
- 测试说明 raw `Server.AddTool` 不会自动校验输出，避免维护者误以为 SDK 已兜底。

## P0: Server Instructions Failure Path

> ✅ **已在 Sprint 1 落地**(`sdkserver.go` 的 `serverInstructions` 已含 `error.nextTool` / `error.recovery` + doctor/probe)。本节为改动前设计记录。

### 当前问题

`serverInstructions` 只描述了 happy path：

```text
resolve -> describe -> invoke_plan -> invoke
```

但项目已经在 `app.Result.error` 里提供了机器可读恢复信息：

- `error.nextTool`
- `error.recovery`
- 按错误类型指向 `sofarpc_describe` / `sofarpc_probe` / `sofarpc_doctor` / `sofarpc_config_list`

入口指引没提这件事，Agent 第一次失败时仍可能从 prose 里猜下一步。

### 推荐实现

直接把初始化 instructions 补成类似下面的语义：

```text
Run sofarpc_resolve before sofarpc_invoke. When multiple servers exist, always pass `server`.
Use sofarpc_describe with query=... to find a service FQN before invoking, and
sofarpc_invoke_plan to validate arguments without sending a request.
On failure, read structuredContent.error.nextTool and error.recovery, then follow that tool.
Use sofarpc_doctor or sofarpc_probe to diagnose config/connectivity issues.
```

### 测试

- initialize response 的 `instructions` 包含 `nextTool` / `recovery`。
- 工具名和实际注册名一致。
- instructions 不包含任何本地路径、配置值或 attachments。

## P0: Invoke Schema / Contract 收敛

> ✅ **已在 Sprint 1 落地**:`invoke.go` 已删除 `types` / `args` 别名,保留 `required:["service","method"]`。本节为改动前设计记录:下面「当前问题」描述的别名是 Sprint 1 之前的状态。

### 当前问题

`InvokeArgs` 支持 `types` 和 `args` 作为别名，但 schema 只展示 `paramTypes` 和 `orderedArguments`。因为当前走 raw `Server.AddTool` + `decodeStrict`，别名可用，但 Agent 从 `tools/list` 看不到。

这会形成“隐藏契约”：

- 人和 Agent 看 schema，以为不能用 `types/args`。
- handler 又实际接受。
- 后续维护者可能误以为 schema 校验会挡住别名。

### 推荐决策

推荐删除别名，只保留：

- `paramTypes`
- `orderedArguments`
- `arguments`

也保留 `required: ["service", "method"]`。它对 host / LLM 填参有提示价值；真正的友好错误仍由 handler 负责，两者不冲突。需要在文档里说明：schema 是 host/LLM hint，业务校验和错误恢复仍走 `app.Result` envelope。

### 测试

- `types` / `args` 作为未知字段时返回友好 BAD_REQUEST。
- `paramTypes` / `orderedArguments` 仍正常工作。
- `required` 仍出现在 `tools/list` 的 input schema。
- 缺 `service` / `method` 时 handler 仍返回 `app.Result` 错误，而不是泄漏 protocol error。

## P1: Enhance Resolve / Describe Candidates

### 为什么这么做

Agent 常见困难不是“不知道有 `sofarpc_describe`”，而是无法稳定猜中：

- server 名称
- service FQN
- method 名
- overload 的 `paramTypes`

但不建议第一阶段新增 `suggest_server` / `suggest_service` / `suggest_method` 三个工具。当前已经有：

- `sofarpc_resolve`：负责确定 project/server/endpoint。
- `sofarpc_describe`：已有 `query` 搜索模式，能返回 `candidates`。

把候选能力并进现有工具，能降低 Agent 选择负担，避免从 11 个工具继续膨胀到 17 个工具。

### 推荐实现

#### 增强 `sofarpc_resolve`

当没有唯一 server 时，不只返回错误文本，而是在 `data.candidates` / `error.details.candidates` 里返回有序候选：

```json
{
  "candidates": [
    {
      "server": "user-test",
      "project": "user",
      "score": 0.92,
      "reason": "project/name matched"
    }
  ]
}
```

这覆盖原本 `suggest_server` 的用途。

#### 增强 `sofarpc_describe`

`query` 模式返回更适合 Agent 直接选择的信息：

```json
{
  "query": "get user",
  "candidates": [
    {
      "service": "com.example.UserService",
      "method": "getUser",
      "paramTypes": ["java.lang.String"],
      "parameterNames": ["userId"],
      "score": 0.95,
      "reason": "method name and service name matched"
    }
  ]
}
```

这覆盖原本 `suggest_service` / `suggest_method` 的用途。

### 测试

- resolve 多 server 时返回 ranked candidates，且 `nextTool` 仍指向 `sofarpc_resolve` 或适当恢复步骤。
- describe query 返回 service/method/paramTypes/parameterNames/score/reason。
- limit 有上限，输出有稳定排序。
- 不泄漏 attachments。
- out-of-prefix 默认不返回，除非显式打开。

## P1/P2: Session / Replay

### 为什么降级

`session/replay` 仍然有价值，但不适合第一阶段就做。它会引入服务端状态、TTL/LRU、重放安全、脱敏策略和更重的测试矩阵；相比之下，schema、instructions、别名收口、候选输出增强都是低状态、低风险、直接改善 Agent 行为的改动。

只有当前面这些改完，并在真实 Codex / Claude host 中观察到可判定的上下文丢失信号时，才启动 session/replay。建议先用下面的触发条件，避免“要不要做”长期靠手感：

- 同一条用户调用链里，Agent 重复传递相同 `service/method/paramTypes` 达到 3 次或更多，且重复不是用户显式要求。
- `sofarpc_invoke_plan` 已成功返回后，到 `sofarpc_invoke` 之间丢失 `server/project/service/method/paramTypes`，导致 Agent 重新调用 `sofarpc_describe` 或重新构造 plan。
- 失败结果已经给出 `error.nextTool` / `error.recovery`，但下一步工具调用缺少前一步已解析出的关键上下文，导致恢复链路中断。
- 在 10 条真实 host 调用链样本中，上述任一问题出现 2 次或更多。

如果短期拿不到足够真实样本，就把 session/replay 明确标为事后观察项，不进入实施排期。

### 可选设计 A：不新增工具

首选方案是不新增 `session_open/get/replay`。让现有 `sofarpc_invoke_plan` 返回一个短期 `planId`，然后 `sofarpc_invoke` 可选择接收 `planId`：

`sofarpc_invoke_plan` 返回：

```json
{
  "dryRun": true,
  "planId": "plan-...",
  "expiresAt": "2026-06-06T12:00:00Z",
  "plan": {
    "project": "user",
    "server": "user-test",
    "service": "com.example.UserService",
    "method": "getUser",
    "paramTypes": ["java.lang.String"]
  },
  "next": {
    "tool": "sofarpc_invoke",
    "arguments": {
      "planId": "plan-..."
    }
  }
}
```

`sofarpc_invoke` 可接收：

```json
{
  "planId": "plan-...",
  "arguments": {"userId": "u002"}
}
```

这样可以复用 plan 的 `server/project/service/method/paramTypes`，但不增加新工具。真实 invoke 仍然是原来的 destructive/open-world tool，host 审批语义不变。

如果将来落地 `planId`，`sofarpc_invoke` 的 input schema 必须如实暴露两种调用模式，不能让 `planId` 变成新的隐藏契约：

- 显式参数模式：`service` + `method` + `paramTypes` + `arguments/orderedArguments`
- planId 模式：`planId` + 可选 `arguments/orderedArguments/timeoutMs/rawResult`

可以用 schema 描述和 tool description 说明这两种模式；即使 JSON Schema 不使用复杂 `oneOf`，也必须让 `tools/list` 里的字段与 handler 接受的字段一致。

### 可选设计 B：最多新增 1 个工具

如果 `planId` 仍不够，可以新增一个 `sofarpc_replay`，但不再新增 `session_open` / `session_get`：

建议入参：

```json
{
  "planId": "plan-...",
  "mode": "plan",
  "patch": {
    "arguments": {
      "userId": "u002"
    },
    "timeoutMs": 8000
  }
}
```

`mode`：

- `plan`：只重新生成 `invoke_plan`，不触网。
- `invoke`：执行真实调用。必须继续标记 destructive/open-world。

建议返回：

```json
{
  "planId": "plan-...",
  "mode": "plan",
  "plan": {},
  "next": {
    "tool": "sofarpc_replay",
    "arguments": {
      "planId": "plan-...",
      "mode": "invoke"
    }
  }
}
```

只有当真实 host 里 `invoke(planId)` 的行为仍不够直观时，才考虑这个工具。

### 数据模型草案

```go
type PlanRef struct {
    ID        string
    CreatedAt time.Time
    ExpiresAt time.Time

    Project string
    Server string
    Service string
    Method string
    ParamTypes []string
    TimeoutMS int

    Arguments json.RawMessage
    OrderedArguments json.RawMessage

    LastPlan *InvocationPlanSnapshot
}
```

存储：

- 先用进程内 LRU + TTL。
- 默认 TTL：2h 或更短。
- 默认容量：128 或 256。
- 不持久化 attachments、原始 secret、完整 rawResult，除非显式允许。

### 测试

- `invoke_plan` 成功后可返回 `planId` 和 `expiresAt`。
- `invoke(planId)` 复用 plan 上的 server/project/service/method/paramTypes。
- `invoke(planId, patch)` 只覆盖指定字段，不改变 `service/method/paramTypes`，除非显式允许。
- `planId` 过期返回友好错误，提示重新调用 `sofarpc_invoke_plan`。
- 如果新增 `sofarpc_replay`，`mode=plan` 不触网，`mode=invoke` 保留 destructive/open-world annotation。
- 不泄漏 attachments。
- Java long 参数通过 `planId` / replay 后仍保持 `json.Number` 精度。

## P1: Prompt Template

### 为什么要做

Server instructions 是全局提示，适合短规则；prompt template 是用户可选择的工作流入口，适合把“如何完成一次 SOFARPC 调用”写成结构化流程。官方 prompts 是 user-controlled，客户端可以把它作为 slash command 或模板展示。

### 推荐 prompt

名称：

```text
sofarpc.invoke_workflow
```

参数：

```json
[
  {"name": "intent", "required": true, "description": "用户想调用或验证的业务意图"},
  {"name": "server", "required": false},
  {"name": "project", "required": false},
  {"name": "serviceQuery", "required": false},
  {"name": "service", "required": false},
  {"name": "method", "required": false}
]
```

模板内容草案：

```text
你要帮助用户完成一次 SOFARPC 泛化调用。

用户意图: {{intent}}

流程:
1. 先调用 sofarpc_resolve，确认 project/server/endpoint。多 server 时必须让 server 明确。
2. 如果 service 不明确，调用 sofarpc_describe query={{serviceQuery 或 intent}} 查候选。
3. 如果 method、paramTypes 或 DTO 字段不明确，调用 sofarpc_describe service=<FQN> method=<method?>。
4. 调用 sofarpc_invoke_plan 校验 service/method/paramTypes/arguments，不触网。
5. 只有 plan 成功且用户需要真实调用时，调用 sofarpc_invoke。
6. 失败时优先读取 structuredContent.error.nextTool 和 recovery，按提示调用恢复工具。
7. probe 成功只代表 TCP 路径可达，不代表 service/method 存在。
```

### 测试

- `prompts/list` 能列出 prompt。
- `prompts/get` 带参数后返回完整 workflow message。
- prompt 不包含 config secrets。
- prompt 中工具名和实际工具注册名一致。

## P2: Safe Resources

### 原则

不要暴露原始 config。`attachments` 可能带凭据，绝不作为 resource 暴露。

可以暴露安全、只读、脱敏、对 Agent 有帮助的上下文。

### 候选资源

#### `sofarpc://compatibility`

返回支持的 Java/Hessian 类型矩阵摘要。

#### `sofarpc://projects`

返回项目/server 的脱敏摘要：

```json
{
  "projects": [{"name": "user", "servicePrefixes": ["com.example."]}],
  "servers": [{"name": "user-test", "project": "user", "protocol": "bolt"}]
}
```

不返回：

- attachments
- token
- password
- 完整 local path，除非明确认为可展示

#### `sofarpc://schema/{project}/{service}`

返回 cached schema description。需要注意：

- 仅服务本地已配置 project。
- 默认只返回 service/method/paramTypes/DTO 字段。
- 不返回源码内容。

### 风险

Resources 会扩大 Agent 可见上下文，必须先完成红线测试：

- attachment sentinel 不泄漏。
- 本地绝对路径是否展示有明确策略。
- large schema 有大小限制和分页/摘要。

## P2: Safety Annotations / Write Defaults

### 当前状态

- `sofarpc_invoke`：destructive/open-world，合理。
- `sofarpc_probe`：read-only/idempotent/open-world，合理，因为它可以拨显式地址。
- remove tools：destructive，合理。
- config save tools：当前不是 destructive，因为只是本地 config 写入。

### 推荐优化

1. 把 `--disable-config-write` 做成安装/host 配置中的显眼选项。
2. MCPB 或默认 host 注册可以考虑 read-only 默认，用户显式 opt-in 后启用写配置工具。
3. 给 config save 增加 preview/dry-run：

```json
{
  "name": "user-test",
  "address": "127.0.0.1:12200",
  "project": "user",
  "dryRun": true
}
```

4. 对 `attachments` 做额外文档警示：即使 values 被脱敏，写入本地 config 仍可能保存凭据。

## P2: Large Result Text Mirror Policy

### 当前问题

`manualResult` 会把同一份 JSON 同时放进：

- `structuredContent`
- `content[0].text`

这符合 MCP Tools 的兼容性建议：返回 structured content 的工具也应该在 TextContent 里提供序列化 JSON，方便只读 `content` 的 client。

但 `sofarpc_invoke` 可能返回较大的 Java 对象树。大结果无条件双发，会让 wire bytes 和部分 host 的上下文占用接近翻倍。

### 推荐策略

不要直接删除 text mirror。更稳妥的是做阈值策略：

- 小结果继续完整双发，保持最大兼容性。
- 大结果，例如超过 8KB 或 16KB，`content[0].text` 只放摘要和提示。
- 完整结构仍保留在 `structuredContent`。
- 首期可以只对 `rawResult=true` 或超大 invoke 结果启用，降低兼容风险。

示例 text：

```json
{
  "ok": true,
  "code": "SUCCESS",
  "summary": "Large structured result omitted from text mirror; read structuredContent for full JSON.",
  "structuredContentBytes": 52341
}
```

### 测试

- 小结果仍完整镜像。
- 大结果 text mirror 降级，但 `structuredContent` 完整。
- 失败结果不要被截断到丢失 `error.nextTool` / `error.recovery`。
- 在 Codex / Claude host 至少实测一次，确认只读 `content` 的体验可以接受。

## P2: Progress Granularity

### 当前问题

当前 progress 足够合规，但对长耗时步骤的“卡住感”改善有限。

### 建议阶段

`sofarpc_describe`：

- `0.0`: building source index
- `0.5`: source index ready
- `0.8`: searching/describing
- `1.0`: done

`sofarpc_invoke`：

- `0.0`: resolving plan
- `0.25`: plan resolved
- `0.5`: invoking remote method
- `0.8`: decoding response
- `1.0`: done

注意：

- 只有 client 提供 progress token 才发送。
- 不要高频刷 progress。
- 出错时最后一个 tool result 已足够，不一定强行发 `1.0`。

## 建议实施顺序

### Sprint 1: Contract Cleanup（新增 0 个工具）

1. 给每个工具拆出真实 `data` output schema；`resolve` / `describe` 预留 candidates/score/reason，`invoke.data.result` 保持开放对象。
2. 增加 structured output schema validation fixtures。
3. 补 `serverInstructions` 的失败恢复路径。
4. 删除 `types` / `args` 隐藏别名，保留 `required: ["service", "method"]`。
5. 更新 README 和 tool descriptions，说明 schema 是 host/LLM hint，友好错误仍由 handler envelope 返回。

### Sprint 2: Existing Tool Candidate Enhancements（新增 0 个工具）

1. 增强 `sofarpc_resolve`：多 server 时返回 ranked candidates。
2. 增强 `sofarpc_describe query`：返回 service/method/paramTypes/parameterNames/score/reason。
3. 补候选排序、limit、脱敏和 out-of-prefix 测试。
4. 在真实 Codex / Claude host 中按 Session/Replay 触发条件记录 10 条调用链，判断是否仍需要 session/replay。

### Sprint 3: Prompt Template（不新增工具）

1. 开启 prompts capability。
2. 实现 `sofarpc.invoke_workflow`。
3. 加 `prompts/list` / `prompts/get` 测试。
4. 在 README 中说明 prompt 是用户选择的 workflow，不是自动执行。

### Sprint 4: Optional State / Safe Resources / Safety Defaults / Progress

1. 若真实 host 仍明显丢上下文，再设计 session/replay；默认仍不新增。
2. 评估并实现 `compatibility` resource。
3. 再考虑 `projects` 和 `schema` resource。
4. 调整安装文档，突出 read-only 模式。
5. 对大结果 text mirror 做阈值降级。
6. 增强 progress 阶段。

## 成功标准

- Agent 能从模糊意图完成一次 plan，不需要新增 `suggest_*` 工具。
- 失败结果稳定给出 `nextTool`，instructions 明确要求跟随 `nextTool` / `recovery`。
- `tools/list` 中没有隐藏契约：input schema 展示的字段和 handler 接受字段一致，output schema 描述每个工具真实的 `data` 结构。
- 真实 invoke 的安全注解清晰，config 写入有明确 opt-in 或 preview。
- 敏感 attachments 在 tools/resources/prompts 中均不泄漏。

## 不建议现在做

- 不建议引入 Sampling：项目不需要服务器反向调用 LLM。
- 不建议用 elicitation 收集凭据：敏感信息不应通过 elicitation 请求。
- 不建议把 config 作为 resource 暴露。
- 不建议把业务失败改回 protocol error；`isError + app.Result` 更适合 Agent recovery。
