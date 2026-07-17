# 项目深度分析报告（2026-07）

> 本文是对 sofarpc-mcp 全仓的一次深度代码评审，审计基线为
> `main@ccabc60`（2026-07-17）。覆盖 direct 编解码、
> javaparser/schema、app/javavalue、mcp/presentation、cli/appconfig 与脚本/CI
> 六个面。高优先级发现均经调用链读码核验；尚未固化成自动化回归测试的复现，
> 统一列入配套实施计划，不以“已复现”替代可重复证据。
> 本文是**过程记录**，不覆盖 `docs/decisions.md` 的既定决策（D1–D17）；
> 与决策冲突处以 decisions.md 为准。

## 总体评价

这是一个**工程素养明显在线**的项目：分层清晰且有 `internal/arch/boundary_test.go`
用 `go list` 做依赖方向门禁；手写的 BOLT/Hessian2 编解码用真实 oracle（BOLT 常驻
CI、Hessian 发布前门禁）交叉验证，且 `oracle-gate.sh` 把 SKIP 当 FAIL 处理；wire
契约用 `apifreeze_test`/`TestFinishWireShape` 冻结；config 原子写 + 0600 + 持锁读改写；
MCP stdio 严守 stdout=协议流、诊断走 stderr；`decisions.md` 作为决策单一事实源。
这些都是清醒且执行到位的设计。

但深挖之后，暴露出**四类系统性问题**，其中两类含可被远端输入或普通输入触发的
正确性/健壮性缺陷：

| 类别 | 性质 | 代表后果 |
|------|------|----------|
| A. 解析失败=文件静默蒸发 | 正确性（零观测） | 一个 `<` 比较 / BOM / 中文类名就让整个 DTO 从 schema 索引消失，Describe 只剩 unresolved 短名，无任何诊断 |
| B. 不可信长度直接分配 | 安全/健壮 | 畸形远端响应触发进程级 OOM（`recover` 拦不住）或 makeslice panic |
| C. Windows 安装链路完全不可用 | 正确性 | `$Host` 只读变量 + zip 布局错位，一键安装两处断裂 |
| D. 平行实现已漂移（主导型代码味道） | 可维护性 | 同一逻辑在多个包各写一份、已开始行为分叉，改一处漏一处 |

下文先按这四类主题展开（最高价值），再补类型映射/配置健壮性/诊断逃生舱三块，
最后给出分包发现清单与建议修复优先级。

优先级口径：

- **P0**：可由运行时输入触发进程不可恢复崩溃、数据破坏或安全边界突破，需立即修复。
- **P1**：支持平台不可用、静默错误结果或关键诊断链路失效，属于发布阻断或近期修复项。
- **P2**：有限场景缺陷、性能/可维护性问题，纳入后续迭代。

其中 Windows 安装问题按 **P1/发布阻断** 管理：它不造成已运行实例崩溃，但在项目公开承诺
Windows 支持的前提下，修复前不应发布新版本。

---

## A. 解析失败 = 整文件静默蒸发（javaparser/schema）

这是 schema 链路**单点价值最高**的正确性问题：schema 提取链路对任何解析错误一律静默丢弃整个
文件，而承接诊断的字段（`Description.Warnings`、`SourceRoot`，`schema.go:71-72`）声明后
从未赋值，现成的观测通道闲置。触发面比想象宽：

- **[P1] `skipFieldInitializer` 的 `<` 启发式在合法比较表达式上误判**
  （`decls.go:777-815`）。字段初始化器含 `size < limit` 之类裸比较时，`<` 前是 ident
  被计入 `angleDepth`，随后 `;` 在 `angleDepth>0` 时被**消费而非终止**（`decls.go:783-788`），
  且无 token 能让 depth 归零，一路吞到 EOF 报 `field initializer hit EOF`。注释
  （`decls.go:775-776`）把影响写成"该字段误算"，实际爆炸半径是**整文件丢失**。
  修：`case TokenSemicolon` 应无条件 `return nil`（表达式里不会出现裸 `;`，lambda/匿名类体
  已被 `{}` balanced-skip 兜住），`,` 保留 angle 追踪即可。
- **[P1] parser 无声明级错误恢复 + schema 层静默吞错**（`parser.go:59-62`、`decls.go:259-261`、
  `schema.go:130-133,156-159`）。任一 member 出错沿链上抛，`gatherCompilationUnits` 直接
  `return nil` 丢文件。修：`parseMember` 失败时 `skipUntil(Semicolon/RBrace)` 继续解析其余成员，
  并把跳过的文件数/名经 `Warnings` 透出。
