# SIT 安全与稳定性规划

> 状态:规划中(2026-06-27)。
> 背景:已有多台 Node 部署在公网 Manager 下,部分节点网络和供电不稳定。目标是保证"断网/断电可以发生,但网络恢复后 Node 必须自动回来",同时收敛远程执行带来的安全风险。

## 1. 当前基线

已经具备的能力:

- Node 主动出站连接 Manager,不监听端口。
- Node 进程内有无限重连循环,连接失败或断开后指数退避重试,退避上限约 60 秒。
- systemd service 使用 `Restart=always`,进程崩溃或断电重启后可自启动。
- Node 连接成功后会重新 register,Manager 侧刷新 online 状态。
- 离线 task 会留在 SQLite 队列,Node register 后 flush queue。
- 一节点一凭证,可单独 revoke。
- Node 默认建议用 `sit` system user 运行,避免直接 root 远程执行。

已发现的风险/缺口:

- 半开连接检测还不够强:当前有应用层 heartbeat notification,但缺少明确的 WebSocket ping/pong 读写超时策略。
- systemd 还需要更稳的 start-limit 配置,避免异常环境下被 systemd 限流。
- 不稳定节点可能因系统时间错误导致 TLS 校验失败。
- `timeout_sec`/deadline 在 REST task 路径存在执行层未强制 timeout 的问题,已在线上测试暴露。
- Manager 是高价值入口,管理员 API、MCP、Node shell task 都需要进一步审计和暴露面控制。

## 2. 优先级总览

| 优先级 | 工作项 | 目标 | 验收标准 |
|---|---|---|---|
| P0 | WebSocket ping/pong 与半开连接主动断开 | 网络恢复后 Node 能稳定重连 | 断网、拔网线、NAT idle 后,恢复网络 60 秒内重新 online |
| P0 | 修复 task timeout/deadline 执行语义 | 防止命令超时仍 succeeded | `sleep 2` + `timeout_sec=1` 必须进入 `timeout` |
| P0 | systemd restart hardening | 断电/异常退出后长期自愈 | service 不进入 start-limit,重启后自动连接 |
| P0 | Node setup/service 与 sudoers 策略文档 | 特权命令可控放行 | 默认非 root;需要 root 的命令走 sudoers 白名单 |
| P1 | 连接事件与失败原因日志 | 排查不稳定节点 | dial fail/connected/disconnected/retry delay 都有日志 |
| P1 | 多 endpoint 运维策略 | 单域名/DNS 故障时仍可回来 | Node 配置支持主备域名,文档给出推荐拓扑 |
| P1 | Manager 侧审计日志增强 | 高危操作可追溯 | shell command 摘要、操作者、node_id、task_id、结果状态可查询 |
| P1 | Manager API/MCP 暴露面收敛 | 降低公网入口风险 | Nginx IP allowlist/rate limit/CORS/MCP token 策略落文档 |
| P2 | Node 配置热加载或轻量 reload | 端点变更无需重启 | 修改 node.yaml 后可平滑加载 endpoints |
| P2 | 可选 HTTP polling fallback | 极端 WebSocket 不通时保底 | 受限网络下仍能拉取指令并上报结果 |
| P2 | 命令策略/RBAC | 降低误操作半径 | 可按 Node 或用户限制 shell/predefined/sudo 命令 |

## 3. P0 近期必须做

### P0-1 WebSocket ping/pong 与半开连接检测

问题:

- 部分网络故障不会立即让 TCP/WebSocket 读写返回错误。
- Node 可能长期卡在"看似已连接,实际不可达"的半开状态。

方案:

- 在 transport 层增加 keepalive,周期性发送 WebSocket ping。
- 设置 pong/read deadline,超时后主动 close conn。
- Node heartbeat 写入失败时主动 close conn,交给 Agent 外层 reconnect loop。
- Manager 侧也可对 stale session 主动 close,避免注册表保留僵尸连接。

验收:

- 断开网络 2 分钟再恢复,Node 在恢复后 60 秒内重新 online。
- Manager 断开/重启后,Node 在恢复后自动 reconnect。
- NAT idle 场景下不会长期停留在 stale online。

### P0-2 修复 task timeout/deadline

问题:

- 线上测试发现 `sleep 2` + `timeout_sec=1` 最终可能 `succeeded`,且 `finished_at` 晚于 `deadline`。

方案:

- Manager 创建 task 时把 `timeout_sec` 持久化或明确写入 Instruction。
- Dispatcher 下发 Instruction 时带上 `TimeoutSec`。
- Node executor 保持以 `TimeoutSec` 强制 kill 进程组。
- Node 执行前检查 deadline,执行后如已超过 deadline 也应按协议语义处理。

验收:

- `sleep 2` + `timeout_sec=1` 返回 `state=timeout`, `exit_code=-1`。
- 离线期间 deadline 过期的 task 不会被 Node 执行,状态为 `expired`。

### P0-3 systemd 自愈配置

建议 service:

