# 深度分析整改实施计划（2026-07）

## 1. 背景与基线

- 输入报告：`docs/deep-analysis-2026-07.md`
- 审计基线：`main@ccabc60`
- 默认分支：本仓库没有 `master`，以 `main` 作为用户所说的 master/default branch。
- 实施分支：从 `main` 新建 `fix/deep-analysis-release-blockers`，再带入文档提交；不直接在分析分支开发。
- 工作区保护：不纳入、删除或改写现有未跟踪历史文档。

## 2. 本轮 Goal

在独立实现分支上完成审计报告中会造成进程崩溃、支持平台不可用、静默 wire 错值、
关键诊断失效和配置迁移/并发不安全的高优先级修复，为每个缺陷增加可重复回归测试，
并通过全量 Go 测试、race 测试、Windows 交叉构建和安装包布局验证。

本轮不是“顺手重构”迭代。只有为完成正确性修复所必需的去重才进入范围；大规模 D 类
结构收敛和其余 P2 打磨留作后续。

## 3. 范围

### Phase 1：direct 解码安全与 BOLT 状态

1. 为 Hessian reader 增加统一容器计数校验：
   - 拒绝负数；
   - 不得超过剩余输入可承载的最小元素数；
   - 限制单容器元素数；
   - 限制一次解码的累计容器预算。
2. 在 `list`、`fixedList`、`classDef` 分配前使用同一校验。
3. 增加畸形、负数、超大、截断输入回归测试，并增加 fuzz seed；测试不得实际触发大内存分配。
4. 校验 BOLT response status，非成功状态返回包含 status/class/header 诊断的类型化错误。

验收：攻击输入返回错误、不 panic、不发生大分配；正常 Hessian/BOLT golden/oracle 测试保持通过。

### Phase 2：Windows 与安装发布链路

1. `install.ps1` 将 `$Host` 改为非保留变量，并兼容 PowerShell 7 的 latest redirect 读取。
2. Windows zip 与 tar.gz 使用相同顶层目录布局。
3. `install.sh` dev 路径不再用 `exec` 绕过 EXIT trap。
4. 发布矩阵与安装脚本支持的架构对齐；修正 PowerShell PATH 提示。
5. CI 增加 `windows-latest` 的 build/test 与 `install.ps1 -h` 冒烟，并增加归档布局断言。

验收：Windows zip 内为 `<base>/sofarpc.exe`；PowerShell help 不触发只读变量错误；Windows Go 测试进入 CI。

### Phase 3：Java parser/schema 可观测性与合法语法

1. 字段初始化器遇顶层分号无条件结束，补 `<` 比较表达式回归测试。
2. lexer 支持 UTF-8 BOM、Unicode Java 标识符和 `\f` 空白，确保 rune/字节推进一致。
3. schema 构建记录读取/解析失败文件，并通过 `Description.Warnings`/统计信息向上透出。
4. Describe 区分 service 不存在与 method 不存在。
5. `referencedTypes` 过滤 wildcard 关键字和方法类型变量。
6. 本轮不做高风险的通用声明级错误恢复；先保证整文件失败可观测，并用回归 seed 驱动后续恢复设计。

验收：比较表达式、BOM、中文标识符样例可被索引；损坏 Java 文件不阻断其他文件且产生 warning。

### Phase 4：RPC wire 类型正确性

1. Map key 按声明的泛型 key 类型编码，非法 key 转换返回明确错误。
2. whole-DTO 回退仅适用于单个 class/DTO 参数；scalar 参数名拼错必须报缺参。
3. ordered 参数在 schema 命中后统一使用解析后的 FQN paramTypes。
4. 泛型继承的外部类型优先解析 owner imports，避免拼出错误 same-package FQN。

验收：增加 `Map<Long, ...>`、scalar 拼错、短名 ordered paramTypes、imported inherited generic 四组 wire/plan 测试。

### Phase 5：MCP 结果与断言可信度