- **[P1] lexer 不支持 Unicode 标识符与 UTF-8 BOM**（`lexer.go:33-47,126-132`）。`class 用户服务 {}`
  报 `expected type name`；文件头 BOM 报 `got Other "ï"`——两者都是合法 Java，结果同样整文件消失。
  修：起始跳 BOM；`isIdentStart/Part` 对 ≥0x80 字节走 `utf8.DecodeRune`+`unicode.IsLetter`。
- **[P1] `? extends X`/`? super X`/类型变量在 Describe 输出泄漏为伪类型**（`schema.go:310-335`）。
  `referencedTypes` 按字母切词只过滤 `isBuiltin`，`List<? extends UserDTO>` 切出 token `extends`；
  `<T> T echo(T in)` 泄漏伪类型 `T`——而 `Method.TypeParams`（专为识别 type variable 加的字段）
  在 Describe 里完全没用。修：过滤 `extends`/`super`，剔除 ∈ `TypeParams` 的 token。
- **[P1] Describe 把"method 不存在"误报成"service 不存在"**（`schema.go:238-240`），
  agent 会去重找 service 而不是修 method 名。修：先判 service 存在，再区分 `method not found`。
- **[P2] 相关**：`\f` 换页符被当 `TokenOther`（`lexer.go:36-44`）、转义+换行吞行号漂移
  （`lexer.go:299-303` 等）、`Col` 按字节计数中文行列号不准、`parseTypeRef` 放行任意关键字当类型名
  （`typeref.go:33-36`，`public if broken();` 能解析出返回类型 `if`）、嵌套类 flat-key 冲突
  与 `import a.b.Outer.Inner` 失效（`adapter_javaparser.go:24-25,238`）。

---

## B. 不可信长度直接分配 → 进程崩溃（direct 编解码）

BOLT 帧体虽限制在 `maxResponseBytes = 16MB`（`invoke.go:32,329`），但 Hessian **内层**的
长度字段读的是完整 int32，`make` 的容量由这个不可信字段驱动，而非实际内容大小——
一个约 6 字节的列表头即可让 `make` 尝试分配约 32GB。三处同源缺陷：

- **[P0] `reader.list()`**（`hessian_reader.go:197-198`）：`n` 来自 `length()` 的 int32 分支
  （可达 `0x7fffffff`），`make([]interface{}, 0, n)` 触发 `fatal error: out of memory`——
  `recover()` **无法捕获**，整进程崩溃。经 `decodeSofaResponse→readValue→list` 可由畸形响应直达。
- **[P0] `reader.classDef()`**（`hessian_reader.go:302`）：`make([]string, n)`，`n` 来自 `intValue()`
  可为负（makeslice panic）或超大（OOM）。`O` tag 在每个 typed object 前都出现，极易触达。
- **[P0] `reader.fixedList()`**（`hessian_reader.go:247`）：`make([]interface{}, 0, n)` 完全不校验
  `n` 符号，负数触发 `makeslice: cap out of range`。

> 三处是同一类缺陷，建议统一封装容器预算校验：除 `0 <= n <= 剩余可读字节数` 外，
> 还必须限制单容器元素数和单次解码累计分配预算。仅按剩余字节校验仍允许 16MB 的紧凑值
> 诱导出数百 MB 的 `[]interface{}` 分配。修复后补一批畸形/截断/超大响应负面测试 +
> `FuzzReadValue`。当前 direct 包对这些
> 路径**零负面覆盖**（包覆盖率 59.7%）。

同层其它：
- **[P1] BOLT 响应 `Status` 读出后从不校验成功性**（`invoke.go:340`，仅塞进 diagnostics `:437`）。
  服务端返回 ServerException/ThreadPoolBusy/Timeout 时，代码仍把可能为空的 content 当正常响应
  Hessian 解码，错误信息丢失。修：定义 status 成功常量，非成功直接返回类型化错误。
- **[P1] writer 标量处理两处重复**（泛型 `switch`（`hessian_writer.go:65-125`）vs
  `writeJavaScalar`（`:333-416`）），已导致无类型路径把整值 float64 编成 Long 而非 Double
  （`hessian_writer.go:102-106`），泛型 map/list 里的 `Double` 元素可能类型不匹配。
