# Agent 接入指南

本工具的设计目标是让 agent（LLM、脚本、CI 步骤）以**单条 JSON 信封进、单条 JSON 信封出**的方式调用 SofaRPC 服务。

Agent 只需关心三件事：

1. 怎么启动 `sofarpc`
2. 写什么样的请求信封
3. 怎么解读返回信封

---

## 1. 构建

```bash
# 打 Java daemon（shaded JAR，不需要业务 API 依赖）
mvn -f daemon-java/pom.xml package -DskipTests
# 产物：daemon-java/target/sofarpcd.jar

# 编 Go 客户端
cd cli-go && go build -o sofarpc ./cmd/sofarpc
# 产物：cli-go/sofarpc
```

两份产物放到 agent 可访问的路径即可。没有其他运行时依赖（Go 纯 stdlib，Java 只要 JDK 8+）。

---

## 2. 调用方式

只需要一条命令：

```bash
echo '<REQUEST_ENVELOPE_JSON>' | sofarpc exec --stdin [--jar /path/to/sofarpcd.jar]
```

规则：

- stdin 读**一条** JSON 信封（单行或多行都行，标准 JSON）
- stdout 写**一条** JSON 信封（单行）
- 退出码：`0` 表示 `ok=true`，`1` 表示 `ok=false`，`2` 表示命令行参数错误
- 首次调用会冷启动 daemon（~5s，JVM 启动成本），之后同机的调用全部走暖复用（p50 < 1ms）
- `--jar` 用于显式指定 daemon JAR；省略时走约定路径自动发现
- daemon 空闲 15 分钟自动退出，下次调用再冷启一次

如果 agent 连 daemon 都起不来（比如 JAR 找不到、端口全满），stdout 仍会输出一条**形状完全一致**的错误信封（`code: "DAEMON_UNAVAILABLE"`），agent 只需要解析 stdout、不用解析 stderr。

---

## 3. 请求信封

所有请求共享同一外层结构：

```json
{
  "requestId": "任意字符串，建议唯一；留空时 CLI 自动生成",
  "op": "invoke | ping | health | shutdown",
  "meta": { "traceId": "可选，用于串联日志" },
  "payload": { ... }
}
```

> **op 区分大小写**，必须小写。写错会返回 `BAD_REQUEST` 并列出合法值。

### 3.1 `op: "invoke"` — 业务调用（主用例）

```json
{
  "requestId": "call-001",
  "op": "invoke",
  "payload": {
    "address": "10.74.194.40:12200",
    "service": "com.example.UserService",
    "method": "getUser",
    "argTypes": ["com.example.GetUserRequest"],
    "args": [{"userId": 123}],
    "rpcTimeoutMs": 5000,
    "assertions": [
      {"path": "$.status", "equals": "ACTIVE"},
      {"path": "$.name", "exists": true}
    ]
  }
}
```

| 字段 | 必填 | 说明 |
|---|---|---|
| `address` | ✅ | `host:port` 直连地址，走 bolt 协议 |
| `service` | ✅ | RPC 接口全限定名 |
| `method` | ✅ | 方法名 |
| `argTypes` | ✅ | 参数类型全限定名数组，顺序和 `args` 对齐 |
| `args` | ✅ | 参数对象数组；Jackson 反射填充 |
| `rpcTimeoutMs` | ❌ | 单次 RPC 超时，默认 5000 |
| `assertions` | ❌ | JSONPath 断言数组，失败时 `code: ASSERTION_FAILED` |

断言支持 `equals`（值严格相等）和 `exists`（true 要求路径有值、false 要求没有）。更复杂的匹配让 agent 自己解析 `data` 做。

> **断言 path 的根是 RPC 原始返回对象**，不是整个响应信封。例如 RPC 返回 `{success: true, data: {...}}`，那么 `$.success` 直接指向顶层 `success` 字段；不要写成 `$.result.success`。

### 3.2 `op: "ping"` — 连通性探测

只 dial + 握手，不做真实业务调用。用来判断"地址通不通"。

```json
{
  "op": "ping",
  "payload": {
    "address": "10.74.194.40:12200",
    "service": "com.example.UserService",
    "rpcTimeoutMs": 3000
  }
}
```

