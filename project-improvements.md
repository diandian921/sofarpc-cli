# SofaRPC CLI 当前项目改进建议

基于当前工作区代码、构建结果和命令行实际行为，对项目现状做了一轮静态阅读和运行验证。

> **更新记录：**
> - 第一轮改造后，Phase 1（结构重构）、Phase 2（行为修复）、Phase 3（文档对齐）已完成。
> - 2026-04-12 第二轮复查发现，仍有若干退出码和参数校验问题未完全收口。下文中已修复的问题标记为 ✅，复查新增遗留问题单独列出。
> - 2026-04-13 第三轮复查确认：第二轮提出的大部分命令行为问题已修复，但配置文件健壮性和测试基线仍有明显缺口。
> - 2026-04-13 第四轮复查确认：`GlobalConfig` 类型校验和 `ping` 的 `timeout` 校验已修复，但又发现了若干新的运行期边界问题。

## 总体判断

- 方向正确：目标明确，CLI 边界清晰，fat jar 可以正常打包。
- Phase 1-3 完成后，核心契约已落地，服务层已抽出，文档已对齐。
- 剩余短板集中在测试基线和可选的体验增强。

## 已确认问题

### ✅ P0：批量用例发现逻辑和文档约定不一致（Phase 2 已修复：改为 Files.walk 递归扫描）

- 代码位置：`src/main/java/com/sofarpc/cli/command/BatchCommand.java:78`
- 文档位置：`README.md:74`
- 当前实现只扫描 `--cases` 目录第一层的 `.json` 文件，不会递归进入子目录。
- README 的示例是直接传 `./cases/`，但当前仓库里的 `cases/` 目录只有子目录，没有顶层 JSON 文件。
- 这意味着用户按文档操作时，很容易直接得到“用例目录下没有 .json 文件”。

建议：

- 将用例扫描改为递归查找。
- 或者明确限制目录结构，并同步修正文档和示例目录。
- 更合理的做法是支持递归，因为这更符合批量测试场景。

### ✅ P0：Exit code 契约没有完整落地（Phase 1 已修复：ExceptionClassifier 统一异常→退出码映射）

- 文档位置：`README.md:112`
- 相关代码：
  - `src/main/java/com/sofarpc/cli/command/InvokeCommand.java:115`
  - `src/main/java/com/sofarpc/cli/command/CallCommand.java:119`
  - `src/main/java/com/sofarpc/cli/command/BatchCommand.java:206`
- README 约定 `2` 表示连接失败，但当前 `invoke` 和 `call` 几乎把所有调用异常都归为 `1`。
- `batch` 也没有单独区分连接失败、断言失败、参数错误，而是统一落成 case fail。
- 对脚本、CI、自动化排障来说，这会显著降低可观测性。

建议：

- 建立统一的异常分类层，把连接失败、调用失败、断言失败、参数错误分开。
- `invoke`、`call`、`batch` 应复用同一套 exit code 语义。
- 文档、代码、帮助输出要保持一致。

### ✅ P0：断言能力被文档写大了，实际只支持非常有限的比较（Phase 1 已修复：AssertionEvaluator 统一断言求值，batch expect 保留原始 JSON 类型）

- 文档位置：
  - `README.md:14`
  - `README.md:26`
- 相关代码：
  - `src/main/java/com/sofarpc/cli/command/InvokeCommand.java:169`
  - `src/main/java/com/sofarpc/cli/command/CallCommand.java:166`
  - `src/main/java/com/sofarpc/cli/command/BatchCommand.java:173`
- 当前 `--assert` 不是通用 JSONPath 表达式求值，而是手工按 `==` 拆分后做字符串比较。
- `batch` 的 `expect` 被强转成 `Map<String, String>`，数字、布尔、`null` 等类型都不稳。
- 文档描述会让用户以为支持更完整的 JSONPath 断言语义，容易产生误用。

建议：

- 明确缩窄文档描述，只承诺当前真正支持的语法。
- 或者补齐实现，支持更可靠的断言表达方式。
- `batch` 的断言值应保留原始 JSON 类型，不应全部压成字符串。

### ✅ P0：子命令 help/version 体验不完整（Phase 1 已修复：所有子命令开启 mixinStandardHelpOptions）

