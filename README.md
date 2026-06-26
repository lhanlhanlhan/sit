# SIT (StayInTouch)

> 轻量级远程管理系统 —— 类 IM 长连接,Manager 下发指示、Node 上报状态。单二进制、跨平台、零外部基础设施。

SIT 由两类角色组成:

- **SIT Manager** —— 服务端,部署在公网,管理所有注册的 Node;同时内嵌 REST API 与 MCP 网关。
- **SIT Node** —— 常驻 agent,部署在受管设备上,**开机自启、只出站、不监听任何端口**,主动挂载到 Manager 随时待命。

二者之间是一条 **WebSocket over TLS(WSS)全双工长连接**:Manager → Node 下发 **Instruction**,Node → Manager 上报 **Notification**(register / heartbeat / result / event)。

## 核心特性

- **单二进制 `sit`**:`manager` / `node` / `node enroll` / `version` 四个子命令,交叉编译 linux/darwin × amd64/arm64。
- **方向收敛**:连接永远 Node → Manager,Node 不监听端口,适配 NAT/内网环境。
- **异步任务模型**:下发命令 = 创建 Task 立即返回 `task_id`;Node 执行完回传结果;`tasks` 表兼作离线队列,节点重连后补投。
- **断线即常态**:心跳保活(30s)+ 指数退避重连 + 多端点故障转移(Happy Eyeballs)。
- **安全分级(B 级)**:一节点一凭证、可单独吊销;全链路 TLS;Node 校验 Manager 证书。三套独立凭证:管理员 token(REST)/ Node 凭证(WSS)/ MCP token(MCP 端点)。所有 secret/token/password **仅存哈希**。
- **内嵌 MCP 网关**:Manager 即 Relay,把 MCP 工具调用(`run_command` / `get_status`)翻译为 Instruction 操作远端 Node;逐节点 `mcp_enabled` 开关,默认关。
- **纯 Go 持久化**:`modernc.org/sqlite`(无 CGO),`CGO_ENABLED=0` 静态交叉编译。

## 架构

```
                公网
   ┌─────────────────────────────┐
   │   SIT Manager (要塞单点)      │
   │   - WSS server  :443         │  ← Node 入站连接
   │   - REST + MCP  :8443        │  ← 管理端 / AI Agent
   │   - SQLite 持久化             │
   └───────▲──────────────▲───────┘
           │ wss (出站)    │ wss (出站)
     ┌─────┴────┐    ┌─────┴────┐
     │ SIT Node │    │ SIT Node │   ← 内网/NAT 后,只出站,不监听端口
     │ (linux)  │    │ (macOS)  │
     └──────────┘    └──────────┘
```

## 快速开始

### 构建

```bash
# 单平台(当前机器)
GOTOOLCHAIN=go1.23.0 go build -o sit ./cmd/sit

# 多平台发布(产出到 dist/)
GOTOOLCHAIN=go1.23.0 scripts/build-release.sh [version]
```

> 工具链固定为 `go1.23.0`:避免自动下载更高版本拉入不兼容的传递依赖。

### 运行 Manager

```bash
sit manager --config /etc/sit/manager.yaml
```

配置样例见 [`deploy/config/manager.yaml`](deploy/config/manager.yaml)(扁平 `key: value` 子集):

```yaml
listen_wss: ":443"
listen_api: ":8443"
store_path: /var/lib/sit/manager.db
tls_cert_file: /etc/sit/tls/fullchain.pem
tls_key_file:  /etc/sit/tls/privkey.pem
admin_user: admin
admin_password: change-me      # 首次启动 seed(bcrypt 存储,后续不再生效)
mcp_token: ""                  # MCP 端点 bearer;空则关闭 MCP 鉴权
```

### 接入 Node(两阶段 enroll)

