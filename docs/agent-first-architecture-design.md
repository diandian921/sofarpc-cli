# SofaRPC Agent-First CLI 架构设计

> 作者：Codex  
> 日期：2026-04-13  
> 状态：Draft v2

## 1. 背景

本项目的真实目标不是“做一个 Java CLI”，而是：

- 提供一个本地可执行入口
- 让 AI agent 可以稳定、低延迟地调用它
- 让它能够测试 SofaRPC 服务
- 且不依赖业务 API jar

当前性能瓶颈的根因很明确：

- agent 每次调用 `java -jar sofarpc.jar ...`
- 每次都是一个新 JVM
- 每次都重复支付类加载、SPI 扫描、SofaRPC 初始化、建连成本
- 真正的 RPC 调用耗时只占一小部分

因此，第一性原理下的正确方向是：

`把冷启动昂贵的部分收敛到一个常驻 Java daemon 中，把 agent 高频调用入口做成一个快启动 thin client。`

---

## 2. 目标

### 2.1 核心目标

- 为 AI agent 提供稳定的本地调用入口
- 将 `invoke` / `ping` 的端到端开销降到“真实 RPC 耗时 + 很小的本地 IPC 开销”
- 支持后续扩展 `batch`
- 输出结构化 JSON，方便 agent 直接消费
- 继续基于 `GenericService`，无需业务 API jar

### 2.2 设计约束

- 第一版优先追求可落地与性能，不追求平台完备性
- 第一版只做最小功能集
- 第一版保留未来扩展的边界，但不提前实现未来功能

### 2.3 非目标（V1）

V1 明确不做：

- Windows 支持
- Unix Domain Socket
- report markdown/html
- server alias 管理
- config 管理
- 复杂 batch 策略
- 多 transport 抽象
- 多重版本协商

---

## 3. 核心决策

本设计采用以下固定决策：

- **CLI 语言：Go**
- **daemon 语言：Java**
- **IPC：仅 `127.0.0.1 TCP`**
- **协议：长度前缀 + JSON**
- **agent 一等入口：`sofarpc exec --stdin`**
- **业务语义只在 daemon 实现**

这是一个有意收缩后的 MVP 方案，不追求“通用、跨平台、可插拔”，只追求：

- 快
- 简单
- 可验证
- 后续可演进

### 3.1 当前仓库实施策略

本方案默认：

- **直接在当前仓库中实施**
- **不新建新仓库**
- **采用新目录并行实现**

当前仓库中的旧实现：

- 先保留
- 只作为参考
- 不作为新架构的承载路径

因此实现时应遵循：

- 不删除当前旧 `src/` 代码
- 不在旧的 Java CLI 架构上继续打补丁
- 不要求新架构兼容旧目录结构
- 新实现统一落到：
  - `cli/`
  - `daemon/`
  - `protocol/`
  - `docs/`

这样做的目的：

- 保留现有实现中的经验和参考代码
- 降低迁移期风险
- 避免“旧单 JVM CLI”和“新 client/daemon 架构”相互污染

---

## 4. 总体架构

### 4.1 进程拓扑

```text
AI Agent / Human User
        |
        v
     sofarpc
   (Go thin client)
        |
        |  length-prefixed JSON over 127.0.0.1 TCP
        v
    sofarpcd
  (Java daemon)
        |
        v
SofaRPC GenericService
        |
        v
  Target SofaRPC Service
```

### 4.2 职责划分

#### `sofarpc`（Go thin client）

负责：

- 提供命令入口
- 提供 `exec --stdin`
- 启动/停止/探测 daemon
- 自动拉起 daemon
- 构造协议请求
- 读取 daemon 响应并输出 JSON

不负责：

- 执行真正的 SofaRPC 调用
- 管理 GenericService 连接缓存
- 实现业务语义

#### `sofarpcd`（Java daemon）

负责：

- 常驻 JVM
- 初始化 SofaRPC
- 维护连接缓存
- 实现 `invoke` / `ping`
- 后续扩展 `batch`
- 统一错误分类
- 返回结构化结果

不负责：

- 解析 shell 命令
- 管理 alias/config/report
- 处理本地文件系统上的 case 扫描

### 4.3 能力边界表

