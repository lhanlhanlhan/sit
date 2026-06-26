# SIT (StayInTouch) — 总体架构设计

> 状态:进行中(逐节确认)。最后更新:2026-06-24
> 本文件随设计推进持续更新;每次设计变更必须同步回写本目录对应文件。

## 1. 项目概述

SIT(StayInTouch)是一个轻量级远程管理系统,采用类 IM 的长连接通讯。
分两类节点:

- **SIT Manager** —— 服务端,部署在公网,管理所有注册的 Node。
- **SIT Node** —— 常驻 agent,部署在受管设备上,开机自启并主动挂载到 Manager,随时接收指示。

Node 与 Manager 之间是一条**全双工长连接**:

- Manager → Node:下发**指示(Instruction)**。
- Node → Manager:上报**状态(Notification)**。

## 2. 规模与运行环境

- 节点规模:当前 1~2 台,未来最多十几到几十台(小规模)。
- Node 运行环境:Linux / macOS,含嵌入式设备(内存 2~4GB)。
- Manager:部署在公网。
- 因规模小,**不引入消息队列、集群、服务发现等重型基础设施**,保持简单。

## 3. 技术选型

- 语言:**Go**(单二进制、跨平台交叉编译、并发原生)。
- 交付形态:**单一二进制 `sit`,两个子命令**:
  - `sit manager` —— 启动服务端。
  - `sit node` —— 启动常驻 agent。
- 前端:**当前不实现**,但 Manager 必须预留 REST API 以便后续接入前端页面。
- 传输:**WebSocket over TLS(WSS)**。选型理由见 `docs/design/DECISIONS.md`。

## 4. 总体架构

```
                公网
   ┌─────────────────────────────┐
   │   SIT Manager (要塞单点)      │
   │   - WSS server  :443         │
   │   - REST API    :8443        │
   │   - 持久化存储                 │
   └───────▲──────────────▲───────┘
           │ wss (出站)    │ wss (出站)
     ┌─────┴────┐    ┌─────┴────┐
     │ SIT Node │    │ SIT Node │   ← 内网/NAT 后,只出站,不监听端口
     │ (linux)  │    │ (macOS)  │
     └──────────┘    └──────────┘
```

### 职责划分

**`sit manager`**(常驻服务端,公网):
1. 接受 Node 的 WebSocket 入站连接并维持会话。
2. 维护节点注册表与在线状态。
3. 暴露 REST API(给未来前端用)。
4. 持久化节点信息、指令历史、上报记录。

**`sit node`**(常驻 agent,Linux/macOS/嵌入式):
1. 开机自启,主动出站连 Manager。
2. 接收并执行 Instruction(预定义命令 + 任意命令)。
3. 上报 Heartbeat / Result / Event / Register 四类 Notification。
4. 断线自愈、多端点故障转移。

## 5. 关键不变量(贯穿全设计的硬约束)

1. **方向收敛**:连接方向永远是 Node → Manager,Node **不监听任何端口**。
2. **传输加密**:全链路 WSS(TLS),Node 必须校验 Manager 证书。
3. **凭证隔离**:一节点一凭证,可单独吊销。
4. **断线即常态**:Node 把"断线"当常态 —— 心跳保活 + 退避重连 + 多端点故障转移;系统正确性不依赖"连接永不断",只依赖"断了能快速回来"。
5. **Manager 是要塞**:Manager 是唯一公网暴露面,按最高安全等级对待。

## 6. Go 工程内部分层

| 包路径 | 职责 | 依赖关系 |
|---|---|---|
| `cmd/sit` | 入口与子命令路由(cobra) | 依赖 manager / node |
| `internal/protocol` | 消息定义与编解码(Instruction/Notification 线格式) | 无业务依赖,双方共享的唯一契约 |
| `internal/transport` | WebSocket 连接管理(握手、心跳、读写泵、重连、端点故障转移) | 依赖 protocol |
| `internal/manager` | 服务端业务(会话注册表、指令调度、REST API、存储) | 依赖 protocol/transport |
| `internal/manager/mcp` | MCP 网关:Manager 作为 Relay,把 MCP 工具调用翻译为 Instruction 操作 Node | 依赖 manager |
| `internal/node` | 客户端业务(命令执行器、上报器、自启集成) | 依赖 protocol/transport |

设计原则:`protocol` 是纯数据契约、`transport` 是纯通道、业务层只管业务,三者互不侵入,各单元职责单一、可独立测试。
