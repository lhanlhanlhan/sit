# SIT 设计决策与特殊规约记录(Decision Log)

> 本文件专门记录**架构决策(ADR)**与**特殊规约**,防止遗忘。
> 每条决策记录:背景、结论、理由、影响。后续若推翻某决策,新增一条并标注"取代 ADR-NNN",不要删除历史。
> 最后更新:2026-06-24

---

## 特殊规约(Conventions)

- **CONV-1 文档落盘**:所有设计必须落盘到 `docs/` 目录;每次设计变更必须同步更新对应文件,防止遗忘。特殊规约与设计决策额外记录在本文件。
- **CONV-2 测试框架**:运行测试包与解析测试结果时,需参考测试框架 v1.0 的 `SKILL.md` 文档。

---

## ADR-001 传输层选用 WebSocket over TLS

- **背景**:需要 Node 与 Manager 间的全双工长连接;Manager 在公网,Node 在内网/NAT 后且可能处于受管网络。
- **结论**:采用 **WebSocket over TLS(WSS)**,而非 gRPC 双向流或纯 HTTP 长轮询/SSE,也不自研 TCP 协议。
- **理由**:
  1. 天然 NAT 穿透:Node 主动出站连接,无需公网入口。
  2. 全双工 + 应用层心跳(ping/pong)。
  3. 走 443/WSS,握手为标准 HTTP Upgrade,在 DPI 眼中与普通 HTTPS 流量难以区分,降低被受管网络误杀概率。
  4. 规模小(几十节点),gRPC/自研协议属过度工程;IM 大厂自研协议(微信 mmtls、Telegram MTProto)是为亿级连接,我们无需。
- **影响**:需自行实现应用层重连、消息确认、离线指令排队等逻辑(量不大)。

## ADR-002 单二进制双子命令

- **背景**:需同时交付服务端与 agent。
- **结论**:服务端与 agent 编译进**同一个二进制 `sit`**,以 `sit manager` / `sit node` 两个子命令区分入口。
- **理由**:简化交付与版本一致性;共享 `protocol` 包保证两端契约一致。

## ADR-003 安全模型当前做到 "B" 级

- **背景**:Manager 在公网,Node 能访问内网资源,且支持任意命令执行 —— 这条长连本质是一条从公网穿透进内网的隧道,风险高。
- **结论**:当前阶段安全模型做到 **B 级**:**每节点独立凭证 + 全链路 TLS 加密 + Node 校验 Manager 证书**。审计(C 级:操作留痕)作为后续增强,有余力再做。
- **硬性底线(不打折)**:
  1. 每节点独立凭证,且**凭证可吊销**。
  2. TLS 强制 + 服务端证书校验,杜绝中间人。
  3. Node 永不监听端口,仅出站,不增加 Node 侧攻击面。
  4. Manager 当要塞:最小化暴露端口、凭证加密存储。
  5. 连接 ≠ 信任:每条 Instruction 仍需校验 Node 身份令牌,令牌可设过期。
- **影响**:命令级 RBAC、危险命令二次确认(D 级)暂不做。

## ADR-004 接入韧性:多端点故障转移 + 可带外更新

- **背景**:担心 Manager 域名/IP 被受管网络列入黑名单,导致节点彻底失联(此情况自动重连无效)。
- **结论**:采用韧性方案,目标是**对抗常规误杀和单点封禁**,**不承诺对抗专门的定向深度封锁**(后者投入产出比极差,且可能触碰合规红线)。
- **具体措施**:
  1. **多接入端点**:Node 配置一组 endpoint(主域名 → 备用域名 → 直连 IP),逐个尝试,一个被封自动切下一个。
  2. **CDN/反向代理前置**:Manager 真实地址藏在 CDN 后,Node 连 CDN 边缘 IP,提高封禁代价。
  3. **配置可带外更新**:端点列表放 Node 本地配置文件,**不硬编码进二进制**;全部端点被封时可人工/带外更新配置恢复,无需重新发版。