下面这张表用于明确：哪些能力应进入 client，哪些能力必须留在 daemon。

| 能力 | 放在 client | 放在 daemon | 原因 |
|---|---|---|---|
| `exec --stdin` 入口 | `Yes` | `No` | 这是 agent 的本地调用入口，属于控制面 |
| `invoke/ping` 命令解析 | `Yes` | `No` | shell/stdio 交互属于本地工具前端 |
| SofaRPC 真正调用 | `No` | `Yes` | 这是热路径执行逻辑，必须复用 warm JVM |
| `GenericService` 与连接缓存 | `No` | `Yes` | 性能收益来自 daemon 持有的长生命周期状态 |
| 断言执行 | `No` | `Yes` | 属于执行语义，不能在两边重复实现 |
| 错误分类 | `No` | `Yes` | 保持业务语义单一来源 |
| daemon 生命周期管理 | `Yes` | `No` | start/stop/status 是本地控制动作 |
| stale `state.json` 清理 | `Yes` | `No` | 属于 launcher 逻辑 |
| alias 管理 | `Yes` | `No` | 属于本地用户体验，不属于执行语义 |
| config 管理 | `Yes` | `No` | 本地默认值与运行偏好属于控制面 |
| batch 的 case 文件扫描与读取 | `Yes` | `No` | 文件系统语义应留在 client |
| batch 真正执行与结果聚合 | `No` | `Yes` | 执行与调度属于 daemon |
| report 生成 | `Yes` | `No` | 本地后处理，不值得进入热路径 |
| install / upgrade | `Yes` | `No` | 分发与升级是工具层能力 |
| debug 开关与请求日志 | `Yes` | `No` | 这是本地调试体验，不是执行内核 |

---

## 5. 为什么保留 Go CLI

这次选择 Go，不是因为“Java thin client 不行”，而是因为目标明确是：

- 高频调用
- 追求快启动
- 追求最终产品形态

Go 相对 Java thin client 的优势：

- 原生进程启动更快
- 单文件分发更干净
- 不依赖用户系统 Java 来跑 client
- 更适合作为 agent 高频工具入口

但为了避免过度设计，本设计同时做了两项收缩：

- 不做双 transport
- 不做复杂本地能力

即：

- 接受 Go/Java 双语言栈
- 但只保留最小必要的那部分双栈复杂度

---

## 6. V1 最小可行版本

### 6.1 V1 提供的命令

V1 只做：

- `sofarpc exec --stdin`
- `sofarpc invoke ...`
- `sofarpc ping ...`
- `sofarpc daemon start`
- `sofarpc daemon stop`
- `sofarpc daemon status`

### 6.2 V1 不做的命令

V1 暂不做：

- `batch`
- `report`
- `server add/list/remove`
- `config get/set`

### 6.3 为什么这样收缩

因为当前最需要验证的是：

- daemon 是否能显著解决冷启动问题
- Go client 与 Java daemon 的协议是否稳定
- GenericService 复用是否可靠
- agent 调用链是否顺手

只要这四件事成立，后面再加功能才有意义。

---

## 7. 机器接口设计

### 7.1 一等入口

AI agent 推荐统一走：

```bash
sofarpc exec --stdin
```

stdin 输入请求 JSON，stdout 输出响应 JSON。

### 7.2 顶层请求结构

```json
{
  "requestId": "uuid",
  "op": "invoke",
  "meta": {},
  "payload": {}
}
```

### 7.3 顶层响应结构

```json
{
  "requestId": "uuid",
  "ok": true,
  "code": "SUCCESS",
  "data": {},
  "error": null,
  "meta": {}
}
```

### 7.4 为什么保留这个包裹层

即使 V1 只有 `invoke` / `ping`，也必须保留这个统一包裹层，因为它是未来扩展的核心支点。

后续加功能时：

- 新增一个 `op`
- 在 `meta` 中增加横切字段
- 扩展该 `payload`
- 不需要改 transport
- 不需要改返回包裹

这就是“砍功能，但不砍边界”。

---

## 8. V1 协议细节

### 8.1 帧格式

采用：

- 4 字节长度前缀
- 后跟 UTF-8 JSON payload

理由：

- 简单
- 边界清晰
- 不受换行和多行文本干扰

### 8.2 `invoke` 请求示例

