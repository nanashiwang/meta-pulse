# AGENTS.md — Meta Pulse Engineering Rules

本仓库是 **Meta Pulse / 元衡脉冲计划**。

任何 AI Coding Agent、Codex、Claude Code 或开发者在修改代码前，必须先阅读：

1. `README.md`
2. `docs/ARCHITECTURE.md`
3. `docs/COMMUNITY.md`（涉及论坛/博客时）
4. `docs/IMPLEMENTATION_PLAN.md`（里程碑顺序与出口标准）
5. 本文件

`docs/ARCHITECTURE.md` 是系统架构基线。实现可以拆分，但不得擅自改变其中的系统边界、事实源和工程红线。

## 1. 项目本质

Meta Pulse 是建立在元衡 API 真实付费调用之上的 **Margin-aware Loyalty Engine**。

它不是模型代理，不是主计费系统，不是支付系统，也不是一个独立博彩/抽奖平台。

```text
new-api = 模型调用与资金事实源
Meta Pulse = 调用之后的增长与权益系统
```

## 2. 最高优先级不变量

以下规则不得破坏：

1. Pulse 故障不得影响 new-api 模型调用、计费、充值和余额。
2. Pulse 禁止进入 new-api relay 请求主链路作为同步依赖。
3. 同一 Usage Event 重放任意次数，只能产生一次业务记账。
4. Ledger 是事实源，Account 只是派生快照。
5. Ledger 历史金额禁止 UPDATE；修正只能新增 reversal / adjustment。
6. 同一 Pulse Action 永远只能生成一个 Reward Grant 和一个随机结果。
7. 一张 Ticket 最多真正消费一次。
8. 一个 Reward Grant 在 new-api 最多真正到账一次。
9. Benefit 状态不确定时先 Query/Reconcile，不允许更换 source_ref 重新发。
10. Reward 不得突破 Hard Budget。
11. Period Active 后核心经济规则、阈值、概率、Budget、Holdout 不允许原地修改。
12. Period Close 必须可重入、可重复执行。
13. 浏览器不能自行声明可信 user_id。
14. 金额、贡献值、Ledger Balance 禁止用 float 做账。
15. Pulse 不持有 new-api 用户余额表写权限。
16. 所有人工财务型调整必须可审计。
17. 论坛内容不得产生 contribution 或 ticket。
18. 论坛不得拥有独立注册通道；身份一律来自 new-api。
19. Pulse 故障不得阻断论坛登录或浏览。
20. 论坛登录回调是浏览器重定向，所有参数在验签通过前一律视为攻击者可控。

## 3. 技术栈

目标技术栈：

- Go 1.22+
- Gin
- GORM
- MySQL 8.0+
- Redis 7+

前端 UI 在 new-api 现有 React 18 / Vite / Semi Design 控制台中薄接入。

社区层：Apache Answer（论坛，不 Fork，仅维护插件）+ VitePress（博客）。

仓库为 monorepo：

```text
services/pulse/                Pulse 主线
services/forum/                Answer 构建定义
services/forum-plugin/         UserCenter 插件
sites/blog/                    VitePress 博客
```

Go 工作区通过根目录 `go.work` 管理。仓库根不是 Go module，`./...` 无法跨模块解析，构建命令须显式列出模块路径。

## 4. 分层规则

推荐依赖方向：

```text
transport / jobs / tools
        ↓
service
        ↓
domain
        ↓
interfaces / stores / adapters
```

约束：

- `domain` 不依赖 Gin、GORM、Redis 或 new-api SDK。
- `service` 编排事务与领域规则，不直接依赖 HTTP request/response。
- `adapter/newapi` 只负责 new-api 数据映射、Benefit Client 和服务认证。
- `store/mysql` 负责持久化，不复制业务判断。
- HTTP handler 不实现 Economics、Ticket、Reward 或 Budget 规则。

## 5. 数据与事务

### Usage Ingest

同一个 Pulse DB 事务内完成：

```text
UsageEvent
→ Contribution Ledger
→ Contribution Account
→ Ticket Entitlement
→ Ticket Ledger
→ Ticket Account
→ User Period Stat
```

不得出现半记账状态。

### Pulse Action

同一个 Pulse DB 事务内完成：

```text
Idempotency
→ Ticket Spend
→ Budget Reservation
→ Reward Grant
→ Settlement Outbox
```

### Pulse → new-api

禁止分布式事务。

使用：

```text
Transactional Outbox
+ Idempotent Benefit Receiver
+ Reconciliation
```

## 6. 幂等规则

- Usage：`source_system + source_event_id`
- API mutation：`scope + Idempotency-Key`
- Reward：`period_id + user_id + action_id`
- Benefit：`source_ref = reward_grant_id`
- Period Reward：稳定 action id，例如 `period_reward:{period_id}:{user_id}`

统一语义：

```text
同 key + 同 payload → 返回第一次结果
同 key + 不同 payload → conflict，不静默覆盖
```

## 7. 数值规则

禁止用浮点做业务账。

建议：

```text
1 contribution = 1000 contribution_milli
1.00x multiplier = 10000 bps
```

金额、贡献值、余额、Budget、Ticket 都使用整数/fixed-point。

## 8. 随机规则

Reward 随机必须可重放。

使用版本化 HMAC 派生随机值，业务输入至少包含：

```text
period_id + user_id + action_id + config_version
```

网络失败不得重新随机。

Secret 不进入 GitHub。

## 9. new-api 数据边界

Pulse 从 new-api 只读取业务必要字段。

默认禁止复制：

- Password
- API Key / Token Key
- Cookie
- 完整 Prompt / Response
- IP
- 支付信息
- 无关的 raw `Other`

Pulse 对 LOG_DB 使用只读账号。

Reward 最终额度只能通过 new-api Internal Benefit API 发放，不能直接操作 `users.quota`。

## 10. Redis 边界

Redis 只用于：

- Cache
- Nonce
- Rate Limit
- Short Lock
- Worker Lease

Redis 不能成为：

- Contribution 事实源
- Ticket 事实源
- Reward 事实源
- Budget 最终事实源
- Settlement 最终事实源

Redis 全丢也不能丢账。

## 11. 测试最低要求

涉及核心逻辑的 PR 必须覆盖相应测试。

必须长期保留的回归场景：

- 同一 Usage 重放 100 次仍只记一次账；
- 同一 Pulse Action 重放 100 次仍只产生一个 Reward；
- 同一 Benefit 请求重放 100 次只增加一次额度；
- 同一 Period Close 重跑不会重复发放；
- 并发 Pulse 不超扣 Ticket、不超 Budget；
- DB commit 后 HTTP response 丢失能安全恢复；
- new-api Benefit 已成功但 Pulse timeout 能通过 Query/Reconciliation 恢复；
- Ledger 与 Account 可重建、可对账。

## 12. 文档同步

如果代码修改改变以下任一项，必须同步更新 `docs/ARCHITECTURE.md`：

- 系统边界；
- 数据事实源；
- 数据库核心模型；
- 幂等语义；
- Settlement 语义；
- Reward / Budget / Period 状态机；
- 身份认证边界；
- 故障恢复模型。

不要让实现和架构文档长期漂移。