# protocol

Go client 与 Java daemon 之间的唯一契约。

- `schema/` — JSON Schema（draft-07），定义请求/响应包裹与各 `op` 的 payload
- `fixtures/` — golden fixtures，Go 与 Java 两边契约测试共用

## 帧格式

- 4 字节 big-endian 长度前缀
- 后跟 UTF-8 编码 JSON 消息

## 包裹层

请求：`requestId` / `op` / `meta` / `payload`
响应：`requestId` / `ok` / `code` / `data` / `error` / `meta`

## op 列表（V1）

- `invoke` — 发起一次 SofaRPC GenericService 调用，可带断言
- `ping` — 探测目标地址能否联通
- `health` — daemon 自检
- `shutdown` — 主动停机

## 错误码

`SUCCESS` / `BAD_REQUEST` / `CONNECT_FAILED` / `RPC_TIMEOUT` / `INVOKE_FAILED` / `ASSERTION_FAILED` / `DAEMON_UNAVAILABLE` / `INTERNAL_ERROR`

## 目录约定

- 每个 `op` 一个子目录
- 文件名形如 `request.<case>.json` / `response.<case>.json`
- `case` 覆盖成功路径与至少一种失败路径
