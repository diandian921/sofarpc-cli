# SofaRPC CLI 设计文档

## 1. 背景与目标

当前工程基于 SofaBoot 框架，应用间通信使用 SofaRPC（Bolt 协议）。为了配合 Claude Code 做接口验证工作，需要一个命令行工具，能够：

- 验证服务是否可达（冒烟测试）
- 传参调用接口并验证返回值
- 批量执行测试用例
- 生成测试报告

CLI 作为独立工具，不依赖业务工程，可移植给团队同事使用。

---

## 2. 技术选型

| 项目 | 选型 | 说明 |
|------|------|------|
| 语言 | Java 8 | 与业务工程保持一致 |
| CLI 框架 | Picocli 4.7.5 | 注解驱动，自动生成 help/version，支持子命令 |
| RPC 调用 | sofa-rpc-all 5.x + GenericService | 泛化调用，无需业务 API jar |
| JSON 处理 | Jackson 2.x | 参数输入与结果输出 |
| YAML 处理 | jackson-dataformat-yaml | 本地配置文件读写 |
| JSONPath | com.jayway.jsonpath:json-path | 断言表达式解析 |
| 打包方式 | maven-shade-plugin | 打包为可执行 fat jar |

---

## 3. 核心设计：泛化调用

**CLI 不依赖业务 API jar**，通过 SofaRPC 泛化调用（GenericService）实现动态调用，无需在 classpath 上持有接口类。

```java
ConsumerConfig<GenericService> config = new ConsumerConfig<GenericService>()
    .setInterfaceId("com.yourco.UserService")
    .setGeneric(true)                          // 开启泛化调用
    .setProtocol("bolt")
    .setDirectUrl("bolt://192.168.1.100:12200")
    .setRegister(false)
    .setSubscribe(false);

GenericService service = config.refer();

// 调用时需指定参数类型
Object result = service.$genericInvoke(
    "getUser",
    new String[]{"com.yourco.GetUserRequest"},  // 参数类型全限定名
    new Object[]{argsMap}                        // 参数值（Map 形式）
);
```

这是整个方案能跑通的核心前提，所有调用命令都基于此实现。

---

## 4. 工程结构

```
sofarpc-cli/
├── pom.xml
├── SKILL.md                               ← Claude Code skill 文件（独立维护）
├── cases/                                 ← 测试用例目录示例
│   └── user-service/
│       ├── getUser.json
│       └── createUser.json
├── reports/                               ← 报告输出目录
└── src/main/java/com/sofarpc/cli/
    ├── Main.java                          ← 程序入口（含日志压制、版本号）
    ├── command/
    │   ├── ServerCommand.java             ← 服务地址管理
    │   ├── PingCommand.java               ← 冒烟测试
    │   ├── InvokeCommand.java             ← 单次调用
    │   ├── BatchCommand.java              ← 批量测试
    │   └── ReportCommand.java             ← 生成报告
    ├── core/
    │   ├── RpcClientFactory.java          ← 泛化调用 + 连接缓存
    │   └── ServerStore.java               ← 服务地址本地存储
    └── output/
        ├── JsonPrinter.java               ← JSON 格式输出
        └── ReportGenerator.java           ← 报告生成
```

**本地配置目录：**

```
~/.sofarpc/
├── sofarpc.jar      ← CLI 工具本体
├── sofarpc          ← shell wrapper，加入 PATH 后可直接用 sofarpc 命令
├── SKILL.md         ← Claude Code skill 文件（随 CLI 工具存放）
├── servers.yaml     ← 服务地址配置
└── config.yaml      ← 全局配置（默认超时等）
```

---

## 5. 服务地址管理

服务地址统一通过 `server` 子命令管理，存储在 `~/.sofarpc/servers.yaml`，使用别名引用，避免记忆 IP 和端口。

### 5.1 添加

```bash
sofarpc server add user-dev 192.168.1.100:12200
sofarpc server add user-dev 192.168.1.100:12200 --desc "用户服务开发环境"
```

输出：
```
✅ 已保存 user-dev -> 192.168.1.100:12200
```

### 5.2 查看列表

```bash
sofarpc server list

# 支持模糊搜索
sofarpc server list user
```

输出：
```
NAME        ADDRESS                 DESC
user-dev    192.168.1.100:12200    用户服务开发环境
user-prod   10.0.0.100:12200       用户服务生产环境
order-dev   192.168.1.101:12200    订单服务开发环境
```

### 5.3 删除

```bash
sofarpc server remove user-dev
```

### 5.4 导出 / 导入（团队共享）

