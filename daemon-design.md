# SofaRPC CLI Daemon 模式设计文档

> 作者：wuweihua · 日期：2026-04-13 · 状态：Draft v4

## 变更日志

- **v5（2026-04-13）**：针对 v4 的 Codex 四次 review
  - **Linux 支持直接下线**：不再是"本期不做"，而是"永不支持"。目标 OS 收敛到 `darwin-arm64` 唯一平台，install.sh 可以安全地覆盖二进制
  - `--overall-timeout` 单一 owner 规则对齐：删除 4.3 和失败表里残留的"客户端主动 close 连接"写法，确保所有章节只描述"daemon 到点返回 partial"一种路径
  - partial `BatchResult` schema 扩展：新增 `overallTimedOut` / `scheduledCount` / `completedCount` / `skippedCases` 字段；同时要求 CLI 输出（OutputFormatter）和 ReportGenerator 显式展示"不完整"状态
- **v4（2026-04-13）**：针对 v3 的 Codex 三次 review
  - `--overall-timeout` 改为 **daemon 单一 owner**，客户端只设 `overall + 10s` 的兜底 read timeout，由 daemon 负责返回 partial result（修复 v3 里双主导致 partial 丢失的 bug）
  - **`daemon.state` 丢失自愈**：客户端 spawn 拿不到 lock 时等 150ms 再读一次 state；daemon 每 30s 自检 state 文件存在性，缺失则重写（防止健康 daemon 因 state 被误删/损坏变砖）
  - 新增 2 节 **Known Tradeoffs**：显式登记"install 自动 restart 切断 RPC""loopback 无鉴权"两条为知情取舍，避免后续 review 反复提
- **v3（2026-04-13）**：针对 v2 的 Codex 二次 review 再次修订
  - **去掉 token 鉴权**：目标场景是"单人 Mac"，同权限进程之间本就无边界，token 挡不住真威胁（恶意软件能读文件），还引入了"token/state 发布不原子"的 bug 面。如未来放多用户机再加
  - **拆开 timeout 语义**：per-RPC timeout、IPC read timeout、overall command deadline 是三件事；客户端 socket read 不再跟 `--timeout` 挂钩；新增 `--overall-timeout` 作为 batch/invoke 的总 deadline
  - **install 自动 restart 保留**：单机测试工具，"忘记升级"比"被切断重跑"更讨厌；4.3 的"不自动杀 daemon"原则改成"**仅超时/连接中断不自动杀**，显式 `daemon restart`（含 install 触发）可以杀"
- **v2（2026-04-13）**：针对 Codex review 修订
  - 新增请求级 token 鉴权 → 在 v3 撤销
  - 启动流程加 OS 文件锁
  - PID 校验加身份核对
  - 去掉请求前的 health 预探活
- **v1（2026-04-13）**：初版

## 1. 背景与目标

### 1.1 现状问题

当前 `sofarpc` CLI 每次执行都是一次独立的 JVM 进程，冷启动耗时经实测拆分：

| 阶段 | 实测耗时 |
|---|---|
| `new ConsumerConfig()`（触发 SofaRPC 首次类加载 + SPI 扫描） | ~17s |
| `config.refer()`（Bolt 客户端 + Netty 冷启动 + 首次 TCP 建连） | ~9s |
| 真正的 RPC 调用 | <1s |
| **单命令总耗时** | **~26s** |

因此每次 `sofarpc invoke/call/ping/batch` 都要重付 ~25s "开机费"。日常测试体验极差。

### 1.2 目标

- **核心**：把常用 RPC 相关命令（`invoke` / `call` / `ping` / `batch`）的每次耗时从 ~26s 降到 **<100ms**（含 IPC 开销，不含真实 RPC 耗时）
- **客户端体验**：执行 `sofarpc` 命令时无感知启动延迟，接近 `kubectl`、`git` 等成熟 CLI 的手感
- **对存量功能零回归**：所有现有命令、参数、输出格式、退出码保持不变
- **自愈**：daemon 崩溃或被 kill 后，下一次命令自动重新拉起，不需要用户干预

### 1.3 非目标

- 不做跨机远程调用（daemon 只监听本机 loopback）
- 不做鉴权：目标是单机单用户自用工具。若将来放到多用户机器上再加 token（见 7.3 预案）
- **仅支持 `darwin-arm64`**：Linux / Windows / darwin-amd64 都不在支持范围内。不做跨平台编译。README 中原有的"Linux 支持"说明需同步删除
- 不做协议层改造（不涉及 SofaRPC Bolt/Hessian2，全部复用 SofaRPC Java 客户端）

### 1.4 成功指标

- `sofarpc ping`（warm daemon）：**<50ms 端到端**
- `sofarpc invoke`（warm daemon，不含 RPC 本身）：**<80ms 端到端开销**
- daemon 首次启动（用户视角）：**≤30s**，之后常驻
- daemon 崩溃后自愈：用户无感知，只是当次命令慢一次