```json
{
  "requestId": "req-001",
  "op": "invoke",
  "meta": {
    "traceId": "trace-001"
  },
  "payload": {
    "address": "10.74.194.40:12200",
    "service": "com.example.UserService",
    "method": "getUser",
    "argTypes": ["com.example.GetUserRequest"],
    "args": [{"userId": 123}],
    "assertions": [
      {"path": "$.status", "equals": "ACTIVE"}
    ],
    "rpcTimeoutMs": 5000
  }
}
```

### 8.3 `ping` 请求示例

```json
{
  "requestId": "req-002",
  "op": "ping",
  "meta": {},
  "payload": {
    "address": "10.74.194.40:12200",
    "service": "com.example.UserService",
    "rpcTimeoutMs": 3000
  }
}
```

### 8.4 `health` 响应示例

```json
{
  "requestId": "req-003",
  "ok": true,
  "code": "SUCCESS",
  "data": {
    "pid": 12345,
    "buildVersion": "1.0.0",
    "startedAtMs": 1760000000000
  },
  "error": null,
  "meta": {}
}
```

### 8.5 错误码

V1 统一使用字符串错误码：

- `SUCCESS`
- `BAD_REQUEST`
- `CONNECT_FAILED`
- `RPC_TIMEOUT`
- `INVOKE_FAILED`
- `ASSERTION_FAILED`
- `DAEMON_UNAVAILABLE`
- `INTERNAL_ERROR`

CLI 再把它映射为 shell exit code。

---

## 9. daemon 生命周期

### 9.1 本地状态目录

```text
~/.sofarpc/
└── daemon/
    ├── state.json
    ├── daemon.lock
    └── daemon.log
```

### 9.2 `state.json`

建议至少包含：

- `pid`
- `port`
- `buildVersion`
- `startedAtMs`
- `status`

### 9.3 启动流程

1. Go client 尝试读取 `state.json`
2. 若 `state.json` 存在，则先根据其中的 `pid` 做本地探活
   - 若 `kill -0 <pid>` 成功，则继续尝试连接 `127.0.0.1:<port>`
   - 若 `kill -0 <pid>` 失败，则视为陈旧状态文件，删除 `state.json`
3. 若连接失败，则视为 daemon 当前不可用
4. Go client 尝试获取 `daemon.lock`
   - 获取成功：本进程负责 spawn daemon
   - 获取失败：说明已有其他 client 正在拉起 daemon，本进程不重复 spawn，而是等待并重试连接
5. daemon 只有在初始化完成并成功 bind 端口后，才写入 `state.json`
6. client 连接成功后发送 `health`，作为 ready 校验
7. 若 `buildVersion` 不匹配，则 client 重启 daemon

### 9.4 并发拉起策略

为避免多个 agent 并发命中“daemon 不可用”分支时重复拉起，launcher 必须遵循：

- 只有持有 `daemon.lock` 的 client 才允许 spawn daemon
- 未持有锁的 client 进入短轮询等待
- 轮询期间重读 `state.json` 并重试连接
- 达到 spawn budget 后仍未就绪才报错退出

### 9.5 版本策略

V1 只保留一个版本字段：

- `buildVersion`

策略非常简单：

- client 连上 daemon 后读取 `buildVersion`
- 若与自身不一致，则重启 daemon

不做：

- `schemaVersion`
- `protocolVersion`
- 复杂兼容协商

### 9.6 idle TTL

V1 保留 daemon 空闲退出能力。

建议：

- 15 分钟无请求自动退出

原因：

- 平时 warm
- 长期不用不占资源
- 仍然保留自动拉起能力

### 9.7 已知陷阱

#### stale `state.json`

daemon 被 kill 或异常退出时，`state.json` 可能残留。

必须明确：

- client 不能只靠 `state.json` 是否存在判断 daemon 存活
- 必须先做 pid 探活，再决定是否复用该状态

#### 并发拉起竞争

多个 agent 并发调用时，最容易出现双重拉起。

必须明确：

- `daemon.lock` 必须参与 launcher 决策
- 没拿到锁的 client 只能等待，不能重复 spawn

#### idle TTL 后的首次冷启动

daemon 因 TTL 退出后，下一次调用会重新支付 JVM 冷启动成本。

