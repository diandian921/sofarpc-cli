# MCP Spec Compliance Review (sofarpc-mcp)

Date: 2026-05-28
Scope: `internal/mcp/*`, `cmd/sofarpc/main.go`, `internal/cli/mcp.go`
Reference specs: MCP `2024-11-05`, `2025-03-26`, `2025-06-18`, `2025-11-25`

本文按 P0/P1/P2 分级对照 MCP 官方规范审视当前 stdio MCP server 实现，给出可落地的修复方向。
最后一节由 codex 二审后形成最终结论。

---

## P0 — 合规硬伤（先修）

### 1. `initialize` 协议版本「直接 echo client 值」违反规范
- 现状：`server.go:189-198` 把 client 传的 `protocolVersion` 原样回传，连 `1.0.0` / `9999-99-99` 都会照单全收
- 规范（2025-11-25 lifecycle）：server **必须返回自己支持的版本**；不支持 client 提议时，返 `-32602` + `data.{supported, requested}`
- 反向加成的测试：`TestInitializeEchoesClientProtocolVersion`（server_test.go:198）把这个错误行为**锁死成 contract**——属于「test 把 bug 钉死」反模式
- 修复：
  ```go
  var supportedVersions = []string{"2025-11-25","2025-06-18","2025-03-26","2024-11-05"}
  // 命中：返该版本；未命中：返最新 + JSON-RPC -32602 error
  ```
  测试改名 `Negotiates*`，覆盖三种 case（命中 / 降级 / 不支持）。

### 2. 6 个 tool 全部没有 `annotations`
2025-03-26 引入的 `ToolAnnotations` 是 host 做风险分级（auto-approve / 二次确认 / 红字警告）的唯一依据。
当前 host 无法区分 `sofarpc_resolve`（纯只读）和 `sofarpc_config remove_project`（删配置）。

建议矩阵：

| Tool | readOnlyHint | destructiveHint | idempotentHint | openWorldHint |
|----|----|----|----|----|
| sofarpc_resolve | true | — | true | false |
| sofarpc_describe | true | — | true | false |
| sofarpc_doctor | true | — | true | false |
| sofarpc_probe | true | — | true | true |
| sofarpc_invoke (dryRun=true) | true | — | true | true |
| sofarpc_invoke (dryRun=false) | false | true | false | true |
| sofarpc_config (list) | true | — | true | false |
| sofarpc_config (save_*) | false | false | false | false |
| sofarpc_config (remove_*) | false | true | false | false |

`sofarpc_invoke` 的 dryRun 双态用单工具 annotation 表达不了——见 P1#8 拆 tool 建议。
`sofarpc_config` 的 5 action 同样——见 P1#6 拆 tool 建议。

### 3. `DisableConfigWrite=true` 时仍在 `tools/list` 暴露写工具
后果：agent 学到 5 个 action 存在，反复试 `save_project`，每次都吃 `config write tools are disabled`。
规范允许动态裁剪 `tools/list`——禁用时直接去掉写 action 是更友好的契约。

---

## P1 — 契约一致性 / 工具面设计

### 4. 错误返回有两种形状（最痛的内部不一致）
- `toolErr`（jsonrpc.go:55）：`{ok, message, error, code?, configPath?}` —— 平面 ad-hoc
- `toolErrRendered`（jsonrpc.go:43）：完整的 `app.Result`（带 `error.kind`、`error.nextTool`）

只有 `sofarpc_invoke` plan 失败走第二条，agent 才能拿到 `nextTool`。
其他所有 bad-arguments / config 错误都是第一条——agent 看到不同字段名，prompt 兼容不了。

**统一回 `app.Result` 形状**，并把 `toolErr` 改成 `toolErrRendered` 的薄包装。

### 5. 缺 `outputSchema`（2025-06-18 引入）
6 个 tool 都没有。后果：
- 强类型 host（python-sdk / ts-sdk）严格校验模式下会忽视 `structuredContent`
- agent 只能从 description 反推字段路径，token 浪费且不稳定
- 你给 resolve/invoke/doctor 的丰富结构化输出（plan / diagnostics / checks）等于半个 schema 浪费

至少给 `sofarpc_resolve`、`sofarpc_invoke`、`sofarpc_doctor` 三个高频结构化工具补 outputSchema。

### 6. `sofarpc_config` 是肥工具（5 action 塞 1 个 name）
- annotations 只能取最宽（=都标 destructive）
- JSON Schema 表达不了「`action=remove_project` ⇒ `confirm` required」
- agent 选错 action 概率高（README 里专门写「remove 必须 confirm=true」就是 workaround）