---

## 2. 整体架构

### 2.1 进程拓扑

```
┌──────────────────────┐   TCP 127.0.0.1:<随机端口>    ┌──────────────────────┐
│  sofarpc             │  ───── line-delimited JSON ──▶│  sofarpcd            │
│  (Go 二进制 ~5MB)    │  ◀────── 单行 JSON 响应 ──────│  (Java 常驻 JVM)    │
│  启动 ~10ms          │                                │  内含 SofaRPC 全栈   │
└──────────────────────┘                                └──────────────────────┘
         │                                                        │
         │ 读/写                                                   │ 读/写
         ▼                                                        ▼
~/.sofarpc/servers.yaml                              ~/.sofarpc/daemon.lock
~/.sofarpc/config.yaml                               ~/.sofarpc/daemon.state
                                                     ~/.sofarpc/daemon.log
```

### 2.2 模块职责

| 组件 | 语言 | 职责 |
|---|---|---|
| **sofarpc**（客户端二进制） | Go | 参数解析；命令路由；daemon 自动拉起；IPC；结果打印；本地处理 `server *` 和 `daemon *` 管理命令 |
| **sofarpcd**（常驻进程）| Java | 监听 TCP loopback；处理 `invoke/ping/batch/report` 请求；复用 `RpcInvokeService`、`BatchCommand`、`ReportGenerator`；维护 `RpcClientFactory` 连接缓存 |
| **sofarpc.jar**（共用产物）| Java | 既可独立作为 CLI 跑（fallback / 调试模式），也可以 `__daemon__` 子命令进入 daemon 模式 |

### 2.3 为什么选择这个架构

| 决策 | 选项 | 结论 | 理由 |
|---|---|---|---|
| IPC 传输 | Unix Domain Socket / TCP loopback / Named Pipe | **TCP loopback** | JDK 8 不支持原生 UDS（JEP 380 是 JDK 16+），TCP 兼容性最好；只绑 `127.0.0.1` 不对外暴露 |
| IPC 协议 | gRPC / Thrift / 自定义 JSON | **自定义 line-delimited JSON** | 简单；调试方便（`nc` 就能手测）；Go/Java 两端都天然支持；单命令流量小，不需要压缩 |
| 客户端语言 | Java / Go / Rust | **Go** | 启动 <10ms；交叉编译方便；生态成熟；与 Java daemon 零协议风险 |
| daemon 协议 | 复刻 SofaRPC Bolt | **不做，复用 SofaRPC 原生 API** | Bolt 协议重写风险高；Hessian2 边界 case 多；复用 Java SofaRPC 客户端，daemon 内部直接 `config.refer().$genericInvoke()` |

---

## 3. IPC 协议规范

### 3.1 传输层

- daemon 启动时：
  1. 绑定到 `127.0.0.1` + 随机空闲端口（`new ServerSocket(0, 50, InetAddress.getLoopbackAddress())`）
  2. 原子写入 `daemon.state`（详见 4.1 / 4.2）
- 客户端连接前：读 `daemon.state` 解析 `port`
- 传输层：TCP，**一条命令一个连接**（短连接）。理由：实现简单；单用户场景无并发压力；daemon 崩溃/重启不会留下僵尸连接

### 3.2 消息格式

**请求**（一行 UTF-8 JSON + `\n`）：

```json
{"id":"<uuid-v4>","cmd":"invoke","args":{...}}
```

**响应**（一行 UTF-8 JSON + `\n`）：

```json
{"id":"<uuid-v4>","ok":true,"exitCode":0,"data":{...},"error":null}
```

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | string | 请求 ID，响应原样回显（方便未来做多路复用） |
| `cmd` | string | 命令名：`invoke` / `ping` / `batch` / `report` / `shutdown` / `health` / `invalidate` |
| `args` | object | 命令参数，结构因 `cmd` 而异（见 3.3） |
| `ok` | bool | 是否业务成功 |
| `exitCode` | int | 建议返回给 Shell 的退出码（沿用现有 `ExitCodes`） |
| `data` | object? | 成功时的业务返回数据 |
| `error` | string? | 失败时的错误描述 |

### 3.3 命令参数结构

#### 3.3.1 `cmd=invoke`

```json
{
  "serverAlias": "sales-test",
  "address": "10.74.194.40:12200",
  "service": "com.example.UserService",
  "method": "getUser",
  "argTypes": ["com.example.GetUserRequest"],
  "args": [{"userId": 123}],
  "timeout": 5000,
  "assertion": "$.status == 'ACTIVE'"
}
```

- `address` 由客户端先查 `servers.yaml` 解析好再传（daemon 不再读配置文件）
- `assertion` 可选

响应 `data` 结构与现有 `invoke --json` 完全一致：

```json
{"success":true,"latencyMs":45,"result":{...},"error":null}
```

#### 3.3.2 `cmd=ping`

