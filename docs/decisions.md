# 当前有效决策（单一事实源）

> 本文件是 sofarpc-mcp 设计决策的**唯一有效结论**。docs/ 下的历轮评审文档
> （agent-first-mcp-review*.md、agent-first-improvement-plan.md、
> agent-first-mcp-followup.md、mcp-best-practices-audit.md 等）只作过程记录，
> 其中与本文件冲突的结论一律以本文件为准。改决策 = 只改这一个文件。
>
> 最后更新：2026-07-17

## 已定决策

| # | 决策 | 结论 | 理由 / 备注 |
|---|------|------|-------------|
| D1 | 大结果处理 | 阈值式：`data.result` 中数组超过 200 项截断为前 200 + `$truncated` 标记（`presentation.MaxArrayItems`）；整个 envelope 序列化超过 32KB 时 text 镜像降级为一行指针（`tools.textMirrorMaxBytes`）；小结果维持 structuredContent + text 双发 | 兼顾 token 成本与 text-only 宿主兼容。escape hatch 是 `resultPath` |
| D2 | rawResult 不截断 | `data.rawResult` 保持完整原始树 | raw 树可能含共享子结构，递归重写有风险；raw 本就是显式逃生舱 |
| D3 | 成功结果去传输噪声 | 成功且无断言失败时不再输出 `data.diagnostics`；失败、断言失败或 `rawResult=true` 时保留；resolve 成功输出不再含恒定 `network` 与重复的 `diagnostics.resolution` | 每次调用的固定 token 税；诊断信息只在需要诊断时给 |
| D4 | nextTool 反自指不变量 | 任何错误路径的 `error.nextTool` 不得等于当前工具名（守卫测试 `TestFailureNextToolNeverPointsBackAtItself`）；probe 失败指 doctor；BAD_REQUEST 无 kind 兜底不指定工具（本地参数问题没有别的工具能修） | agent 被指回失败工具只会原地循环 |
| D5 | invoke_plan 保留独立工具 | 不合并为 invoke 的 dryRun 参数，仅在两个工具描述里互相对齐 | 独立工具是"永不发请求"的结构性保证；合并后一个布尔传错就是真调用 |
| D6 | config 写工具默认开启 | 维持 writeEnabled 默认 true + `--disable-config-write` 门控 + destructive 注解 + dryRun；不改默认只读 | beta 期暴露度低；真要收紧应做在安装链路（MCPB 默认只读），等真实反馈 |
| D7 | describe 候选保留 score/sourceFile | 不降级 | 有测试有意锁定（排序参考 + 源文件可导航），移除收益小 |
| D8 | summary 不并入 Result 信封 | 维持 r3 裁决：summary 只在 wire `_meta` | 与 `TestFinishWireShape` 冻结的 wire 契约冲突 |
| D9 | 显式地址 invoke 保持关闭 | MCP `sofarpc_invoke` 不接受 address 参数 | 放开会绕过一切服务器级护栏（feature review #8 原决策维持） |
| D10 | suggest_* 独立工具不做 | 候选合并进 resolve/describe 的错误 details（Sprint 2 已实现） | 减少工具数；`agent-first-mcp-review.md` P1 的相反结论已过期 |
| D11 | repeat / 延迟统计不做 | 维持否决 | 生产环境滥用风险 |
| D12 | Session / Replay 继续观察 | 不实现，直到出现真实触发信号（同一调用链重复传参 ≥3 次等，见 improvement-plan 的量化门槛） | 维持 improvement-plan 的降级结论；review/followup 中的 P0 定位已过期 |
| D13 | 错误码体系维持现状 | `errorCode` 兜底仍为 INVOKE_FAILED，保留字符串嗅探 | 未知传输错误标 INTERNAL_ERROR 会误导（多数确属调用失败）；嗅探兜住丢失类型的包装错误。收敛为 DomainError 自带 code 属大改，收益不明,暂缓 |
| D14 | CLI 与 MCP 共用 Result 信封 | `sofarpc project/server` 子命令与 ping 全部输出统一 envelope（表格模式除外）；地址解析统一走 `app.ProbeEndpoint`/`app.Resolve` | 2026-07-06 落地；agent 可用同一套解析逻辑处理 CLI 与 MCP 输出 |
| D15 | BOLT oracle 常驻 CI，Hessian oracle 留本地 | BOLT oracle 在独立 `oracletest/` 模块（自带 go.mod，隔离 sofastack 依赖树）并进 CI；Hessian oracle 需内网 JVM+jar，仍走 `scripts/oracle-gate.sh` 发布前门禁 | 2026-07-06 落地 |
| D16 | 发布走 tag 触发流水线 | `.github/workflows/release.yml` 复用 package.sh/build-mcpb.sh 自动建 Release 上传产物；含 `-` 的 tag 标 prerelease | 消除人工上传的 checksum 出错面 |
| D17 | activeProfile 显式化 | 保留"首个 profile 自动成为 activeProfile"（配置契约要求多 profile 必有 active，且删掉就无法自举）；但 save_server（MCP/CLI）回显 `activeProfile`/`activeProfileChanged`，非 active 的保存带 `warning`；显式切换入口：`setActive=true`（MCP）/ `--set-active`、`sofarpc project use <project> <profile>`（CLI），底层 `appconfig.SetActiveProfile` | 2026-07-06 落地；消除"保存第二个 server 时路由已被首个 profile 静默决定"的陷阱 |
| D18 | 平行实现采用单一所有者 | 项目/服务端选择由 `internal/app` 的 `SelectProject`/`SelectServer` 所有；Java base type/byte array 语义由 `internal/javavalue` 所有；app 内 RPC source type 解析由 `rpcTypeResolver` 所有；顶层/嵌套声明共享 `parseTypeDeclWithPreamble`；普通 MCP 工具错误统一经 `failureResult` 路由 | 2026-07-17 落地；规则跨层复制已经产生错误类型、数组、type-variable、`@interface` 和配置错误码漂移，单一 seam 让修改与回归测试落在同一位置 |
| D19 | MCP 工具数据源统一经过 app.Service | `SourceIndex` 的 interface 为项目级 `Load`；invoke、describe、doctor 均通过同一个 `app.Service` 的 ConfigStore/SourceIndex adapter，tools 不直接打开全局 config 或 schema cache | 2026-07-17 落地；避免同一 MCP server 内注入 store/source 与默认磁盘数据源分叉 |
| D20 | Hessian item budget 采用 2M/4M 分层上限 | 单容器最多 `2<<20` 项，单次解码累计最多 `4<<20` 项；仍叠加 16 MiB response、remaining-input、depth/ref 校验，不开放关闭预算的配置项 | 允许超过旧 100 万项阈值的合法大批量响应；按 64 位 interface slot 粗算，将单 slice/累计 slot 控制在约 32/64 MiB 起步范围，继续阻断小包诱导数十 GiB 分配 |
| D21 | Parser 在 type-body member seam 做可观测恢复 | 成员解析失败后回滚到 checkpoint；仅能同步到顶层 `;`、平衡 member body 或 owner `}` 时继续，丢弃整个坏成员并产生 `ParseWarning`；结构不闭合和 lexer 错误仍为文件级 fatal；schema cache bump 到 v7 | 让单个不支持成员不再导致整文件消失，同时避免不安全同步生成半 AST；warning 经 Index/Describe 暴露 |