- **[P2]**：reader 递归深度用魔法字面量 `128`（`hessian_reader.go:73`）而 writer 用具名
  `maxHessianDepth`（`hessian_writer.go:16`）；`encodeBoltRequest` 的 error 返回是死代码却
  伴随 length 字段静默截断（`invoke.go:254-256` 的 `uint16(...)`）；`decodeSimpleMap` 越界静默
  吞掉后续 header（`invoke.go:361-376`）。

---

## C. Windows 安装链路完全不可用（scripts）

发布产物含 windows zip + install.ps1，但一键安装两处断裂，且 CI 无 Windows 作业
（`ci.yml:19` 矩阵只有 ubuntu/macos），手写的 `LockFileEx`（`lock_windows.go`）从未被执行过。

- **[P1/发布阻断] `install.ps1` 对只读自动变量 `$Host` 赋值**（`install.ps1:30,38-40,64,115`）。
  `$Host` 是 PowerShell 内置只读变量（宿主对象），赋值抛 `Cannot overwrite variable Host`，
  且 `if ($Host)` 恒真。修：改名 `$TargetHost`，并加一条 `pwsh -File scripts/install.ps1 -h` CI 冒烟。
- **[P1/发布阻断] Windows zip 布局与 install.ps1 期望不一致**（`package.sh:48` 从 `WORK_DIR` 内部
  `zip -qr "$archive" .`，产物无顶层目录；而 tar 路径 `:52` 带顶层目录；install.ps1 期望
  `sofarpc-<ver>-windows-<arch>/sofarpc.exe`）。修：改为 `(cd "$DIST_DIR" && zip -qr "$archive" "$base")`，
  并在 CI 对两种归档做布局断言。
- **[P1] install.sh 用 `exec` 移交控制权，EXIT trap 永不执行，每次安装泄漏临时目录**
  （`install.sh:63` 设 trap，`:67/:69` 用 `exec` 替换进程镜像，bash 不跑 EXIT trap）。
  网络路径每次安装在 `/tmp` 残留约 10MB。修：改普通调用 `"$bin" install "$host"; exit $?`。
- **[P1] 其它**：install.ps1 的 latest-tag 解析依赖 .NET Framework 专有 `ResponseUri`，PS7 失效
  （`:83`）；Windows PATH 提示 `setx PATH "%PATH%;..."` 在 PowerShell 下不展开 `%PATH%`，破坏环境
  （`selfinstall.go:217`）；发布矩阵缺 arm64 而安装脚本会去下 arm64（`package.sh:17` vs `install.sh:82`）；
  `.mcpb` 产物哈希未并入发布 `SHA256SUMS`（`build-mcpb.sh:145` vs `release.yml:46`）。
- **[P1] CI 应加 `windows-latest` 跑 `go test ./...`**（纯 Go，代价小），否则 `lock_windows.go`
  的 unsafe/syscall 与 `copyExecutable` 的 rename 回退零覆盖。

---

## D. 平行实现已漂移（主导型代码味道）

全仓最普遍的坏味道：同一逻辑被复制到多处，且**已经开始行为分叉**，属于"改一处漏一处"
的活跃隐患，而非无害重复。按包收敛价值排序：

- **resolveProject/resolveServer 双份**（`app/resolve.go:76,161` vs `mcp/tools/helpers.go:34,72`）——
  已知 backlog 3b。app 版返回带 `Kind`/`Details` 的 `DomainError`，helpers 版返回朴素
  `fmt.Errorf`；doctor 走前者、resolve/invoke 走后者。app 版**严格更优**，helpers 应委托过去。
- **describe/doctor 绕过 `app.Service` 注入点**（`mcp/tools/helpers.go:12-18` 直读全局 config +
  `schema.LoadOrBuildIndex`），而 resolve/probe/invoke 走可注入的 `appSvc`——同一 server 实例
  两组工具数据源可能分叉（`view.go:100-105` 注释自己都指出这点）。这是 3b 的**相邻但不同**问题。
- **`eraseRPCGeneric`/`isByteArrayType` 在 app 与 direct 两包平行**（`app/rpc_types.go:298-311`
  vs `direct/hessian_writer.go:446-461`）：`isByteArrayType` 逐字重复；`eraseRPCGeneric` 只剥一层
  `[]` 且剥 `final `，`eraseJavaType` 循环剥 `[]` 但不剥 `final `——多维数组与 `final` 前缀行为已分叉。
  两包都依赖 `javavalue`，应把这对函数移入 javavalue。
