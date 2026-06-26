# SIT Node 内部设计(`internal/node`)

> 状态:已确认设计(2026-06-24)。常驻 agent 业务层。

## 1. 组成

```
node
├── bootstrap     启动:加载/生成 node_id、加载凭证与 endpoints 配置
├── client        持有 transport.Conn,收发循环
├── executor      命令执行器(predefined 白名单 + shell)
├── reporter      生成 register/heartbeat/result/event 上报
└── identity      node_id 与长期凭证的本地持久化
```

## 2. 身份与配置

- **node_id**:首次启动随机生成 ULID,持久化本地;全局唯一、不可改(显示名在 Manager 侧)。
- **凭证**:首次用 enroll_token 换长期凭证(见 02-transport §2),持久化本地,文件权限 0600。
- **endpoints**:本地配置文件维护一组接入端点,**不硬编码**,可带外更新(ADR-004)。
- 配置文件路径(约定):
  - Linux:`/etc/sit/node.yaml` + 状态 `/var/lib/sit/`
  - macOS:`/usr/local/etc/sit/node.yaml` + 状态 `~/Library/Application Support/sit/`

## 3. 命令执行器 executor

- **predefined**:内置白名单 handler 映射(如 `restart_service`/`collect_logs`/`update_config`),按 `name` + `args` 调用。可控低危。
- **shell**:执行任意非交互命令(`sh -c <command>` / 平台对应),捕获 stdout/stderr/exit_code。
- **超时**:按 instruction.timeout_sec,用 context + 进程组 kill 强制终止超时进程(护嵌入式内存)。
- **截断**:stdout/stderr 超 64KB 截断,置 truncated。
- **deadline**:执行前检查 deadline(毫秒),已过则不执行,回 `expired` 结果。
- **非交互约束**:不支持需 stdin 交互的命令(对应用户预期:布置任务即走,结果后取)。
- **去重**:维护近期已执行 instruction id 窗口,重复直接回放已知结果或丢弃,保证幂等。

## 4. 收发循环 client

1. 连接建立(transport)→ 发 register。
2. 启动心跳(30s ping)。
3. read:收到 instruction → 立即回 ack → 交 executor 异步执行 → 完成后 reporter 发 result。
4. 断线 → transport 自动重连 → 重新 register → 接收补投的离线指令。

## 5. 上报 reporter

- `register`:启动/重连时发,带 os/arch/version/hostname/addrs(枚举本机所有网卡的 v4/v6 地址,带 iface/scope)。
- `heartbeat`:30s 周期,带 uptime/load/mem。
- `result`:每条指令执行完发,ref_id=instruction.id。
- `event`:主动事件,如启动 `online`、收到关机信号 `shutting_down`、被监控进程 `process_died`。

## 6. 安全

- 仅出站,不监听端口。
- 校验 Manager TLS 证书(防 MITM)。
- 凭证文件 0600;日志不打印凭证与完整命令敏感参数。
