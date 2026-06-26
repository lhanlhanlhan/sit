# SIT 测试与质量设计

> 状态:已确认设计(2026-06-24)。
> 注:运行测试包与解析测试结果时,需参考**测试框架 v1.0 的 SKILL.md** 文档(CONV-2)。

## 1. 分层测试策略

| 层 | 范围 | 重点 |
|---|---|---|
| 单元测试 | protocol 编解码、executor、dispatcher 状态机、退避算法 | 纯逻辑,无网络 |
| 集成测试 | transport 收发、握手认证、重连、离线队列投递 | 起内存/本地 WSS server |
| 端到端 | manager + node 双进程,跑完整下发→执行→回报 | 真实流程 |

## 2. 关键测试用例(对应硬约束与风险点)

**协议(01)**
- 毫秒时间戳序列化/反序列化一致。
- 双栈多 IP register 往返。
- 超大帧(>1MB)被拒。
- 版本不匹配处理。

**传输(02)**
- enroll_token 一次性:第二次使用被拒。
- 长期凭证 revoke 后握手被拒、已连会话被踢。
- 心跳超时(>90s)判 offline。
- 断线后指数退避重连 + 抖动范围正确。
- 多端点故障转移:首端点不可达切下一个。
- Happy Eyeballs:v6 不通时 v4 仍能连上。

**调度/任务(03/04)**
- 离线下发 → queued;Node 上线 → 自动投递 → sent。
- result 回传配对 task,exit_code 决定 succeeded/failed。
- deadline 过期 → expired,Node 不执行。
- 执行超时 → timeout,进程被 kill。
- 重复 result 去重幂等。

**执行器(05)**
- shell 命令 stdout/stderr/exit_code 捕获。
- 超 64KB 输出截断 + truncated 标记。
- 超时进程被强制终止。
- predefined 白名单命令调度。

**安全**
- node_id 自报不被信任(身份以握手为准)。
- 凭证/token/password 只存哈希。
- 仅出站,Node 不监听端口(集成测试断言无 listener)。

## 3. CI 约定

- `go test ./...` 全绿为合并门槛。
- `go vet` + 静态检查。
- 交叉编译矩阵构建(linux/darwin × amd64/arm64)确保多平台可出包。
