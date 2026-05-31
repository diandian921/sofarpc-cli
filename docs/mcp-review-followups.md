# MCP 三层重构 —— 第二轮外部 review 跟进

Date: 2026-05-31
Branch: `refactor/mcp-three-layer`
Status: **已实现(2026-05-31),`go test ./...` 全绿(14 包)+ arch 边界保持,待 codex 复审**

来源:三层重构 + 安装本地 build 后的第二轮外部 review(P0–P3),逐条对着代码核过;另经一轮精修(范围/做法/措辞收紧);第三轮 review(structuredContent 措辞、view.go 收口 + sentinel 全扫、SelfTest handle 注释、per-tool schema 压 backlog、DisableConfigWrite 测试已存在、旧计划文档加指针)亦逐条对代码核过后折入。**代码一行未动。**

---

## P0 — attachments 泄给模型(出口级脱敏)

**问题**:多个工具把 server 的 `attachments` **值**(token / SSO / divisionId / tenant / trace)明文塞进 `structuredContent` → 进模型上下文 → 出网 / 回显 / 落 transcript。与蓝图 §16 N20「拒绝把 config 暴露成 resource,因含凭证」自相矛盾。**非本次重构引入(老代码同样),重构顺手收口。**

**已知泄露点(5 处;实现前再做全量 grep 防漏)**:
| 工具 | 出口 | 载体 |
|---|---|---|
| `sofarpc_resolve` | `endpoint`(单)+ `servers[].server`(多) | app.Endpoint / appconfig.Server |
| `sofarpc_config_list` | `servers[].server` | appconfig.Server |
| `sofarpc_config_save_server` | `server`(返回 `saved`,config.go:189) | appconfig.Server |
| `sofarpc_doctor` | server check 的 `endpoint`(helpers.go:126) | endpointData map |
| `sofarpc_invoke_plan` | `plan.endpoint`(plan.Display) | app.Endpoint |

不泄露(已核):`config_save_project`(Project 无 attachments)、`remove_*`(只回 `{removed:name}`)、`sofarpc_invoke` 真调用(输出 `{result,elapsedMs,diagnostics}`,不含)。

**方案(已定:出口级 public view,非散装 redact)**:
散装 `redactAttachments` 容易漏套。改为 tools 层建一组 **public-view 构造器**,所有 MCP structured output 统一走它:
- `publicServer(appconfig.Server) map` —— attachments 值 → `[redacted]`,key 保留
- `publicEndpoint(app.Endpoint) map` —— 同上
- `publicServers([...]) []map`
- `publicPlanDisplay(app.InvocationPlan) map` —— = `plan.Display()` 但 endpoint 走 `publicEndpoint`

上面 5 个出口全部改走 public view。粒度=**留 key、值打 `[redacted]`**(agent 知道有哪些 attachment,拿不到值)。

**收口位置(三审补:脱敏不能只靠约定)**:`success()` 收 `interface{}` 直接 marshal(`render.go:48`),任何工具都能绕过 public view 直塞 `app.Endpoint` / `appconfig.Server`。故 view 构造器统一放 `internal/mcp/tools/view.go`,并立约定:**凡 server/endpoint/plan 出口只许从 view helper 出**。不引新抽象层,就是一个文件 + 一条规矩 + 下面的全扫测试当网。

**红线(不变)**:
- 只在 **tools 层** 建 view,**不动 app / CLI / 执行路径**(CLI `server list` 是操作者本地终端看自己配置,不过模型,照常明文)。
- view **返回新 map,绝不原地改** `plan.Endpoint.Attachments` / config map。
- 执行路径独立:真调用 `ExecuteInvocation → directRequestFromPlan → copyStringMap(plan.Endpoint.Attachments)` 读 config 真值发 BOLT;attachments 也不是 agent 能传的 invoke 入参,恒从 config 读 → 脱敏与调用成功彻底解耦,**发给 RPC server 的是原值**。

