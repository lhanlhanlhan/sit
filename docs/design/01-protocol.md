# SIT 消息协议设计(`internal/protocol`)

> 状态:已确认(2026-06-24)。两端唯一契约。
> 变更协议必须同步更新本文件与 DECISIONS.md(ADR-006)。

## 0. 通用约定

- 传输:WebSocket **text frame**,载荷为 JSON(UTF-8)。
- **所有时间字段统一为 unix 毫秒(int64)**。涵盖 `ts` / `deadline` / `duration_ms` 等。
  protocol 包提供 `NowMillis()` 等 helper,禁止传秒。
- 消息 ID:ULID(字符串),用于关联(ref)、去重、审计。
- 单帧大小上限:**1 MB**;解码前先校验长度,超限直接拒绝(防 OOM,尤其嵌入式 2~4GB)。

## 1. 统一信封 Envelope

```json
{
  "v": 1,                  // 协议版本
  "type": "instruction",   // instruction | notification | ack
  "id": "01HX...",         // 消息唯一 ID(ULID)
  "ts": 1782268613000,     // 发送时间(unix 毫秒)
  "payload": { ... }       // 按 type 决定的结构
}
```

`type`:
- `instruction` —— Manager → Node 下发。
- `notification` —— Node → Manager 上报。
- `ack` —— 双向确认收据。

## 2. Instruction(下发)

```json
{
  "type": "instruction",
  "id": "01HX...A",
  "payload": {
    "kind": "predefined",        // predefined | shell
    "name": "restart_service",   // predefined 时:白名单命令名
    "args": { "service": "nginx" },
    "command": "",               // shell 时:任意命令字符串
    "timeout_sec": 60,           // 执行超时,Node 强制 kill
    "deadline": 1782269000000    // 可选(毫秒):过期则 Node 丢弃不执行
  }
}
```

- `kind=predefined`:Node 内置白名单命令(如 `restart_service`/`collect_logs`/`update_config`),可控低危。
- `kind=shell`:任意 shell 字符串,兜底远程控制。**最高危面**,协议上显式区分,便于将来加节点级开关/RBAC。
- `deadline`:防离线积压指令重连后被误执行(配合去重实现幂等)。

## 3. Notification(上报)

四类,以 `kind` 区分。当前冻结这四类,保留扩展余地。

### (a) register — 首次连上,自报身份(设备自描述,**非身份依据**)

```json
{ "type":"notification", "payload":{
    "kind":"register",
    "node_id":"<随机生成>", "hostname":"...", "os":"darwin",
    "arch":"arm64", "version":"sit/0.1.0",
    "addrs": [
      { "ip":"10.0.0.5",        "family":"v4", "iface":"en0", "scope":"private" },
      { "ip":"2408:8000::1234", "family":"v6", "iface":"en0", "scope":"global"  },
      { "ip":"fe80::1",         "family":"v6", "iface":"en0", "scope":"link"    }
    ]
} }
```

- **多 IP / 双栈**:`addrs` 为数组,每项含 `family`(v4/v6)、`iface`(网卡名)、`scope`(global/private/link/loopback)。
- 安全:`node_id` 与 `addrs` 均**自报**,不作身份依据;身份以握手认证为准(见传输层)。

### (b) heartbeat — 周期保活 + 轻量指标

```json
{ "type":"notification", "payload":{
    "kind":"heartbeat", "uptime_sec":3600,
    "load":[0.2,0.3,0.1], "mem_used_mb":1500 } }
```

### (c) result — 某条 Instruction 的执行结果

```json
{ "type":"notification", "payload":{
    "kind":"result", "ref_id":"01HX...A",
    "exit_code":0, "stdout":"...", "stderr":"...",
    "duration_ms":1234, "truncated":true } }
```

- `ref_id` 指回 `instruction.id`,用于结果配对。
- `stdout/stderr` 超阈值(64KB)截断并置 `truncated=true`。
- 回传内容视为**不可信字节**,存储/展示必须转义(防二次注入)。

### (d) event — Node 主动事件

```json
{ "type":"notification", "payload":{
    "kind":"event", "event":"shutting_down", "detail":"..." } }
```

常见 event:`online` / `shutting_down` / `process_died` 等。

### 预留:增量流式输出(后续扩展,v1 不实现)

为支持 MCP 增量流式与交互式会话(见 09-mcp §7/§8),协议预留:

- Instruction 预留 `"stream": true` 标志位:置位时 Node 分块上报输出而非一次性 result。
- 预留 Notification kind **`result_chunk`**:`{ kind:"result_chunk", ref_id, seq, stdout_delta, stderr_delta, eof }`,Manager 据此转成 MCP 流式/progress。
- v1 不实现,仅在协议层占位,避免将来破坏兼容。

## 4. 可靠性:ACK + 去重 + 离线队列

- **下行确认**:Manager 发 instruction 后等 Node `ack`(payload 含 `ref_id`);超时未 ack,Node 重连后重发(复用同一 `id`,幂等)。
- **去重**:两端各维护近期已处理 `id` 滑动窗口,重复 id 丢弃。
- **离线队列**:Node 离线时,发往它的 instruction 在 Manager 侧持久化排队,重连后按序投递;过 `deadline` 的丢弃。
- 语义:**至少一次投递 + 幂等丢弃**。

## 5. 编解码

- 统一 JSON(可读、易调试、与 REST/前端互通)。
- `protocol` 包提供:
  - `Encode(msg) []byte` / `Decode([]byte) (Envelope, error)`。
  - 各 payload 强类型 struct + 按 `kind` 分发的解析器。
  - `v` 版本协商:不匹配时按规则降级或拒绝。
