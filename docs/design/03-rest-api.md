# SIT Manager REST API 设计(`internal/manager` / http)

> 状态:已确认设计(2026-06-24)。**前端尚不实现**,本文仅为将来前端预留 API 契约。
> 所有响应 JSON;时间字段 unix 毫秒;错误结构统一:
> `{ "error": { "code": "STRING_CODE", "message": "..." } }`

## 0. 设计要点

- **Node ID vs 显示名**:`node_id` 由 Node 首次启动随机生成(ULID),持久化于 Node 本地,全局唯一、**不可改**;Manager 侧另存可编辑的 `display_name`。前端展示用 `display_name`,寻址用 `node_id`。
- **异步命令模型**:前端"下命令"= 创建一个 **Task**,立即返回 `task_id`,不同步等结果。Manager 将其作为 instruction 投递(在线即发,离线进队列);Node 执行完用 result Notification 回传;Manager 落库;前端轮询 `GET /tasks/{task_id}` 取最终 stdout/stderr。
- **两套独立认证,勿混**:
  - **管理员 ↔ Manager**:登录换 token(Bearer),保护 REST API(本文)。
  - **Node ↔ Manager**:每节点凭证,保护 WSS 长连(见 02-transport)。
- 基础路径:`/api/v1`。

## 1. 认证(管理员)

| Method | Path | 说明 |
|---|---|---|
| POST | `/api/v1/auth/login` | body `{username,password}` → `{token, expires_at}` |
| POST | `/api/v1/auth/logout` | 失效当前 token |
| GET  | `/api/v1/auth/me` | 当前登录管理员信息 |

后续请求头:`Authorization: Bearer <token>`。

## 2. Node 列表与详情

| Method | Path | 说明 |
|---|---|---|
| GET    | `/api/v1/nodes` | 列表。`?status=online\|offline&q=<关键字>`。每项:`node_id, display_name, status, last_seen, os, arch, version, addrs[], mcp_enabled` |
| GET    | `/api/v1/nodes/{node_id}` | 详情:基础信息 + 最近一次 heartbeat 指标 + 注册信息(双栈多 IP `addrs`)+ `mcp_enabled` |
| PATCH  | `/api/v1/nodes/{node_id}` | 重命名:body `{ "display_name": "我的Mac Studio" }` |
| DELETE | `/api/v1/nodes/{node_id}` | 注销/移除 Node(同时吊销凭证) |
| POST   | `/api/v1/nodes/{node_id}/mcp:enable` | 开启该 Node 的 MCP 可达(见 09-mcp §5) |
| POST   | `/api/v1/nodes/{node_id}/mcp:disable` | 关闭该 Node 的 MCP 可达(默认即关闭) |

`status`:`online | offline`(由心跳超时判定,见 02-transport)。

## 3. 最近活动 / Last Seen

| Method | Path | 说明 |
|---|---|---|
| GET | `/api/v1/nodes/{node_id}/activities` | 活动时间线(注册、上下线 event、心跳摘要、指令记录)。`?limit=&before=` 分页 |

`status` / `last_seen` 在详情接口已含;本接口回答"最近发生了什么"。

## 4. 异步任务(执行命令,核心)

| Method | Path | 说明 |
|---|---|---|
| POST | `/api/v1/nodes/{node_id}/tasks` | **下发命令**。body:`{kind:"shell", command, timeout_sec}` 或 `{kind:"predefined", name, args}`。返回 `{task_id, state}`,立即返回不等执行 |
| GET  | `/api/v1/nodes/{node_id}/tasks` | 该 Node 任务历史。`?state=&limit=&before=` |
| GET  | `/api/v1/tasks/{task_id}` | 任务详情与结果 |

任务详情结构:
```json
{
  "task_id":"...", "node_id":"...",
  "kind":"shell", "command":"...", "args":{},
  "state":"succeeded",
  "created_at":0, "sent_at":0, "finished_at":0,
  "exit_code":0, "stdout":"...", "stderr":"...",
  "truncated":false, "duration_ms":0
}
```

### Task 状态机
```
queued ──(Node在线/重连)──> sent ──(result回传)──> succeeded | failed
  │                           │
  │(过 deadline)              │(执行超时被kill)
  └────> expired              └────> timeout
```
- `queued`:Node 离线,排队中。
- `sent`:已下发,等执行。
- `succeeded`/`failed`:由 exit_code 决定。
- `expired`:过 deadline 未执行。
- `timeout`:执行超时被 Node kill。

前端轮询 `GET /tasks/{task_id}` 直到终态即可。

## 5. 凭证管理(落实安全 B 级:每节点凭证可吊销)

| Method | Path | 说明 |
|---|---|---|
| POST | `/api/v1/nodes/enroll` | 生成一次性 **enrollment token**,供新 Node 首次接入认证。返回 `{enroll_token, expires_at}` |
| POST | `/api/v1/nodes/{node_id}/revoke` | 吊销该 Node 凭证(踢下线 + 拒绝再连) |

Node 如何用 enrollment token 完成首次握手并换取长期凭证,见 `02-transport.md`。

## 6. 覆盖性对照(对应用户前端预期)

| 用户预期 | 对应 API |
|---|---|
| 登录 Manager | §1 login |
| 查看 Node 列表 | §2 GET /nodes |
| 查看状态 / Last Seen / 最近活动 | §2 详情 + §3 activities |
| 异步执行非交互 shell,结果后取 | §4 POST tasks + GET tasks/{id} |
| 重命名 | §2 PATCH display_name |
| Node ID 随机、可命名 | node_id(ULID 随机)+ display_name |