**落地注意(critical pass 补)**:
- **动手前先 grep 全量**:`rg 'attachments|Attachments'` + 所有 server/endpoint 序列化点,确认 5 处是全集(invoke 真调用 data 已核不含;probe diagnostics / 各处 meta 尚属**推断**,需实地确认),别只认 reviewer 列的。
- **resolve 多 server 分支落地选择(已被 codex 一审推翻,最终选 B)**:`resolved.Servers` 是 **app `boundServers` 预建的 `[]map{"name","server":Server}`**。初版按本注「tools 层自己从 config 重列(A)」实现,但 **codex 一审指出 A 是正确性 bug**:`app.Service.Store` 可注入(`fakeStore`),`appSvc.Resolve()` 读注入 store,而 tools 的 `loadConfig()` 读全局默认路径 → 注入场景下 resolve 返回空/陈旧 server 或报错。**改为 B:直接 `publicServers(resolved.Servers)` 原地脱敏**,与 app 解析的同一 store 一致,且省掉双读。`publicServers` 对非 `appconfig.Server` 的 `"server"` 值直接丢弃(不透传),配 `TestResolveMultiServerUsesInjectedStore` 钉死。
- **露 key 藏 value 是有意识取舍**:`{"_sofa_token":"[redacted]"}` —— key 名仍暴露(会泄鉴权模型/租户维度)。判定可接受(agent 需知配了什么),但记在案。
- **save_server 是 5 个里最弱的**:attachments 是 agent 同轮自己传的入参,回显脱敏对该轮无保护意义;仍做(一致性 + transcript 卫生),但不是风险点。真正泄露是 config_list/resolve/doctor 读**早先/别人**配的。
- **回归测试(必带,两层)**:
  - **逐出口**:对 5 个出口各写测试,断言 MCP 输出 **不含 attachment value、含 attachment key + `[redacted]`**(正反都钉:key 留、value 没)。
  - **全扫网(三审补)**:再加一条 sentinel 测试——用含已知哨兵值(如 `SENTINEL_ATTACHMENT_VALUE`)的 config 驱动**每个**工具,断言任何工具的 JSON 输出里都不出现该哨兵。这是 catch-all,防止以后**新增**工具又把原值塞进 structuredContent(逐出口测试只盯现有 5 处,挡不住未来新出口;光 grep 更挡不住)。

---

## 已决定:不做 / 推迟

### structuredContent 序列化进 text(原 #6)→ 不做(2026-05-31)
规范是 **SHOULD 非 MUST**。**Accepted compatibility gap**(三审措辞收紧):当前只保证 **能读 structuredContent 的 host**;面向**会忽略 structuredContent 的 host**(老/严格 client)的 text 镜像暂不做——那要为结果付 describe/rawResult 翻倍体积 + 先脱敏再序列化的代价。判定不是「绑定 Claude」,而是明确的兼容性取舍:真撞上忽略 structuredContent 的 host 时,再加 **output-budget-aware text mirror**。

