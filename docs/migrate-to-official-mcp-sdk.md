# 迁移 MCP 协议层到官方 modelcontextprotocol/go-sdk(渐进式原生重写)

> 状态:方案已定稿(待实施) · 日期:2026-06-04 · 已过 codex 审阅
>
> 本文是迁移方案文档,记录决策、范围、步骤与风险。实施尚未开始。

## Context

当前 `internal/mcp` 自研了整个 MCP 协议栈(JSON-RPC、生命周期、取消、进度、日志)和一套 tool 框架(`Tool[A]`/`ToolSpec`/`Registry`)。目标是把协议层换成官方 `modelcontextprotocol/go-sdk`,以后不再自己跟 MCP 规范演进。已选定**原生重写**:tool 直接按 SDK 的泛型 `mcp.AddTool` 重写,最终删掉自研框架。经 codex 审阅后,删除改为**渐进式收尾**(先接入、再逐个迁、最后删),而非一次性删光。

**代价(已确认接受)**:
- **Go 1.19 → 1.25**(项目选定基线)。codex 建议只需 1.23(SDK v1.2.0 的 go directive 即 1.23),但**已决定取 1.25**;后续若想扩大可装范围可回调到 1.23。
- **新增依赖树**:`jsonschema-go`、`segmentio/encoding`、`golang-jwt`、`go-cmp`、`uritemplate`、`golang.org/x/{oauth2,time,tools}`。
- **交出协议行为控制权**:版本协商、取消语义、未知 tool 错误码/文案、帧上限、错误脱敏均由 SDK 决定(见下方行为变更)。
- **逐步淘汰 ~1500 行经多轮 review 的 proto/framework 代码与测试**。

换来:不再自维护协议正确性、免费跟随规范升级、与 `sofarpc-cli` 选型对齐、未来可低成本用 resources/sampling/elicitation。

## Scope

**不动**:`internal/app`(全部业务逻辑)、`internal/direct`(wire 编解码 + 双 oracle)、`internal/schema`、`internal/appconfig` 等。

**改动边界已核实**:`internal/mcp/proto` 与 `internal/mcp/server` 仅被 `internal/mcp` 自身引用;公共门面 `internal/mcp.Server` 只被 `internal/cli/mcp.go` 和 `internal/perf/performance_test.go` 引用。CI(`.github/workflows/ci.yml`)用 `go-version-file: go.mod`,go.mod 一改即生效。

## 实施步骤(渐进式 / strangler,顺序很重要)

### 1. 引入依赖
- `go get github.com/modelcontextprotocol/go-sdk@v1.2.0`(显式 pin,不用 `@latest`);手动设 `go` directive `1.25.0`(高于依赖要求的 1.23 是允许的);`go mod tidy`。

### 2. SDK server 接到现有公共 API 背后(不删任何东西)
- 在 `internal/mcp` 新增基于 SDK 的实现,`Server.Run()`/`SelfTest()` 切到它,**保持 `mcp.Server` 公共字段/方法不变**。
- `mcp.NewServer(&mcp.Implementation{Name:"sofarpc",Version:BuildVersion}, &mcp.ServerOptions{Instructions: serverInstructions})`。
- `Run()`:`srv.Run(ctx, &mcp.StdioTransport{})`(`StdioTransport` 是空结构体、固定用进程 stdio)。**注意**:这会和 `Server.Stdin/Stdout/Stderr` 注入契约冲突——真实运行走进程 stdio;注入流仅测试用,测试一律走 `NewInMemoryTransports`(不要指望 `StdioTransport` 包装自定义流)。
- **强制约束**:迁移 tool 一律用**顶层泛型 `mcp.AddTool`**,禁止 `srv.AddTool`——只有泛型版才在 `Content==nil` 时自动塞「JSON 文本块 + `StructuredContent`」,wire 兼容依赖它。