```json
{"serverAlias":"...","address":"...","service":"...","timeout":5000}
```

响应 `data` 沿用现有 ping 的 JSON 结构（`status/server/address/service/latencyMs`）。

#### 3.3.3 `cmd=batch`

```json
{
  "serverAlias":"...",
  "address":"...",
  "casesDir":"/absolute/path/to/cases",
  "parallel":4,
  "timeout":5000,
  "overallTimeoutMs":0
}
```

- `casesDir` 必须是**客户端先解析好的绝对路径**（避免 daemon CWD 不同）
- `timeout`：**每条 case 单独的 RPC 超时**，透传到 SofaRPC `setTimeout`
- `overallTimeoutMs`：整个 batch 的总 deadline（毫秒）。`0` 表示不限制。**daemon 单独负责执行**：到点后停止派新 case，等已提交的跑完，打包 partial `BatchResult` 返回（响应里 `overallTimedOut=true`）。客户端**不在 socket 层重复执行该 deadline**，语义详见 6.3

响应 `data` 为 `BatchResult`，v5 在现有字段基础上**新增**：

| 字段 | 类型 | 说明 |
|---|---|---|
| `overallTimedOut` | bool | 是否因 `overallTimeoutMs` 截断。`false` 时下列字段可忽略 |
| `scheduledCount` | int | daemon 实际派发到线程池的 case 数 |
| `completedCount` | int | 真正跑完并有结果的 case 数（= `results.size()`）|
| `skippedCases` | string[] | 因 overall-timeout 未被派发的 case 文件名列表（相对 `casesDir`）|

**契约升级要求（必须同步改，不可只改协议）**：
- `BatchResult` Java model 增加上述 4 个字段
- `OutputFormatter` 对 `overallTimedOut=true` 的结果必须在人类可读输出里显著标注，例如头部打 `⚠ INCOMPLETE BATCH (timed out): <completed>/<total> completed, <skipped.size> not run`
- `ReportGenerator`（markdown / html）在 `overallTimedOut=true` 时，报告顶部加一段"本次 batch 未跑完"的 callout，并列出 `skippedCases`
- 用户看到 partial 结果时**不应该**以为是正常一次完整跑（避免 Codex 指出的"partial 伪装成完整"风险）

#### 3.3.4 `cmd=report`

```json
{
  "inputPath":"/abs/result.json",
  "outputDir":"/abs/reports/",
  "format":"markdown"
}
```

#### 3.3.5 `cmd=health`

无参数。daemon 立即返回：

```json
{"ok":true,"data":{"pid":12345,"uptimeMs":123456,"cachedConnections":3}}
```

用途：`sofarpc daemon status` 命令显式用。**不**用于每条业务请求前的预探活（见 4.3 的修订）。

#### 3.3.6 `cmd=shutdown`

daemon 回一个 `{"ok":true}` 后优雅退出：关闭连接池、`RpcClientFactory.destroyAll()`、清理状态文件、System.exit(0)。

### 3.4 错误约定

- 所有业务错误走响应体的 `ok=false` + `error` 字段，**不走 TCP 断开**
- daemon 内部异常：打日志 + 返回 `{"ok":false,"exitCode":1,"error":"internal: ..."}`
- 非法请求（JSON 解析失败 / `cmd` 未知）：`{"ok":false,"exitCode":3,"error":"bad request: ..."}`
- daemon 不可达：客户端负责自愈（见 4.3）

---

## 4. 生命周期与状态管理

### 4.1 状态文件

| 文件 | 内容 | 写入方 | 权限 |
|---|---|---|---|
| `~/.sofarpc/daemon.lock` | 空文件，仅用于 `flock` 启动互斥 | daemon 启动时持有独占锁 | 0600 |
| `~/.sofarpc/daemon.state` | **单一状态文件**（JSON）：`{"pid":...,"port":...,"startedAtNs":...,"cmdFingerprint":"..."}` | daemon 启动完成后原子写入；退出时删除 | 0600 |
| `~/.sofarpc/daemon.log` | 滚动日志（INFO/WARN/ERROR） | daemon 持续追加 | 0600 |

**为什么 pid 和 port 合并到单文件**：避免两个独立文件 + 非原子 check-then-act 的竞态。单文件用 "写临时文件 + rename" 原子可见。

**`cmdFingerprint` 字段**：daemon 启动时读自己的 `/proc/self/cmdline`（macOS 用 `ps -p $$ -o command=`），取 `java -jar <absolute-path>/sofarpc.jar __daemon__` 的 SHA-256 前 16 位。用途：在发信号（尤其 SIGKILL）前核对目标 PID 的命令行确实是 daemon，防 PID 复用误杀（应对 Codex review 第 3 条）。

**`startedAtNs`**：daemon 启动时的高精度时间戳。`sofarpc daemon status` 用来算 uptime；另一层防 PID 复用的保险。

### 4.2 daemon 启动流程

