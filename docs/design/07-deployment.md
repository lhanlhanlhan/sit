# SIT 部署与自启设计

> 状态:已确认设计(2026-06-24)。

## 1. 交付物

- 单二进制 `sit`,交叉编译多平台:
  - linux/amd64, linux/arm64(含嵌入式), darwin/amd64, darwin/arm64。
- 子命令:
  - `sit manager --config <path>` 启动服务端。
  - `sit node --config <path>` 启动 agent。
  - 辅助:`sit version`、`sit node enroll --token <t> --endpoint <url>`(首次接入引导)。

## 2. Manager 部署(公网)

- 监听 `:443`(WSS)与 `:8443`(REST)。
- 需有效 TLS 证书(建议 CDN/反代前置,ADR-004);真实地址可隐于 CDN 后。
- 数据目录:SQLite 文件 + 凭证。建议以 systemd 常驻。
- 最小化对外端口,按要塞对待。

## 3. Node 自启(开机自启,核心需求)

### Linux — systemd
```
/etc/systemd/system/sit-node.service
[Unit]
Description=SIT Node
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/sit node --config /etc/sit/node.yaml
Restart=always
RestartSec=5
User=sit

[Install]
WantedBy=multi-user.target
```
`systemctl enable --now sit-node` 即开机自启 + 崩溃自拉起。

### macOS — launchd
`~/Library/LaunchAgents/com.sit.node.plist`(或 `/Library/LaunchDaemons` 系统级),`RunAtLoad=true`、`KeepAlive=true`,`launchctl load` 加载。

### 嵌入式
- 有 systemd 的同 Linux;无 systemd 的用 init/rc 脚本或 supervisor,要点一致:开机自启 + 异常自拉起。

> 进程级自启(systemd/launchd Restart)与应用级重连(transport 退避重连)是两层兜底:前者管"进程死了拉起来",后者管"连接断了重连上"。

## 4. 配置文件(Node)

> 实现采用**极简 YAML 子集**解析(无第三方 YAML 依赖,规避 Go 1.25 传递依赖);支持 `endpoints` 列表 + 扁平 `key: value`。仓库样例见 `deploy/config/node.yaml`。

`node.yaml`(示意):
```yaml
endpoints:           # 多端点,按序 + 故障转移;可带外更新(ADR-004)
  - wss://sit.example.com/sit/connect
  - wss://sit-backup.example.com/sit/connect
state_dir: /var/lib/sit
heartbeat_sec: 30
insecure_skip_verify: false   # 仅测试用,生产必须 false(Node 校验 Manager 证书)
# 凭证与 node_id 由程序在状态目录维护(0600),不写在此文件
```

## 5. 配置文件(Manager)

> 同样为扁平 `key: value` 子集(`tls`/`store` 不嵌套)。仓库样例见 `deploy/config/manager.yaml`。

```yaml
listen_wss: ":443"
listen_api: ":8443"
store_path: /var/lib/sit/manager.db
tls_cert_file: /etc/sit/tls/fullchain.pem
tls_key_file:  /etc/sit/tls/privkey.pem
admin_user: admin                 # 首次启动 seed(密码 bcrypt 存储,后续不再生效)
admin_password: change-me
mcp_token: ""                     # MCP 端点 bearer;空则关闭 MCP 鉴权(不建议)
```

## 6. 进程装配与交叉编译

- **装配层 `internal/managerd`**:为打破 `internal/manager` ↔ `api`/`mcp` 的导入环,Server 装配(store/auth/registry/dispatcher/reports + WSS/REST/MCP 监听器)上移到独立的 `managerd` 包;`manager` 仍持有 `Config`/`LoadConfig`。`cmd/sit/manager.go` 调用 `managerd.NewServer`。
- **交叉编译**:`scripts/build-release.sh [version]` 以 `CGO_ENABLED=0`(纯 Go,modernc.org/sqlite)+ `GOTOOLCHAIN=go1.23.0` 产出 linux/darwin × amd64/arm64 静态二进制到 `dist/`。
- **自启样例**:`deploy/systemd/sit-node.service`、`deploy/launchd/com.sit.node.plist`。