- **rpc_types.go 内三套并行类型解析链**（`resolveBaseType` `:113`、`rpcParamTypeForMethod` `:188`、
  `rpcFieldTypeForType` `:226`）是同一条链（builtin→含"."→imports→typeParams→pkg fallback）三份手抄，
  但只有 `resolveBaseType` 有 same-pkg lookup 与 type-variable 兜底。漂移：`rpcParamTypeForMethod("T")`
  在老 schema cache（`TypeParams=nil`）时返回 bogus FQN `com.x.facade.T` 并进 wire。应收敛为一个
  参数化 resolver。
- **`parseTypeDecl` ≡ `parsePreamble`+`parseTypeDeclWithPreamble`（约 75 行整段复制）**
  （`decls.go:104-214` vs `:386-479`），含两份 `goto bodyStart`；已漂移：前者校验
  `TokenAt` 后跟 `interface`，后者盲吃两 token。修：`parseTypeDecl` 一行委托。这是 decls.go
  860 行的主要复杂度来源，去重后 ~765 行无需强拆。
- **config.json 损坏在不同工具映射出三种错误码**（`config_sdk.go:16` 保 `CONFIG_INVALID`、
  `describe_sdk.go:32` 压成 `INTERNAL_ERROR`、`resolve_sdk.go:29`/`invoke_sdk.go:31` 压成
  `BAD_REQUEST`）——按 advice 表，BAD_REQUEST 的 recovery 是"改工具参数"，对配置损坏是错误指引。
  修：失败出口统一 `errors.As(*appconfig.ConfigError)` 路由到 `RenderConfigFailure`。
- **工具注册样板**：read-only 四字段 annotations 字面量 5 处逐字复制、12 个 `Add*` 逐个穿
  `stderr`、`invokeInputSchema` 与 `invokePlanInputSchema` 约 90% 文本重复（`invoke.go:67-119`，
  P 见下节 plan schema 漂移正是此双份维护的产物）。建议 `readOnlyAnnotations()` 预设 + 持有
  `stderr`/`appSvc` 的 registry。
- **javaparser 内四份 annotation-skip 拷贝**（`parser.go:72-92,110-143`、`decls.go:77-99`、
  `typeref.go:85-107`），且 typeref 里"复用会循环依赖"的理由是错的（同包无循环依赖）。
- **`*_sdk.go` 双文件已是迁移遗迹**：8+ 处注释引用已删除的 `ProbeTool`/`ResolveTool`
  （`sdkserver.go:20-27` 等），双文件收益已消失（`describe.go` 35 行 + `describe_sdk.go` 77 行可合一）。

---

## E. RPC 类型映射的静默错值（app）

这一组不报错、但把错误数据发上 wire，业务侧静默拿到 null/异常——排查成本最高。

- **[P1，最接近 P0] Map key 声明类型被忽略，一律硬编码 `java.lang.String`**
  （`rpc_typedvalue.go:78-89`：正确取了 `args[1]` 做 value 类型，但 key 写死 String，`args[0]` 丢弃）。
  `Map<Long, TagItem>` 的 key "7" 以 String 上线，provider 不做 key 强转则 `map.get(7L)` 得 null。
  修：用 `args[0]` 对 key 做与 value 相同的 `typedValueForJavaType` 强转。
- **[P1] 单参数方法的 whole-DTO 回退吞掉参数名拼写错误**（`invoke.go:284-293`）。`getUser(String id)`
  传 `{"userId":"u1"}` 会静默进入整体-DTO 模式把 map 按 String 编码，而非报 `missing argument "id"`。
  修：仅当 `param.Type` 解析为 class（非 JDK scalar）时才允许 whole-DTO 回退。
- **[P1] ordered 路径 schema 命中后仍把用户原始短名 paramTypes 发上 wire**（`invoke.go:311,328-348`）：
  参数树被正确类型化，但 `plan.Method.ParamTypes` 仍是 `"UserRequest"`，远端按非法类名解析必失败。
  修：schema 命中后统一返回 `rpcParamTypesForMethod(methodSchema)`。