拆成 `sofarpc_config_list / save_project / save_server / remove_project / remove_server` 是 MCP idiomatic 做法（每个 tool 一个语义、一组 annotations、一份完整 inputSchema）。

### 7. `sofarpc_invoke` 同时暴露 `paramTypes|types`、`orderedArguments|args` 两套别名
schema 里 4 个字段，agent 永远在猜该用哪个；description 又没说明优先级。
规范不禁止但浪费 token + 增加歧义。
保留主名，alias 改成 server-side 静默兼容（不写进 schema）。

### 8. 把 `sofarpc_invoke` 的 dryRun 单独拆成 `sofarpc_invoke_plan`
当前 invoke 同时是「只读规划」+「真实远程调用」，annotation 没法表达。
拆出 `sofarpc_invoke_plan`（readOnlyHint=true）后：
- agent 可以无副作用反复试 schema 解析
- host 可以 auto-approve plan，但拦截真正的 invoke

### 9. `initialize` 缺 `instructions` 字段
2025-06-18 加的 server-level 指引，最适合放：
> "Run `sofarpc_resolve` before `sofarpc_invoke`. When multiple servers exist, always pass `server`. Use `sofarpc_describe query=...` to find a service FQN before invoking."

现在这些只能塞每个 tool description 里——重复、稀释 prompt 信号。

### 10. `-32700` parse error 用错
`session.go:44` 把 `errJSONRPCLineTooLong` 也归到 `-32700`。
规范上 -32700 **仅限 JSON 文本解析失败**，line-too-long 更准确是 `-32600 Invalid Request`。

---

## P2 — 体验 / 可观测 / 安全

### 11. 完全没用 `logging` capability
当前诊断要么塞 toolResult 要么写 stderr（host 看不见 stderr）。
声明 `capabilities.logging` 后可发 `notifications/message`，Claude Code/Desktop 调试面板可见。
invoke 慢、describe 索引重建场景特别需要。

### 12. 长任务没有 `notifications/progress`
`sofarpc_invoke` 真调用可能跑 20s，agent 干等无信号。
stdio MCP 支持 progress，可以在「TCP 已连接 / Hessian 解码中」节点发。

### 13. `serverInfo` 缺 `title` / `description` / `icons`
2025-06-18 引入。host UI 里现在只能看到 `sofarpc-mcp` 程序员名。

### 14. `_meta` 字段完全没用
现在把 `requestId` 塞业务 data 里。
规范专门留 `_meta` 给 trace_id / elapsed_ms / debug_flag。
挪到 `_meta` 后业务 data 更干净，host 可在 UI 上对应。

### 15. `describe` 没走异步
第一次调用会触发 `LoadOrBuildIndex` 扫整个 workspace，大仓秒级，同步阻塞会卡住并发的 tools/list / tools/call。
`shouldRunAsync` 只放行了 invoke/probe。

### 16. `panic` 详情未脱敏直接进 `error.message`
`jsonrpc.go:27` 用 `fmt.Sprintf("internal error: %v", recovered)`——stack / 路径 / 敏感串都可能泄到 agent，agent 又会回给用户。
建议 message 固定 `"internal error"`，详情只写 stderr + 在 `_meta.errorId` 给查询 id。

### 17. `maxJSONRPCLineBytes = 128MB` 太大
stdio 单帧 8–16MB 远够用。128MB 给了失控 agent 一次性吃 RAM 的机会。

### 18. `numberSchema` 名字叫 number 但 `type:"integer"`
其他维护者看到 `numberSchema(...)` 会以为浮点。改名 `integerSchema`。

### 19. `enumStringSchema` 不带 `default`
`config.action` 默认 `list` 但 schema 里没声明。加 `"default": "list"`。

### 20. 可以把 `~/.sofarpc/config.json` 暴露为 MCP `resource`
声明 `capabilities.resources` 后，agent 可以 `resources/read` 直接拿原文，不必绕 `sofarpc_config action=list`。
同理「常用调用模板」可以做成 `prompts`。规范级别的低成本扩展。

### 21. `sofarpc_invoke` 没标 `openWorldHint`
调任意 IP:Port = 等价于内网横向；annotation 不标，host 没法在第一次 invoke 时给二次确认。

---

## 工程 / 测试

### 22. 缺 contract test
官方 JSON Schema 在 `/modelcontextprotocol/modelcontextprotocol`。CI 里用 ajv 跑一遍 tools/list 输出做契约校验，能挡住 P0/P1 大半。

### 23. `selftest` 覆盖太薄
只跑 initialize + tools/list。加一条 `tools/call sofarpc_config action=list` 才能在 config 路径炸时本地就 fail，不用等 agent。

### 24. README/CLAUDE.md 没说「协议合规等级」
应当声明支持的 MCP 协议版本列表 + 已知不支持的能力（roots、sampling、resources、prompts），避免 host 误开能力。