- 顶层命令位置：`src/main/java/com/sofarpc/cli/Main.java:20`
- 未开启标准 help 的子命令：
  - `src/main/java/com/sofarpc/cli/command/PingCommand.java:20`
  - `src/main/java/com/sofarpc/cli/command/InvokeCommand.java:24`
  - `src/main/java/com/sofarpc/cli/command/BatchCommand.java:32`
  - `src/main/java/com/sofarpc/cli/command/ReportCommand.java:17`
- 实测 `invoke --help`、`ping --help`、`batch --help`、`report --help` 返回的是缺参错误，而不是帮助信息。
- 这和 CLI 工具的基本使用习惯不一致，也和设计文档里“自动生成 help/version”的预期不一致。

建议：

- 所有用户可直接调用的命令统一开启 `mixinStandardHelpOptions = true`。
- 补一轮命令帮助输出测试，防止后续回归。

### ✅ P0：文档和真实实现已经出现漂移（Phase 3 已修复：README、design.md、SKILL.md 全部对齐）

- 文档位置：
  - `README.md:80`
  - `README.md:134`
- 代码位置：
  - `src/main/java/com/sofarpc/cli/Main.java:29`
  - `src/main/java/com/sofarpc/cli/command/CallCommand.java:25`
- 当前代码里已经有 `call` 命令，但 README 的命令总览和工程结构没有体现这个新增入口。
- 设计文档和 README 中多处描述的能力，也与当前真实行为存在细节差异。

建议：

- 把 README、design、帮助输出当成同一份契约来维护。
- 每次新增命令或改动行为时，同步更新文档。
- 更进一步，可以考虑用测试去校验帮助输出和 README 中的关键命令样例。

## 第二轮复查结果

### 已确认修复

- `batch` 已改为递归扫描子目录中的 `.json` 用例文件。
- `invoke` / `ping` / `batch` / `report` 子命令已支持标准 `--help` / `--version` 行为。
- `report --format` 已增加枚举校验，非法格式会返回参数错误。
- `ServerStore` 读取损坏 YAML 时不再静默吞错，且写入已改为原子写。

### 第二轮提出的问题（截至 2026-04-13 已大部分修复）

#### ✅ P0：`server` 子命令失败时仍然返回 exit code 0（2026-04-13 已修复：统一改为 `Callable<Integer>` 并返回非零退出码）

- 相关代码：
  - `src/main/java/com/sofarpc/cli/command/ServerCommand.java:40`
  - `src/main/java/com/sofarpc/cli/command/ServerCommand.java:161`
  - `src/main/java/com/sofarpc/cli/command/ServerCommand.java:213`
- 当前 `server add/remove/import` 仍是 `Runnable`，失败时只打印错误并 `return`，不会向进程返回非零退出码。
- 实测：
  - `server add bad badaddr --json`
  - `server remove missing --json`
  - `server import no-such-file.yaml`
  都会输出失败信息，但进程退出码仍为 `0`。

建议：

- 将 `server` 子命令统一迁移为 `Callable<Integer>`。
- 让地址格式错误、别名不存在、文件不存在、导入失败等场景返回非零退出码。
- 保持与 `invoke` / `ping` / `batch` / `report` 的退出码模型一致。

#### ✅ P0：`batch` 仍未把连接失败正确上抛为 exit code 2（2026-04-13 已修复：`CaseResult` 保留 `exitCode`，汇总时优先返回 `2`）

- 相关代码：
  - `src/main/java/com/sofarpc/cli/command/BatchCommand.java:155`
  - `src/main/java/com/sofarpc/cli/command/BatchCommand.java:195`
  - `src/main/java/com/sofarpc/cli/service/RpcInvokeService.java:81`
- `RpcInvokeService` 已能区分连接失败和调用失败，但 `BatchCommand` 在汇总结果时丢掉了这个分类信息。
- 当前只要有失败，最终就固定返回 `1`，这和 README 中 `2 = 连接失败` 的契约仍不完全一致。

建议：

- 在 `CaseResult` 或 batch 汇总阶段保留 `exitCode` / `failureType`。
- 如果 batch 中出现连接类失败，最终退出码应优先返回 `2`。
- 至少要明确 batch 的退出码优先级，并在文档中写清楚。