- **影响**:Node 需支持配置文件热加载或重启加载;transport 层需实现端点列表轮询逻辑。

## ADR-006 协议层设计与安全

- **背景**:JSON 信封协议是两端唯一契约,且 shell 指令为最高危面。
- **结论(协议要点)**:
  1. 统一 Envelope(v/type/id/ts/payload)+ 类型化 payload,JSON over WS text frame。
  2. **所有时间戳统一 unix 毫秒(int64)**。
  3. register 的 `addrs` 为**数组**,支持多 IP / IPv4+IPv6 双栈,每项带 family/iface/scope。
- **安全要点**:
  - **a. 身份不信任自报**:`register.node_id`/`addrs` 仅为设备自描述,身份认证以 WSS 握手凭证为准;Manager 用握手身份绑定会话。
  - **b. 高危面可追溯**:instruction 显式区分 predefined/shell;协议预留审计字段(id/ref_id/ts/kind/command 摘要),将来开 C 级审计零改协议。
  - **c. 输出不可信**:stdout/stderr 强制截断 + 视为不可信字节,存储/展示必须转义,防二次注入。
  - **d. 重放/过期**:`deadline`(毫秒)+ `id` 去重,防离线积压指令被重放误执行;重发复用同一 id 保证幂等。
  - **e. 资源耗尽防护**:单帧 ≤1MB,解码前校验长度;timeout_sec 由 Node 强制 kill 超时进程(护嵌入式内存)。
  - **f. 版本协商**:`v` 不匹配按规则降级/拒绝。
- **影响**:transport/manager/node 各层据此实现;审计字段先留不填。

## ADR-007 拨号采用 Happy Eyeballs(双栈)

- **背景**:endpoint DNS 可能同时返回 A/AAAA,某一栈不通会长时间卡顿。
- **结论**:Node 拨号采用 Happy Eyeballs(RFC 8305)并发尝试 v4/v6,取先连上者。
- **影响**:transport 层拨号逻辑。

## ADR-005 受管网络下的连接保活与自愈

- **背景**:长连接可能被 NAT 空闲老化、DPI 启发式规则、强制代理等"误杀"。
- **结论**:把断线当常态,通过以下机制自愈:
  1. **应用层心跳**:Node 每 ~30s 发 WebSocket ping,避免 NAT 空闲老化。
  2. **断线即重连**:指数退避 + 抖动,避免重连雪崩。
  3. **443/WSS 伪装**:贴近正常 HTTPS 流量。
  4. (可选,后置)读 `HTTP_PROXY` 穿透强制代理。
  5. (可选,YAGNI)极端情况下降级为 HTTP 轮询拉取指令。
- **影响**:transport 层实现心跳与退避重连;4、5 项先留接口不实现。

## ADR-008 异步任务模型与 REST API 契约

- **背景**:前端(将来实现)需要登录 Manager、看 Node 列表、看状态/Last Seen/最近活动、异步下发 shell 命令(布置即走、结果后取)、重命名。
- **结论**:
  1. **node_id 随机不可改 + display_name 可编辑**:node_id 为 Node 本地随机生成 ULID;Manager 侧另存可改显示名。
  2. **异步任务模型**:下发命令 = 创建 Task,立即返回 task_id;Node 执行完经 result 回传;前端轮询 `GET /tasks/{id}` 取结果。Task 状态机:queued→sent→succeeded/failed/expired/timeout。
  3. **两套独立认证**:管理员↔Manager(Bearer token)与 Node↔Manager(每节点凭证)严格分离。
  4. REST 基础路径 `/api/v1`;时间毫秒;统一错误结构。
- **状态**:前端**暂不实现**,本 ADR 仅冻结 API 契约供将来对接。

## ADR-009 持久化选用 SQLite

- **背景**:几十节点小规模,需存节点、凭证、任务、活动、管理员。
- **结论**:用 **SQLite(WAL)** 单文件;store 层接口隔离,日后可换 Postgres。
- **要点**:secret/token/password 只存哈希;tasks 表兼作离线队列(queued 记录重连后投递)。

