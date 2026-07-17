# 设计方案:java.time / BigInteger 编码下沉到 direct 层

> 输入:`docs/deep-analysis-2026-07.md` 第一档剩余项 —— special 类型的 Hessian
> wire 形态知识错位在 app 层。
> 基线:`fix/complete-deferred-resilience-work`。
> 本文只做设计,不改代码。落地按第 7 节的提交边界单独立项。

## 1. 背景与现状

"某个 Java 类型在 Hessian 线上序列化成什么形态" 属于**编解码知识**,应归 direct
(codec)层。但目前这类知识被劈成两半:

**direct 层(正确归属)** —— `internal/direct/hessian_writer.go` 的 `writeJavaScalar`:
- `java.util.Date` → `writeDate`(tag `'d'` + epoch 毫秒)
- `java.math.BigDecimal` → `writeTypedObject{name, fields:{value}}`
- `byte[]` → `writeBytes`(binary)

**app 层(错位)** —— `internal/app/rpc_specialtypes.go`:
- `java.time.LocalDate/LocalDateTime/LocalTime/Instant` → 手工构造
  `com.caucho.hessian.io.jdk8.*Handle` 代理对象(`localDateHandle` 等)
- `java.math.BigInteger` → 手工构造 signum/mag 对象布局(`bigIntegerHandle` + `magFromBigInt`)