1. `rawResult=true` 对循环引用做 JSON-safe 切环；成功 RPC 不因 raw marshal 失败被替换成 INTERNAL_ERROR。
2. 断言比较取消跨类型 `fmt.Sprint` 等价，仅保留同 JSON 类型或明确的数值等价。
3. `invoke_plan` schema 与共享 `InvokeArgs` 对齐，并增加字段镜像守卫。
4. `resultPath` 支持数组下标，恢复被截断数组任意项的访问能力。
5. 阈值描述从 `presentation.MaxArrayItems` 生成，避免 prose 漂移。

验收：循环 map、`1` vs `"1"`、plan 三个扩展字段、`$.items.200` 均有回归测试。

### Phase 6：配置迁移与并发安全

1. v1→v2 迁移时反向物化 project profile，迁移后 `project use` 可用。
2. 先宽松读取 version 并执行版本闸门，再严格解析完整配置。
3. `configForSave` 不修改调用方 map；`Update` 返回与落盘一致的规范化版本。
4. 检测派生 server 名跨项目冲突并返回确定性错误。
5. scaffold 创建走 appconfig 持锁 API，消除 check-then-write 覆盖窗口。
6. schema cache 使用临时文件 + rename 原子落盘；清理与写入共享跨进程锁。

验收：迁移、未来版本、输入不可变、名称冲突、并发 scaffold/cache 均有聚焦测试，race 模式通过。

## 4. 明确不在本轮范围

- D 类大规模包边界/registry/文件合并重构。
- Java 声明级通用错误恢复器。
- java.time/BigInteger 编码架构整体下沉。
- 搜索预索引、注释语言统一、依赖常规升级及其它 P2 打磨。
- 未跟踪历史文档的取舍与提交。

这些项目不影响本轮 Goal 完成；后续可从报告生成独立 goal，避免与安全/正确性修复混杂。

## 5. 实施顺序与提交边界

建议保持可回滚的小提交：

1. `docs: finalize deep analysis remediation plan`
2. `fix(direct): bound Hessian container allocations and validate response status`
3. `fix(install): repair Windows bootstrap and archive layout`
4. `fix(schema): preserve valid Java sources and surface parse warnings`
5. `fix(app): preserve declared RPC wire types`
6. `fix(mcp): make raw results and assertions JSON-safe and strict`
7. `fix(config): harden migration and concurrent persistence`
8. `docs: record remediation verification`

若某阶段暴露与既定 D1–D17 决策冲突，先停在该阶段并以 `docs/decisions.md` 为准，不静默改变外部契约。

## 6. 验证矩阵

每阶段先跑聚焦测试，最终至少执行：

```bash
gofmt -w <changed-go-files>
go vet ./...
go test ./...
go test -race ./...
GOOS=windows GOARCH=amd64 go build ./...
go test -race -tags bolt_oracle ./...   # 在 oracletest 模块
```

安装/发布脚本额外验证：

- `bash -n scripts/install.sh scripts/package.sh`
- 有 `pwsh` 时执行 `pwsh -NoProfile -File scripts/install.ps1 -h`
- 生成最小 Windows archive 并断言 zip 顶层目录布局
- CI workflow YAML 语法与 matrix 结构检查

Hessian JVM oracle 依赖本地 Java/内部 jar；环境不具备时明确记录为未跑，不以 SKIP 冒充 PASS。

## 7. 完成定义

同时满足以下条件才可将 Goal 标记完成：

- Phase 1–6 的范围项全部落地，或因 D1–D17 冲突明确从范围移除并记录理由；
- 每个修复至少一个失败前/通过后的回归测试；
- 全量 `go test ./...`、`go vet ./...`、`go test -race ./...` 通过；
- Windows 交叉构建和归档布局验证通过；
- 未引入新的 stdout 协议污染、wire shape 漂移或配置文件权限回归；
- 报告与本计划更新最终状态、已跑验证和剩余风险；
- 工作区原有未跟踪文件未被误提交。