```bash
# 导出，丢到群里或内部 Wiki
sofarpc server export > servers.yaml

# 同事导入
sofarpc server import servers.yaml
```

`servers.yaml` 格式：

```yaml
servers:
  user-dev:
    address: 192.168.1.100:12200
    desc: 用户服务开发环境
  order-dev:
    address: 192.168.1.101:12200
    desc: 订单服务开发环境
```

---

## 6. 命令设计

### 6.1 ping — 冒烟测试

ping 不做 TCP 层探测，而是**通过泛化调用一个不存在的方法**来验证 RPC 层是否正常。能连上服务端并收到响应（哪怕是方法不存在的错误）即视为 UP；连不上则为 DOWN。

**用法：**
```bash
sofarpc ping \
  --server user-dev \
  --service com.yourco.UserService
```

**输出：**
```json
{
  "status": "UP",
  "server": "user-dev",
  "address": "192.168.1.100:12200",
  "service": "com.yourco.UserService",
  "latencyMs": 12
}
```

**参数说明：**

| 参数 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `--server` | 是 | - | 服务别名 |
| `--service` | 是 | - | 接口全限定类名 |
| `--timeout` | 否 | 由 config.yaml 决定 | 超时时间（ms）|

---

### 6.2 invoke — 单次调用

**用法：**
```bash
# 单参数（对象）+ 断言
sofarpc invoke \
  --server user-dev \
  --service com.yourco.UserService \
  --method getUser \
  --arg-types 'com.yourco.GetUserRequest' \
  --args '{"id": 123}' \
  --assert '$.status == "ACTIVE"' \
  --json

# 多参数（数组）
sofarpc invoke \
  --server user-dev \
  --service com.yourco.AccountService \
  --method transfer \
  --arg-types 'java.lang.String,java.lang.String,java.math.BigDecimal' \
  --args '["A001", "B002", 100.00]' \
  --json
```

**参数格式规则：**

| 场景 | `--args` 格式 | `--arg-types` |
|------|--------------|---------------|
| 单个对象参数 | `{"field": value}` | 该对象的全限定类名 |
| 多个参数 | `[arg1, arg2, ...]` | 逗号分隔的各参数类型 |
| 无参数 | 不传 | 不传 |

**输出（无断言）：**
```json
{
  "success": true,
  "latencyMs": 45,
  "result": {
    "id": 123,
    "name": "张三",
    "status": "ACTIVE"
  },
  "error": null
}
```

**输出（有断言）：**
```json
{
  "success": true,
  "latencyMs": 45,
  "result": {
    "id": 123,
    "name": "张三",
    "status": "ACTIVE"
  },
  "assertionPassed": true,
  "error": null
}
```

**参数说明：**

| 参数 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `--server` | 是 | - | 服务别名 |
| `--service` | 是 | - | 接口全限定类名 |
| `--method` | 是 | - | 方法名 |
| `--arg-types` | 当 --args 存在时必填 | - | 参数类型，逗号分隔；无参调用时不需要 |
| `--args` | 否 | - | JSON 格式参数，单参数用对象，多参数用数组 |
| `--assert` | 否 | - | JSONPath 断言，失败时 exit code=1 |
| `--timeout` | 否 | 由 config.yaml 决定 | 超时时间（ms）|
| `--json` | 否 | false | 以 JSON 格式输出，默认人类可读格式 |

---

### 6.3 batch — 批量测试

**用法：**
```bash
sofarpc batch \
  --server user-dev \
  --cases ./cases/user-service/ \
  --parallel 4 \
  --json > result.json
```

**测试用例文件格式：**
```json
{
  "service": "com.yourco.UserService",
  "method": "getUser",
  "argTypes": ["com.yourco.GetUserRequest"],
  "args": { "id": 123 },
  "expect": {
    "$.name": "张三",
    "$.status": "ACTIVE"
  }
}
```

**输出：**
```json
{
  "total": 10,
  "passed": 9,
  "failed": 1,
  "startTime": "2026-04-12 14:30:00",
  "duration": "2.3s",
  "results": [
    {
      "case": "getUser.json",
      "passed": true,
      "latencyMs": 45
    },
    {
      "case": "createUser.json",
      "passed": false,
      "latencyMs": 120,
      "error": "Assertion failed: $.status expected ACTIVE but got PENDING"
    }
  ]
}
```

**参数说明：**

| 参数 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `--server` | 是 | - | 服务别名 |
| `--cases` | 是 | - | 用例目录路径 |
| `--parallel` | 否 | 由 config.yaml 决定 | 并发线程数，使用 ExecutorService 管理 |
| `--json` | 否 | false | JSON 格式输出，默认人类可读格式 |

