# SIT 持久化设计(`internal/manager/store`)

> 状态:已确认设计(2026-06-24)。
> 规模小(几十节点),选用 **SQLite**(单文件、零运维、Go 有成熟驱动),足够且简单。日后规模上来可换 Postgres,store 层接口隔离便于替换。

## 1. 存储选型

- **SQLite**(WAL 模式),单文件数据库。
- store 层定义接口,业务只依赖接口,实现可替换。

## 2. 数据表

### nodes — 节点
| 字段 | 类型 | 说明 |
|---|---|---|
| node_id | TEXT PK | 随机 ULID,Node 自报但以握手身份校验 |
| display_name | TEXT | 可编辑显示名 |
| os / arch / version | TEXT | 设备信息 |
| hostname | TEXT | |
| addrs_json | TEXT | 双栈多 IP 列表(JSON) |
| status | TEXT | online/offline(运行态,可不持久或定期刷) |
| mcp_enabled | INTEGER(bool) | 是否对 MCP 网关开放,默认 0(关) |
| last_seen | INTEGER | 毫秒 |
| created_at | INTEGER | 毫秒 |

### node_credentials — 凭证(安全 B 级)
| 字段 | 类型 | 说明 |
|---|---|---|
| node_id | TEXT | 关联 nodes |
| secret_hash | TEXT | 长期凭证哈希(不存明文) |
| state | TEXT | active / revoked |
| issued_at / revoked_at | INTEGER | 毫秒 |

### enroll_tokens — 一次性接入令牌
| 字段 | 类型 | 说明 |
|---|---|---|
| token_hash | TEXT | 不存明文 |
| state | TEXT | unused / used / expired |
| expires_at | INTEGER | 毫秒 |

### tasks — 异步任务(= 下发的 instruction + 结果)
| 字段 | 类型 | 说明 |
|---|---|---|
| task_id | TEXT PK | = instruction.id |
| node_id | TEXT | 目标 |
| kind | TEXT | shell / predefined |
| command / name / args_json | TEXT | |
| state | TEXT | queued/sent/succeeded/failed/expired/timeout |
| created_at/sent_at/finished_at | INTEGER | 毫秒 |
| deadline | INTEGER | 毫秒 |
| exit_code | INTEGER | |
| stdout / stderr | TEXT | 截断后,视为不可信 |
| truncated | INTEGER(bool) | |
| duration_ms | INTEGER | |

> tasks 表同时充当**离线队列**:`state=queued` 且 Node 离线的记录,重连后按 created_at 顺序投递。

### activities — 活动时间线
| 字段 | 类型 | 说明 |
|---|---|---|
| id | INTEGER PK | |
| node_id | TEXT | |
| type | TEXT | register/online/offline/event/task_sent/task_result |
| detail_json | TEXT | |
| at | INTEGER | 毫秒 |

### admins — 管理员(REST 登录)
| 字段 | 类型 | 说明 |
|---|---|---|
| username | TEXT PK | |
| password_hash | TEXT | bcrypt/argon2 |
| created_at | INTEGER | 毫秒 |

## 3. 安全

- 所有 secret/token/password **只存哈希**,不存明文。
- 凭证吊销 = 置 `state=revoked`,握手时拒绝。
- stdout/stderr 原样存(截断后),输出到 API 时由前端负责转义展示。
