# SofaRPC CLI

[中文](#中文) | [English](#english)

---

## 中文

命令行工具，用于测试和验证 SofaRPC 接口。基于泛化调用（GenericService），**无需依赖业务 API jar**，即可直连调用任意 RPC 服务。

### 核心能力

- **服务探活** — 验证 RPC 层连通性
- **单次调用** — 传参调用任意接口方法，支持 JSONPath 断言
- **批量测试** — 并行执行测试用例目录，自动校验返回值
- **测试报告** — 生成 Markdown / HTML 格式报告

### 技术选型

| 项目 | 选型 | 说明 |
|------|------|------|
| 语言 | Java 8 | 与业务工程保持一致 |
| CLI 框架 | Picocli 4.7.5 | 注解驱动，自动生成 help/version |
| RPC 调用 | sofa-rpc-all 5.12.0 | 泛化调用，无需业务 API jar |
| JSON | Jackson 2.16 | 参数输入与结果输出 |
| 断言 | json-path 2.9.0 | JSONPath 表达式校验返回值 |
| 打包 | maven-shade-plugin | Fat jar，含 SPI 文件合并 |

### 环境要求

| 项目 | 要求 |
|------|------|
| JDK | 8+ (已在 JDK 8、11、17 上测试) |
| OS | macOS、Linux（依赖 bash wrapper） |
| Maven | 3.6+（仅构建时需要） |

### 安装

```bash
# 构建
mvn clean package -DskipTests

# 安装
bash install.sh

# 加入 PATH（写入 ~/.zshrc 或 ~/.bashrc）
export PATH="$HOME/.sofarpc:$PATH"

# 验证
sofarpc --version
```

### 升级

```bash
# 拉取最新代码后重新构建安装
git pull
mvn clean package -DskipTests
bash install.sh
```

`install.sh` 会覆盖 `~/.sofarpc/sofarpc.jar` 和 wrapper 脚本，`servers.yaml` 和 `config.yaml` 不受影响。

### 快速开始

```bash
# 1. 注册服务地址
sofarpc server add my-server 192.168.1.100:12200 --desc "测试环境"

# 2. 探活
sofarpc ping --server my-server --service com.example.UserService

# 3. 调用接口（完整语法）
sofarpc invoke \
  --server my-server \
  --service com.example.UserService \
  --method getUser \
  --arg-types 'com.example.GetUserRequest' \
  --args '{"userId": 123}' \
  --json

# 3b. 调用接口（简写语法）
sofarpc call my-server com.example.UserService/getUser '{"userId": 123}' \
  --arg-types 'com.example.GetUserRequest' --json

# 4. 带断言调用
sofarpc invoke \
  --server my-server \
  --service com.example.UserService \
  --method getUser \
  --arg-types 'com.example.GetUserRequest' \
  --args '{"userId": 123}' \
  --assert '$.status == "ACTIVE"' \
  --json

# 5. 批量测试（递归扫描子目录）
sofarpc batch --server my-server --cases ./cases/ --parallel 4 --json > result.json

# 6. 生成报告
sofarpc report --input result.json --format markdown
```

### 命令一览

| 命令 | 说明 |
|------|------|
| `server add/list/remove/export/import` | 服务地址管理 |
| `ping` | RPC 层连通性探测 |
| `invoke` | 单次泛化调用（完整语法） |
| `call` | 单次泛化调用（简写语法） |
| `batch` | 批量执行测试用例（递归扫描子目录） |
| `report` | 生成测试报告 |

### 参数格式

| 场景 | `--args` | `--arg-types` |
|------|----------|---------------|
| 单个对象参数 | `'{"field": value}'` | 该对象的全限定类名 |
| 多个参数 | `'[arg1, arg2, ...]'` | 逗号分隔的各参数类型 |
| 无参数 | 不传 | 不传 |

### 批量测试用例格式

```json
{
  "service": "com.example.UserService",
  "method": "getUser",
  "argTypes": ["com.example.GetUserRequest"],
  "args": { "userId": 123 },
  "expect": {
    "$.status": "ACTIVE"
  }
}
```

### Exit Code

| Code | 含义 |
|------|------|
| 0 | 成功 |
| 1 | 调用失败 / 断言不通过 |
| 2 | 连接失败 |
| 3 | 参数错误 |
| 4 | 服务别名不存在 |

### 配置文件

```
~/.sofarpc/
├── sofarpc.jar      # CLI 本体
├── sofarpc          # shell wrapper
├── servers.yaml     # 服务地址
└── config.yaml      # 全局配置（timeout、parallel）
```

配置优先级：命令行参数 > config.yaml > 内置默认值

### 工程结构

```
src/main/java/com/sofarpc/cli/
├── Main.java                       # 入口（含日志压制）
├── command/
│   ├── ServerCommand.java          # 服务地址管理
│   ├── PingCommand.java            # 冒烟测试
│   ├── InvokeCommand.java          # 单次调用（完整语法）
│   ├── CallCommand.java            # 单次调用（简写语法）
│   ├── BatchCommand.java           # 批量测试
│   └── ReportCommand.java          # 生成报告
├── core/
│   ├── ExitCodes.java              # 统一退出码常量
│   ├── ExceptionClassifier.java    # 异常 → 退出码映射
│   ├── JacksonHolder.java          # 共享 ObjectMapper 实例
│   ├── RpcClientFactory.java       # 泛化调用 + 连接缓存
│   ├── ServerStore.java            # 服务地址存储（原子写入）
│   └── GlobalConfig.java           # 全局配置
├── service/
│   ├── RpcInvokeService.java       # 核心调用服务
│   ├── ArgParser.java              # 参数解析
│   ├── AssertionEvaluator.java     # 断言求值
│   └── OutputFormatter.java        # 统一输出格式化
└── output/
    ├── JsonPrinter.java            # JSON 输出
    └── ReportGenerator.java        # 报告生成
```

### 核心设计

**泛化调用**：通过 `GenericService.$genericInvoke()` 实现动态调用，无需 classpath 上持有接口类。调用时指定参数类型全限定名和 JSON 参数即可。

**连接缓存**：`RpcClientFactory` 按 `address::interfaceId::timeout` 缓存 `GenericService` 实例，避免重复建连。地址变更或超时调整会自动创建新连接。支持按地址定向销毁，batch 执行完毕后自动释放。JVM shutdown hook 兜底释放。

**日志压制**：CLI 输出必须干净（stdout 只有 JSON），因此在入口处压制 SofaRPC、Netty、Hessian 等框架日志。

---

## English

Command-line tool for testing and verifying SofaRPC interfaces. Built on generic invocation (GenericService), it calls any RPC service **without requiring the business API jar** on the classpath.

### Features

- **Ping** — Verify RPC-layer connectivity
- **Invoke** — Call any interface method with parameters, supports JSONPath assertions
- **Batch** — Run test cases in parallel with automatic result validation
- **Report** — Generate Markdown / HTML test reports

### Tech Stack

| Component | Choice | Notes |
|-----------|--------|-------|
| Language | Java 8 | Consistent with business projects |
| CLI Framework | Picocli 4.7.5 | Annotation-driven, auto-generated help/version |
| RPC | sofa-rpc-all 5.12.0 | Generic invocation, no business API jar needed |
| JSON | Jackson 2.16 | Parameter input and result output |
| Assertions | json-path 2.9.0 | JSONPath expression validation |
| Packaging | maven-shade-plugin | Fat jar with SPI file merging |

### Requirements

| Item | Requirement |
|------|-------------|
| JDK | 8+ (tested on JDK 8, 11, 17) |
| OS | macOS, Linux (requires bash wrapper) |
| Maven | 3.6+ (build-time only) |

### Installation

```bash
# Build
mvn clean package -DskipTests

# Install
bash install.sh

# Add to PATH (append to ~/.zshrc or ~/.bashrc)
export PATH="$HOME/.sofarpc:$PATH"

# Verify
sofarpc --version
```

### Upgrade

```bash
# Pull latest code and rebuild
git pull
mvn clean package -DskipTests
bash install.sh
```

`install.sh` overwrites `~/.sofarpc/sofarpc.jar` and the wrapper script. `servers.yaml` and `config.yaml` are preserved.

### Quick Start

```bash
# 1. Register a server
sofarpc server add my-server 192.168.1.100:12200 --desc "test env"

# 2. Ping
sofarpc ping --server my-server --service com.example.UserService

# 3. Invoke a method (full syntax)
sofarpc invoke \
  --server my-server \
  --service com.example.UserService \
  --method getUser \
  --arg-types 'com.example.GetUserRequest' \
  --args '{"userId": 123}' \
  --json

# 3b. Invoke a method (shorthand)
sofarpc call my-server com.example.UserService/getUser '{"userId": 123}' \
  --arg-types 'com.example.GetUserRequest' --json

# 4. Invoke with assertion
sofarpc invoke \
  --server my-server \
  --service com.example.UserService \
  --method getUser \
  --arg-types 'com.example.GetUserRequest' \
  --args '{"userId": 123}' \
  --assert '$.status == "ACTIVE"' \
  --json

# 5. Batch test (recursive subdirectory scan)
sofarpc batch --server my-server --cases ./cases/ --parallel 4 --json > result.json

# 6. Generate report
sofarpc report --input result.json --format markdown
```

### Commands

| Command | Description |
|---------|-------------|
| `server add/list/remove/export/import` | Manage server aliases |
| `ping` | RPC-layer connectivity check |
| `invoke` | Single generic invocation (full syntax) |
| `call` | Single generic invocation (shorthand) |
| `batch` | Batch execute test cases (recursive subdirectory scan) |
| `report` | Generate test report |

### Argument Format

| Scenario | `--args` | `--arg-types` |
|----------|----------|---------------|
| Single object param | `'{"field": value}'` | Fully-qualified class name |
| Multiple params | `'[arg1, arg2, ...]'` | Comma-separated type names |
| No params | Omit | Omit |

### Batch Test Case Format

```json
{
  "service": "com.example.UserService",
  "method": "getUser",
  "argTypes": ["com.example.GetUserRequest"],
  "args": { "userId": 123 },
  "expect": {
    "$.status": "ACTIVE"
  }
}
```

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Invocation failed / assertion failed |
| 2 | Connection failed |
| 3 | Bad arguments |
| 4 | Server alias not found |

### Configuration

```
~/.sofarpc/
├── sofarpc.jar      # CLI binary
├── sofarpc          # Shell wrapper
├── servers.yaml     # Server aliases
└── config.yaml      # Defaults (timeout, parallel)
```

Priority: CLI flags > config.yaml > built-in defaults

### Project Structure

```
src/main/java/com/sofarpc/cli/
├── Main.java                       # Entry point (with log suppression)
├── command/
│   ├── ServerCommand.java          # Server alias management
│   ├── PingCommand.java            # Smoke test
│   ├── InvokeCommand.java          # Single invocation (full syntax)
│   ├── CallCommand.java            # Single invocation (shorthand)
│   ├── BatchCommand.java           # Batch testing
│   └── ReportCommand.java          # Report generation
├── core/
│   ├── ExitCodes.java              # Unified exit code constants
│   ├── ExceptionClassifier.java    # Exception → exit code mapping
│   ├── JacksonHolder.java          # Shared ObjectMapper instances
│   ├── RpcClientFactory.java       # Generic invocation + connection cache
│   ├── ServerStore.java            # Server alias storage (atomic write)
│   └── GlobalConfig.java           # Global configuration
├── service/
│   ├── RpcInvokeService.java       # Core invocation service
│   ├── ArgParser.java              # Argument parsing
│   ├── AssertionEvaluator.java     # Assertion evaluation
│   └── OutputFormatter.java        # Unified output formatting
└── output/
    ├── JsonPrinter.java            # JSON output
    └── ReportGenerator.java        # Report generation
```

### Design

**Generic Invocation**: Uses `GenericService.$genericInvoke()` for dynamic calls without requiring interface classes on the classpath. Specify parameter types as fully-qualified class names and pass JSON arguments.

**Connection Cache**: `RpcClientFactory` caches `GenericService` instances by `address::interfaceId::timeout`, avoiding repeated connection setup. Address changes or timeout adjustments trigger new connections automatically. Supports targeted cleanup by address; batch mode releases connections after execution. A JVM shutdown hook ensures final cleanup.

**Log Suppression**: CLI output must be clean (stdout is JSON only), so all framework logs (SofaRPC, Netty, Hessian) are suppressed at startup.

### License

MIT