这意味着：

- 首次调用会明显慢于 warm path
- 若 client 或 agent 的等待预算过短，容易误判失败

因此 launcher 必须区分两类等待预算：

- daemon 已存在时的常规请求预算
- 需要 spawn daemon 时的更长 budget，例如 `30s`

ready 判定建议采用双保险：

- `state.json` 已写入
- `health` 请求成功返回

---

## 10. daemon 内部设计

### 10.1 最小模块划分

V1 不做重模块化，只保留最小必要边界：

```text
daemon/
├── server/      # TCP server, accept loop, framing
├── handler/     # 按 op 分发
├── engine/      # invoke/ping 执行逻辑
└── rpc/         # SofaRPC 封装、连接管理
```

### 10.2 关键边界

V1 虽然简单，但下面四个边界必须明确：

#### `handler dispatcher`

daemon 必须按 `op` 分发，而不是把当前流程写死成“只处理 invoke”。

即使 V1 只有：

- `invoke`
- `ping`
- `health`
- `shutdown`

内部也应采用：

- `op -> handler`

这样后续新增 `batch` 时不需要重写 server 主流程。

#### `engine`

`engine` 负责业务执行：

- 参数校验
- 调用 GenericService
- flatten 结果
- 执行断言
- 错误分类

#### `rpc`

`rpc` 模块必须单独存在，至少包含：

- `SofaRpcGateway`
- `ConnectionManager`

因为后续所有性能收益都建立在连接复用之上。

#### `server`

`server` 只关心：

- TCP accept
- 长度前缀收发
- 请求解码
- 响应编码

不承担业务语义。

---

## 11. Go client 内部设计

### 11.1 最小模块划分

```text
cli/
├── cmd/         # invoke/ping/exec/daemon 子命令
├── protocol/    # 请求响应结构
├── ipc/         # TCP client
└── launcher/    # daemon 启停、自愈、版本检查
```

### 11.2 关键边界

#### `protocol`

Go client 和 Java daemon 的唯一真实契约就是协议结构。

所以 V1 必须把协议对象独立出来，而不是把 shell flag 直接散落进发送逻辑。

#### `launcher`

`launcher` 负责：

- 查找 `state.json`
- 判断 daemon 是否可用
- 拉起 daemon
- 处理 `daemon.lock` 竞争
- 清理 stale `state.json`
- 检查 `buildVersion`
- 必要时 restart daemon
- 在 spawn 场景下使用更长的等待预算

#### `ipc`

V1 只实现：

- `127.0.0.1 TCP`

不做：

- UDS
- transport interface
- transport fallback

---

## 12. 为什么这个 MVP 仍然具备扩展性

虽然 V1 砍了很多功能，但后续仍然能平滑扩展，原因是下面几个“扩展支点”被保留下来了。

### 12.1 保留了稳定协议包裹

顶层始终是：

- `requestId`
- `op`
- `meta`
- `payload`
- `ok`
- `code`
- `data`
- `error`

所以未来加 `batch`、`report`、`alias`，不是推翻协议，而是扩展已有协议。

### 12.2 保留了 daemon dispatch 结构

未来新增：

- `batch`
- `health details`
- 其他内部操作

只需：

- 新增一个 handler
- 注册到 dispatcher

### 12.3 保留了 client/daemon 职责分离

client 负责：

- 输入归一化
- 进程控制
- 用户交互

daemon 负责：

- 执行

所以未来新增 alias/report/case 文件扫描时，都可以加在 client，不会污染 daemon。

### 12.4 保留了独立连接管理器

后续无论加：

- `batch`
- 连接失效重建
- 空闲连接清理

都以 `ConnectionManager` 为锚点扩展，而不是让命令层直接控制连接。

### 12.5 保留了版本兼容扩展位

后续很可能会遇到这样的场景：

- 项目 A 使用一套较新的 SOFARPC 版本
- 项目 B 使用一套较老的 SOFARPC 版本
- 两边都希望复用同一个 `sofarpc` 工具

对此，文档明确采用以下策略：

- V1 只固定一套 SOFARPC 客户端版本
- 通过兼容矩阵验证这套版本对目标项目群的适配情况
- 不在一个 daemon / classpath 中混装多套 SOFARPC 依赖

