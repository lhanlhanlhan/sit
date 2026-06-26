# SIT MCP 网关设计(`internal/manager/mcp`)

> 状态:已确认设计(2026-06-26)。
> 让 AI Agent 通过 Manager 操作其下挂的 Node。参考三方工具 MiraMCP,裁剪到 SIT 的同步、非交互模型。

## 1. 角色与链路

**Manager 自身就是 MCP Relay / 网关。**

```
AI Agent ──(MCP, Streamable HTTP)──> Manager(Relay/网关) ──(WSS Instruction)──> Node(执行器)
                                          │
对照 MiraMCP:  AI Agent → MCP Relay → 本地 bash MCP 客户端(执行器)
对应 SIT:      AI Agent → Manager(Relay) → Node(执行器)
```

**关键边界:Node 不实现 MCP 协议。** Manager 对外"假装"成每个 Node 的 MCP Server,对内仍用既有 Instruction/Notification 协议与 Node 通讯。Node 保持极薄、零改动。

## 2. 部署形态

- **内嵌进 Manager 进程**(选型 B),走 MCP **Streamable HTTP** transport(远程 transport,非 stdio)。
- 这是 AI 原生应用的正确形态:Agent 经 Relay 接入,无需本地拉起进程。
- MCP 端点可整体开关;且每个 Node 有独立 `mcp_enabled` 开关(见 §5),收敛暴露面。

## 3. Node 寻址:每个 Node = 一个 MCP Server

- 从 Agent 视角,**每个 Node 就是一个 MCP Server**,URL 一致,靠 **Header 或 URL Param 指定目标 Node**。
  - 例:`POST /mcp` + header `X-SIT-Node: <node_id>`,或 `GET /mcp?node=<node_id>`。
- 因此**不提供 `list_nodes` 工具** —— 节点选择是连接路由的职责,不占工具位。Agent 要看有哪些 Node,走 Manager REST/前端。

## 4. 工具集(v1)

裁剪到同步、非交互模型:

### `run_command`
在(由 Header/Param 指定的)Node 上**同步执行**一条非交互命令,阻塞到结果或超时。

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `command` | string | 是 | 非交互 shell 命令 |
| `timeout_sec` | integer | 否 | 执行超时,默认 60,上限对齐 Node 约束 |

返回:`{ stdout, stderr, exit_code, duration_ms, truncated, timed_out }`。

实现:内部复用既有 **Task** 机制 —— 创建 Task → 经 WSS 下发 instruction → 阻塞等 result Notification → 转成 MCP 结果返回。等待超过 MCP 调用超时则返回 task_id,Agent 可后续查询(降级)。

### `get_status`
查目标 Node 的状态/指标/last_seen。

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| (无,Node 由 Header/Param 指定) | | | |

返回:`{ node_id, display_name, status, last_seen, os, arch, version, addrs[], last_heartbeat }`。

## 5. 逐节点开关 `mcp_enabled`

- nodes 表新增布尔字段 `mcp_enabled`,**默认关**。
- 控制:REST `POST /api/v1/nodes/{node_id}/mcp:enable` 与 `:disable`(也可 PATCH)。
- Manager 处理任何 MCP 工具调用前,先校验目标 Node 的 `mcp_enabled`;关闭则拒绝(403)。
- 效果:即便 MCP 端点被触达,爆炸半径收敛到主动放开的节点。与"Manager 是要塞、最小暴露面"一致。

## 6. 认证

- **MCP 接入凭证**:独立 bearer token,放 `Authorization` header,Manager 服务端鉴权。
- **三套凭证严格分离**:管理员 token(REST)、Node 凭证(WSS)、MCP token(MCP 端点)。
- MCP token 可签发/吊销;MCP 端点可整体开关。

## 7. 流式输出

- **v1:同步一次性返回**。命令执行完,Manager 把完整 stdout/exit_code 作为一个 MCP 结果返回。
- **传输层已具备**:MCP Streamable HTTP 原生支持 server→client 流式与 progress 通知。
- **内容层(增量 stdout)后置**:需 Node 分块上报(`result_chunk`),属协议扩展,与交互式会话同批实现。协议预留 `stream` 标志位与 `result_chunk` 通知类型(见 01-protocol)。

## 8. 后续规划(Roadmap)

按优先级,均依赖协议扩展:

1. **增量流式输出**:Node `result_chunk` 分块上报 → Manager 转 MCP 流式/progress,Agent 边跑边看 stdout。
2. **交互式会话**:对标 MiraMCP 的 `bash_start`/`bash_interact_process`/`bash_read_output`/`bash_close_session` —— 透传 stdin、信号、增量输出到远端 Node。需协议新增会话类消息。
3. **文件读取工具**:对标 `read_file`,在远端 Node 读文件并回传(注意大文件分块与体积上限)。
4. **远端安全策略**:对标 `security_config`,下沉危险命令黑名单/敏感路径到 Node executor,作为 shell 执行的额外护栏(与安全 C 级审计协同)。
