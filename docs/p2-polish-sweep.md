# P2 打磨清扫（2026-07）

> 输入报告：`docs/deep-analysis-2026-07.md` 的全部 P2 项。
> 基线：`fix/complete-deferred-resilience-work`（含前四个整改分支的全部 P1/P2 前序工作）。
> 本轮只做 P2：魔法数常量化、死代码/文档腐化清理、一致性收敛、健壮性小修；
> 不改动已冻结的 wire 契约与 D1–D17 决策。

## 验证

- `gofmt -l .` 无输出；`go vet ./...` 干净；`staticcheck ./...` 干净（新引入）
- `go test -race ./...` 全绿（12 包）
- `GOOS=windows GOARCH=amd64/arm64 go build ./...` 通过
- `go -C oracletest test -tags bolt_oracle ./...` 通过

## 已完成（按包）

**direct**：depth 用 `maxHessianDepth` 常量；`encodeBoltRequest` 校验 class/header uint16、
content uint32 长度（复活死 error）；`decodeSimpleMap` 空输入返 nil 对称、注释 best-effort 语义；
`orderedHeaderKeys`/`requestHeader` 共用 `boltHeaderOrder`；`utf16Length` 去恒真判断；
long-tag case 去 SA4003 恒真半段。

**javaparser**：`punctMap` 提为包级 var；`advanceEscaped` 修 `\`+换行的行号漂移；
`parseTypeRef` 拒绝硬保留关键字（只放行标识符/上下文关键字/primitive/void）；ctor 快路径用
`isIdentLike`（类名为上下文关键字时也识别）；`skipFileLevelAnnotation`/`skipTypeUseAnnotations`
共用 `skipAnnotation`；删死代码 `matchIdentValue`；`Token.Col` 多字节近似注释、`peekJavadoc`
不变量注释；清 `Task N`/`ArgsRaw` 腐化注释。

**schema**：`fallbackParamName` 用 `strconv.Itoa`；`Search` 命名 5/20 常量 + 纯标点查询返回空；
`CachePath` sanitize `project.Name` 防路径逃逸；`DiscoverSourceRoots` 支持 workspace 本身即
`src/main/java`、深度上限命名。

**app / javavalue**：`errorCode` 加 `context.Canceled` 分支（新 `CodeCanceled`）+ 收窄 timeout 嗅探
+ 表驱动测试（原 0% 覆盖）；probe 配置解析失败改 `BAD_REQUEST`；`rpcParamType` 补 `BigInteger`；
`java.time.LocalTime` 补编码 + 校验；`firstMalformedSpecial` 校验 map key；`isJDKBuiltinType`
单一真值化 JDK 前缀集合（含 `java.time`，消除 java.time 参数的伪 `SCHEMA_ANNOTATION_SKIPPED`）；
javavalue 包/类型 doc；`close` 变量改名避免遮蔽内建。

**mcp / presentation**：`TruncateArrays` 无截断时复用原节点（省一次全树深拷贝）；doctor 补 1.0
完成进度、失败码按首个失败 check 映射（server/project/describe→BAD_REQUEST，其余→INTERNAL_ERROR）；
`exactJSONFieldNames` 处理 embedded struct；删 `application()==nil` 死检查；`schemaCacheTTL` 命名。

**appconfig**：`Save` fsync + 显式 0600 + rename 失败清 tmp + 目录 0700；unix `flock` 重试 EINTR；
Windows 锁错误哨兵改可读文案；`lock_other` 注释降级语义；`Home`/`InstallRoot` 相对 `SOFARPC_HOME`
转绝对；`CanonicalWorkspaceRoot` 支持裸 `~` 与 Windows `~\`。

**cli**：`parseMixed` 每轮尊重 `--` 终止符；`runMCP` 先兜底 nil 流；`binVersion` 加 5s 超时；
`--uninstall` 如实报告删除失败；Windows 二进制替换改 rename-aside（可替换运行中 exe）；去重
`ProjectNames/ServerNames` 的重复排序；删 `ensureScaffold` 死参数。

**ci / scripts**：setup-go 缓存键含 `oracletest/go.sum`；CI 与 release 门禁补 oracletest vet；
release 门禁补 gofmt+vet；`package.sh` 加 `CGO_ENABLED=0`；install.ps1 强制 TLS1.2、`-Version`
缺值报错、未知参数走 stderr+usage+exit（避免 `Write-Error` 在 Stop 下抛出跳过后续）。

## 复查后确认「已被前序工作修复」（本轮无需改动）

- `Load` 尾随内容：版本闸门的 `json.Unmarshal` 已拒绝（实测 `{}garbage`/`{}{}`/`{} trailing` 均报错）。
- Windows PATH 提示：`selfinstall.go` 已用 `[Environment]::SetEnvironmentVariable(...,'User')`，非 `setx %PATH%`。
- `\f` 换页符、BOM、Unicode 标识符、`eraseGeneric` 多维、`lookupPath` 数组下标、阈值 200 prose 常量化：
  分别在 Phase 3/4/5 与收敛分支已修。

## 后续补做（本轮之后单独立项完成）

- ✅ **writer 标量 tag 编码收敛**：加共享 `writeBool`，抽出文档化的 `writeUntypedScalar`
  作为 `writeJavaScalar` 的无类型兄弟,两条路径归一到同一组底层 primitive。纯内部重构,
  BOLT oracle + Hessian golden 字节级不变。
- ✅ **javaparser 嵌套类 keying**：`emitTypeSchemas` 改真实 FQN 主键(`pkg.Outer.Inner`)
  + 短名别名(仅未占用时)。修掉同包顶层被嵌套类静默覆盖、以及 `import a.b.Outer.Inner`
  不可解析两个 bug;`TypeSchema.Type` 保持短名,不动 Describe/wire 契约。带三条回归测试。

## 本轮有意未做（价值低或需更大改动，留后续）
- **search.go 跨调用 token 预索引**：真正的跨调用缓存需在 Index 上加可变状态（并发 + cache schema 版本），
  收益（小型 in-memory 方法列表）不抵风险；本轮只做了查询内的清晰化。
- **prompt/resource handler panic 兜底**：两 handler 只 marshal 包级静态数据，实测不可 panic；加兜底需
  向 `AddInvokeWorkflowPrompt`/`AddCompatibilityResource` 穿 stderr，无 stderr 的兜底反而静默吞异常，
  收益≈0，暂不做。
- **app 层 named/ordered 互斥与未知参数名校验**：互斥现只在 MCP 层;app 层加是为未来 CLI invoke 防御，
  当前无 CLI invoke 入口，等真实入口落地再加。
- **`boundServers` 类型化契约、`details["kind"]` 侧信道、`InvocationPlan.Display` 的 paramTypes/argTypes
  别名、install/setup 的 flag 透传与 claude scope 贯穿**：属中等重构或 API 形态调整，非纯打磨，留后续迭代。
- **golden 的 Pos 序列化噪声**：测试基建改动，收益有限。
- **依赖常规升级、注释语言统一**：非本轮范围（见原报告与各设计文档的「明确不做」清单）。
