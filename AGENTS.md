# AGENTS.md

面向 AI 编码代理的工程约定。开始改动前请通读本文件,它编码了本仓库的硬约束与既定决策。

## 项目速览

SIT(StayInTouch)是 Go 实现的轻量级远程管理系统:Manager(公网服务端)经 WSS 长连接下发 Instruction,Node(常驻 agent)上报 Notification。单二进制 `sit`,子命令 `manager` / `node` / `node enroll` / `version`。模块路径 `github.com/sit/sit`。

## 环境与命令

- **Go 工具链固定 `go1.23.0`**:所有 go 命令前置 `GOTOOLCHAIN=go1.23.0`。否则会自动下载更高版本并拉入与 Go 1.23 不兼容的传递依赖。
- 构建:`GOTOOLCHAIN=go1.23.0 go build ./...`
- 测试:`GOTOOLCHAIN=go1.23.0 go test ./...`
- 静态检查:`GOTOOLCHAIN=go1.23.0 go vet ./...`
- 交叉编译:`GOTOOLCHAIN=go1.23.0 scripts/build-release.sh`(`CGO_ENABLED=0`,linux/darwin × amd64/arm64)
- 提交前必须 build + vet + test 全绿。

> 若环境中 `go test ./...` 因联网下载工具链而卡住,改用预编译再执行:
> `go test -c -o /tmp/x.test ./internal/<pkg>/ && timeout 120 /tmp/x.test -test.v`

## 工作流程(强制)

1. **TDD,小步快跑**:先写失败测试 → 跑 → 最小实现 → 跑 → vet → 提交。每个可独立验证的小单元一次提交。遵循 DRY / YAGNI。
2. **文档同步(CONV-1)**:所有设计落在 `docs/design/`。**任何设计层面的变更必须同步回写对应文档**,新决策追加到 `docs/design/DECISIONS.md`(ADR 格式,顺延编号,当前到 ADR-011)。代码与文档不一致视为缺陷。
3. **提交信息**:沿用现有风格 —— `type(scope): 中文摘要`,正文列要点(见 `git log`)。不要 `--no-verify`、不要跳过签名,除非用户明确要求。

## 架构硬约束(不可违反)

1. **方向收敛**:连接永远 Node → Manager。**Node 不监听任何端口**,只出站。
2. **传输加密**:全链路 WSS/TLS;Node 必须校验 Manager 证书(`insecure_skip_verify` 仅测试用)。
3. **凭证隔离**:一节点一凭证,可单独吊销。三套独立凭证系统,严禁混用:
   - 管理员 token —— REST(`Authorization: Bearer`)
   - Node 凭证 —— WSS 握手(`<node_id>:<secret>`)
   - MCP token —— MCP 端点 header bearer
4. **只存哈希**:secret/token 用 SHA-256,password 用 bcrypt。**任何明文凭证都不得落盘或入库**。
5. **身份以握手为准**:Node 身份取自 WSS 握手认证(`authNodeID`),**绝不信任 register 上报里的自报 node_id**。
6. **断线即常态**:正确性只依赖"断了能快速回来"(心跳 + 退避重连 + 多端点故障转移),不依赖"连接永不断"。
7. **Manager 是要塞**:唯一公网暴露面,最小化端口,按最高安全等级对待。
8. **输出不可信**:Node 回传的 stdout/stderr 视为不可信字节,展示时转义。
9. **MCP 默认关**:逐节点 `mcp_enabled` 默认 OFF,收敛爆炸半径。

## 分层与依赖方向

```
cmd/sit ──> internal/managerd ──> internal/manager/{api,mcp} ──> internal/manager(core)
                                                                       │
internal/node ──┐                                                      ▼
                ├──> internal/transport ──> internal/protocol <────────┘
```

- `internal/protocol`:纯数据契约,无业务依赖,Manager/Node 共享。
- `internal/transport`:纯通道(WSS、心跳、读写泵、重连),只依赖 protocol。
- `internal/manager`(core):auth/registry/dispatcher/reports/store/config。
- `internal/manager/api`、`internal/manager/mcp`:薄 HTTP 层,依赖 core。
- `internal/managerd`:**进程装配层**。务必把"组装 core + 挂载 api/mcp/wss 监听器"的代码放这里,**不要放回 `internal/manager`** —— 那会因 api/mcp 反向依赖 manager 形成导入环(ADR-011 已踩过此坑)。
- `internal/node`:executor/reporter/client/bootstrap/identity/config。

新增功能前,先确认它属于哪一层;不要让契约层/通道层反向依赖业务层。

## 约定与口味

- **时间戳**:一律 unix 毫秒(int64),用 `protocol.NowMillis()`。
- **消息 ID**:ULID,用 `protocol.NewID()`。
- **HTTP 路由**:Go 1.22 ServeMux 模式(如 `"POST /api/v1/nodes/{node_id}/tasks"`,`r.PathValue("node_id")`)。
- **错误响应**:统一 `{"error":{"code","message"}}`,经 `writeError` 输出。
- **配置解析**:自实现的极简 YAML 子集(扁平 `key: value` + `endpoints` 列表),**不引第三方 YAML 库**(会拉入 Go 1.25 传递依赖)。新增配置项时扩展现有 `parseConfig`/`LoadConfig` 的 switch。
- **协议**:JSON 信封走 WS text frame;帧上限 1MB(`MaxFrameBytes`);`ProtocolVersion=1`;类型 `TypeInstruction`/`TypeNotification`/`TypeAck`。
- **Task 状态机**:`queued→sent→succeeded/failed/expired/timeout`。
- **依赖**:新增第三方库前先评估其传递依赖是否兼容 Go 1.23;能手写的小工具(JSON-RPC、YAML 子集)优先手写。

## 测试约定

- 单测与被测代码同包同目录(`*_test.go`)。
- HTTP 层用 `httptest`;`internal/managerd/e2e_test.go` 是黑盒全链路验收,改动接线/路由/协议后务必跑通它。
- 用户偏好:运行测试包与解析结果时参考 test framework v1.0 的 `SKILL.md`(若环境提供)。

## 禁止事项

- 不在沙箱/运行环境内启动任何监听端口的进程。
- 不生成直接请求 LLM Gateway 的代码。
- 不把明文凭证写入日志、配置或数据库。
- 不改动 `protocol` 线格式而不同步 `docs/design/01-protocol.md`。