### per-tool `data` schema → backlog,本轮不做(2026-05-31)
三审建议给高价值工具(resolve / invoke_plan / describe)的 `data` 加精确 schema,让 host 能从 schema 读懂结果结构。属实方向,但当前 `data` 为 opaque object 是**有意的一致性取舍**(P2 #5);per-tool 精度是收益递减的增量,**YAGNI,记 backlog**,等真有 host 要据此自动编排再做。

### probe 显式 address → 保持现状(2026-05-31)
`probe` 暴露显式 `address`(invoke 无 address 入参,只能打配置 server;probe 是唯一显式地址面,backlog SSRF)。纯 TCP 连通性探测、本地单用户、低危,且显式地址是 probe 主用法(先探再配 server),默认关会破主流程。**不加开关、不实现**。日后要硬化做成 opt-in `--disable-explicit-address`(默认放行,对称 `--disable-config-write`),不默认关。

---

## 立场记录(reviewer 已认同)

- **网络策略**(原措辞「YAGNI」收紧为 accepted-risk posture):当前 access control = **invoke 只能打已配置 server**;**probe 显式 address 是有意保留的诊断能力**;**rate limit 暂不实现,列为 accepted risk**;**output sanitization 即 P0 的 public view**。规范安全三项(access control / rate limit / output sanitization)逐项有明确处置,而非一句 YAGNI 带过。
- **严格 decode 边界**:schema 类型违例(bool 塞字符串、未声明字段)维持 `-32602`;业务校验(service/server 找不到)走 `isError` + app.Result。区分合理,保留。
- **serverInfo.name**:`sofarpc-mcp` 作为 MCP server 角色名可接受,非功能问题。改与不改皆可,归 P3。
- **DisableConfigWrite 的 tools/list 形状(三审建议钉测试)→ 已满足,无需动**:`server_test.go:98` `TestDisableConfigWriteHidesWriteTools` 已断言禁写模式恰 7 工具、无 save/remove;且 registry 懒构建一次、运行时不变(`toolRegistry()` 在单线程读循环首调),capability `listChanged=false` 成立。reviewer 缺此上下文,记录免后续 agent 重复造测试。

---

## 修复清单(待批量实现)

### P0
1. **attachments 出口级脱敏**:tools 层 public view(`publicServer`/`publicEndpoint`/`publicServers`/`publicPlanDisplay`),覆盖 **resolve / config_list / config_save_server / doctor / invoke_plan** 5 处。red line 见上。

### P1
2. **`ping`**:proto 层处理,**任意 lifecycle state 都回 `{}`**(兼容 liveness probe;不 gating)。测试覆盖 **StateNew / Initializing / Ready** 三态。
3. **二次 initialize 守卫**:`handleInitialize` 在 `state != StateNew` 时拒绝(回错误),**不回退状态**。
4. **unknown tool → 锁定 `-32602`(InvalidParams)**:`tools/call` 这个 JSON-RPC method 是存在的,只是 `params.name` 无效 → 属 InvalidParams 而非 MethodNotFound;MCP Tools 规范例子也是 -32602。registry `Call` 未知 tool 从 `CallResult{isError}` 改回 `*proto.Error{-32602}`。**输入校验维持现状**。

### P2
5. **outputSchema 覆盖所有已注册工具**:所有工具都经 success/failure/rendered 回 app.Result,故给**每个工具的 ToolSpec** 挂 `resultOutputSchema`(含 config_save_project/save_server/remove_*)。`Validate` **收紧**为「所有已注册工具必须有 outputSchema」(原来只要求 3 个;`--disable-config-write` 时已注册=7,也都要有)。**不写死 11**——禁写模式下只 7 个。(注:schema 是通用 app.Result 信封,`data` 为 opaque object;是一致性而非 per-tool 精度。)
6. **progressToken 类型校验**:proto 只接受 **JSON string 或 JSON integer**;若是 number,必须**能无损解析为整数**(`1.2` 这种带小数的拒绝),否则忽略、不发 progress(现 progress.go 收任意 raw JSON)。
7. **修 README progress 漂移**:已核——发 progress 的是 **invoke / doctor / describe**;**probe / invoke_plan 没发**。README compliance 段从 progress 清单去掉这俩(或给它俩补 progress,但它们本就快,倾向改文档)。
8. **SelfTest 走真实 session**:用内存 stdin/stdout 跑真 `proto.NewSession`,**喂完整 JSON-RPC 帧**走完整生命周期——不是直调 `s.handle`/`dispatchTool`(现 `server.go:81/84` 正是直调,反面教材)。至少:`initialize` → `notifications/initialized` → `{"method":"tools/list"}` → `{"method":"tools/call","params":{"name":"sofarpc_config_list","arguments":{}}}`,断言各步回包正常。**三审补**:改完后 SelfTest 不再是 `handle` 的唯一非-dispatcher 调用方,给 `handle` 加一句注释钉死「**只给 dispatcher 调,不是测试/自检入口**」——防止它再被当成「半个 MCP server」绕过 proto.Session 的 lifecycle/ping/cancel/transport。

### P3
9. **serverInfo name + 注释修正**:serverInfo.name 视意见改/不改;修 `InvokeTool` 那句「can call any reachable address」误导注释(MCP invoke 只能打配置 server)。
10. **旧计划文档加指针(三审补)**:`docs/mcp-layer-refactor-plan.md` 仍像主实施计划(PR 分段、"PR7 才 polish"),会误导后续 agent。顶部加一句:**后续以 `mcp-review-followups.md` 为准,本文为历史实施计划**。

---

实现顺序:P0(1)→ P1(2/3/4)→ P2(5/6/7/8)→ P3(9/10)。改完 `go test ./...` 绿 + `codex review --commit` 复审。

---

## 实现落点(2026-05-31 完成)

| 项 | 落点 |
|---|---|
| P0 出口脱敏 | 新 `tools/view.go`(`redactAttachments`/`publicServer`/`publicEndpoint`/`publicBoundServers`/`publicPlanDisplay`);接线 resolve.go(单/多)、config.go(list/save_server)、helpers.go(endpointData,doctor 用)、invoke.go(invoke_plan)。测试 `tools/view_test.go`:逐出口 5 个 + sentinel 全扫 `TestNoToolLeaksAttachmentValue` + view 单测 |
| P1 ping | `proto/session.go` `handlePing`(任意态回 `{}`);测试 `proto/ping_test.go` `TestPingAnsweredInEveryState`(三态) |
| P1 二次 initialize | `proto/session.go` `handleInitialize` 守卫(state≠New → -32600,不回退);测试 `TestSecondInitializeIsRejectedWithoutRollback` |
| P1 unknown tool | `server/registry.go` `Call` 未知 tool → `-32602`;测试 `registry_test.go` `TestCallUnknownToolIsInvalidParams` |
| P2 outputSchema 全覆盖 | 8 个工具补 `resultOutputSchema`;`registry.go` `Validate` 收紧为「全部已注册工具必须有」;测试 `annotations_test.go` 反转 + `server_test.go` `TestAllToolsAdvertiseOutputSchema` |
| P2 progressToken 校验 | `proto/progress.go` `validProgressToken`(string/整数,整数范围 ±2^53);测试 `proto/progress_test.go` |
| P2 README progress 漂移 | `README.md`:progress=invoke/doctor/describe,cancellation=全 async 工具 |
| P2 SelfTest 真实 session | `server.go` 抽 `newSession`,SelfTest 喂完整帧走生命周期 + `verifySelfTest`;`handle` 加「仅 dispatcher」注释 |
| P3 InvokeTool 注释 | `tools/invoke.go`:删「can call any reachable address」,改为「只打配置 server,无 address 入参」 |
| P3 旧计划文档指针 | `docs/mcp-layer-refactor-plan.md` 顶部加「以 followups 为准」 |

DisableConfigWrite tools/list 形状:三审建议钉测试,核实 `server_test.go` 已有 `TestDisableConfigWriteHidesWriteTools`,未重复。per-tool data schema:记 backlog,本轮不做。

### codex 一审(commit d3149bb)→ 2 处 P2,均已修

1. **resolve 多 server 读错 store**:见上「落地选择」——A 在注入 `ConfigStore` 时读全局配置,与 `appSvc.Resolve` 发散。改 B(原地脱敏 `resolved.Servers`)。`view.go`/`resolve.go`,新增 `TestResolveMultiServerUsesInjectedStore`。
2. **progressToken `Int64` 路径绕过 ±2^53**:`9007199254740993` 能进 int64 但超安全范围,旧码 `Int64()` 成功即放行,违背自己写的契约。整数路径补范围检查;`progress_test.go` 加 `9007199254740992`(收)/`9007199254740993`(拒)用例。
