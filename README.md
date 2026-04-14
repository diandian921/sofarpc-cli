# SofaRPC CLI

[中文](#中文) | [English](#english)

Agent-first 的 SofaRPC 调用工具：**Go 瘦客户端 + Java 常驻 daemon**，一条 JSON 信封进、一条 JSON 信封出。为 LLM agent、脚本、CI 步骤而造。

---

## 中文

### 设计要点

- **单行 JSON 信封协议**：`echo '{...}' | sofarpc exec --stdin` 读一条、写一条
- **冷启一次、之后全暖**：首次调用启动 daemon（~6s，JVM 冷启），随后同机调用 p50 < 1ms；空闲 15 分钟自动回收
- **泛化调用**：基于 SOFARPC 5.12 `GenericService` + bolt 直连，不需要业务 API jar
- **稳定错误码**：8 个 `ErrorCode` 精确分类（`CONNECT_FAILED` / `RPC_TIMEOUT` / `INVOKE_FAILED` / `ASSERTION_FAILED` / …），agent 可根据 code 决定重试策略
- **纯 stdlib**：Go 客户端零三方依赖，Java daemon 只要 JDK 8+

### 仓库布局

```
sofarpc-cli/
├── cli/          # Go 瘦客户端（sofarpc 二进制）
├── daemon/       # Java 常驻 daemon（sofarpcd.jar）
├── protocol/     # JSON Schema + fixtures（双端共用契约）
├── scripts/      # 安装脚本
└── docs/
    ├── agent-integration.md               # ← Agent 接入唯一入口文档
    ├── agent-first-architecture-design.md # 架构设计细节
    └── daemon-design.md                   # daemon 进程模型设计笔记
```

### 安装

```bash
# 一键安装：构建 + 拷贝到 ~/.sofarpc/
./scripts/install.sh

# 加入 PATH 后即可使用
export PATH="$HOME/.sofarpc:$PATH"

# 卸载
./scripts/install.sh --uninstall
```

`scripts/install.sh` 会同时构建 Go 客户端和 daemon jar，拷贝到 `~/.sofarpc/sofarpc` 和 `~/.sofarpc/sofarpcd.jar`。如果检测到 daemon 正在跑，会给 3s 宽限后停掉（下次调用自动拉起新 jar）。

### 单独构建

```bash
# Java daemon：产出 daemon/target/sofarpcd.jar
mvn -f daemon/pom.xml package -DskipTests

# Go 客户端：产出 cli/sofarpc
cd cli && go build -o sofarpc ./cmd/sofarpc
```

Go 1.19+，JDK 8+，Maven 3.6+。macOS / Linux 已测试（Windows 未支持）。

### 最小示例

```bash
JAR=daemon/target/sofarpcd.jar

# daemon 自检（冷启 ~6s）
echo '{"op":"health","payload":{}}' | ./sofarpc exec --stdin --jar $JAR

# 连通性探测
echo '{"op":"ping","payload":{"address":"10.0.0.1:12200","service":"com.example.UserService"}}' \
  | ./sofarpc exec --stdin

# 业务调用（暖复用，p50 < 1ms）
echo '{
  "op":"invoke",
  "payload":{
    "address":"10.0.0.1:12200",
    "service":"com.example.UserService",
    "method":"getUser",
    "argTypes":["com.example.GetUserRequest"],
    "args":[{"userId":123}]
  }
}' | ./sofarpc exec --stdin
```

完整的信封字段、错误码含义、断言语义见 **[docs/agent-integration.md](docs/agent-integration.md)**。

### 地址别名

把常用地址起名，`address` 字段直接填别名：

```bash
sofarpc server add user-test 10.74.194.40:12200 --desc "测试环境"
sofarpc server list
sofarpc server remove user-test
```

别名表在 `~/.sofarpc/servers.json`，纯客户端，daemon 无感知。含端口的字面地址直接透传不走查表。

### 辅助命令

```bash
sofarpc daemon status   # 查看 daemon 状态
sofarpc daemon stop     # 优雅退出
sofarpc version         # 打印构建版本
```

运行态文件在 `~/.sofarpc/daemon/`：`state.json`（pid/端口）、`daemon.log`（日志）、`daemon.lock`（启动锁）。别名表在 `~/.sofarpc/servers.json`。

### 测试

```bash
# Java daemon（23 个单元测试）
mvn -f daemon/pom.xml test

# Go 客户端（单元 + 契约）
cd cli && go test ./...

# E2E（会真实起 JVM daemon，tag 隔离）
cd cli && go test -tags=e2e ./internal/e2e/...
```

### 开发参考

- 架构设计：[docs/agent-first-architecture-design.md](docs/agent-first-architecture-design.md)
- Daemon 进程模型：[docs/daemon-design.md](docs/daemon-design.md)
- 协议 Schema：[protocol/schema/](protocol/schema/)
- 双端契约样本：[protocol/fixtures/](protocol/fixtures/)

修协议 → 改 `protocol/schema/` 并同步 fixtures，两侧 contract test 会自动发现。

---

## English

Agent-first SofaRPC invocation tool: **Go thin client + resident Java daemon**. One JSON envelope in, one JSON envelope out. Built for LLM agents, scripts, and CI pipelines.

### Highlights

- **Single-line JSON envelope protocol**: `echo '{...}' | sofarpc exec --stdin` reads one, writes one
- **Cold start once, warm thereafter**: first call boots the daemon (~6s JVM cold start); subsequent same-host calls p50 < 1ms; 15-minute idle auto-reaper
- **Generic invocation** via SOFARPC 5.12 `GenericService` over bolt — no business API jar required
- **Stable error codes**: 8 `ErrorCode` values precisely classify failures (`CONNECT_FAILED` / `RPC_TIMEOUT` / `INVOKE_FAILED` / `ASSERTION_FAILED` / …), so agents can decide retry strategy from `code` alone
- **Stdlib-only**: Go client has zero third-party deps; Java daemon needs only JDK 8+

### Layout

```
sofarpc-cli/
├── cli/          # Go thin client (sofarpc binary)
├── daemon/       # Java resident daemon (sofarpcd.jar)
├── protocol/     # JSON Schema + fixtures (shared wire contract)
├── scripts/      # install scripts
└── docs/
    ├── agent-integration.md                # ← THE entry point for agents
    ├── agent-first-architecture-design.md  # architecture deep dive
    └── daemon-design.md                    # daemon process model notes
```

### Install

```bash
# One-shot: build + copy to ~/.sofarpc/
./scripts/install.sh

# Add to PATH
export PATH="$HOME/.sofarpc:$PATH"

# Uninstall
./scripts/install.sh --uninstall
```

`scripts/install.sh` builds both the Go client and the daemon jar, then copies them to `~/.sofarpc/sofarpc` and `~/.sofarpc/sofarpcd.jar`. If a daemon is already running, it stops it after a 3-second grace window so the next call spawns a fresh JVM with the new jar.

### Manual build

```bash
# Java daemon → daemon/target/sofarpcd.jar
mvn -f daemon/pom.xml package -DskipTests

# Go client → cli/sofarpc
cd cli && go build -o sofarpc ./cmd/sofarpc
```

Go 1.19+, JDK 8+, Maven 3.6+. macOS / Linux tested (Windows not supported).

### Minimal Example

```bash
JAR=daemon/target/sofarpcd.jar

# daemon self-check (~6s cold start)
echo '{"op":"health","payload":{}}' | ./sofarpc exec --stdin --jar $JAR

# connectivity probe
echo '{"op":"ping","payload":{"address":"10.0.0.1:12200","service":"com.example.UserService"}}' \
  | ./sofarpc exec --stdin

# business invocation (warm reuse, p50 < 1ms)
echo '{
  "op":"invoke",
  "payload":{
    "address":"10.0.0.1:12200",
    "service":"com.example.UserService",
    "method":"getUser",
    "argTypes":["com.example.GetUserRequest"],
    "args":[{"userId":123}]
  }
}' | ./sofarpc exec --stdin
```

Full envelope schema, error-code semantics, and assertion behavior: **[docs/agent-integration.md](docs/agent-integration.md)**.

### Address Aliases

Name your frequent endpoints so envelopes stay short:

```bash
sofarpc server add user-test 10.74.194.40:12200 --desc "test env"
sofarpc server list
sofarpc server remove user-test
```

Aliases live in `~/.sofarpc/servers.json` (client-side only — daemon never sees them). Literal `host:port` passes through without a lookup.

### Ancillary Commands

```bash
sofarpc daemon status   # inspect daemon state
sofarpc daemon stop     # graceful shutdown
sofarpc version         # print build version
```

Runtime files live in `~/.sofarpc/daemon/`: `state.json` (pid/port), `daemon.log`, `daemon.lock`. Aliases at `~/.sofarpc/servers.json`.

### Testing

```bash
# Java daemon (23 unit tests)
mvn -f daemon/pom.xml test

# Go client (unit + contract)
cd cli && go test ./...

# E2E — boots a real JVM daemon, isolated by build tag
cd cli && go test -tags=e2e ./internal/e2e/...
```

### References

- Architecture: [docs/agent-first-architecture-design.md](docs/agent-first-architecture-design.md)
- Daemon process model: [docs/daemon-design.md](docs/daemon-design.md)
- Protocol schema: [protocol/schema/](protocol/schema/)
- Contract fixtures: [protocol/fixtures/](protocol/fixtures/)

Protocol changes go in `protocol/schema/` with matching fixtures — both sides' contract tests will pick them up automatically.

### License

MIT