1. `java -jar sofarpc.jar __daemon__` 进入 daemon 模式
2. **打开 `daemon.lock`，尝试 `FileChannel.tryLock()` 独占锁**
   - 拿不到 → 说明有另一个 daemon 正在启动或已运行：打印 `another daemon holds the lock`，exit 1（活着的 daemon 的自检线程会在 ≤ 5s 内补回 state，见步骤 7）
   - 拿到 → 继续，锁保持到进程结束
3. **在锁内**读 `daemon.state`（如果存在）
   - 存在且 PID 还活着 → 对比 `cmdFingerprint` 和当前进程的 fingerprint
     - 一致 → `already running at pid=<pid>`，exit 1
     - 不一致 → PID 已复用给无关进程，视为陈旧，清理
   - 存在但 PID 已死 → 清理
4. 绑定 `127.0.0.1:0`，获取实际端口
5. 启动工作线程池（`newFixedThreadPool(16)`）
6. **原子写入** `daemon.state`（临时文件 → `rename`，确保对外可见瞬间即完整）
7. 启动一个 **state 自检定时线程**（v4 新增）：每 **5s** 检查 `daemon.state` 文件是否还在且内容与当前进程一致；不在/损坏则立即重写（临时文件 + rename）。防止 state 被误删/磁盘满损坏导致健康 daemon 不可达。5s 间隔与客户端重试窗口配合（见 4.4），确保最坏情况下用户 ≤ 5.2s 能连到 daemon
8. 注册 JVM shutdown hook：`destroyAll()` 连接 → 删 `daemon.state` → 释放锁
9. 进入 accept 循环

### 4.3 客户端自愈逻辑

```
读 daemon.state 文件
    ├─ 不存在/损坏 → spawn daemon → 轮询等 state 出现（30s 超时）→ 连 + 发请求
    ├─ 存在 → 解析 pid/port/fingerprint
    │       ├─ 核对 pid 对应的进程 fingerprint（若能读到）
    │       │   ├─ 不匹配 → daemon 挂了且 PID 被复用：清理 state → spawn → 等 → 连
    │       │   └─ 匹配或无法读取 → 继续
    │       └─ 连 TCP
    │             ├─ connect 失败（ECONNREFUSED）→ daemon 死了：清理 state → spawn → 等 → 连
    │             └─ connect 成功 → 发业务请求 → 读响应（socket read 超时 = overall-timeout + 10s 兜底）
    │                   ├─ 读到 TCP reset / EOF → 报错给用户，不自动重拉
    │                   ├─ socket read 兜底超时（daemon 对 overall-timeout 不响应）→ 视为 daemon 卡死：报错给用户
    │                   ├─ 正常响应且 overallTimedOut=true → 输出 partial 结果 + 警示
    │                   └─ 正常响应 → 返回给用户
```

**关键原则**：
- TCP connect 成功即进入业务请求，liveness 由真实响应判定（不再做 `cmd=health` 预探活）
- **客户端不在 socket 层执行 overall-timeout**。daemon 是唯一 owner，客户端老实等 daemon 返回（配 `overall-timeout + 10s` 兜底 read deadline，仅防 daemon 卡死）。详见 6.3
- **超时/连接中断不自动杀 daemon**：daemon 可能只是忙不是死；且业务 RPC 可能已经到服务端，自动重试会导致重复调用。报错给用户，让用户决定下一步
- **显式 `sofarpc daemon restart`（包含 `install.sh` 升级触发）可以杀**：用户/运维动作视为授权。2s 优雅超时后 SIGTERM，再 2s 后 SIGKILL；发信号前必须核对 `cmdFingerprint`（见 5.3）
- 真正的自动 spawn 只在"connect 阶段就失败"（端口明确没监听）时触发

### 4.4 spawn 细节

客户端（Go）用：

```go
cmd := exec.Command("java", "-jar", jarPath, "__daemon__")
cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}  // 脱离终端
logFile, _ := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
cmd.Stdout = logFile
cmd.Stderr = logFile
cmd.Start()  // 不 Wait
```

然后轮询 `daemon.state` 文件，每 200ms 查一次，最多等 30s。读到后额外校验一次 pid 的 fingerprint 防止是陈旧文件。

**spawn 失败的特殊处理（v4 新增，应对 Codex 指出的"state 丢但 daemon 活着"）**：

若 `exec` 出来的 daemon 进程很快退出（日志里是 `another daemon holds the lock`），说明**有健康的 daemon 正在持锁但 state 文件丢了**。处理：

1. 轮询 `daemon.state` 每 500ms 一次，最多等 **6s**（大于 daemon 端 5s 的自检周期 + 1s 安全垫）
2. 出现 → 正常走连接路径
3. 仍没有 → 报 `failed to locate a live daemon, run 'sofarpc daemon status' to diagnose` 并退出，给用户手动介入的机会（避免无限重试）

客户端 socket read timeout 设置见 6.3：`overall-timeout + 10s`，兜底用。