#### ✅ P1：`--parallel` 和配置值缺少下界校验，可能直接抛 Java 栈（2026-04-13 已部分修复：命令行范围校验已补上，配置文件类型错误见第三轮复查）

- 相关代码：
  - `src/main/java/com/sofarpc/cli/command/BatchCommand.java:95`
  - `src/main/java/com/sofarpc/cli/core/GlobalConfig.java:45`
- 当前没有校验 `parallel > 0`。
- 实测 `batch --parallel 0` 会在 `ThreadPoolExecutor` 构造时抛出 `IllegalArgumentException`，用户看到的是完整 Java stack trace，而不是参数错误提示。
- `config.yaml` 如果写入非法 `parallel` 值，也会有同类问题。

建议：

- 对命令行参数和配置文件统一做范围校验。
- 对非法值返回 `ExitCodes.BAD_ARGS`，不要把底层栈直接暴露给用户。
- `timeout` 也建议一起补上正数校验。

#### ✅ P1：`server import` 仍然绕过地址格式校验（2026-04-13 已修复：无效地址会被跳过并提示）

- 相关代码：
  - `src/main/java/com/sofarpc/cli/command/ServerCommand.java:56`
  - `src/main/java/com/sofarpc/cli/core/ServerStore.java:147`
- `server add` 已对 `host:port` 做了格式校验，但 `server import` 仍会把无效地址直接写入配置。
- 实测导入 `address: not-an-address` 会成功，之后 `server list --json` 也会把这条坏数据当成正常配置返回。

建议：

- 复用 `server add` 的地址校验逻辑，保证手工新增和批量导入遵循同一规则。
- 对非法导入项给出明确错误，并决定是“全量失败”还是“跳过坏项继续导入”。

#### ✅ P2：`server list <keyword>` 过滤结果为空时提示语不准确（2026-04-13 已修复：区分“无配置”和“无匹配”）

- 相关代码：`src/main/java/com/sofarpc/cli/command/ServerCommand.java:147`
- 当前如果已有服务配置，但 keyword 过滤后结果为空，仍然打印 `No servers configured`。
- 这会误导用户去排查配置缺失，而不是过滤条件不匹配。

建议：

- 区分“配置为空”和“过滤后无匹配”两种场景。
- 过滤后无匹配时给出更准确提示，例如 `No servers matched keyword: xxx`。

## 第三轮复查结果（2026-04-13）

### 已确认修复

- `server add/remove/import` 失败时已返回非零退出码。
- `batch` 已能在连接失败场景下返回 `ExitCodes.CONNECT_FAIL`。
- `batch --parallel 0` 已返回参数错误，不再直接抛出 `ThreadPoolExecutor` 栈。
- `server import` 已复用地址校验逻辑，无效地址会被跳过并提示。
- `server list <keyword>` 在无匹配时已输出更准确的提示语。

### 第三轮提出的问题（截至 2026-04-13 已部分修复）

#### ✅ P0：非法 `config.yaml` 类型会导致 CLI 直接崩溃（2026-04-13 已修复：`GlobalConfig` 已增加类型/范围校验并回退默认值）

- 相关代码：`src/main/java/com/sofarpc/cli/core/GlobalConfig.java:45`
- 当前 `GlobalConfig` 读取 `timeout` / `parallel` 时直接把配置值强转为 `Number`，并且只捕获了 `IOException`。
- 如果 `config.yaml` 中写入了错误类型，例如：

```yaml
defaults:
  timeout: bad
```

- 命令会直接抛出 `ClassCastException`，打印 Java 栈并以 `1` 退出，而不是回退默认值或返回受控的参数错误。

建议：

- 对配置值做显式类型校验和范围校验。
- 不要只捕获 `IOException`，至少应兜住配置格式错误导致的运行时异常。
- 非法配置建议统一处理为“打印明确错误 + 返回 `ExitCodes.BAD_ARGS`”或“告警后回退默认值”。

#### ✅ P1：`ping` 的 `timeout` 校验仍不完整（2026-04-13 已修复：已补齐 `timeout > 0` 校验）