---

### 6.4 report — 生成测试报告

**用法：**
```bash
sofarpc report \
  --input ./result.json \
  --format markdown \
  --output ./reports/
```

**报告内容：**
- 执行时间戳
- 总体通过率
- 各用例执行结果
- 失败用例详情（实际值 vs 期望值）
- 耗时统计（平均、最大、最小）

**参数说明：**

| 参数 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `--input` | 是 | - | batch 输出的 JSON 文件 |
| `--format` | 否 | markdown | 报告格式：markdown / html |
| `--output` | 否 | ./reports/ | 报告输出目录 |

---

## 7. 连接管理

### 7.1 连接缓存

`RpcClientFactory` 按 `server + service` 缓存 `GenericService` 实例，同一组合复用同一连接，避免 batch 模式下频繁建立 TCP 连接。

```
缓存 key: "user-dev::com.yourco.UserService"
缓存 value: GenericService 实例
```

### 7.2 生命周期

- CLI 启动时懒加载，首次调用时建立连接
- batch 结束后统一 `unRefer()` 释放
- 注册 JVM shutdown hook 兜底释放所有连接

---

## 8. 日志压制

SofaRPC 启动时 Netty、Hessian 会输出大量日志到 stdout，干扰 JSON 输出解析。必须在 `Main.java` 第一行压制：

```java
public static void main(String[] args) {
    // 第一行：压制框架日志，保证 stdout 只有业务输出
    System.setProperty("com.alipay.sofa.rpc.log.level", "ERROR");
    System.setProperty("logging.level.root", "ERROR");

    int exitCode = new CommandLine(new Main()).execute(args);
    System.exit(exitCode);
}
```

同时提供 `src/main/resources/logback.xml`，将所有非业务日志设为 OFF。

---

## 9. 版本号

在 `Main.java` 的 `@Command` 注解中声明版本号，Picocli 自动支持 `--version`：

```java
@Command(
    name = "sofarpc",
    version = "sofarpc 1.0.0",
    mixinStandardHelpOptions = true,   // 自动包含 --help 和 --version
    ...
)
public class Main { ... }
```

使用：
```bash
sofarpc --version
# 输出: sofarpc 1.0.0
```

---

## 10. 全局配置

`~/.sofarpc/config.yaml` 存储全局默认配置，参数优先级为：

```
命令行参数 > config.yaml > 内置默认值
```

```yaml
defaults:
  timeout: 5000      # 默认超时（ms）
  parallel: 1        # batch 默认并发数
```

> 输出格式默认人类可读，需要 JSON 时显式传 `--json`。Claude Code 在 SKILL.md 中约定所有调用都带 `--json`。

---

## 11. Exit Code 规范

| Code | 含义 |
|------|------|
| 0 | 成功 |
| 1 | 调用失败 / 断言不通过 |
| 2 | 连接失败 |
| 3 | 参数错误 |
| 4 | 服务别名不存在 |

---

## 12. 关键依赖

```xml
<!-- CLI 框架 -->
<dependency>
    <groupId>info.picocli</groupId>
    <artifactId>picocli</artifactId>
    <version>4.7.5</version>
</dependency>

<!-- SofaRPC（泛化调用，不需要业务 API jar）-->
<dependency>
    <groupId>com.alipay.sofa</groupId>
    <artifactId>sofa-rpc-all</artifactId>
    <version>5.12.0</version>
</dependency>

<!-- JSON 处理 -->
<dependency>
    <groupId>com.fasterxml.jackson.core</groupId>
    <artifactId>jackson-databind</artifactId>
    <version>2.16.0</version>
</dependency>

<!-- YAML 配置读写 -->
<dependency>
    <groupId>com.fasterxml.jackson.dataformat</groupId>
    <artifactId>jackson-dataformat-yaml</artifactId>
    <version>2.16.0</version>
</dependency>

<!-- JSONPath 断言 -->
<dependency>
    <groupId>com.jayway.jsonpath</groupId>
    <artifactId>json-path</artifactId>
    <version>2.9.0</version>
</dependency>

<!-- 打包为 fat jar，必须加 ServicesResourceTransformer 防止 SPI 文件被覆盖 -->
<plugin>
    <groupId>org.apache.maven.plugins</groupId>
    <artifactId>maven-shade-plugin</artifactId>
    <version>3.5.1</version>
    <configuration>
        <transformers>
            <transformer implementation=
              "org.apache.maven.plugins.shade.resource.ManifestResourceTransformer">
                <mainClass>com.sofarpc.cli.Main</mainClass>
            </transformer>
            <transformer implementation=
              "org.apache.maven.plugins.shade.resource.ServicesResourceTransformer"/>
        </transformers>
    </configuration>
</plugin>
```

