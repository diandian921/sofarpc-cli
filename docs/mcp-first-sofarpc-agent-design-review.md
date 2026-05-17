# Review: MCP-First SofaRPC Agent Design

被评审文档: `docs/mcp-first-sofarpc-agent-design.md`
评审日期: 2026-05-17(第三轮复查 —— 终轮)
评审人: wuwh
评审结论: **通过 —— 全部问题闭环,可作为实施基线**

---

## 1. 终轮总评

三轮评审提出的全部问题(B1–B4、H1–H4、M1–M4、威胁模型,以及第二轮的 §3.1–§3.4)均已正面、技术准确地解决。文档已从"看着像 greenfield"演进为"模型准确、风险内化、有工程纪律的现有代码演进设计",具备实施基线条件。

第二轮剩余 4 项收口情况:

- §3.1 GenericService 框架:✅ 已改写为"泛化调用不实例化本地 DTO,风险是无法对未知类型字段做校验/转换";补充"方法参数类型 FQN 未解析才阻断(GenericService 仍需 argTypes)"——技术准确。
- §3.2 CJK 分词:✅ 已改为保留整串 + 字符 bigram(`查询用户` → `查询用户/查询/询用/用户`),标准做法。
- §3.3 配置写硬开关:✅ 新增独立 *Hard disable* 段,`--disable-config-write`,明确不依赖 MCP host 确认行为,服务端可强制。
- §3.4 ping_service 语义:✅ 已写明"仅证明本地传输路径可建 consumer,不证明远端接口/方法存在"。

---

## 2. 第一轮问题闭环情况

| 编号 | 问题 | 状态 | 说明 |
|------|------|------|------|
| B1 | 缺与现有实现关系/迁移 | ✅ 已解决 | 新增 *Relationship To Existing Implementation*,reuse/rename/replace/fixture 迁移齐全,含双路径字节等价契约测试 |
| B2 | Consumer 生命周期与上限缺失(OOM 热路径) | ✅ 已解决 | 新增 *Consumer/proxy lifecycle*:有界 LRU、key 不含 timeout、per-call context 超时、evict 调 unRefer、`maxConsumers=256`、显式声明替换旧 `ConnectionManager` |
| B3 | Agent 经 MCP 改 config 信任边界 | ⚠️ 部分解决 | 新增 *Sensitive MCP write tools* + 写前确认。但硬开关推迟到 future,见 §3.3 |
| B4 | 启动失败诊断不回传 | ✅ 已解决 | 新增 *Startup failure diagnostics*,结构化 reason + stderr/日志尾,明确不让 agent 自己翻日志 |
| H1 | 两个 Go 二进制逻辑漂移 | ✅ 已缓解 | 保留双二进制(有意决策),用字节等价契约测试兜底漂移与复现保真度 |
| H2 | 跨 jar 父类阻断构造 | ⚠️ 部分解决 | 新增 *Known schema source limit* 并澄清"只设叶子字段不阻断"。但 GenericService 下"construct DTO"框架本身不准确,见 §3.1 |
| H3 | 缓存盲区:新增源文件不失效 | ✅ 已解决 | 新增 source-set fingerprint(路径+size+mtime,不在热路径 hash 内容),指纹变则重查 |
| H4 | JSON-RPC 传输理由不足 | ✅ 已解决 | 新增 *Rationale* + *Migration note*,并给出"成本大于收益则回退 envelope 并先记录决策"的明确出口 |
| M1 | search 分词/CJK 未规定 | ⚠️ 部分解决 | 新增 *Tokenization*,camelCase/CJK 已覆盖。但 CJK 整段=单 token 对中文 Javadoc 匹配偏弱,见 §3.2 |
| M2 | 跨 session 重启杀 in-flight | ✅ 已解决 | Versioning 增加跨 session 协调 + sofarpc-mcp 透明重连 |
| M3 | 序列化假设 | ✅ 已解决 | 新增 *Known RPC compatibility limits*,protobuf/自定义序列化列为已知限制 |
| M4 | idleTTL/alias 与现有不一致 | ✅ 已解决 | 迁移章节显式声明"复用 IdleTracker,默认值按本设计调整"——已是有意决策 |
| 小 | Token 威胁模型夸大 | ✅ 已解决 | 新增 *Threat model*,表述已收紧准确 |
| 小 | Engine 中途自退重连 | ✅ 已解决 | 同 M2 |