### 4.5 daemon 空闲退出策略

**决定**：**不自动退出**。理由：
- 单机自用场景，常驻成本低（~300MB 内存）
- 自动退出会引入"用户突然等 26s"的体验退化
- 显式 `sofarpc daemon stop` 给有洁癖的用户

（如果后续运营中发现内存压力大，可以加一个"空闲 60 分钟后主动退出，下次请求自动拉起"的选项，由 `config.yaml` 开关控制。）

### 4.6 daemon 的 RpcClientFactory 缓存策略

**变更**：daemon 内部的 `RpcClientFactory` 缓存**永不过期**，但暴露以下清理点：
- `cmd=shutdown` 时 `destroyAll()`
- `sofarpc server remove <alias>` 时，客户端额外发一个 `cmd=invalidate`（参数：address），daemon 收到后调 `destroyByAddress(address)`
  - 避免用户改了配置但 daemon 还拿着旧连接

---

## 5. 命令路由

### 5.1 哪些走 daemon

| 命令 | 走 daemon？ | 理由 |
|---|---|---|
| `sofarpc invoke ...` | ✅ | 核心 RPC 命令 |
| `sofarpc call ...` | ✅ | 同上 |
| `sofarpc ping ...` | ✅ | 同上 |
| `sofarpc batch ...` | ✅ | 同上，且 daemon 内能复用连接 |
| `sofarpc report ...` | ✅ | 简单，顺手让 daemon 做（避免 Go 端再写一份模板引擎） |

### 5.2 哪些客户端本地处理

| 命令 | 为什么本地 | 如何实现（Go 端） |
|---|---|---|
| `sofarpc server add/list/remove/export/import` | 只是读写 `servers.yaml`，无需 JVM | Go 直接操作 YAML 文件（`gopkg.in/yaml.v3`）|
| `sofarpc daemon start/stop/status/logs` | 要直接管进程，不能依赖 daemon 自己 | Go 读 pid/port + 发信号 |
| `sofarpc --version` / `--help` | 纯文本输出 | Go 硬编码 |

### 5.3 `sofarpc daemon` 子命令详规

| 命令 | 行为 |
|---|---|
| `daemon start` | 幂等。若已运行则打印 pid/port；否则 spawn 并等待就绪 |
| `daemon stop` | 读 pid，先发 `cmd=shutdown`（优雅）；2s 超时 fallback 到 SIGTERM；再 2s fallback 到 SIGKILL |
| `daemon status` | 输出 JSON：`{"running":true,"pid":...,"port":...,"uptimeMs":...,"cachedConnections":...}` |
| `daemon logs [--follow]` | 输出 `~/.sofarpc/daemon.log`，`--follow` 时 `tail -f` |
| `daemon restart` | `stop` 后 `start` |

### 5.4 透明 fallback（no-daemon 开关）

提供 `--no-daemon` 全局标志：

```
sofarpc --no-daemon invoke ...
```

走老路径：Go 客户端 exec 本地的 `java -jar sofarpc.jar invoke ...`，每次新 JVM。用于：
- 调试 daemon 本身出 bug 时
- CI 单次跑，不想留下后台进程
- 环境变量 `SOFARPC_NO_DAEMON=1` 等效

---

## 6. 并发模型

### 6.1 daemon 端

- Accept 线程：单线程循环 `accept()`
- Worker 线程池：`newFixedThreadPool(16)`，每个连接分到一个线程处理（因为是短连接 + 一问一答）
- `RpcClientFactory` 已经是线程安全的（`ConcurrentHashMap`），直接共享
- `ServerStore` 文件读写本身线程安全（原子 rename），但并发测试用例里从多 worker 同时读同一 YAML 没有问题

### 6.2 批量场景（`sofarpc batch --parallel N`）

- 客户端发一条请求 `cmd=batch`，参数含 `parallel`
- daemon 在**该请求的处理线程内部**再起一个线程池跑用例，和现有 `BatchCommand` 完全一致
- 客户端只阻塞等 daemon 返回整个 `BatchResult`

### 6.3 超时语义（v3 新增，v4 修订单一 owner）

**三个独立的 timeout**，不能混用：

| 概念 | 作用范围 | 默认值 | 用户怎么配 | 实现层 |
|---|---|---|---|---|
| per-RPC timeout | 单条 RPC 调用上限，透传给 SofaRPC `ConsumerConfig.setTimeout` | 5000ms | `--timeout`（维持现状）| daemon 内部 |
| overall command deadline | 整个命令（尤其 batch）的总时长上限 | `0`（不限制）| `--overall-timeout <ms>` | **daemon 单一 owner** |
| IPC read timeout | Go 客户端 socket 读超时。**仅作 daemon 卡死兜底**，不是用户可配的业务超时 | `overall-timeout + 10s`（若用户没设则不设上限）| 不暴露 | Go 客户端 |