### 3.3 `op: "health"` — daemon 自检

探测 daemon 自身状态，不触达任何业务地址。payload 为空对象 `{}`。

### 3.4 `op: "shutdown"` — 显式退出 daemon

```json
{"op":"shutdown","payload":{"graceMs":0}}
```

通常不需要 agent 主动发——idle TTL 会自动回收。

---

## 4. 响应信封

```json
{
  "requestId": "call-001",
  "ok": true,
  "code": "SUCCESS",
  "data": { ... },
  "error": null
}
```

- `ok`：布尔，成功/失败的最快判断依据
- `code`：稳定枚举（见下），`ok=true` 时恒为 `SUCCESS`
- `data`：成功时的返回数据，shape 随 op 变化
- `error`：失败时的结构化错误 `{"message": "..."}`

### 4.1 ⚠ `ok` 只代表 RPC 过程，不代表业务结果

对于 `op: "invoke"`，`ok=true` 仅说明 **RPC 调用完成**——连接成功、请求被接收、服务端返回了一个对象。业务自身是否成功，要看 `data.result` 里业务模型自己定义的字段（通常是 `success` / `code` / `status`）。

```json
{
  "ok": true,            // ← RPC 过程 OK
  "code": "SUCCESS",
  "data": {
    "result": {
      "success": false,  // ← 业务校验失败
      "message": "组合代码不能为空"
    },
    "elapsedMs": 69
  }
}
```

agent 必须做**双层判断**：外层 `ok` → 内层 `data.result.*`。或者：用 `assertions` 把内层校验声明出来（`{"path":"$.success","equals":true}`），让 daemon 帮你把失败归类到 `ASSERTION_FAILED`。

### ErrorCode 完整列表

| Code | 含义 | Agent 应如何响应 |
|---|---|---|
| `SUCCESS` | 成功 | 解析 `data` 继续 |
| `BAD_REQUEST` | 请求字段缺失/类型错/op 不认识 | 修请求本身；不要重试 |
| `CONNECT_FAILED` | 目标地址不可达（UnknownHost / connection refused / network unreachable） | 检查地址、服务是否在线；可换地址重试 |
| `RPC_TIMEOUT` | RPC 超出 `rpcTimeoutMs` | 重试一次；仍超时说明服务慢或挂了 |
| `INVOKE_FAILED` | RPC 建连成功但业务侧抛异常（参数不匹配、方法不存在、服务端异常） | 读 `error.message`，修参数或汇报 |
| `ASSERTION_FAILED` | RPC 返回正常但断言不通过 | 读 `error.message` 看哪条断言挂了 |
| `DAEMON_UNAVAILABLE` | agent 端发出但 daemon 没起来 / 不可达 | 检查 JAR 路径、端口、日志 `~/.sofarpc/daemon/daemon.log` |
| `INTERNAL_ERROR` | daemon 内部异常（不该发生） | 记录 requestId 上报，看 daemon 日志 |

---

## 5. 最小示例（复制即可用）

```bash
# 冷启 + health
echo '{"op":"health","payload":{}}' \
  | sofarpc exec --stdin --jar ./sofarpcd.jar

# 连通性
echo '{"op":"ping","payload":{"address":"10.74.194.40:12200","service":"com.example.UserService"}}' \
  | sofarpc exec --stdin

# 业务调用
echo '{
  "op":"invoke",
  "payload":{
    "address":"10.74.194.40:12200",
    "service":"com.example.UserService",
    "method":"getUser",
    "argTypes":["com.example.GetUserRequest"],
    "args":[{"userId":123}]
  }
}' | sofarpc exec --stdin
```

---

## 6. 辅助命令（agent 一般用不到）

```bash
sofarpc daemon status   # 查看当前 daemon 状态
sofarpc daemon stop     # 优雅退出 daemon
sofarpc version         # 打印构建版本
```

运行态文件全部在 `~/.sofarpc/daemon/`：`state.json` 当前 pid/端口，`daemon.log` 日志，`daemon.lock` 启动互斥锁。
