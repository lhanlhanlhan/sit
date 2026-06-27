# SIT (StayInTouch) 技术设计文档

轻量级远程管理系统,类 IM 长连接通讯。Manager(公网服务端)管理多个 Node(常驻 agent,开机自启、出站长连)。

## 文档索引

| 文档 | 内容 |
|---|---|
| [00-overview.md](./00-overview.md) | 总体架构、技术选型、关键不变量、工程分层 |
| [01-protocol.md](./01-protocol.md) | 消息协议:Envelope / Instruction / Notification / ACK |
| [02-transport.md](./02-transport.md) | 传输层:握手认证、心跳、重连、多端点故障转移、双栈 |
| [03-rest-api.md](./03-rest-api.md) | Manager REST API(为将来前端预留,暂不实现) |
| [04-manager.md](./04-manager.md) | Manager 内部:注册表、调度、上报处理、安全 |
| [05-node.md](./05-node.md) | Node 内部:身份、执行器、收发循环、上报 |
| [06-storage.md](./06-storage.md) | 持久化:SQLite 表结构 |
| [07-deployment.md](./07-deployment.md) | 部署、TLS、开机自启(systemd/launchd)、配置文件 |
| [08-testing.md](./08-testing.md) | 测试分层与关键用例 |
| [09-mcp.md](./09-mcp.md) | MCP 网关:Manager 即 Relay,AI Agent 操作 Node;工具集、逐节点开关、Roadmap |
| [10-security-stability-roadmap.md](./10-security-stability-roadmap.md) | 安全与稳定性规划:弱网自愈、systemd、sudoers、审计、运维检查 |
| [DECISIONS.md](./DECISIONS.md) | **决策记录(ADR)与特殊规约** —— 改设计先看这里 |

## 维护约定(CONV-1)

所有设计变更必须同步回写本目录对应文件,并在 `DECISIONS.md` 追加/更新 ADR,不删历史。
