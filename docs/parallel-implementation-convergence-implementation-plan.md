# D 类平行实现收敛实施计划（2026-07）

## 1. 输入与 Goal

- 设计文档：`docs/parallel-implementation-convergence-design.md`
- 需求来源：`docs/deep-analysis-2026-07.md` D 类、`docs/decisions.md` backlog 3b
- 基线：`fix/deep-analysis-release-blockers@a82ac0a`
- 实施分支：`refactor/converge-parallel-implementations`

Goal：在不改变 MCP 成功契约、RPC wire shape 和配置格式的前提下，完成配置选择、Java 类型基础语义、
RPC 类型解析、Java 类型声明解析和配置错误路由五组平行实现收敛，为每个新 seam 建立接口级回归测试，
并通过全量、race、Windows 构建及双 oracle 验证。

## 2. Phase 1：配置选择 seam

1. 在 `internal/app` 增加 selector/selection 类型和 `SelectProject`/`SelectServer`。
2. 迁移 app 内 resolve/probe/invoke 调用方。
3. 迁移 describe/doctor 到 app 选择 seam，删除 tools 私有 resolve 实现。
4. 将 `helpers_test.go` 的选择规则测试移到 app 接口测试；保留 tools 端到端渲染测试。

验收：显式、profile、active profile、单候选、optional/required 歧义、跨项目不匹配、悬空引用全部通过；
错误均为含 kind/details 的 `DomainError`。

建议提交：`refactor(app): centralize config selection`

## 3. Phase 2：Java 类型基础语义与 RPC resolver

1. 在 `internal/javavalue` 增加 `BaseJavaType`/`IsByteArrayType` 及表驱动测试。
2. app/direct 删除三份本地 helper，全部委托共享函数。
3. 在 app 建立 `rpcTypeResolver`，把 method/field/value 的解析优先级收敛到一个实现。
4. 收窄 legacy type-variable heuristic；补 `T`、`T1`、`ID`、`URL`、explicit import、declared param、
   same-package schema、nested generic、wildcard、数组测试。
5. 保持 identity/value 分工，增加 wire argTypes 与 TypedValue 回归测试。

验收：`rg` 不再找到 `eraseRPCGeneric`/`eraseJavaType`/app/direct 私有 `isByteArrayType`；三条解析路径通过
同一 resolver；Hessian byte array/generic golden 不变。

建议提交：`refactor(rpc): centralize Java type semantics`

## 4. Phase 3：Java 声明主体单一实现

1. `parseTypeDecl` 改为 preamble + 委托。
2. `parseTypeDeclWithPreamble` 严格校验 `@interface`。
3. 补顶层/嵌套 class/interface/enum/record/annotation 等价测试和 malformed annotation 回归。

验收：声明主体只有一份；合法 AST 不变；错误位置和消息保持可诊断；不引入 partial AST/recovery。

建议提交：`refactor(parser): share type declaration parsing`

## 5. Phase 4：配置错误单一路由

1. 在 tools 建立 `failureResult(err, fallbackCode)`。
2. 合并现有 `configFailureResult`，替换 resolve、invoke、invoke_plan、describe 等普通失败出口。
3. 增加 corrupt/future config 的工具矩阵测试，断言 code、configPath、nextTool/recovery。
4. 保持 doctor 的检查集合模型和反自指不变量。

验收：同一损坏配置在所有普通配置消费者中返回同一稳定 code；非配置 DomainError 仍保留 kind/details。

建议提交：`refactor(mcp): centralize config failure routing`

## 6. Phase 5：决策更新与最终验证

1. 在 `docs/decisions.md` 记录配置选择与 Java 类型语义的单一所有者，将 backlog 3b 标为完成。
2. 回填本计划的提交、验证结果和明确剩余项。
3. 确认原有六份未跟踪历史文档未改写、未提交。

最终验证：

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

建议提交：`docs: record parallel implementation convergence`

## 7. 停止条件

- 若需要改变 MCP input/output schema、配置 v2 格式或 wire argTypes 契约，停止并先更新设计；
- 若共享类型 helper 需要 direct 反向依赖 app/schema，停止并重新选择 seam；
- 若 parser 去重暴露成员级恢复需求，只记录 seed，不在本 Goal 扩张；
- 若真实 oracle 与现有 Go 测试冲突，以 oracle 结果为兼容性门禁，不静默更新 golden。

## 8. 完成定义

- Phase 1–5 全部完成；
- 每个被删除的平行实现都有新 seam 的等价测试；
- 全量、race、Windows build、BOLT oracle、Hessian JVM oracle 全部通过；
- 设计文档、实施计划和决策记录一致；
- Goal 标记完成，未跟踪用户文档保持原状。
