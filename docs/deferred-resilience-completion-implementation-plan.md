# 延后健壮性工作补全实施计划（2026-07）

## 1. 输入与 Goal

- 设计：`docs/deferred-resilience-completion-design.md`
- 基线：`refactor/converge-parallel-implementations@07f1f6e`
- 分支：`fix/complete-deferred-resilience-work`

Goal：补齐 describe/doctor 的 app.Service 数据源注入、Hessian 超大批量预算策略和 javaparser 成员级通用
错误恢复；保持 MCP 与 RPC wire 契约，建立跨 seam 回归测试，并通过全部发布门禁。

## 2. Phase 1：app.Service 数据源一致性

1. 将 `SourceIndex` interface 从 `Describe` 改为 `Load`，调整 `LocalSourceIndex` 和 invoke。
2. 在 `app.Service` 暴露带 context 检查的 `LoadConfig`、`LoadSourceIndex`。
3. `AddDescribe`/`AddDoctor` 接收共享 app service，删除 tools 对全局 config/schema loader 的直接调用。
4. 更新 server 注册与现有测试 helper。
5. 新增 fake store/source 的 MCP 级注入测试，证明磁盘数据不会旁路覆盖注入数据。

验收：`rg` 在 describe/doctor 中找不到 `loadConfig`/`schema.LoadOrBuildIndex`；invoke/describe/doctor 均经过
同一 SourceIndex interface；config error 与 doctor check 契约不变。

建议提交：`refactor(app): complete source index injection`

## 3. Phase 2：Hessian 大批量预算

1. 单容器上限调整为 `2<<20`，累计上限调整为 `4<<20`，补内存上限注释。
2. 增加跨旧阈值成功、新阈值失败、remaining-input 优先失败的表驱动测试。
3. 增加实际 compact list 解码测试，覆盖超过 100 万项的合法 payload。
4. 运行 direct race、fuzz seed 与 Hessian oracle。

验收：旧阈值以上合法批量通过；新上限仍稳定返回 budget error，不发生 panic/OOM；oracle bytes 不变。

建议提交：`fix(direct): tune Hessian item budgets`

## 4. Phase 3：Parser 成员恢复

1. 增加 `ParseWarning` 和 cursor checkpoint/recovery helper。
2. `parseClassBodyMembers` 在 member error 时回滚并同步；不可同步结构继续返回 fatal error。
3. 补 class/interface/annotation/nested type 恢复矩阵和 fatal 边界测试。
4. schema 汇聚 recover warning，保留 partial compilation unit。
5. bump `indexCacheVersion` 至 7，并补 cache 重建测试。
6. 增加 schema/describe warning 端到端测试。

验收：坏成员被整段丢弃但后续成员可用；warning 带文件和位置；正常 golden 不变；lexer/结构错误仍按文件跳过。

建议提交：`feat(parser): recover unsupported type members`

## 5. Phase 4：决策与最终门禁

1. 更新 `docs/decisions.md`，记录 app service 数据源一致性、Hessian 预算和 parser recovery 契约。
2. 回填设计/计划的提交和验证结果。
3. 确认原有六份未跟踪历史文档未进入 diff/index。

最终命令：

```bash
gofmt -w <changed-go-files>
git diff --check
go vet ./...
go test ./...
go test -race ./...
GOOS=windows GOARCH=amd64 go build ./...
go -C oracletest test -race -tags bolt_oracle ./...
bash scripts/oracle-gate.sh
```

建议提交：`docs: record deferred resilience completion`

## 6. 停止条件

- 若 SourceIndex 迁移要求改变 MCP schema 或跨项目搜索语义，停止并更新设计。
- 若超过旧阈值的实测 payload 在 CI 内存预算下不稳定，保留 reserve-level 测试并缩小实际 decode fixture，
  不删除安全上限。
- 若某类成员无法在不吞后续声明的情况下同步，保持该类 fatal，并把 seed/原因写入 warning 测试，不伪装恢复成功。
- 若 oracle 与现有 Go 测试冲突，以真实 oracle 为 wire 门禁，不更新 oracle 迎合实现。

## 7. 完成定义

- Phase 1–4 完成并分阶段提交；
- 三项原遗漏均有 interface 级与端到端证据；
- 全量、race、Windows、双 oracle 通过；
- Goal 完成并推送当前分支；
- 用户原有未跟踪文档保持原状。

## 8. 实施记录

| Phase | 提交 | 状态 |
| --- | --- | --- |
| 设计与计划 | `853c714` | 完成 |
| app.Service 数据源一致性 | `51b587a` | 完成；app/mcp/tools 聚焦测试与注入端到端测试通过 |
| Hessian 大批量预算 | `b073493` | 完成；direct 普通/race 与跨旧阈值实际解码通过 |
| Parser 成员恢复 | `31cfeec` | 完成；parser/schema/tools 普通与 race 测试通过 |

最终验证（2026-07-17）：

| 命令 | 结果 |
| --- | --- |
| `git diff --check` | 通过 |
| `go vet ./...` | 通过 |
| `go test ./...` | 通过 |
| `go test -race ./...` | 通过 |
| `GOOS=windows GOARCH=amd64 go build ./...` | 通过 |
| `go -C oracletest test -race -tags bolt_oracle ./...` | 通过 |
| `bash scripts/oracle-gate.sh` | 通过；Hessian JVM 与 BOLT real oracle 均验证成功 |

原有六份未跟踪历史文档未修改、未暂存、未提交。
