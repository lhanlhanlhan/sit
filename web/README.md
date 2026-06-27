# SIT Manager GUI(独立交付)

一套**纯静态、零构建**的管理前端,用原生 HTML + JS + 极简 CSS 调用 SIT Manager 的 REST API。
与后端**完全解耦、独立交付**:不由 Manager 托管,部署方式由你自行决定(反代 / 任意静态服务器)。

## 文件

| 文件 | 说明 |
|---|---|
| `login.html` | 登录:填 API 地址 + 用户名 + 密码,换取 admin token |
| `nodes.html` | 节点列表:状态/Last Seen/OS/Arch/版本,过滤 + 自动刷新(轮询),行操作:任务/重命名/MCP 开关/吊销 |
| `node.html?id=<node_id>` | 节点详情:基础信息 + 心跳指标 + 网络地址 + 活动时间线 |
| `tasks.html?node=<node_id>` | 任务下发(shell / predefined)+ 结果轮询 + 任务历史 |
| `enroll.html` | 生成一次性 enrollment token,给新节点接入用 |
| `app.js` | 共享:配置/会话(localStorage)、fetch 封装(自动 bearer、401 跳登录)、格式化辅助 |
| `style.css` | 极简样式,无框架 |

> 实时刷新采用**定时轮询**(节点列表/详情每 5 秒,任务结果每 1 秒);不使用 WSS(`/sit/connect` 是 Node 专用通道,不面向管理端)。

## 运行

把 `web/` 目录交给任意静态托管即可,例如本地直接用浏览器打开 `login.html`,或放到 Nginx 静态根目录。
登录页填写的 **API 地址是 Manager 的 REST 监听根地址**(如 `https://mgr.example:8443`),**不含** `/api/v1`(代码会自动拼接)。

## 跨域(CORS)—— 必读

前端与 Manager API **不同源**时,浏览器会拦截跨域请求。本前端定位为「独立交付,跨域由部署方用反代解决」,Manager **未内置 CORS 头**。推荐用反向代理把前端与 API 收敛到**同一来源**,从根本上规避 CORS。

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