---

## 推荐落地顺序

| 顺序 | 改动 | 收益 | 工作量 |
|----|----|----|----|
| 1 | 修协议版本协商 + 改测试名 | 合规、解钉子 | 0.5 天 |
| 2 | 6 个 tool 加 annotations | host 安全分级生效 | 0.5 天 |
| 3 | 错误契约统一为 `app.Result` | `nextTool` 全路径生效 | 1 天 |
| 4 | 拆 `sofarpc_config` 5 action 为独立 tool | agent 选择准确率↑ | 1 天 |
| 5 | 加 `outputSchema` + `instructions` + `serverInfo.title` | host UI / agent prompt 质量↑ | 1 天 |
| 6 | logging capability + describe 异步 + progress | 调试体验 | 1.5 天 |
| 7 | resources 暴露 config.json + prompts 模板 | 工具面扩展 | 1 天 |

最大杠杆是 1–4 这四项——属于「用相同代码量、规范红利一次性收割」。

---

## 最终结论（与 codex 两轮对审后）

经 codex 两轮独立对审（findings 逐条 + 架构蓝图），最终判定如下。

### 一、对 24 条 findings 的处置

**采纳并调整（codex 指出）**
- N1 协议版本：fix 改为「优先返回 server 支持的版本（可降级匹配），`-32602` 仅作完全不匹配时的回退」
- N2 annotations：P0 → **P1**（spec 里是 OPTIONAL hint，非硬合规失败；但对 host auto-approve 影响大，置 P1 顶部）
- N3 禁用写工具：fix 改为「裁 enum / 拆 tool」，不隐藏整个 tool
- N9 instructions：P1 → **P2**（prompt 卫生，非契约）
- N12 progress：补前置——必须先解析 `_meta.progressToken` 才能发，否则白发
- N14 _meta：caveat——`requestId` 已是 `app.Result` 对外契约，挪动需先确认 CLI 消费方（breaking）
- N15 describe 同步：P2 → **P1**（阻塞整个 read loop，连 cancel 通知都收不到，重一个量级）

**推翻（codex 反对，已采纳）**
- N20 config.json as resource：**删除**——config 含 attachments（可能含凭证），暴露为 resource 是泄密
- N21 invoke 缺 openWorldHint：**删单条**——openWorldHint 默认即 true，并入 N2 annotations 矩阵

**维持原判**：N4–N8、N10、N16、N18、N19、N22、N23（codex AGREE）；N11/N13/N17/N24 维持 P2。

### 二、codex 补漏的 10 条（原审完全漏掉）

| 新增 | 定级 | 要害 |
|----|----|----|
| lifecycle gating 缺失（initialize 握手前 tools/list 就响应） | **P0** | 真协议违反 |
| `dryRun:"true"` 字符串静默变 false | **P0 安全级** | 本想 dry run → 变真打 RPC，事故级 |
| JSON-RPC 校验太松（jsonrpc 版本 / 空 method / error.data 缺） | P1 | 健壮性 |
| cancel 后仍发最终响应（spec SHOULD NOT） | P1 | 又一处 test 钉死 bug |
| 重复 request ID 覆盖 running map | P1 | 并发正确性 |
| outMu 长 write 阻塞所有响应 | P2 | |
| panic 详情泄 client 且无 stderr 留底 | P2 | |
| client capabilities 被忽略 | P2 | |
| stderr read error 泄本地路径 | P2 | |

### 三、最终优先级（实施序）

```
P0  1. initialize 版本协商 + lifecycle gating
P0  2. dryRun 严格 bool + JSON-RPC 严格校验
P1  3. cancellation 语义（cancel 后不发响应）+ dup ID
P1  4. tool annotations 矩阵
P1  5. 错误契约统一 app.Result
P1  6. 拆 config 5 tool + 拆 invoke_plan
P1  7. describe 异步 + progressToken 解析后发 progress
P2  8. logging + serverInfo.title + instructions + panic/stderr 脱敏
P2  9. 杂项（命名 / default / maxLine / outMu writer 化）
P2 10. contract test + selftest 扩展 + README 合规等级
删    原 N20（config as resource）、原 N21 单条
```

### 四、架构与实施路径（codex 第二轮蓝图审）

见 `mcp-architecture-blueprint.md` §16。**历史审查记录**：codex 两轮均判三层分包对 6 工具属过度设计、推荐单包路径 B。**最终裁决：走路径 A（三层分包）** —— 取长期架构整洁，接受多花 ~2 天 + 更高弃坑风险；invoke 直接删；重构期冻结 app 接口 + 加跨 render contract test；落地守「< 1 周否则回退」。