**关键设计（v4 修订）**：

- **daemon 是 overall-timeout 的唯一执行者**。客户端只把 `overallTimeoutMs` 塞进请求参数，然后**老老实实等 daemon 响应**，不设激进的 socket 超时
- daemon 收到 `cmd=batch` 时：
  1. 记录 `deadline = now + overallTimeoutMs`
  2. 每次准备调度下一条 case 前检查 deadline，超了就**停止派新的 case**（但不中断已提交到线程池的任务，它们按各自 `--timeout` 自然结束）
  3. 等所有已提交 case 跑完（最多再花一个 per-RPC `--timeout`），把完成的部分打包成 `BatchResult` 返回，并在响应里打上 `overallTimedOut:true`
- 客户端侧的 socket read timeout = `overallTimeoutMs + 10s`（默认不限制）。这 10s 是兜底："daemon 说好到点返回，但自己卡死没返回"的唯一场景下才触发，触发时**视为 daemon 死**，走自愈路径
- `--timeout` **只**控制单条 RPC 的超时，不影响 IPC 层
- 绝对不要把 `--timeout` 当 socket read deadline 用（否则 batch 里"3 条 4s 的 case"会被 5s 超时误杀）
- 用户 `Ctrl-C` 永远优先于任何 timeout 生效（Go 进程退出 → TCP 关闭 → daemon 记日志后放弃当前响应）

---

## 7. 安全与隔离

### 7.1 威胁模型

明确 daemon 要防什么、不防什么：

| 威胁 | 是否在保护范围 | 说明 |
|---|---|---|
| 远端机器扫描 | ✅ | 只绑 `127.0.0.1`，外网/内网他机无法连接 |
| 同机同用户的其他进程调 RPC | ❌ | 同权限进程本来就能读 `servers.yaml`、直接跑 `java -jar sofarpc.jar invoke`，加 token 也挡不住（它能读 token 文件）。单机自用工具不在这里做人为边界 |
| 同机**其他用户**扫 127.0.0.1 端口调 RPC | ❌（本期）| 本期目标是单人 Mac。如果放多用户机，按 7.3 补 token |
| 内存转储 / root 接管 | ❌ | 不在单机自用工具的防护范围 |
| DoS（刷连接耗尽 worker 线程）| 部分 | worker 池固定 16，刷满最多拖慢本用户自己；不做限流 |

### 7.2 具体措施

1. **只绑 loopback**：daemon 的 `ServerSocket` 显式构造 `new ServerSocket(0, 50, InetAddress.getLoopbackAddress())`，**不会**监听 `0.0.0.0`
2. **文件权限**：`daemon.lock` / `daemon.state` / `daemon.log` 全部 `0600`。写入采用"临时文件 `0600` 创建 → rename"确保中间状态不可读
3. **资源限制**：JVM 启动参数默认 `-Xmx512m`；单次 invoke 的返回结果大小不设上限（维持现状，未来若出问题再加）
4. **敏感参数日志**：SofaRPC 默认 ERROR 级别，不会打印业务参数。若用户调到 DEBUG，daemon 日志里业务 `args` 可能泄露——在文档里提醒，不做自动脱敏
5. **信号发射前核对身份**：任何来自 `daemon stop` / `daemon restart` 的 SIGTERM/SIGKILL，发射前必须读目标 PID 的 cmdFingerprint 并与 `daemon.state` 中记录的匹配（见 4.1 / 5.3），避免 PID 复用误杀

### 7.3 未来：多用户机器 token 预案

如果之后这个工具被搬到跳板机/共享测试机上，需要防"同机他用户扫 127.0.0.1"，按以下顺序加回鉴权：

1. daemon 启动时生成 32 字节随机值，作为 token 和 `port`、`pid` 等一起写到**同一个** `daemon.state`（单文件原子发布，避免 state/token 分开写的竞态）
2. 请求加 `token` 字段，daemon 用 `MessageDigest.isEqual` 常量时间比较
3. 客户端收到 `unauthorized` 时**重读一次 `daemon.state`** 再重试一次：应对 daemon 刚重启、客户端还拿着旧 token 的临时窗口
4. 日志打印请求时把 `token` 脱敏为 `"***"`

本期不实现，但协议 DTO 预留 `token` 字段为可选（daemon 忽略），减少未来兼容成本。

---

## 8. 失败模式与可观测性