### 3. 先迁一个只读 tool(probe)+ 立刻补兼容性测试
- 用 probe 跑通形态:
```go
mcp.AddTool(srv, &mcp.Tool{
    Name: "sofarpc_probe", Title: "...", Description: "...",
    Annotations: &mcp.ToolAnnotations{/* 按 v1.2.0 实际字段类型填:部分 bool 部分 *bool,勿无脑 ptr(true) */},
    InputSchema:  toSchema(probeInputSchema),    // 见下「schema」
    OutputSchema: toSchema(resultOutputSchema),
}, func(ctx context.Context, req *mcp.CallToolRequest, a ProbeArgs) (*mcp.CallToolResult, app.Result, error) {
    r := appSvc.ProbeEndpoint(ctx, app.ProbeInput{...})
    res := app.RenderProbe(r); res.RequestID = app.NewRequestID("ping")
    return finish(res, "Probe completed..."), res, nil
})
```
- `finish()`(取代 `server/result.go` 的 `wrapResult`):`CallToolResult.Meta = {elapsedMs, summary, ...res.Meta}`、**Content 留 nil**、返回 `Out=app.Result`、`IsError=!res.OK`。
- **schema**:`toSchema` 把现有 `json.RawMessage` 字面量转成 SDK 接受的形态,**保真**(`additionalProperties:false`+描述不变)。v1.2.0 里 `Tool.InputSchema/OutputSchema` 的确切类型(`*jsonschema.Schema` 还是可直接吃 raw)**编译期核实**,取改动最小且不丢字面量的那条。
- **补齐对照测试(在删旧框架之前)**:用 in-memory client(server 先 Connect,再 client Connect,`ListTools`/`CallTool`)+ 一条**原始 wire 断言**(logging transport 或裸 JSON,typed client 会掩盖 `_meta`/content 回归),钉死:
  - `tools/list` 含 probe 且带 outputSchema
  - `tools/call` 结果形状 `{content:[{type:text,text:<结构化JSON>}], structuredContent:{...}, "_meta":{elapsedMs,summary,...}, isError}`
  - panic→脱敏(见步骤 5)
  - 取消、未知 tool 错误、版本协商的**迁移前/后快照**(见行为变更)

### 4. 迁移其余 10 个 tool
- 同形态逐个迁(`describe` 的进度:`req.Session.NotifyProgress(...)` 用 `req.Params.GetProgressToken()` 守卫;仅此一个用进度,无 tool 用 Log)。去掉 `server.Runtime` 入参。
- `DisableConfigWrite` 时跳过 4 个 config-write 工具,逻辑同现状。

### 5. panic 脱敏(随每个 handler 落地)
- 每个 handler 包 `defer recover()`,把 panic 折成固定 `errorId` 的脱敏错误,细节进 stderr——复刻现状(现 `server_test.go:243` 断言 panic→`"internal error"`)。**迁移版 panic 测试要先于删旧测试写好**。

### 6. OutputSchema 校验不要误伤
- 泛型 `mcp.AddTool` 会拿 `OutputSchema` 校验 `Out`。现 `resultOutputSchema`(`tools/render.go`)把 `data` 限定为 object;若有 tool 返回数组/字符串/数字/null 的 `data` 会被 SDK 判失败。**先审计各 tool 的 `data` 实际类型**:放宽 schema 允许任意 JSON,或对这些 tool 改用 `Server.AddTool` 手动填以绕过校验。

### 7. 删除自研层(全部迁完、测试全绿后)
- 删 `internal/mcp/proto/`(8 文件)、`internal/mcp/server/`(tool/registry/decode/result/runtime)、`internal/mcp/jsonrpc.go`、`internal/mcp/session.go`;删 `proto/*_test.go`、`server/*_test.go`。
- `internal/perf/performance_test.go`:确认仍可编译,必要时改走 client。

### 8. 文档
- `README.md` + `README.zh-CN.md`:去掉「自研 MCP / 零 MCP SDK / Go 1.19」,改为「官方 go-sdk + Go 1.25」。复核 `docs/single-binary-install-target.md`、`docs/pure-go-runtime.md`。

## 行为变更(SDK 接管后会变,需显式确认并测试)
- **协议版本**:现自研协商 4 档(`2025-11-25…2024-11-05`),旧测试断言未知版本回退 `2025-11-25`;SDK 自决,可能不同。抓快照、确认 host(Claude Code)能正常 initialize。
- **取消语义**:现测试期望取消的 invoke 不发最终响应;SDK 客户端取消返回 `context canceled`。定下新契约并测试。
- **未知 tool 错误**:现 `registry.go:70` 回 `-32602` + `"unknown tool: name"`;SDK 用自己的 `jsonrpc.Error` 文案/码。若有 client 依赖,包一层或至少测新形状。

## 验证
1. Go 1.25 下 `go build ./...`、`go vet ./...`。
2. `go test ./...`(`internal/direct` 双 oracle 未动应照常过)。
3. `sofarpc mcp --selftest` 输出 `ok`。
4. **实时 MCP 冒烟**:用在跑的 `mcp__sofarpc__*` —— `config_list`、`resolve`、`describe`、一次 `invoke_plan` dry-run —— 抓迁移前/后响应对比,确认 `{ok, code, data, error.nextTool, _meta.summary, structuredContent, isError}` 形状一致。
5. `scripts/oracle-gate.sh` 兜底。

## Codex 审阅(v1.2.0 源码+文档,只读)
- **已采纳**:渐进式删除顺序、强制泛型 `mcp.AddTool`、OutputSchema 误伤风险、`ToolAnnotations` 字段类型不一、panic recover 包裹+先补测试、取消/版本/错误码行为变更需快照、SelfTest 加原始 wire 断言、显式 pin `@v1.2.0`、`_meta` 加精确断言。
- **未采纳(决策优先)**:Go 版本回 1.23——保留 1.25。
- **编译期核实**:`Tool.InputSchema/OutputSchema` 的确切接受类型。
