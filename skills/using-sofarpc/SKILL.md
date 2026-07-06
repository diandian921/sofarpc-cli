---
name: using-sofarpc
description: Use when invoking any SofaRPC service from this machine — calling any mcp__sofarpc__* tool (sofarpc_invoke / sofarpc_invoke_plan / sofarpc_describe / sofarpc_probe / sofarpc_resolve / sofarpc_doctor), selecting project/profile/server endpoint config, looking up a Java Facade method signature, composing an invoke payload, decoding a SOFA bolt error such as INVOKE_FAILED / CONNECT_FAILED / no provider, or troubleshooting a probe/ping result.
---

# Using SofaRPC

## 三条不能忘的真相

1. **invoke 之前必先 describe**(只要 workspace 配过)—— 别靠猜方法名,也别现场 grep Java 源码
2. **probe / ping 通 ≠ service 在那台机器上发布** —— 它只验 bolt 握手
3. **`ok:true` ≠ 业务成功** —— envelope 层和业务层是两层,要分开判

## Endpoint 选择

配置 v2 使用 Spring profile 风格:每个 project 下有自己的 `profiles` 和
`activeProfile`。优先用 `project + profile` 指定环境;只给 `project` 时会走
该项目的 `activeProfile`。旧的 `server` 名仍可用,但它只是兼容入口,通常由
`<project>-<profile>` 派生,例如 `salesfundmp-test`。

同一次排查里,`resolve`、`probe`、`describe`、`invoke_plan`、`invoke` 要使用同一个
endpoint 选择方式。不要先 probe `salesfundmp-test`,再 invoke 只传 `project` 而误走
另一个 `activeProfile`。

保存 server/profile 时注意:**首个 profile 会自动成为该项目的 activeProfile**,
之后保存的 profile 不会改变它(save_server 的输出带 `activeProfile` / `warning`
字段提示)。切换默认环境用 `sofarpc project use <project> <profile>`,或
save_server 时传 `setActive=true`。

## 标准工作流(陌生 facade 必走这五步)

| 步 | 工具 | 目的 |
|---|---|---|
| 1 | `sofarpc_resolve` | 确认 project/profile/server 配好,拿到 endpoint |
| 2 | `sofarpc_describe`(`service=<FQN>`) | 拿 method 列表 + paramTypes + DTO fields |
| 3 | `sofarpc_probe`(可选) | 确认 IP 可达;但**不能**当作 service 一定能调 |
| 4 | `sofarpc_invoke_plan` | 渲染 plan,确认 paramTypes / args 没写错(不发请求) |
| 5 | `sofarpc_invoke` | 真打 |

**跳过 step 2 几乎一定撞墙**:method 名 camelCase 猜错、`List<Long>` 泛型擦除、DTO 漏必填字段。撞了再重做,白浪费一个 invoke 周期。

## probe 的真相

`sofarpc_probe` 只验:
- TCP 握手能不能完成
- bolt 协议握手是否成功
- 远端 IP 上有 SOFA 进程

它**不验** service 名是否在那台机器上发布。常见踩坑:同集群里 A 机器有 SOFA 进程但只发了 service X,你 probe A 通了就以为 service Y 也能调 —— 直到 invoke 拿到 `INVOKE_FAILED: no provider`。

**判定 service 真可达的唯一方法**:真的 invoke 一次(或 `sofarpc_invoke_plan` 看是否能 resolve 到 provider)。

## 业务错误的双层

```
{
  "ok": true,                         ← envelope 层(bolt 通了、调到 method 了)
  "data": {
    "result": {
      "success": false,               ← 业务层
      "errorMsg": "用户不存在"         ← 业务错误在这
    }
  }
}
```

判 success 的正确写法:

```bash
jq -e '.ok and (.data.result.success // (.data.result.errorMsg | not))' response.json
```

千万**不要**只看 `.ok` 就上报"调用成功"。

## 没有 daemon:一次性成本在 schema 索引

纯 Go 进程内直连 BOLT,没有常驻 daemon,invoke 本身几乎无启动开销。 唯一的一次性成本在 **schema 索引**:首次 `sofarpc_describe` 会扫描整个 `workspaceRoot` 的 Java 源码并缓存到 `~/.sofarpc/cache/schema/`,大 workspace 第一次慢一点,之后命中缓存。

- 源码改了 → 缓存按内容指纹自动失效重建,不用手动清
- "先 resolve 暖 daemon""第一次 invoke 加大 timeout" 是旧 Java sidecar 时代的事,已不适用

## describe 的局限(决定退化路径)

- describe 走**本地 Java 源码扫描**(`workspaceRoot/src/main/java/**/*.java`),不是 provider 反射
- **跨团队、没拿到对方源码** → describe 用不了 → 退化:`sofarpc_invoke_plan` 看错误码反向探签名,或者去问对方
- **注册中心反射这条路不可用** —— 办公网常连不到 nacos / zk,所以**不要**建议走"从注册中心拿 metadata"
- 外部 jar 里的父类字段本地源码看不到,可能漏字段;describe 给空 fields 时**先怀疑工具,再怀疑接口**

## 写 payload

两种形态,优先第一种:

- **named arguments**(describe 能解析签名时):`arguments` 按参数名给普通 JSON 值,系统用本地源码推断参数顺序和 Java 类型,不用手写类型
- **exact**(describe 缺失 / 重载歧义时):`paramTypes` + `orderedArguments` 显式给全

复杂 / 嵌套 payload 先用 `sofarpc_invoke_plan` 跑一遍,确认 plan 里 paramTypes、orderedArguments、Java 类型都对,再 `sofarpc_invoke` 真打。 别靠猜类型硬拼。

## 请求上下文 / attachments

`attachments` 是 SOFARPC request baggage,服务端用
`RpcInvokeContext.getContext().getRequestBaggage(key)` 读取。值必须是字符串。

- **本次请求上下文**(traceId、requestId、临时灰度、临时租户)放在
  `sofarpc_invoke_plan` / `sofarpc_invoke` 入参的 `attachments`。
- **稳定默认值**(固定租户、环境标识、调用来源)才放到 server/profile 配置里的
  `attachments`。
- 最终发送时会合并:配置默认 attachments + 调用级 attachments;同名 key 由调用级覆盖。

调用示例:

```json
{
  "server": "user-test",
  "service": "com.example.UserService",
  "method": "getUser",
  "paramTypes": ["java.lang.String"],
  "orderedArguments": ["u001"],
  "attachments": {
    "traceId": "trace-001",
    "tenant": "blue"
  }
}
```

## 反 pattern(看到立刻 stop)

- ❌ 不 describe 直接 invoke,靠猜方法名
- ❌ 看 `ok:true` 就上报 "调用成功"
- ❌ 看 `probe reachable:true` 就断言 service 一定能调
- ❌ 只凭 `server` 名猜环境,不看 `profile` / `activeProfile`
- ❌ 复杂 payload 不过 `sofarpc_invoke_plan` 就直接 invoke
- ❌ 把 traceId / requestId 这类一次性上下文写进 config attachments
- ❌ describe 报错就改去 grep 源码 —— 先检查 `workspaceRoot` 配置
- ❌ "service 找不到" → 想去查注册中心 —— 办公网连不上,放弃这条路

## 红灯自检

出现以下任意一种,**回到 step 2 重新 describe**:

- `INVOKE_FAILED: no provider`
- `METHOD_AMBIGUOUS`
- `ARGUMENT_TYPE_MISMATCH`
- `SERVICE_NOT_FOUND`
- 业务层 `success:false` 且 errorMsg 提示 "参数缺失 / 参数错误"