- **[P1] 泛型继承绑定遇无 schema 的外部类编出错误 FQN**（`rpc_inherited.go:73-77,85-111`）：
  `OrderDTO extends Base<ForeignDto>`（ForeignDto 无 schema）→ 继承字段类型化为 `com.x.base.ForeignDto`
  bogus 类名。修：`resolveTypeTokensToFQN` 在 types miss 时继续查 `owner.Imports[name]`。
- **[P1] java.time/BigInteger 的 Hessian wire 知识错位在 app 层**（`rpc_specialtypes.go:52-81,116-130`
  手工构造 handle/mag 布局），与 direct 层的 BigDecimal/Date 编码割裂，迫使 app 发明 `validateSpecialArgs`
  补偿机制，新增一种类型要改两处（`java.time.LocalTime` 有 handle 构造器却无编码/校验路径，`:25-40`——
  `"10:30:00"` 会原样发上 wire 炸远端）。修：handle/mag 构造下沉 direct，app 只留 plan 期校验。
- **[P2] errorCode 分类兜底**（`errors.go:59-91`，D13 保留字符串嗅探的前提下评估实现）：
  `context.Canceled` 无分支被归为 `INVOKE_FAILED`；`strings.Contains(msg,"timeout")` 过宽；
  同一 dial 超时 invoke 得 `CONNECT_FAILED`、probe 得 `RPC_TIMEOUT`（`probe.go:72,100`）不一致；
  且该 switch **零测试**（app 包 64.2%，此函数 0%）。修：显式 `context.Canceled` 分支、收窄嗅探片段、
  统一 dial 超时语义、补表驱动单测。

---

## F. 配置与迁移健壮性（appconfig）

原子写与锁的骨架是对的，但迁移与并发窗口有洞：

- **[P1] v1→v2 迁移把推断的 `server.Profile` 持久化为悬空引用**（`config.go:531-534`）：迁移后磁盘
  `version:2` 且 server 带 `profile:"test"`，但 project 的 `profiles` 为空、`activeProfile` 为空，
  随后 `project use` 报 `profile "test" not found`。修：迁移时反向物化 `project.Profiles[profile]`。
- **[P1] `DisallowUnknownFields` 让 `CONFIG_UNSUPPORTED_VERSION` 门禁形同虚设**（`config.go:153` 的严格解码
  在 `:157` 版本闸门之前）：未来 v3 新增任何字段，旧二进制先在 Decode 报 `CONFIG_INVALID("unknown field")`，
  精心准备的"版本过新"提示永不触发。修：先宽松解 `{"version":int}` 做闸门，再严格解全量。
- **[P1] `configForSave` 浅拷贝改写调用方 map，且 `Version` 不回写**（`config.go:603-623`）：`Save` 就地
  改写调用方 `Servers`/`Projects`，而标量 `Version` 只改本地副本——`Update` 对 v1 文件返回 `Version=1`
  磁盘却已是 2，状态半新半旧。修：`configForSave` 深拷贝，`Update` 显式返回规范化后的最终配置。
- **[P1] 派生 server 名跨项目静默冲突且不确定**（`config.go:450-455` 的 `project+"-"+profile`，名字允许 `-`）：
  项目 `a`/profile `b-c` 与项目 `a-b`/profile `c` 都派生为 `a-b-c`，`applyDefaults` 按 map 迭代序覆盖，
  每次加载谁赢不确定，invoke 可能静默打到错端点。修：在 `AddProfile`/`validateLoadedConfig` 检测冲突报错。
- **[P1] `ensureScaffold` 绕过文件锁的 check-then-write TOCTOU**（`selfinstall.go:134-139`，全仓唯一
  绕过 `lockConfig` 写真实 config 的点）：并发 MCP 会话恰在窗口期写入用户数据会被默认配置整体覆盖。
  修：提供持锁的 `appconfig.EnsureExists(path, lockPath)`。
- **[P1] cache.go 无跨进程并发防护**（`cache.go:38-42,71-84,149-159`）：`writeCache` 非原子 `os.WriteFile`，
  命中路径为 bump `LastAccessedAt` 整体重写多 MB JSON，`CleanupUnused`（每次 MCP 启动跑，`server.go:51`）
  与写入存在删除竞态；多个 MCP host 共享 `~/.sofarpc`，竞态面真实。appconfig 已有 flock 助手却未用于 cache。
  修：temp+`os.Rename` 原子落盘；LRU touch 改 `os.Chtimes`/sidecar，不重写索引本体。