| 失败场景 | 客户端表现 | 用户操作 |
|---|---|---|
| `daemon.state` 不存在 | spawn → 轮询 state 出现（30s 超时） | 无需干预 |
| `daemon.state` 存在但对应 PID 已死 | 客户端 connect 立刻 `ECONNREFUSED` → 清理 state → spawn | 无需干预（自愈） |
| `daemon.state` 存在但 PID 被复用给无关进程 | fingerprint 核对不一致 → 清理 state → spawn；旧 PID **不发任何信号** | 无需干预 |
| daemon 启动时端口被其他进程占用 | daemon 退出、state 文件不出现 → 客户端 30s 超时提示 `failed to start daemon, see ~/.sofarpc/daemon.log` | 看日志，`lsof -i :<port>` 排查 |
| daemon OOM/卡死 | 连接成功但响应超时或 TCP reset → **报错给用户，不自动杀 daemon** | 用户自行决定是否 `sofarpc daemon restart` 重试；**非幂等 RPC 请先确认服务端状态** |
| 单条 RPC 超时 | SofaRPC 抛 `RpcException` → daemon 封装进响应 `ok=false`，daemon 继续运行 | 按业务场景处理 |
| 整个 batch 超过 `--overall-timeout` | **daemon 在 deadline 停止派新 case，等已提交的跑完，回 partial `BatchResult` 且 `overallTimedOut=true`**。客户端正常接收，输出 partial 结果 + 警示 | 按 partial 结果决定是否重跑未完成 case（`skippedCases` 字段列出）|
| daemon 对 overall-timeout 毫无响应（bug/卡死）| 客户端兜底 read deadline（`overall-timeout + 10s`）触发，报错退出码 1 | `sofarpc daemon restart` 后排查 |
| 用户 `Ctrl-C` | Go 进程立即退出；daemon 写响应时 `BrokenPipe` 被捕获 → 丢弃结果继续运行 | 无需干预 |
| install.sh 升级时 daemon 正在处理请求 | `daemon restart` 会强制停掉旧进程，正在跑的 RPC/batch 被切断 → 客户端端报错 | 升级前自己保证无在跑任务；非幂等场景需确认服务端是否已收到；必要时人工对账 |
| Java 未安装 | Go `exec.Start()` 失败 | 友好错误：`sofarpcd requires Java 8+, run 'brew install openjdk@8'` |
| 多个客户端并发首次拉起 daemon | 只有一个 `FileChannel.tryLock(daemon.lock)` 成功；其他回退到"state 出现后直接连" | 无需干预 |
| `daemon.state` 写到一半被读 | 单文件 + 临时文件 + rename 原子可见；不可能读到半成品 | - |

### 8.1 日志

daemon 用 `java.util.logging` 写 `~/.sofarpc/daemon.log`：
- 格式：`[2026-04-13 14:25:30.123] [INFO] [worker-3] DaemonServer: handled invoke in 47ms`
- 滚动：启动时若日志 >10MB，rename 为 `daemon.log.1`，新建 `daemon.log`
- 保留：只保留 1 份历史（`daemon.log.1`），再滚动时覆盖

### 8.2 客户端 debug

- `SOFARPC_DEBUG=1` 环境变量：Go 端把 IPC 请求/响应原文打到 stderr
- `sofarpc daemon logs --follow`：直接查看 daemon 日志

---

## 9. 安装与升级

### 9.1 install.sh 新逻辑

```bash
#!/bin/bash
set -e

# 1. Build Java jar (existing)
mvn clean package -q

# 2. Build Go client
(cd client-go && GOOS=darwin GOARCH=arm64 go build -o ../target/sofarpc-bin .)

# 3. Install
mkdir -p "$HOME/.sofarpc"
cp target/sofarpc.jar "$HOME/.sofarpc/"
cp target/sofarpc-bin "$HOME/.sofarpc/sofarpc"
chmod +x "$HOME/.sofarpc/sofarpc"

# 4. PATH 提示
echo "Add $HOME/.sofarpc to PATH if not already."
```

### 9.2 升级

`install.sh` 会替换 `sofarpc.jar`。问题：**daemon 还拿着旧 jar 代码在跑**。

**方案**：`install.sh` 末尾检测若 daemon 正在跑，**先打印明显提示**再执行 `sofarpc daemon restart`：

```
⚠ Existing sofarpcd detected. Restarting to pick up the new jar.
  If a batch/invoke is running in another terminal, it will be interrupted.
  Press Ctrl-C within 3s to skip the restart.
```

取舍：
- 不自动 restart → 用户经常忘，跑一周发现用的还是旧代码
- 无提示直接 restart → 可能切断另一个 terminal 正在跑的请求
- **当前方案**：提示 + 3s 宽限，覆盖 99% 场景；重要非幂等操作请避免升级时执行

### 9.3 卸载

提供 `install.sh --uninstall`：stop daemon + 删除 `~/.sofarpc/sofarpc{,.jar}`。

---

## 10. 测试计划

### 10.1 单元测试

- `DaemonProtocolTest`：Request/Response JSON 序列化、字段缺失、未知 cmd
- `DaemonServerTest`：起 daemon（随机端口），用 Java 客户端发各种命令，断言响应

### 10.2 集成测试