这是一个硬规则：

`不在单 JVM 中混装多版本 SOFARPC 依赖。`

如果未来真实出现版本族不兼容问题，扩展方式不是“一个 daemon 里塞多套依赖”，而是：

- 引入 `compatibility profile`
- 每个 profile 对应一套独立 daemon jar / classpath / state 文件 / 端口

例如：

```bash
sofarpc --profile default invoke ...
sofarpc --profile legacy-5x invoke ...
```

这样扩展的好处是：

- 不破坏当前单版本主路径
- 通过进程与 classpath 隔离避免 jar hell
- 只在真实需求出现时增加复杂度

---

## 13. 后续规划

本项目建议按阶段演进，不建议一开始做大而全。

### Phase 1：MVP

目标：

- 验证 daemon 化收益
- 跑通 agent 调用主链路

范围：

- Go thin client
- Java daemon
- TCP only
- `exec --stdin`
- `invoke`
- `ping`
- `daemon start/stop/status`
- 自动拉起
- idle TTL
- `buildVersion` 不匹配自动重启
- stale `state.json` 清理
- 并发拉起锁竞争处理

验收标准：

- warm daemon 下，invoke/ping 的端到端额外开销显著小于当前方案
- agent 可以稳定消费 JSON 输出
- TTL 触发后的首次调用不会因为过短等待预算而误判失败

### Phase 2：Batch

目标：

- 在不破坏 V1 骨架的前提下补上批量执行

范围：

- 新增 `op=batch`
- client 侧读 case 文件并组装 `BatchSpec`
- daemon 新增 `BatchHandler`
- daemon 内新增 `BatchRunner`

本阶段先不做：

- `overallTimeoutMs`
- 可配置 `parallelism`
- partial batch 复杂语义

第一版 batch 可采用：

- 固定并发度
- 简单聚合结果

### Phase 3：本地辅助能力

目标：

- 提升人类可用性，但不影响 agent 主链路

范围：

- alias 管理
- config 管理
- report 生成

原则：

- 全部加在 client 侧
- daemon 仍然只认最终执行请求

### Phase 4：性能和工程优化

目标：

- 在真实使用和实测数据驱动下继续打磨

可选项：

- 更细的连接清理策略
- 更好的日志与诊断
- 更好的安装与升级体验
- batch 高级策略

### Phase 5：平台与 transport 扩展

仅当真实需求出现时再做：

- UDS
- Windows
- transport 抽象

注意：

- 这一阶段是后置需求
- 不应反向影响 Phase 1 的简单实现

### Phase 6：版本兼容扩展

仅当真实需求出现时再做：

- compatibility matrix 持续维护
- `--profile` 支持
- 多 daemon 隔离运行
- legacy profile 打包与分发

原则：

- 通过 profile 隔离版本族
- 不在单 JVM 中混多套 SOFARPC 依赖

---

## 14. 当前明确不做，但未来可平滑加入的能力

这些能力都应被视为“后加功能”，不是当前架构前提：

- `batch`
- alias
- config
- report
- UDS
- Windows
- compatibility profile
- 更丰富的错误模型
- 更复杂的版本兼容策略

之所以能后加，是因为当前已经保留了：

- 统一协议包裹
- dispatcher
- client/daemon 边界
- 独立连接管理

---

## 15. 目录结构建议

```text
sofarpc/
├── docs/
│   └── agent-first-architecture-design.md
├── cli/
│   ├── cmd/
│   ├── internal/
│   └── go.mod
├── daemon/
│   ├── src/main/java/
│   └── pom.xml
├── protocol/
│   ├── schema/
│   └── fixtures/
├── testdata/
│   └── golden/
└── scripts/
    └── install.sh
```

V1 不需要更多目录层次。

---

## 16. 实施顺序

建议按以下顺序开发：

1. 先定义请求/响应 JSON 结构
   - 落成 JSON schema 文件
   - 落成 golden fixtures
   - Go 和 Java 两边都用同一份 fixture 做契约测试
2. 先做 Java daemon：
   - TCP server
   - `invoke`
   - `ping`
   - `health`
   - `shutdown`
3. 再做 Go client：
   - `exec --stdin`
   - `invoke`
   - `ping`
   - `daemon start/stop/status`