`com.caucho.hessian.io.jdk8.LocalDateHandle` 这种**具体到 Hessian 库实现的类名**、以及
BigInteger 的 signum/mag 字段布局,是纯 codec 细节。app 层本应类型中立(只知道"这是个
LocalDate"),却知道了它的 alipay Hessian wire 形态。

### 现状数据流

```
typedValueForJavaType(app)                          writeTypedValue / writeJavaScalar(direct)
  ├─ java.time.*  → localDateHandle() 拼 Object ──►  当普通 typed object 写字节(不知它是 special)
  ├─ BigInteger   → bigIntegerHandle() 拼 Object ──► 当普通 typed object 写字节
  ├─ (Date/BigDecimal 走 Scalar) ───────────────►  writeJavaScalar 自己识别并写(正确归属)
  └─ validateSpecialArgs 扫"残留未强转 scalar"兜底
```

### 现状的三个症状

1. **direct 知道却不做**:`writeJavaScalar` 的 BigInteger 分支直接
   `return fmt.Errorf("BigInteger must be encoded as its signum/mag object form, not a scalar")`
   —— 它知道正确形态,却把活儿甩回 app(`hessian_writer.go:427`)。
2. **加一种 java.time 类型要改两处**:app 的编码 `javaTimeTypedValue` + app 的校验
   `isSpecialEncodedType`/`validateSpecialArgs`。`LocalTime` 上一轮补的就是这两处。
3. **逼 app 发明补偿机制**:因为 app 把 LocalDate 拆成了通用 `Object`,direct 无法识别它
   原本是 special 类型,所以 app 得写 `firstMalformedSpecial` 去"扫有没有漏强转的残留
   scalar"来做 plan 期校验兜底。

## 2. 目标 / 非目标

**目标**
- 把 java.time / BigInteger 的 **Hessian wire 形态构造**(Handle 类名、signum/mag 布局)
  从 app 挪到 direct,与 Date/BigDecimal 同层同款。
- app 只保留**输入解析 + plan 期校验**(给出清晰的 `ARGUMENT_TYPE_MISMATCH`),不再碰
  caucho Handle / mag。
- wire 字节**逐字节不变**(现有 golden 全过)。

**非目标**
- 不新增 special 类型(如 `ZonedDateTime`、`java.sql.*` 子类型)—— 那是净增功能,另议。
- 不动 Date/BigDecimal 现有实现(已在 direct,正确)。
- 不改 `javavalue.TypedValue` 的对外契约。

## 3. 设计:定义清晰的层间 seam

**seam:app 产出「类型中立的规范化 Scalar」;direct 拥有「Hessian wire 形态」。**

### 目标数据流

```
typedValueForJavaType(app)                       writeJavaScalar(direct,新增分支)
  ├─ java.time.LocalDate → Scalar("java.time.LocalDate","2024-01-15") ──► 解析→构造 LocalDateHandle→写字节
  ├─ BigInteger          → Scalar("java.math.BigInteger","123...")     ──► 解析→构造 signum/mag→写字节
  └─ plan 期:normalizeSpecialScalar 解析失败 → ARGUMENT_TYPE_MISMATCH
```

### 3.1 app 侧改动(`internal/app/`)

- `typedValueForJavaType`(rpc_typedvalue.go):对 java.time/BigInteger 类型,产出
  `javavalue.Scalar(javaType, canonical)`,其中 canonical 是规范化字符串:
  | 类型 | canonical 形态 |
  |---|---|
  | LocalDate | `2024-01-15`(ISO date) |
  | LocalDateTime | `2024-01-15T10:30:00` |
  | LocalTime | `10:30:00` |
  | Instant | `2024-01-15T10:30:00Z`(RFC3339 UTC) |
  | BigInteger | 十进制字符串 `9223372036854775807` |
  解析失败时保留原始值(交给 plan 校验拦截)。
- 新增 `normalizeSpecialScalar(javaType, value) (canonical string, ok bool)`:把现有
  `javaTimeTypedValue`/`bigIntegerTypedValue` 里的**解析部分**保留下来,但只返回规范化
  字符串,**不再构造 Object**。app 仅保留 ISO/十进制解析这类**输入校验知识**(通用格式,
  非 Hessian 专有)。
- **删除**(移入 direct):`localDateHandle`/`localTimeHandle`/`localDateTimeHandle`/
  `instantHandle`/`bigIntegerHandle`/`magFromBigInt`/`javaIntScalar`/`javaLongScalar`。
- `firstMalformedSpecial`(rpc_specialtypes.go):special-typed scalar 的"是否 malformed"
  改为**调用 `normalizeSpecialScalar` 重解析**判定,而非"是否还是 scalar"。语义等价,
  但不再依赖"valid 会被拆成 Object"这个隐式约定。`validateSpecialArgs` 整体保留(plan 期
  仍要拦),但内部判定简化。

### 3.2 direct 侧改动(`internal/direct/hessian_writer.go`)

- `writeJavaScalar` 新增分支(与 Date/BigDecimal 并列):
  - `case "java.time.LocalDate", "java.time.LocalDateTime", "java.time.LocalTime", "java.time.Instant"`:
    从 scalar 取 canonical 字符串 → 解析 → 构造对应 `*Handle` `typedObject`(逻辑从 app 移来)
    → `writeTypedObject` / 复用嵌套写入。
  - `case "java.math.BigInteger"`:把现在的报错替换为真实实现 —— 解析十进制 → `big.Int` →
    signum/mag → 写 object(`magFromBigInt` 移来)。
- 新增 direct 内部 helper(app 移来,unexported):`localDateHandle` 等 + `magFromBigInt`,
  产出 `typedObject`(direct 已有的内部结构),不再依赖 `javavalue.Object`。
- `writeTypedValue` 的 scalar 路径已经过 `writeJavaScalar`(`hessian_writer.go` 中
  `writeJavaScalar(value.JavaType, value.Scalar)`),所以 `Scalar("java.time.LocalDate",...)`
  自动命中新分支,无需改分派。

### 3.3 依赖方向

改后 `internal/app` 不再 import `math/big`、不再持有 caucho 常量;`internal/direct` 增加
`math/big` 依赖(编码 BigInteger 本就该在这)。符合 `internal/arch/boundary_test.go` 的
既有方向(app→direct 允许,direct 不依赖 app)。

## 4. 兼容性与不变量

- **wire 字节不变**:direct 构造的 Handle/signum-mag 对象树必须与 app 现在产出的**逐字段
  逐顺序一致**,从而写出的字节与冻结 golden 完全相同。这是硬约束。
- **TypedValue 契约不变**:对外仍是 tagged union;只是 special 类型在 app→direct 之间的
  中间表示从 `KindObject` 变为 `KindScalar`(内部细节,不出包)。
- **plan 期校验契约不变**:非法 LocalDate/BigInteger 仍在 invoke_plan 阶段以
  `ARGUMENT_TYPE_MISMATCH` 拒绝,不发请求(D5 精神)。
- **rawResult 契约**:invoke 的 rawResult 是**响应**解码树,与请求编码路径无关,不受影响。

## 5. 验证矩阵(关键:CI 就能验字节)

**重要**:java.time/BigInteger 的字节 golden 已存在于**无 build tag** 的
`internal/direct/hessian_golden_test.go`(`hessianBigIntegerGoldenHex`、LocalDateHandle/
InstantHandle golden),从真实 Java oracle 抓下冻结,**普通 CI 就跑**。所以本重构的字节正确性
**在 CI 可验**,不必依赖内网 JVM。

分层验证:
1. **encode 侧 golden(CI,新增)**:对 `Scalar("java.time.LocalDate","2024-01-15")` 等
   断言 `encode == 冻结 golden hex`。现有 golden 多为 decode 侧(读 golden→查形态);本重构
   改的是 writer,需补 encode 侧断言(value→bytes==golden),复用已冻结的 hex,无需 JVM。
2. **`go test -race ./...`**:app/direct/mcp 全绿;app 的 plan 校验测试(malformed date、
   非整数 BigInteger)保持通过。
3. **BOLT oracle(CI)**:framing 不受影响,回归确认。
4. **Hessian JVM oracle(发布前,`scripts/oracle-gate.sh`)**:`hessian_java_contract_test.go`
   已有 LocalDate/Instant/BigInteger 用例(现以 Object 形态驱动);改后新增/改为 scalar 形态
   驱动,跨语言二次确认。**此步需内网 JVM + alipay jar,只在发布前;不是 CI 门禁,不阻塞落地。**

> 风险修正:先前判断"必须内网 JVM 才能验、风险不可控"**过于悲观**。字节 golden 已进 CI,
> 重构可在 CI 达到字节级验证;JVM oracle 是确认性的、发布前跑。真正需要 JVM 的场景是**新增**
> 一种 special 类型(要抓新的 golden),本重构不涉及。

## 6. 风险与回滚

- **主要风险**:direct 构造的对象树与 app 旧版有一字段/顺序差异 → 字节漂移。由第 5.1 的
  encode golden 当场拦截。
- **次要**:app 的 normalize 与 direct 的 encode 各解析一次 canonical(app 校验、direct 编码)
  —— 轻微重复,可接受;canonical 用字符串避免结构知识扩散。
- **回滚**:改动集中在 `rpc_specialtypes.go`/`rpc_typedvalue.go`/`hessian_writer.go` 三文件,
  单提交可 revert;golden 未改则回滚零残留。

## 7. 实施步骤与提交边界

建议小步、每步 CI 绿:
1. `test(direct): pin java.time/BigInteger encode golden`(先补 encode 侧 golden 断言,
   锁住当前字节 —— 作为重构的安全网,**先行独立提交**)。
2. `refactor(direct): own java.time/BigInteger hessian encoding`(direct 新增分支 + 内部
   helper;此时 app 仍产 Object,direct 两条路径都能写 —— 过渡)。
3. `refactor(app): emit neutral scalar for special types`(app 改产 Scalar,删 handle/mag
   构造;`firstMalformedSpecial` 改 parse 判定)。
4. `test(app): plan validation for special scalars`(malformed 用例对齐新表示)。
5. 发布前:`scripts/oracle-gate.sh` 跑 Hessian JVM oracle 二次确认,记录结果。

## 8. 明确不做(本方案范围外)

- 新增 special 类型(ZonedDateTime、OffsetDateTime、java.sql.Date/Time/Timestamp 等)。
- Date 的 SQL 子类型 wrapper 编码(`hessian_writer.go:444` 的既有 follow-up)。
- 响应侧(reader)对 Handle 的解码优化(现由 presentation 层扁平化,另议)。