## ADR-010 MCP 网关(Manager 即 Relay)

- **背景**:让 AI Agent 通过 Manager 操作下挂 Node。参考三方工具 MiraMCP(本地 bash 执行器,经 MCP Relay 接入),但 MiraMCP 是"操作本机",我们要"操作远程 Node"。
- **结论**:
  1. **Manager 自身即 MCP Relay/网关**。链路:`AI Agent → Manager(Relay) → Node(执行器)`。从 Agent 视角,每个 Node 是一个 MCP Server。
  2. **Node 不实现 MCP**:Manager 对外假装成各 Node 的 MCP Server,对内仍用既有 Instruction/Notification 协议。Node 零改动、保持极薄。
  3. **部署形态 B**:MCP 内嵌进 Manager 进程,走 MCP Streamable HTTP 远程 transport(非 stdio)。
  4. **执行模型 A**:同步包装 —— MCP 工具内部复用 Task 机制,阻塞等结果再返回,对 Agent 表现为同步。
  5. **Node 寻址**:URL 一致,靠 Header/URL Param 指定目标 Node;故**不提供 list_nodes 工具**(节点选择属连接路由)。
  6. **工具集 v1**:`run_command`(同步非交互执行)、`get_status`。会话/文件/安全策略类工具后置。
  7. **逐节点开关 `mcp_enabled`**:nodes 表布尔字段,默认关;经 REST `mcp:enable/disable` 控制;调用前校验,关闭则拒绝。收敛暴露面。
  8. **三套凭证分离**:管理员 token(REST)、Node 凭证(WSS)、MCP token(MCP 端点 bearer header)。MCP 端点可整体开关。
  9. **流式 v1 不做、仅预留**:协议预留 `stream` 标志位与 `result_chunk` 通知,增量流式与交互式会话同批后置实现。
- **影响**:nodes 表加 `mcp_enabled`;REST 加 mcp 开关接口;协议预留 stream/result_chunk;新增 `internal/manager/mcp`。
- **后续规划(Roadmap)**:① 增量流式输出 ② 交互式会话(对标 bash_start/interact/read/close)③ 远端文件读取(对标 read_file)④ 远端安全策略(对标 security_config)。均依赖协议扩展。

## ADR-011 装配层 managerd 与配置/交叉编译(实现期)

- **背景**:实现 Task 18 接线子命令时,`internal/manager/server.go` 需同时 import `api` 与 `mcp`,而两者都 import `manager`,触发 Go 导入环。
- **结论**:
  1. **新增装配包 `internal/managerd`**:Server 装配(store/auth/registry/dispatcher/reports + WSS/REST/MCP 监听器、reapLoop、优雅退出)上移至此;`manager` 仅保留核心服务 + `Config`/`LoadConfig`。`cmd/sit/manager.go` 改调 `managerd.NewServer`。
  2. **配置格式落地为扁平 YAML 子集**:不引第三方 YAML 库(规避 Go 1.25 传递依赖)。Node 支持 `endpoints` 列表 + `state_dir`/`heartbeat_sec`/`insecure_skip_verify`;Manager 用扁平 `key: value`(`tls`/`store` 不嵌套,键名 `tls_cert_file`/`tls_key_file`/`store_path` 等)。07-deployment.md 已同步。
  3. **交叉编译脚本** `scripts/build-release.sh`:`CGO_ENABLED=0` + `GOTOOLCHAIN=go1.23.0`,产出 linux/darwin × amd64/arm64 静态二进制。
  4. **端到端验收测试** `internal/managerd/e2e_test.go`:登录→签发 enroll token→exchange 接入→Agent 经 WSS 连接→下发 shell 任务→轮询结果 succeeded,全链路黑盒通过。
- **影响**:目录新增 `internal/managerd`、`deploy/`、`scripts/`;`cmd/sit/manager.go` 依赖 `managerd`。
