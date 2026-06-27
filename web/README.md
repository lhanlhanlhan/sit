# SIT Manager GUI(独立交付)

一套**纯静态、零构建**的管理前端,用原生 HTML + JS + 极简 CSS 调用 SIT Manager 的 REST API。
与后端**完全解耦、独立交付**:不由 Manager 托管,部署方式由你自行决定(反代 / 任意静态服务器)。

## 文件

| 文件 | 说明 |
|---|---|
| `login.html` | 登录:填 API 地址 + 用户名 + 密码,换取 admin token |
| `nodes.html` | 节点列表:状态/Last Seen/OS/Arch/版本,过滤 + 自动刷新(轮询),行操作:任务/重命名/MCP 开关/吊销 |
| `node.html?id=<node_id>` | 节点详情:基础信息 + 心跳指标 + 网络地址 + 活动时间线 + MCP 配置复制 |
| `tasks.html?node=<node_id>` | 任务下发(shell / predefined)+ 结果轮询 + 任务历史 |
| `enroll.html` | 生成一次性 enrollment token,给新节点接入用 |
| `app.js` | 共享:配置/会话(localStorage)、fetch 封装(自动 bearer、401 跳登录)、格式化辅助 |
| `style.css` | 极简样式,无框架 |

> 实时刷新采用**定时轮询**(节点列表/详情每 5 秒,任务结果每 1 秒);不使用 WSS(`/sit/connect` 是 Node 专用通道,不面向管理端)。

## MCP 配置

当节点的 `mcp_enabled` 已开启时,节点列表会出现「MCP配置」入口;节点详情页会显示可复制的 Codex TOML 与通用 MCP JSON 配置。

Codex 推荐配置:

```toml
[mcp_servers.sit-node-name]
url = "https://go.meating.cc/sit/mcp"
http_headers = { X-SIT-Node = "<node_id>" }
env_http_headers = { Authorization = "SIT_MCP_AUTHORIZATION" }
```

启动 Codex 前设置:

```bash
export SIT_MCP_AUTHORIZATION='Bearer <MCP_TOKEN>'
```

macOS 桌面版 Codex 可用:

```bash
launchctl setenv SIT_MCP_AUTHORIZATION 'Bearer <MCP_TOKEN>'
```

通用 JSON 配置:

```json
{
  "mcpServers": {
    "sit-node-name": {
      "type": "http",
      "url": "https://go.meating.cc/sit/mcp",
      "headers": {
        "Authorization": "Bearer <MCP_TOKEN>",
        "X-SIT-Node": "<node_id>"
      }
    }
  }
}
```

`<MCP_TOKEN>` 为 Manager 配置中的 `mcp_token`。SIT 使用 `X-SIT-Node` header 指定目标 Node;如果某个 MCP 客户端不支持自定义 header,可改用 `?node=<node_id>` 查询参数寻址。token 不应放入 query。

## 运行

推荐用本地静态服务器运行,不要直接用 `file://` 打开:

```bash
scripts/serve-web.sh
```

默认访问:

```text
http://localhost:45101/login.html
```

如需使用 Manager 服务端已放行的指定端口:

```bash
SIT_WEB_PORT=xxxx scripts/serve-web.sh
```

登录页填写的 **API 地址是 Manager 的 REST 监听根地址**(如 `https://mgr.example:8443` 或 `https://go.meating.cc/sit`),**不含** `/api/v1`(代码会自动拼接)。

## 跨域(CORS)—— 必读

前端与 Manager API **不同源**时,浏览器会执行 CORS 校验。可选两种部署方式:

1. 本地 GUI:用 `scripts/serve-web.sh` 固定在 `http://localhost:<port>` 运行,并在 Manager API 前面的 Nginx 放行这个 origin。
2. 线上 GUI:用反向代理把前端与 API 收敛到同一来源,从根本上规避 CORS。

本地 GUI 访问公网 Manager API 时,推荐在 Nginx 层处理 CORS,不用改 Manager 二进制。示例:

```nginx
set $cors_origin "";
if ($http_origin = "http://localhost:45101") {
    set $cors_origin $http_origin;
}

location /sit/api/ {
    if ($request_method = OPTIONS) {
        add_header Access-Control-Allow-Origin $cors_origin always;
        add_header Access-Control-Allow-Methods "GET, POST, PATCH, DELETE, OPTIONS" always;
        add_header Access-Control-Allow-Headers "Authorization, Content-Type" always;
        add_header Access-Control-Max-Age 600 always;
        add_header Vary Origin always;
        return 204;
    }

    add_header Access-Control-Allow-Origin $cors_origin always;
    add_header Vary Origin always;
    proxy_pass http://127.0.0.1:8443/api/;
}
```

Nginx 示例(前端与 API 同源,API 走 `/api/` 与 `/mcp` 转发到 Manager):

```nginx
server {
    listen 443 ssl;
    server_name sit-ui.example.com;
    # ... ssl_certificate / ssl_certificate_key ...

    # 静态前端
    root /var/www/sit-web;       # 这里放 web/ 的内容
    index login.html;

    # REST API 反代到 Manager(同源,无跨域)
    location /api/ {
        proxy_pass https://127.0.0.1:8443;
        proxy_set_header Host $host;
    }
    # 如需在同源下访问 MCP
    location /mcp {
        proxy_pass https://127.0.0.1:8443;
    }
}
```

此时登录页 API 地址直接填 `https://sit-ui.example.com`(与前端同源)。

## 安全提示

- admin token 存于浏览器 `localStorage`,仅在可信设备 / 浏览器使用;用完点「退出」清除。
- 强烈建议整条链路走 HTTPS(前端与 Manager 均如此),避免 token 明文传输。
- 本前端不持久化任何凭证到磁盘文件,只在浏览器本地存储 API 地址、token、用户名。