```ini
[Unit]
Description=SIT Node
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=sit
Group=sit
ExecStart=/usr/local/bin/sit node --config /etc/sit/node.yaml
Restart=always
RestartSec=5
StartLimitIntervalSec=0

[Install]
WantedBy=multi-user.target
```

说明:

- `Restart=always` 处理进程崩溃。
- `StartLimitIntervalSec=0` 避免极端反复失败后被 systemd 限流。
- 是否加入 `NoNewPrivileges`/`ProtectSystem` 要谨慎:如果未来依赖 sudoers 白名单执行特权命令,过强 sandbox 可能让 sudo 路径失效。

验收:

- 手动 `kill` Node 进程后自动拉起。
- 机器断电重启后 Node 自动 online。
- 网络不可达时进程保持运行并持续重试,不被 systemd 标记 failed。

### P0-4 sudoers 白名单与最小权限

原则:

- Node 进程默认使用 `sit` system user,不直接 root。
- 需要系统权限的能力通过 sudoers 精确白名单放行。
- 不允许默认配置 `sit ALL=(ALL) NOPASSWD: ALL`。

示例:

```sudoers
sit ALL=(root) NOPASSWD: /usr/bin/systemctl restart nginx
sit ALL=(root) NOPASSWD: /usr/bin/journalctl
```

验收:

- 普通 shell task 以 `sit` 用户运行。
- 白名单命令可 sudo。
- 未列入白名单的 sudo 命令失败。

## 4. P1 稳定性与运维增强

### P1-1 连接日志

需要记录:

- dial start/end。
- 每个 endpoint 的失败原因。
- connected, disconnected。
- 下次重连 delay。
- register 成功/失败。

目标:

- 不稳定节点出现问题时,仅看 `journalctl -u sit-node` 就能判断是 DNS、TLS、HTTP 401、网络不可达还是 Manager 拒绝。

### P1-2 多 endpoint 策略

建议:

- 至少配置两个 endpoint。
- 主备域名尽量走不同 DNS/CDN/线路。
- 对无公网 DNS 稳定性的节点,可保留一个直连 IP endpoint 作为最后兜底,但 TLS 证书/SNI 要单独设计。

示例:

```yaml
endpoints:
  - wss://go.meating.cc/sit/connect
  - wss://backup.example.com/sit/connect
```

### P1-3 Manager 操作审计

需要补齐:

- admin 用户名。
- task_id。
- node_id。
- shell command 摘要或 hash。
- 创建时间、下发时间、完成时间、状态、exit_code。
- revoke/delete/mcp enable 等管理操作。

目标:

- 任何远程命令和凭证变更都能追溯。

### P1-4 Manager 网关安全

建议在 Nginx 层处理:

- 管理 API IP allowlist 或 VPN/Zero Trust 入口。
- 登录/API rate limit。
- CORS 只放行明确 origin,例如 `http://localhost:45101`。
- MCP 独立 token,并考虑单独路径限流。
- TLS 强制,禁用明文公网访问。

## 5. P2 后续能力

### P2-1 Node 配置热加载

目标:

- endpoints 变更无需重启进程。
- 适合被封域名/备用域名切换。

可选实现:

- SIGHUP reload。
- 定期检查 node.yaml mtime。
- Manager 下发"更新 endpoints" predefined task。

### P2-2 HTTP polling fallback

目标:

- WebSocket 被局部网络拦截时,Node 仍可通过 HTTPS polling 拉取 task。

代价:

- 协议与 Manager API 都要扩展。
- 实时性下降,但可作为极端兜底。

### P2-3 命令策略/RBAC

目标:

- 对不同 Node 或不同用户限制能执行的命令类型。
- 支持只允许 predefined、禁用 shell、限制 sudo 命令。

## 6. 运维检查清单

Node 侧:

```bash
systemctl status sit-node
journalctl -u sit-node -n 200
systemctl is-enabled sit-node
timedatectl status
ls -ld /var/lib/sit
```

期望:

- `sit-node` enabled 且 active/running。
- `/var/lib/sit` owner 为 `sit:sit`,权限建议 `0700`。
- 系统时间已同步。
- `node.yaml` 中至少一个 endpoint 可连通。

Manager 侧:

```bash
curl -i https://<manager>/sit/api/v1/auth/me
curl -i https://<manager>/sit/connect
```

期望:

- 未带 token 的 `/auth/me` 返回 401 JSON。
- 未带 Node 凭证的 `/sit/connect` 返回 401,说明 WSS 路由打到 Manager。

## 7. 推荐执行顺序

1. P0-2 修复 task timeout/deadline,因为它已被实测证明有偏差。
2. P0-1 增加 WebSocket ping/pong 和半开连接主动断开,这是弱网自愈的核心。
3. P0-3 更新 `sit node setup` 生成的 systemd service,加 `StartLimitIntervalSec=0`。
4. P0-4 写 sudoers 白名单文档和示例,明确不要 root 跑 Node。
5. P1-1 增强 Node 连接日志,方便排查不稳定机器。
6. P1-2 梳理多 endpoint 部署方案。
7. P1-3/P1-4 做审计与公网入口安全收敛。