4. 再做自动拉起与 idle TTL
5. 最后跑端到端压测与真实 agent 集成

不要一开始就做：

- batch
- report
- alias
- config
- UDS
- Windows

### 16.1 实施计划表

下面的表按“先跑通主链路，再补稳定性”的原则拆分，默认面向 Phase 1。

| 编号 | 任务 | 主要产出 | 位置 | 依赖 | 优先级 |
|---|---|---|---|---|---|
| P1 | 定义协议 schema 与 fixtures | 请求/响应 JSON schema、golden fixtures | `protocol/schema` `protocol/fixtures` | 无 | P0 |
| P2 | 搭 daemon TCP server 骨架 | 长度前缀收发、请求解码、响应编码 | `daemon/server` | P1 | P0 |
| P3 | 实现 daemon handler dispatcher | `op -> handler` 路由、基础错误返回 | `daemon/handler` | P2 | P0 |
| P4 | 实现 `rpc` 层最小封装 | `SofaRpcGateway`、`ConnectionManager` | `daemon/rpc` | P2 | P0 |
| P5 | 实现 `invoke` 执行链路 | 参数校验、GenericService 调用、结果 flatten、断言 | `daemon/engine` | P3 P4 | P0 |
| P6 | 实现 `ping` / `health` / `shutdown` | 基础健康检查与停机路径 | `daemon/engine` `daemon/handler` | P3 P4 | P0 |
| P7 | 搭 Go protocol 与 TCP client | 请求编码、响应解码、超时与错误处理 | `cli/protocol` `cli/ipc` | P1 | P0 |
| P8 | 实现 launcher | `state.json` 读写、stale 清理、`daemon.lock`、spawn、版本检查 | `cli/launcher` | P6 P7 | P0 |
| P9 | 实现 `exec --stdin` | stdin 读请求、发给 daemon、stdout 输出结果 | `cli/cmd` | P7 P8 | P0 |
| P10 | 实现 `invoke` / `ping` 命令 | 人类命令转协议请求 | `cli/cmd` | P7 P8 | P1 |
| P11 | 实现 `daemon start/stop/status` | 管理命令与冷启动等待预算 | `cli/cmd` `cli/launcher` | P8 | P1 |
| P12 | 增加 idle TTL 与 ready 双保险 | TTL、spawn 长等待预算、`health` ready 校验 | `daemon` `cli/launcher` | P6 P8 | P1 |
| P13 | 契约测试 | Go/Java 共用 fixtures 的协议测试 | `protocol` `cli` `daemon` | P1 P7 | P1 |
| P14 | 端到端联调 | 从 `exec --stdin` 到真实 daemon 的集成测试 | 集成测试目录或脚本 | P5 P6 P9 P12 | P1 |
| P15 | 基准验证 | cold/warm 调用耗时对比 | 基准脚本 | P14 | P1 |

### 16.2 建议开发顺序

如果只按最短闭环推进，建议按这条链路开发：

1. P1 `schema + fixtures`
2. P2 P3 `daemon server + dispatcher`
3. P4 P5 P6 `rpc + invoke/ping/health/shutdown`
4. P7 P8 `Go protocol + launcher`
5. P9 `exec --stdin`
6. P10 P11 `invoke/ping/daemonctl`
7. P12 P13 P14 P15 `稳定性、测试、基准`

### 16.3 开工检查表

开始实现前建议先确认：

- daemon 使用哪个 JDK 版本
- Go client 如何定位 daemon jar
- `daemon.lock` 采用哪种文件锁实现
- `state.json` 的最终字段集合
- shell exit code 与 `code` 的映射表
- golden fixtures 的目录命名规则

---

## 17. 最终结论

当前最合理的方案不是继续增强现有 `java -jar` CLI，也不是一次性做成完整平台，而是：

`Go thin client + TCP only + Java daemon`

其中：

- Go client 负责快启动和 agent 入口
- Java daemon 负责 warm JVM 与 SofaRPC 执行
- JSON 协议负责稳定扩展点

V1 要做的不是“大而全”，而是：

- 把性能问题先解决
- 把边界先立住
- 把后续扩展支点先保留下来

一句话概括：

`先把骨架跑通，再按真实使用数据长肉。`