---

## 13. SKILL.md 与 Claude Code 集成

### 存放位置

SKILL.md 随 CLI 工具一起存放在 `~/.sofarpc/` 目录下，与工具本体绑定，升级时一并更新。

### 用户项目接入

用户只需在项目的 `CLAUDE.md` 中加一行引用：

```markdown
@~/.sofarpc/SKILL.md
```

### SKILL.md 内容

```markdown
# SofaRPC CLI — Skill

## 工具调用方式
sofarpc <命令>   （需已完成安装，见安装说明）

## 环境要求
- JDK 8+
- 网络可达目标服务

## 配置目录
~/.sofarpc/servers.yaml   ← 服务地址
~/.sofarpc/config.yaml    ← 全局配置

## 命令速查

### 服务地址管理
sofarpc server list                                    # 查看全部
sofarpc server list <keyword>                          # 模糊搜索
sofarpc server add <n> <ip:port> [--desc <备注>]   # 添加
sofarpc server remove <n>                           # 删除
sofarpc server export > servers.yaml                   # 导出给同事
sofarpc server import servers.yaml                     # 从同事处导入

### 冒烟测试
sofarpc ping --server <n> --service <接口全限定类名>
Exit: 0=UP 1=DOWN 2=连接失败 4=别名不存在

### 单次调用（单参数）
sofarpc invoke \
  --server <n> \
  --service <接口全限定类名> \
  --method <方法名> \
  --arg-types '<参数全限定类名>' \
  --args '<json对象>' \
  --assert '<JSONPath表达式>' \
  --json

### 单次调用（多参数）
sofarpc invoke \
  --server <n> \
  --service <接口全限定类名> \
  --method <方法名> \
  --arg-types '<类型1>,<类型2>' \
  --args '[值1, 值2]' \
  --json

### 批量测试
sofarpc batch --server <n> --cases <dir> --json > result.json

### 生成报告
sofarpc report --input result.json --format markdown

## 标准工作流
1. server list 确认目标服务已配置
2. ping 确认服务存活
3. invoke 验证单个接口（建议带 --assert 做返回值校验）
4. batch 批量跑用例目录
5. report 生成测试报告

## 注意事项
- 所有调用必须带 --json，保证输出可解析
- --args 必须是合法 JSON
- --arg-types 在有 --args 时必须指定，无参方法可省略
- 正常输出到 stdout，错误信息到 stderr
- batch 结果建议重定向到文件再生成报告
```

---

## 14. 实施步骤

1. **搭工程骨架** — 创建 Maven 工程，配置 pom.xml（重点验证 ServicesResourceTransformer）
2. **验证泛化调用** — 最小 demo 跑通 GenericService，这是最大技术风险点
3. **实现 server 命令** — `~/.sofarpc/servers.yaml` 读写正常
4. **实现 ping** — 基于泛化调用探测 RPC 层连通性
5. **实现 invoke** — 支持单参数对象和多参数数组，含 --assert 断言
6. **实现 batch** — ExecutorService 并发控制，连接复用
7. **实现 report** — markdown / html 双格式，含时间戳
8. **写 SKILL.md** — 放到 `~/.sofarpc/`，验证 Claude Code @引用生效
9. **提供安装脚本** — 生成 shell wrapper 并写入 PATH 提示：

```bash
# install.sh
#!/bin/bash
INSTALL_DIR="$HOME/.sofarpc"
mkdir -p "$INSTALL_DIR"

# 复制 jar
cp sofarpc.jar "$INSTALL_DIR/sofarpc.jar"

# 写 shell wrapper
cat > "$INSTALL_DIR/sofarpc" << 'EOF'
#!/bin/bash
exec java -jar "$HOME/.sofarpc/sofarpc.jar" "$@"
EOF
chmod +x "$INSTALL_DIR/sofarpc"

# 复制 SKILL.md
cp SKILL.md "$INSTALL_DIR/SKILL.md"

echo "✅ 安装完成，请将以下内容加入你的 ~/.zshrc 或 ~/.bashrc："
echo ""
echo '  export PATH="$HOME/.sofarpc:$PATH"'
echo ""
echo "执行后重新打开终端，即可使用 sofarpc 命令。"
```