- 真实服务 `10.74.194.40:12200` 上的 `SalesPortfolioAssetsFacade`：
  1. `sofarpc daemon start` → 首次耗时 <30s
  2. `sofarpc ping` 第二次 → <200ms
  3. `sofarpc invoke queryPortfolioLatestAsset` → 总耗时 ≤ `latencyMs + 80ms`
  4. `sofarpc batch` 含 3 条用例 → 总耗时 ≤ `max(latencyMs) + 100ms`（并发执行）

### 10.3 容错测试

- daemon 运行中手动 `kill -9`，再跑 `sofarpc invoke` → 自动重拉
- 改 `servers.yaml` 换地址，再 `sofarpc invoke` → 生效
- `SOFARPC_NO_DAEMON=1 sofarpc invoke` → 走老路径，耗时 ~26s

### 10.4 基准对比

用 `hyperfine` 量化：

```bash
hyperfine --warmup 1 \
  'sofarpc ping --server sales-test --service X' \
  'SOFARPC_NO_DAEMON=1 sofarpc ping --server sales-test --service X'
```

目标：`with daemon` / `without daemon` ≤ 1/50。

---

## 11. 工作量拆解

| 任务 | 文件 | 预估 |
|---|---|---|
| T1 - 协议 DTO | `daemon/DaemonProtocol.java` | 1h |
| T2 - Daemon 服务器 | `daemon/DaemonServer.java` | 3h |
| T3 - 路径与状态文件 | `daemon/DaemonPaths.java` | 1h |
| T4 - 管理命令 | `command/DaemonCommand.java` | 2h |
| T5 - Main.java `__daemon__` 入口 | `Main.java` | 1h |
| T6 - Go 客户端骨架（cmd 路由 / flag 解析）| `client-go/main.go` | 3h |
| T7 - Go IPC 封装 + daemon spawn | `client-go/daemon.go` | 2h |
| T8 - Go `server/*` 本地实现 | `client-go/server.go` | 2h |
| T9 - install.sh 改造（删 Linux 分支 + daemon restart 钩子）| `install.sh`、`README.md` | 1h |
| T10 - `BatchResult` 新字段 + Formatter + ReportGenerator 适配 partial | `BatchResult.java`、`OutputFormatter.java`、`ReportGenerator.java` | 2h |
| T11 - 真实接口联调 + 基准 | - | 2h |
| **合计** | | **~2.5 个工作日** |

---

## 11.5 Known Tradeoffs（已知取舍，非缺陷）

本节登记在 review 中被多次提及、但**已经按单机自用工具场景做出取舍**的设计决策。若后续 review 再次发现这些点，**按此节回答即可，不再迭代**。若目标场景改变（例如放到多用户共享机器），再行评估。

### 11.5.1 `install.sh` 升级时自动 restart daemon 会中断正在跑的 RPC

- **决策**：保留自动 restart（带 3s 宽限提示），**不做** drain/force-flag/active-request 计数/版本握手
- **权衡**：
  - 单机测试工具，大部分调用是 `getUser` / `queryAsset` 类幂等查询
  - "忘记升级继续跑旧代码"的成本（隐性 bug、混淆）远大于"升级时恰好有 batch 在跑被切断"的成本
  - 非幂等调用（下单、转账）本来就需要用户在 RPC 测试中自己当心，不是这个工具能代劳的责任
- **用户契约**：升级前自己确认无在跑任务；非幂等 RPC 测试请勿与 `install.sh` 并发执行
- **未来重评触发点**：如果被纳入 CI 自动跑且包含非幂等测试，再做 active-request 感知

### 11.5.2 loopback daemon 不做请求鉴权

- **决策**：本期**不实现** token/鉴权；7.3 已设计好未来补回的方案（单文件原子发布 token + unauthorized 触发重读重试）
- **威胁模型**：
  - 同机**同用户**的其他进程能读 `servers.yaml`、能直接 `java -jar sofarpc.jar invoke`，加 token 也挡不住（它能读 token 文件）。同权限进程无人为边界
  - 同机**其他用户**扫 127.0.0.1 端口——本期目标机器是单人 Mac，不存在此场景
- **部署约束（硬性）**：**本设计不适用于共享开发机 / 跳板机 / CI runner 等多用户环境**。若需在此类环境部署，必须先实现 7.3 的 token 方案
- **未来重评触发点**：工具分发到团队共享测试机；被要求装到 CI runner；macOS 多账号使用

---

## 12. 遗留问题 / 未来工作

1. **守护进程化**：目前依赖 `setsid`，没有 launchd 集成。后续可做 `brew services` 风格
2. **Unix Domain Socket**：升 JDK 16+ 后替换 TCP loopback（性能 + 安全都更好）
3. **多 daemon 隔离**：现在只支持单 daemon 实例。如果未来需要"一个项目一个 daemon"，加 namespace 到 port/pid 文件名
4. **RPC 追踪 / Metrics**：daemon 内部可以加 Prometheus endpoint，暴露缓存命中率、并发数等
5. **servers.yaml 变更监听**：inotify/fsevents 方式让 daemon 自动失效相关连接，省掉 `invalidate` 命令