- **[P2] 锁实现**：`lock_unix.go:19` 的阻塞 flock 未处理 EINTR（本包大量 `exec` 宿主 CLI，SIGCHLD 可能伪失败）；
  `lock_windows.go:18` 错误哨兵 `Errno(1)` 渲染成 "Incorrect function." 误导；`lock_other.go` 是静默空锁无注释。
  建议改用 `x/sys`（已是间接依赖）封装并补注释。

---

## G. 诊断逃生舱的实现缺口（mcp/presentation）

D1–D17 的截断/rawResult/resultPath/assertions 决策方向正确，但实现有洞，偏偏落在最需要它们的场景：

- **[P1] `rawResult=true` 遇循环引用响应时整个 invoke 结果被替换为 INTERNAL_ERROR**
  （`render.go:120-124` 对含循环 map 的 `exec.Data` 做 `json.Marshal` 返回 cycle 错误 → 整个成功 RPC 结果
  被 `RenderFailure` 丢弃，`nextTool` 还指向无关的 doctor）。而兼容矩阵（`resource_sdk.go:41`）声称
  "Cyclic responses: supported" 且推荐用 `rawResult=true` 诊断——最需要它的场景全军覆没。
  修：rawResult 输出前做只切环、不改数组的拷贝（复用 flatten 的 seen 思路打 `$circularRef`），
  或 marshal 失败时降级为丢弃 rawResult、保留其余 data 并附 warning。
- **[P1] 断言比较 `valuesEqual` 的 `fmt.Sprint` 兜底产生跨类型假阳性**（`result.go:538-542`）：
  `cloneJSONValue` 用 `UseNumber` 归一化后，`json.Number("1")` 与 `"1"` 经 `fmt.Sprint` 都是 `"1"`，
  实测 `1=="1"`、`true=="true"`、`nil=="<nil>"`、`{"a":1}=={"a":"1"}` 均判相等。断言是用户显式的
  正确性检查，假通过会静默掩盖类型回归，且此宽松语义无测试锁定。修：删 `fmt.Sprint` 兜底，或收窄为
  仅 `json.Number` 间数值等价。
- **[P1] invoke_plan 的 input schema/描述/handler 三方不一致**（`invoke.go:102-119` 声明
  `additionalProperties:false` 且不含 `rawResult`/`assertions`/`resultPath`，但 handler 复用含这三字段的
  `InvokeArgs`，raw `AddTool` 不校验输入 → 静默接受）。而描述承诺"与 invoke 参数相同"，严格校验输入的
  宿主会拒绝这条官方推荐工作流。修：schema 追平描述，加"schema properties ≡ Args struct 字段"守卫测试。
- **[P2] resultPath 不支持数组下标**（`result.go:508-530` 只 map key 步进），而截断恢复话术是
  "use resultPath to narrow"——对被截断的数组本身 narrow 后仍会再截断，第 200 项后除 rawResult 外不可达。
  修：支持 `$.list.0` 下标，或把话术改指向 rawResult。
- **[P2] 阈值 200 的 prose 三处硬编码**（`sdkserver.go:18`、`invoke_sdk.go:21`、`render.go:114`），
  真值是 `presentation.MaxArrayItems`，改 D1 阈值必漂移。修：用 `fmt.Sprintf` 拼接常量值。
- **[P2] 其它**：大小写守卫 `rejectInexactKeys` 只覆盖顶层、嵌套层仍大小写不敏感（`sdk.go:169-185`）；
  `TruncateArrays` 即使无截断也无条件深拷贝整树（`result.go:43-77`）；doctor 进度只发 0%（`doctor_sdk.go:45`）；
  doctor 任何失败 check 一律 `INTERNAL_ERROR` code（`:104-114`）；prompt/resource handler 无 panic 防护
  （工具路径有）。

---

## H. 测试盲区（能兜住上面大半的缺口）

测试整体扎实（javavalue 100%、mcp/tools 92.7%、真实 client-server 握手驱动、wire 契约冻结、
回归 pin 带来源注释），但盲区恰好对应上面的高危项：

- **手写 lexer/parser 无任何 fuzz**——A 类问题（`a<b` 级联、BOM、`\f`）全属 `go test -fuzz` 一晚能扫出的类别；
  `decls.go:775` 明文记录的 `a<b` limitation 无 pin 测试，真实爆炸半径（整文件失败）无人察觉。