- 相关代码：`src/main/java/com/sofarpc/cli/command/PingCommand.java:41`
- `invoke` / `call` / `batch` 已增加 `timeout > 0` 校验，但 `ping` 仍未补齐。
- 实测 `ping --timeout 0` 或 `config.yaml` 中 `timeout: 0` 时，会触发底层 SofaRPC 异常，先输出后台线程栈，再返回 `CONNECT_FAIL`。
- 这既不符合参数错误语义，也破坏了“输出干净、便于脚本解析”的约定。

建议：

- 为 `ping` 增加与其他命令一致的 `timeout > 0` 校验。
- 配置文件中的非法 `timeout` 也应在进入 RPC 调用前被拦截。

#### P1：工程化基线仍然缺失

- 当前仍没有 `src/test`、测试依赖或 CI 配置。
- 这次几轮修复涉及退出码、配置解析、批量汇总、导入校验等高回归风险路径，但还没有自动化测试覆盖。
- `mvn test` 通过的原因仍然只是“没有测试”。

建议：

- 优先补以下回归测试：
  - `server` 子命令退出码测试
  - `batch` 连接失败退出码测试
  - `GlobalConfig` 非法类型/非法数值测试
  - `ping` / `invoke` / `batch` 的 `timeout` 参数校验测试
- 再补最基础的 CI，保证构建和测试至少能自动跑起来。

## 第四轮复查结果（2026-04-13）

### 新发现的遗留问题

#### P0：`invoke` / `call` 对 `servers.yaml` 字段类型错误仍不够健壮，会直接崩溃

- 相关代码：
  - `src/main/java/com/sofarpc/cli/core/ServerStore.java:53`
  - `src/main/java/com/sofarpc/cli/service/RpcInvokeService.java:43`
- 当前 `ServerStore.loadAll()` 默认 `address` 一定是 `String`，如果 YAML 语法合法但字段类型错误，例如：

```yaml
servers:
  bad:
    address: 123
```

- `invoke` / `call` 通过 `RpcInvokeService.invoke()` 调用时，只捕获了 `IOException`，不会兜住这里的 `ClassCastException`。
- 实测这会直接打印 Java 栈并以 `1` 退出，而不是像 `ping` / `batch` 一样受控返回配置错误。

建议：

- 在 `ServerStore` 中对 `address` / `desc` 做显式类型校验。
- 或者在 `RpcInvokeService.invoke()` 中把配置读取阶段的运行时异常统一映射为 `ExitCodes.BAD_ARGS`。
- 最好统一所有命令在“配置文件字段类型错误”场景下的行为。

#### P0：`batch` 在遍历用例目录时遇到不可访问子目录会直接抛栈退出

- 相关代码：`src/main/java/com/sofarpc/cli/command/BatchCommand.java:176`
- `findJsonFiles()` 使用 `Files.walk()`，但这里只捕获了 `IOException`。
- 实际在遍历过程中，如果某个子目录无权限访问，流消费阶段抛出的通常是 `UncheckedIOException` / `AccessDeniedException`，当前不会被拦住。
- 实测当用例目录下存在不可访问子目录时，命令会直接打印 Java 栈并以 `1` 退出。

建议：

- 对 `Files.walk()` 的遍历错误做显式处理，不要只依赖外层的 `IOException`。
- 可以考虑改为 `Files.walkFileTree()`，对访问失败给出受控错误信息。
- 至少要把这类目录读取失败统一转成 `ExitCodes.BAD_ARGS` 或明确的 I/O 错误。

#### P1：`install.sh` 在 fat jar 缺失时仍会报告安装成功

- 相关代码：`install.sh:6`
- 当前脚本在 `cp target/sofarpc.jar` 失败后不会中止，仍然继续生成 wrapper 并打印“安装完成”。
- 实测在没有 `target/sofarpc.jar` 的目录里执行脚本，退出码仍为 `0`，但最终安装目录里只有 wrapper，没有 jar。

建议：

- 在脚本开头增加 `set -euo pipefail`。
- 安装前显式检查 `target/sofarpc.jar` 是否存在，不存在时直接失败退出。
- 成功提示应以 jar 拷贝和 wrapper 生成都完成为前提。

#### P1：Markdown 报告没有转义表格特殊字符，真实错误信息会破坏表格结构

