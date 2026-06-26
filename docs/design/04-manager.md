# SIT Manager 内部设计(`internal/manager`)

> 状态:已确认设计(2026-06-24)。服务端业务层。

## 1. 组成

```
manager
├── server        启动 WSS(:443)与 REST(:8443)两个监听
├── registry      会话注册表:map[node_id]Conn,在线状态、last_seen
├── dispatcher    指令调度:下发/排队/重发/结果配对
├── api           REST handlers(见 03-rest-api)
├── auth          管理员认证(JWT/session)+ Node 凭证管理(enroll/revoke)
└── store         持久化(见 05-storage)
```

## 2. 会话注册表 registry

- 维护每个在线 Node 的 `Conn`、`status`、`last_seen`、最近一次 heartbeat 指标。
- 心跳超时(>90s)将 Node 置 `offline` 并关闭会话。
- 提供按 node_id 查会话、列出全部节点等查询给 api 层。

## 3. 指令调度 dispatcher(核心)

**下发流程(对应 POST /tasks):**
1. api 收到下发请求 → 创建 Task(状态 `queued`),落库,返回 `task_id`。
2. 若 Node 在线:封装为 instruction(task_id 作为 instruction.id),经 registry 的 Conn 下发 → 状态 `sent`,等 ack。
3. 若 Node 离线:留在持久化离线队列,Node 重连后按序投递。
4. 收到 Node `ack`:确认已达;超时未 ack → 重连后重发(同一 id 幂等)。
5. 收到 result Notification(ref_id=task_id):配对 Task,落 stdout/stderr/exit_code → 状态 `succeeded`/`failed`/`timeout`。
6. 过 deadline 仍未执行的:置 `expired`。

**幂等与去重**:维护近期处理过的消息 id 窗口;重复 result 丢弃。

## 4. 上报处理

- `register`:绑定到**握手认证出的身份**(非自报),更新设备信息(os/arch/version/addrs 双栈多 IP),写 store + activity。
- `heartbeat`:刷新 last_seen 与指标。
- `result`:交 dispatcher 配对 Task。
- `event`:写 activity 时间线(online/shutting_down/process_died...)。

## 5. 安全职责(要塞)

- 唯一公网暴露面,最小化端口;凭证加密存储(见 05-storage)。
- 管理员认证与 Node 凭证认证分离。
- Node 凭证可吊销(revoke → 踢断 + 拒连)。
- 所有 stdout/stderr 落库/出 API 时按不可信数据处理(转义留给前端,Manager 不拼接执行)。
- 预留审计锚点(C 级):dispatcher 与上报处理处预留 hook,将来记录"谁对哪个 Node 下发了什么命令、结果如何"。