```bash
# 1. 管理员在 Manager 上签发一次性 enroll token(REST)
#    POST /api/v1/nodes/enroll  ->  {enroll_token, expires_at}

# 2. 在受管设备上用该 token 换取长期凭证
sit node enroll --token <enroll_token> --endpoint https://mgr.example:8443

# 3. 启动常驻 agent
sit node --config /etc/sit/node.yaml
```

`node.yaml` 样例见 [`deploy/config/node.yaml`](deploy/config/node.yaml)。node_id 与长期凭证由程序在状态目录(0600)维护,不写进配置文件。

### 开机自启

- Linux(systemd):[`deploy/systemd/sit-node.service`](deploy/systemd/sit-node.service) → `systemctl enable --now sit-node`
- macOS(launchd):[`deploy/launchd/com.sit.node.plist`](deploy/launchd/com.sit.node.plist) → `launchctl load ...`

> 两层兜底:进程级自启(systemd/launchd Restart)管"进程死了拉起来",应用级退避重连管"连接断了重连上"。

## REST API(`/api/v1`)

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| POST | `/auth/login` | 公开 | 管理员登录,返回 bearer token |
| POST | `/enroll/exchange` | enroll_token | 节点换取长期凭证(两阶段接入) |
| GET | `/nodes` | 管理员 | 节点列表(状态以注册表实时为准) |
| GET / PATCH / DELETE | `/nodes/{node_id}` | 管理员 | 查看 / 重命名 / 吊销 |
| POST | `/nodes/{node_id}/mcp:enable` `:disable` | 管理员 | 逐节点 MCP 开关 |
| POST / GET | `/nodes/{node_id}/tasks` | 管理员 | 下发任务(202 + task_id)/ 列表 |
| GET | `/tasks/{task_id}` | 管理员 | 轮询任务结果 |
| GET | `/nodes/{node_id}/activities` | 管理员 | 节点活动时间线 |
| POST | `/nodes/enroll` `/nodes/{node_id}/revoke` | 管理员 | 签发 enroll token / 吊销凭证 |

MCP 端点为 `/mcp`(JSON-RPC 2.0 over Streamable HTTP),通过 `X-SIT-Node` 头或 `?node=` 参数寻址目标 Node。

## 工程结构

| 包路径 | 职责 |
|---|---|
| `cmd/sit` | 入口与子命令路由(cobra) |
| `internal/protocol` | 消息定义与编解码(线格式契约) |
| `internal/transport` | WSS 连接管理(握手、心跳、读写泵、重连、故障转移) |
| `internal/manager` | 服务端核心(auth / registry / dispatcher / reports / store / config) |
| `internal/manager/api` | REST API 层(thin HTTP over core) |
| `internal/manager/mcp` | MCP 网关(Manager 即 Relay) |
| `internal/managerd` | 进程装配层(组装 core + WSS/REST/MCP 监听器) |
| `internal/node` | 客户端核心(executor / reporter / client / bootstrap / identity / config) |

> `internal/managerd` 独立于 `internal/manager`:因 `api`/`mcp` 依赖 `manager`,装配若放在 `manager` 内会形成导入环,故上移到独立包(见 ADR-011)。

## 测试

```bash
GOTOOLCHAIN=go1.23.0 go test ./...
```

含 `internal/managerd/e2e_test.go` 全链路验收:登录 → 签发 enroll token → exchange 接入 → Agent 经 WSS 连接 → 下发 shell 任务 → 轮询至 `succeeded`。

## 设计文档

完整设计见 [`docs/design/`](docs/design/):总体架构、协议、传输、REST API、Manager、Node、存储、部署、测试、MCP,以及全部架构决策记录 [`DECISIONS.md`](docs/design/DECISIONS.md)。

## 技术栈

Go 1.23 · [coder/websocket](https://github.com/coder/websocket) · [spf13/cobra](https://github.com/spf13/cobra) · [oklog/ulid](https://github.com/oklog/ulid) · [modernc.org/sqlite](https://gitlab.com/cznic/sqlite)(纯 Go)· golang.org/x/crypto(bcrypt)