- 相关代码：`src/main/java/com/sofarpc/cli/output/ReportGenerator.java:63`
- 当前 Markdown 报告直接把 `caseName` 和 `error` 写进表格单元格，没有处理 `|`、换行等特殊字符。
- 实测错误信息包含管道符或换行时，生成出来的 Markdown 表格会直接错位，报告不可读。

建议：

- 对 Markdown 表格内容做最基本的字符转义和换行规整。
- 或者对复杂错误信息改用代码块/列表，而不是直接嵌入表格单元格。
- HTML 和 Markdown 两种报告格式应分别按各自语法处理转义。

## P1：应该尽快补强的问题

### ✅ 配置存储存在静默降级后覆盖原文件的风险（Phase 2 已修复：读取失败抛异常 + 原子写入）

- 代码位置：
  - `src/main/java/com/sofarpc/cli/core/ServerStore.java:35`
  - `src/main/java/com/sofarpc/cli/core/ServerStore.java:66`
- `servers.yaml` 读取失败时，当前实现只打印错误并返回空 map。
- 后续如果执行 `add`、`import`、`remove` 等操作，很可能基于空数据重新写回，从而覆盖已有配置。
- 此外，导入时对内容结构和地址格式也缺少校验。

建议：

- 读配置失败时不要静默退回空数据，应直接中断写操作。
- 采用原子写入，避免异常情况下写坏配置。
- 增加 schema 校验和地址格式校验。
- 必要时保留备份文件或回滚策略。

### ✅ 命令层大量 `System.exit`，可测试性偏弱（Phase 1 已修复：Runnable→Callable\<Integer\>，消除所有 System.exit）

- 相关代码：
  - `src/main/java/com/sofarpc/cli/command/InvokeCommand.java:68`
  - `src/main/java/com/sofarpc/cli/command/BatchCommand.java:68`
  - `src/main/java/com/sofarpc/cli/command/ReportCommand.java:37`
- 命令类内部直接 `System.exit`，会导致单元测试困难，也不利于后续把核心能力复用到别的入口。
- 当前命令层既负责参数解析，也负责业务执行，还负责退出码处理，耦合偏重。

建议：

- 将核心逻辑提取到 service 层，命令类只负责参数映射和结果输出。
- 返回结构化结果对象，再由统一出口映射为 exit code。
- 这样测试可以绕开进程级退出，粒度会清晰很多。

### ✅ `invoke` / `call` / `batch` 逻辑重复较多（Phase 1 已修复：抽出 RpcInvokeService、ArgParser、AssertionEvaluator、OutputFormatter）

- 相关代码：
  - `src/main/java/com/sofarpc/cli/command/InvokeCommand.java`
  - `src/main/java/com/sofarpc/cli/command/CallCommand.java`
  - `src/main/java/com/sofarpc/cli/command/BatchCommand.java`
- 参数解析、断言判断、错误包装、输出风格存在明显重复。
- 这种重复会直接导致行为不一致和修复遗漏。

建议：

- 提取共用的调用服务层，例如：
  - 参数解析器
  - 断言执行器
  - RPC 调用结果封装器
  - 错误分类器
- `call` 最好只作为 `invoke` 的薄封装，不要维护一份近乎重复的调用逻辑。

### ✅ 结果模型大量使用 `Map<String, Object>`，缺少类型约束（Phase 5 已修复：引入 BatchResult、CaseResult 类型化模型）

- 相关代码：
  - `src/main/java/com/sofarpc/cli/command/BatchCommand.java:164`
  - `src/main/java/com/sofarpc/cli/command/ReportCommand.java:42`
  - `src/main/java/com/sofarpc/cli/output/ReportGenerator.java:26`
- 现在很多输入输出都依赖 `Map<String, Object>` 和强制类型转换。
- 这会把很多错误延后到运行时，也让文档、报告、测试之间更难保持一致。

建议：

- 引入明确的数据模型，例如：
  - `CaseDefinition`
  - `InvokeResult`
  - `BatchResult`
  - `ServerConfig`
- 先把边界模型类型化，代码可读性和重构安全性会明显提升。

## P1：工程化基线不足

### 没有测试目录、测试依赖和 CI 基线