---

## 3. 仍需处理(建议 v1 内解决)

### 3.1 (技术准确性,高)GenericService 下没有"构造本地 DTO"这回事

文档现在的 *Unresolved types* / *Known schema source limit* 用 "the Engine can construct the concrete local DTO" / "block construction" 来描述未解析类型逻辑。

问题:SofaRPC `GenericService` 泛化调用**不实例化真实 DTO 类**,而是用泛化表示(`GenericObject` / map + Hessian)按字段名组装 payload,服务端反序列化成自己的类。Engine 根本不 new 用户的 DTO class。

影响:

- "外部 jar 父类阻断构造"这个担忧在泛化路径下基本不成立——根本不需要父类可加载就能 putField。
- schema 的真实作用是**字段类型校验与 JSON→泛化值转换**,不是"能否实例化类"。
- 阻断逻辑应按"能否对调用方提供的字段做类型校验/转换"来表述,而非"能否构造类"。

建议:把 *Unresolved types* 和 *Java Source Parsing Boundary* 的措辞从"construct/instantiate"改为"validate/convert provided fields under generic representation"。这会让 H2 的限制更轻,也避免实现时按错误模型设计。

### 3.2 (技术准确性,中)CJK 整段单 token 对中文 Javadoc 近乎无效

*Tokenization* 写 "Treat contiguous CJK characters ... as searchable tokens"。中文无词边界,整段 CJK 当一个 token,只有 query 与 Javadoc 的 CJK 串完全相同(或子串)才命中。实际企业服务 Javadoc 多为中文,英文只在标识符——这等于把"中文语义召回"几乎关掉,只能靠"agent 自己补英文词"兜。

建议:CJK 改用 **bigram 分词**(相邻两字成 token),这是 CJK BM25 的标准且廉价做法,显著改善中文 Javadoc 召回。仍可保留"agent 补英文词"作为增强而非唯一依赖。

### 3.3 (安全控制,中)config 写工具的硬关闭开关不应推迟到 future

改版要求"MCP host/用户写前确认""future hardened mode 可禁用"。但 MCP 协议层没有服务端可强制的确认原语,确认与否完全取决于 host 实现,不是所有 host 都会逐工具确认。把唯一的硬控制(禁用配置写工具)推到 future,等于这道信任边界在 v1 没有服务端可强制的兜底。

建议:v1 即提供服务端硬开关(config 标志或环境变量,如 `engine.allowConfigWriteTools=false` / `SOFARPC_DISABLE_CONFIG_WRITE=1`),默认可开,但存在即用,不依赖 host 行为。文字工作量极小,属于"湖"而非"海"。

### 3.4 (小,可随迭代)ping_service 结果语义

bolt 直连建 consumer 只证明 host:port TCP 可达,远端并不校验接口 FQN 是否注册在该地址。当前 "Creates/checks consumer/proxy only" 仍可能被 agent/人误读为"接口存在"。建议结果文案显式写明"仅连接可达;接口/方法是否存在未验证"。

---

## 4. 文档优点(继续保留)

- in/out scope 纪律严明;Go vs Java Engine 职责切分清晰。
- 惰性启动 + `engine.lock` + `engine.hello` 作为发现权威,`engine.json` 仅诊断。
- 缓存 size/mtime 快路径 + sha256 慢路径 + source-set fingerprint,设计完整。
- 稳定错误码 + "JSON-RPC 协议错误 vs 工具结果 ok=false"边界清楚。
- config 作为稳定用户契约,不自动迁移。
- 对解析能力退化边界(Lombok/MapStruct/复杂泛型/外部 jar)诚实声明。
- 迁移章节提供了"双路径字节等价"契约测试与"JSON-RPC 回退并记录决策"两个明确决策点,工程纪律到位。

---

## 5. 处置建议

文档评审通过,可冻结为实施基线。无遗留阻塞项。

实现阶段建议:
- search 第一版即落地 CJK bigram,并对中文 Javadoc 召回做一组真实用例回归。
- Consumer 缓存(B2)与 source-set fingerprint(H3)是两处易回归的设计点,实现时配套单测,纳入项目既有契约测试纪律。
- 开工前可走一次 `/plan-eng-review` 做实现拆解与边界用例的最终锁定(评审范围已收敛,此步是流程而非补救)。
