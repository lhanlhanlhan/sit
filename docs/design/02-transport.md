# SIT 传输层设计(`internal/transport`)

> 状态:已确认设计(2026-06-24)。
> 负责 WebSocket 连接的建立、认证、保活、重连、端点故障转移。屏蔽底层细节,向业务层暴露"发消息/收消息"的纯通道。

## 1. 连接方向与暴露面(硬约束)

- 永远 **Node → Manager** 出站拨号;Node **不监听任何端口**。
- Manager 暴露:`:443` WSS(给 Node)、`:8443` REST(给前端/管理员)。两者独立监听。
- 传输层为 WSS:`wss://<endpoint>/sit/connect`。

## 2. 首次接入与认证(两阶段)

**阶段一 — Enrollment(首次)**:
1. 管理员在 Manager 调 `POST /api/v1/nodes/enroll` 取一次性 `enroll_token`,带外交付给新设备(配置文件/手动)。
2. Node 首次启动:本地无长期凭证 → 用 `enroll_token` 发起握手(HTTP header `Authorization: Bearer <enroll_token>`)。
3. Manager 校验 enroll_token(一次性、有过期)→ 为该 Node 生成并下发**长期凭证**(node 专属 secret/证书)+ 确认其随机 `node_id`。
4. Node 持久化长期凭证与 node_id 到本地。enroll_token 即作废。

**阶段二 — 常规连接**:
- 之后每次连接用**长期凭证**做握手认证。
- 认证发生在 **WSS 握手阶段**(Upgrade 请求头);认证出的身份绑定会话,**覆盖** register 自报的 node_id/addrs。
- 凭证被 `revoke` 后,握手即拒绝(401),已建会话被踢断。

> Node 身份认证 ≠ 管理员认证(后者见 03-rest-api §1),两套独立。

## 3. 拨号策略:多端点 + 双栈

- Node 本地配置 `endpoints: [主域名, 备用域名, 直连IP, ...]`(**不硬编码**,可带外更新 — ADR-004)。
- 按序尝试每个 endpoint;失败切下一个,全部失败则进入退避后重头再来。
- 单个 endpoint 解析可能同时得到 A/AAAA → 采用 **Happy Eyeballs(RFC 8305)** 并发尝试 v4/v6,取先连上者(ADR-007)。
- 支持读取 `HTTP_PROXY`/`HTTPS_PROXY` 环境变量穿透强制代理(ADR-005,可后置)。

## 4. 保活(心跳)

- Node 每 **30s** 发 WebSocket ping(应用层 ping/pong);避免 NAT 空闲老化(ADR-005)。
- Manager 侧:超过 **90s**(3 个心跳周期)未收到任何帧 → 判定该 Node `offline`,标记 last_seen,关闭会话。
- `last_seen` = Manager 最后一次收到该 Node 任意帧的时刻(毫秒)。

## 5. 重连(断线即常态)

- 任意断开(网络抖动、被误杀、Manager 重启)→ Node 自动重连。
- **指数退避 + 抖动**:base=1s,factor=2,cap=60s,叠加 ±20% 随机抖动,避免重连雪崩。
- 重连成功后:重新认证 → 重发 register → Manager 补投离线期间排队的 instruction。

## 6. 读写泵与并发模型

- 每条连接:一个 **read pump**(收帧 → 解码 → 分发到业务)+ 一个 **write pump**(从发送队列取帧 → 写出),经一个 channel 解耦,避免并发写同一连接。
- 业务层只与 channel 交互,不直接碰 websocket conn。
- 单帧 ≤1MB(协议约束),读侧设置 read limit 防 OOM。

## 7. 向业务层暴露的接口(抽象)

```go
type Conn interface {
    Send(ctx, Envelope) error      // 入队待发(write pump 负责真正写出)
    Recv() <-chan Envelope         // 收到的已解码消息
    Close(reason) error
    Info() SessionInfo             // node_id(认证后)、远端地址、连上时刻
}
```

Manager 侧管理 `map[node_id]Conn`;Node 侧只有一条到 Manager 的 Conn。