- 构建文件：`pom.xml:19`
- 当前 `pom.xml` 只有运行时依赖和 shade 打包插件。
- 仓库里没有 `src/test`，也没有持续集成配置。
- 我执行 `mvn -q test` 时没有失败，但原因只是当前没有测试。

建议：

- 至少补这几类测试：
  - Picocli 命令参数测试
  - 断言解析测试
  - `ServerStore` 读写测试
  - 报告生成测试
  - `batch` 用例发现测试
- 在 Maven 中补充 surefire 和基础测试依赖。
- 接一个最轻量的 CI，保证每次提交至少能编译和跑测试。

### ✅ 构建产物较大，但缺少发布和兼容性说明（Phase 5 已修复：README 补充环境要求、升级方式）

- 构建文件：`pom.xml:63`
- 当前打出的 `target/sofarpc.jar` 体积较大，属于典型 fat jar。
- 作为团队工具是合理的，但仓库中还缺少更明确的发布策略、版本约束和兼容性说明。

建议：

- README 中补充 Java 版本、目标运行环境、升级方式。
- 考虑在发布时提供 checksum、版本变更记录和安装说明。
- 如果后续工具规模继续扩大，再评估是否拆分依赖或优化打包。

## P2：中期可以优化的方向

### ✅ 配置与输出可以更机器友好（Phase 5 已修复：所有命令支持 --json 输出）

- 当前只有部分命令支持 `--json`。
- 如果目标用户包含脚本、AI Agent、CI 任务，建议所有命令都提供稳定 JSON 输出。
- 这样才能真正做到 stdout 可解析、stderr 可诊断、exit code 可自动处理。

建议：

- 为 `server list`、`server add/remove/import/export`、`report` 等命令补统一 JSON 输出模式。
- 明确 stdout/stderr 约定，避免人类输出和机器输出混杂。

### ✅ 报告生成链路的输入和输出校验还不够严格（Phase 2 已修复：--format 枚举校验 + HTML 全字段转义）

- 相关代码：
  - `src/main/java/com/sofarpc/cli/command/ReportCommand.java:45`
  - `src/main/java/com/sofarpc/cli/output/ReportGenerator.java:146`
- `report --format` 对非法值没有显式报错，非 `html` 一律按 markdown 处理。
- HTML 报告对 `error` 做了转义，但 `case` 名等字段仍然直接写入。

建议：

- 对 `format` 做枚举校验。
- 报告中的所有外部输入字段统一转义。
- 为报告生成增加边界测试，覆盖空结果、特殊字符和非法输入。

### ✅ 连接缓存策略还可以进一步明确（Phase 5 已修复：缓存 key 改为 address::interfaceId::timeout，支持按地址定向销毁，batch 执行后自动释放）

- 代码位置：`src/main/java/com/sofarpc/cli/core/RpcClientFactory.java:37`
- 当前缓存 key 是 `serverAlias::interfaceId`。
- 这在大多数场景够用，但如果后续引入更多维度，例如 group、version、序列化协议、动态超时策略，缓存键会变得不完整。
- 另外，当前配置和连接生命周期都绑在静态缓存里，后续扩展时容易变复杂。

建议：

- 提前定义调用上下文模型，而不是只用 alias 和 interfaceId 组合。
- 为缓存命中条件和失效策略写清楚边界。
- 在 batch 并发场景下补一轮连接复用与资源释放验证。

## 推荐实施顺序

1. ✅ ~~先修 P0：递归发现用例、统一 help 行为、统一 exit code、修正文档漂移。~~
2. **当前优先**：修复 `invoke/call` 的配置类型错误崩溃，以及 `batch` 的目录遍历异常兜底。
3. **然后**：补强安装和报告链路，避免 `install.sh` 假成功、Markdown 报告结构损坏。
4. **最后**：补工程化——测试基线（JUnit 5 + Mockito）、CI。
5. ✅ ~~体验增强：类型化结果模型、统一 JSON 输出、发布说明、缓存策略优化。~~

## 结论

本轮重构已经把最核心的一批结构性问题修掉了，项目质量明显提升；第二轮和第三轮复查暴露的大多数命令行为问题也已经收口。当前剩余的主要风险集中在少数配置/文件系统边界条件下的崩溃路径、安装脚本的失败处理，以及 Markdown 报告在真实错误内容下的稳健性；测试基线仍然缺失。