- **direct 无畸形/截断/超大响应负面测试**——B 类三个 make 路径零覆盖；`readBoltResponse` 的错误 codec/
  截断/超限/非成功 status 均无测试；ctx 取消/超时路径无测试。
- **无 Windows CI**——C 类的 `LockFileEx`、rename 回退零运行覆盖。
- **appconfig 无并发 `Update` 竞争测试、无 v1→v2 迁移端到端测试**（F 类的迁移悬空引用正是漏网）、
  无 `Save` 权限位断言、`CleanupUnused` 的 `os.RemoveAll` 零测试。
- **app 包 errorCode/ExecuteInvocation/ProbeEndpoint/probeFailure 均 0%**（app 包 64.2%），
  `planOrderedArguments` 37.5%——未测的 `needsSchemaAnnotation` 成功分支正是 E 类 P1-3 所在。

建议优先补：`FuzzParse`/`FuzzReadValue`（断言不 panic/不死循环）、direct 畸形响应负面用例、
errorCode 表驱动、v1→v2 迁移后 `project use` 可用性、windows-latest 加入 CI 矩阵。

---

## 其它打磨项（不成主题但值得清）

- **staticcheck 三项**：`cursor.go:94` `matchIdentValue` 死代码（且其注释谎称"用于 non-sealed 拼接"，
  实际 non-sealed 解析在 `decls.go:41-50` 内联手写、根本没调它）；`setup_test.go:102` `isGet` 死代码；
  `hessian_reader.go:110` `tag <= 0xff` 恒真（SA4003，无害但可删）。
- **`decisions.md` 引用 5 个未纳入审计基线版本控制的历史文档**（agent-first-mcp-review.md、
  -followup.md、-feature-review.md、-review-r3-verification.md、mcp-best-practices-audit.md）。当前工作区
  存在对应未跟踪副本，但只合并已提交内容时链接仍会断。应明确选择：审阅后提交这些文档，或把状态表
  改为“已归档删除”；本次实施不擅自纳入未跟踪文件。
- **中英文注释混用**：65 个非测试文件中 14 个含中文注释，其余英文——同一仓库注释语言不统一。
- **手写 `itoa`**（`adapter_javaparser.go:155-178`）复刻 24 行整数格式化只为省一个 `strconv` import。
- **`search.go` 每次查询对每个 method 重新 `Tokenize` 全文**（`search.go:50`），应在 BuildIndex 时预计算 token 集。
- **依赖可小幅更新**：`golang.org/x/sys v0.41→v0.47`、`golang.org/x/oauth2 v0.35→v0.36` 等（非安全项）。

---

## 建议修复优先级

按“运行时影响 × 发布阻断 × 修复成本”排序：

1. **B 类三个 make 越界**（direct）——唯一能被远端输入直接触发进程崩溃的一类；一个 `checkCount` helper
   覆盖 `list`/`fixedList`/`classDef`，同时增加单容器上限和累计预算，再补畸形响应测试。**成本低、收益最高。**
2. **C 类 Windows 安装发布阻断**（scripts）——`$Host` 改名 + zip 布局对齐 + install.sh 去 `exec`（trap 泄漏）+
   windows-latest 进 CI。
3. **A 类 `skipFieldInitializer` + 解析失败可观测化**（javaparser/schema）——一个普通 `<` 比较就丢整个 DTO；
   `TokenSemicolon` 硬终止 + `Warnings` 透出跳过的文件 + 解析回归测试/Fuzz seed。
4. **E 类 Map key 类型 + whole-DTO 回退 + ordered 短名 paramTypes**（app）——静默错值，业务侧最难排查。
5. **G 类 rawResult 循环引用 + 断言严格性 + plan schema**（mcp/presentation）——恢复诊断逃生舱可信度。
6. **F 类迁移/并发**（appconfig）——v1→v2 profile 物化、版本闸门前置、configForSave 深拷贝、cache 原子写。
7. **D 类平行实现收敛**（跨层）——resolveProject/Server 委托、erase/byteArray 移入 javavalue、
   三套类型解析链参数化、parseTypeDecl 委托、config 错误码统一路由。可维护性，非紧急但持续付息。
8. 剩余 P2 打磨 + 测试补洞 + 文档腐化清扫。

> 说明：本报告只做分析、未改动任何代码。具体范围、阶段、验收命令与完成定义见
> `docs/deep-analysis-implementation-plan.md`。