## 未做事项（backlog，按价值排序）

1. **traceId 生成/回显**（feature review #6 后半）：比调用级 attachments 更高价值的诊断锚点，net-new 未做。
2. **invokePolicy 写操作护栏**（feature review #3）：高价值但需先拍策略语法/默认值/校验点，不是自治实现项。
3. ~~mcp/tools 与 javavalue 补测~~：已完成（2026-07-06，tools 92.7%、javavalue 100%）。
3a. ~~save_server 静默设置 activeProfile 的提示~~：已完成（2026-07-06，见 D17）。
3b. ~~tools/helpers.go 与 app/resolve.go 的 resolveProject/resolveServer 双份实现~~：已完成（2026-07-17，扩展收敛 Java 类型语义、RPC 类型解析、声明解析与普通工具配置错误路由，见 D18）。
4. **正式 `vX.Y.Z` Release**：目前全是 `v0.1.0-beta.X` 预发布 tag；install.sh 的 `releases/latest` 路径需要一个正式 Release 验证（release.yml 已就绪）。
5. **golangci-lint**：本机未安装，暂未引入；引入时需先本地跑通再上 CI 门禁。
6. probe 升级 BOLT 心跳（feature review #2）、describe `exampleArguments` 参数骨架（#4）、describe 跨项目搜索（#7）：C/D 档按需。
7. `sofarpc://projects` / `sofarpc://schema/{...}` resources：等宿主对 resources 的消费成熟再说。
8. config_save_* 的 `IdempotentHint=true`（audit 可选项）。
9. Windows 真实 CI（当前仅交叉编译 + macOS/ubuntu 矩阵）；install.ps1 无自动化测试。
10. 类型兼容矩阵长尾（Enum 无 schema、provider 扩展、远端异常展开）：见 compatibility-matrix.md。

## 历史文档状态

| 文档 | 状态 |
|------|------|
| agent-first-improvement-plan.md | 过程记录；Sprint 1-4 已落地部分与正文"当前问题"描述存在时态错位 |
| agent-first-mcp-review.md | 过程记录；其中 suggest_* 新工具、session P0 结论**已过期**（见 D10/D12） |
| agent-first-mcp-followup.md | 过程记录；"11 个工具""建议无一落地"的现状表**已过期** |
| agent-first-mcp-feature-review.md | 过程记录；#1/#5/#6(attachments) 已实现，#8/#11 维持否决（D9/D11） |
| agent-first-mcp-review-r3-verification.md | 过程记录；#4 裁决即 D8，#5 仍被 SDK 阻塞 |
| mcp-best-practices-audit.md | 过程记录；B1 已修，text 镜像项即 D1 |
| profile-config-design-plan.md | 有效设计文档（配置 v2 契约） |
| pure-go-runtime.md / compatibility-matrix.md / single-binary-install-target.md | 有效参考文档 |
| migrate-to-official-mcp-sdk.md | 已完成的迁移记录；wire 契约冻结约定仍有效（见 D8） |
