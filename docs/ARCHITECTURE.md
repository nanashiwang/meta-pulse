# 元衡脉冲计划 Meta Pulse｜完整项目架构

## 1. 项目说明

### 一句话定义

**元衡脉冲计划（Meta Pulse）是一套建立在元衡 API 真实付费调用之上的 C 端用户增长与回馈系统。**

它把原本单向的：

```text
充值 → 调用模型 → 扣费 → 再充值
```

变成：

```text
正常使用元衡 API
        ↓
产生真实付费消耗
        ↓
获得贡献值
        ↓
积累脉冲券
        ↓
开启一次脉冲
        ↓
获得即时回馈
        ↓
参与 10 天周期奖励
        ↓
形成持续使用动力
```

用户看到的是“**调用 → 积累 → 开启 → 获得回馈**”；后台运行的是一套严格的 **Economics + Ledger + Idempotency + Budget + Reward + Settlement + Experiment** 系统。

## 2. 为什么做

元衡 C 端 API 业务天然容易落入价格、模型数量、通道稳定性的横向比较。Meta Pulse 不是为了把元衡做成游戏平台，而是解决三个问题：

1. **提高留存**：让过去的真实调用形成持续积累；
2. **提高有效调用**：权益只来自真实付费使用，不鼓励纯签到和无成本薅羊毛；
3. **建立差异化**：从纯 Token/API 交易平台，逐步形成自己的用户关系、经济规则和数据资产。

Pulse 成功后应同时产生：

- 用户价值：真实使用形成持续积累与回馈；
- 增长价值：提升券激活、次期参与、10 日留存、真实付费调用、沉默用户唤醒；
- 经济价值：活动真实成本 / 贡献毛利正常目标 8%–12%，硬上限 15%；
- 长期能力：沉淀模型成本、贡献倍率、Provider、用户行为与实验数据，反哺 Enterprise FinOps、AI Analytics、Provider Health、模型情报和智能路由。

战略边界：

> **C 端养现金流，B 端建壁垒。**

## 3. 用户体验

用户进入元衡控制台后看到：

```text
元衡脉冲 · 第 N 期
剩余时间

本期贡献值
脉冲券
已获得回馈

[ 开启一次脉冲 ]
      1 券

周期奖励池
最近贡献
最近用券
最近奖励
本期规则
```

用户不需要理解 Provider 成本、Ledger、幂等、Outbox 或 Budget，只需理解：

> 正常使用元衡 → 获得贡献值 → 获得脉冲券 → 开启脉冲 → 获得回馈。

## 4. 产品本质

Meta Pulse 不是签到积分，也不是充值后购买抽奖机会。

价值链：

```text
真实付费调用
        ↓
平台产生真实贡献毛利
        ↓
按模型 / 渠道经济性计算贡献值
        ↓
贡献值形成脉冲券
        ↓
脉冲券产生用户回馈
        ↓
奖励成本从可承受毛利预算释放
```

因此 Meta Pulse 首先是一套 **Margin-aware Loyalty Engine**，其次才表现为用户可感知的增长玩法。

## 5. 与 new-api 的关系

`new-api` 是模型调用与资金事实源，继续负责：

- 用户、登录、API Key / Token；
- 模型请求与 Provider / Channel；
- 计费、预扣、结算；
- 充值、余额、订阅；
- 消费与退款日志。

Meta Pulse 是调用完成后的增长与权益系统，负责：

- Usage Event；
- Economics Rule；
- Contribution Ledger；
- Ticket Ledger；
- 10 天 Period；
- Reward / Budget；
- Settlement 状态；
- Holdout / Experiment；
- 增长分析与对账。

核心关系：

```text
new-api = 模型调用与资金事实源
Meta Pulse = 调用之后的增长与权益系统
```

硬原则：

> **Pulse 可以停，new-api 不能停。Pulse 故障不得影响模型请求、计费、充值和余额。**

## 6. 总体系统架构

```text
                         用户
                          │
                          ▼
                 ┌─────────────────┐
                 │    new-api Web  │
                 │ /console/pulse  │
                 └────────┬────────┘
                          │ Signed BFF
                          ▼
                 ┌─────────────────┐
                 │ meta-pulse-api  │
                 └────────┬────────┘
                          │
                          ▼
                 ┌─────────────────┐
                 │   Pulse MySQL   │
                 │ Usage Event     │
                 │ Ledger          │
                 │ Account         │
                 │ Reward          │
                 │ Budget          │
                 │ Period          │
                 │ Experiment      │
                 │ Outbox          │
                 └────────┬────────┘
                          ▲
                          │
                 ┌────────┴────────┐
                 │meta-pulse-worker│
                 └───┬─────────┬───┘
                     │         │
             Read Only│         │ Benefit
                     ▼         ▼
              new-api LOG_DB   new-api Internal Benefit API
                                  │
                                  ▼
                            用户最终额度
```

不做分布式事务；Pulse → new-api 使用 **Transactional Outbox + 幂等 Benefit Receiver + Reconciliation** 实现最终一致。

## 7. 技术栈

Backend：

- Go 1.22+
- Gin
- GORM
- MySQL 8.0+
- Redis 7+

Frontend：复用 new-api 的 React 18 + Vite + Semi Design。

Runtime：Docker / Docker Compose / Nginx。

## 8. 代码结构

```text
meta-pulse/
├── cmd/
│   ├── api/main.go
│   ├── worker/main.go
│   └── tool/main.go
├── internal/
│   ├── app/
│   ├── config/
│   ├── domain/
│   ├── service/
│   ├── adapter/newapi/
│   ├── store/mysql/
│   ├── transport/http/
│   ├── job/
│   ├── security/
│   └── observability/
├── migrations/
├── tests/
├── tools/
│   ├── backfill/
│   ├── backtest/
│   ├── reconcile/
│   └── ledger-check/
└── docs/
```

### `meta-pulse-api`

负责用户 Summary、Ledger 查询、Reward 查询、开启脉冲、当前 Period、Admin API、Health API。

### `meta-pulse-worker`

负责 Usage Ingest、Settlement、Benefit Reconciliation、Period Close、Period Reward、Metrics Aggregation、Ledger Reconciliation。

### `meta-pulse-tool`

负责 Backfill、Backtest、Reconcile、Ledger Check、Period Close、Reward Retry。

## 9. Period

固定 10 天周期：

```text
draft → active → settling → closed
```

进入 `active` 后冻结：

- Economics Rules；
- 模型贡献倍率；
- Ticket Threshold；
- Reward 权重 / 概率；
- Budget；
- Holdout；
- Random Version。

禁止原地改写历史周期经济规则。

## 10. Usage Event

Pulse 不直接把 new-api 日志当成贡献值，而是先标准化：

```text
consume
refund
correction
```

消费产生正向价值；退款/纠错产生负向冲正。

主幂等身份：

```text
source_system = new-api-log
source_event_id = new-api log.id
```

唯一约束：

```text
UNIQUE(source_system, source_event_id)
```

同一事件重放 N 次只处理一次；同 ID 但 payload hash 不同进入 `pulse_ingest_conflict`，不得静默覆盖。

Backfill 与实时 Ingest 必须共用同一 Mapper 和同一业务逻辑。

## 11. Economics Engine

核心公式：

```text
Contribution = Eligible Paid Usage × Contribution Multiplier
```

规则可以按 Model、Channel、Model+Channel、Pattern 匹配。

倍率使用 basis points：

```text
10000 = 1.00x
12000 = 1.20x
```

贡献值使用 fixed-point：

```text
1 contribution = 1000 contribution_milli
```

禁止使用 float 做账。

每个 Usage Event 必须保存当时命中的规则、倍率、eligibility、contribution 快照，后续规则变更不得重算旧事件。

## 12. Ledger

核心事实源：`pulse_ledger_entry`。

资产：

```text
contribution
ticket
```

Contribution 操作：

```text
contribution_earn
contribution_reverse
contribution_adjustment
```

Ticket 操作：

```text
ticket_mint
ticket_spend
ticket_reverse
ticket_expire
ticket_adjustment
```

规则：

- append-only；
- 历史金额禁止 UPDATE；
- 修正必须追加 reversal / adjustment；
- 每条业务 mutation 具有稳定 idempotency key；
- `pulse_account` 只是 Ledger 快照。

必须满足：

```text
SUM(ledger.amount) = account.balance
```

## 13. Ticket Entitlement 与退款

产券按用户周期累计净贡献计算：

```text
entitled_tickets = floor(net_contribution / threshold)
```

不按单次 API 请求独立取整，避免大量小额调用造成 rounding 偏差。

退款导致 entitlement 下降时写 `ticket_reverse`。

若券已消费，允许形成内部 Ticket Debt；未来新券先抵债，用户可用券显示 `max(balance, 0)`，避免“调用 → 拿券 → 领奖 → 退款”套利。

## 14. Reward Engine

用户接口：

```text
POST /v1/me/pulses
Idempotency-Key: <required>
```

单事务：

```text
BEGIN
 ↓
校验 Idempotency
 ↓
检查 Period / Experiment Cohort
 ↓
锁 Ticket Account
 ↓
校验可用 Ticket
 ↓
锁 Reward Budget
 ↓
选择 Reward
 ↓
预占 Budget
 ↓
写 ticket_spend
 ↓
创建 Reward Grant
 ↓
创建 Settlement Outbox
 ↓
保存 Idempotency Response
 ↓
COMMIT
```

同一个 Action 无论网络重试多少次，只能对应一个 Reward Grant。

## 15. 确定性随机

随机结果不能因重试改变。

使用版本化 HMAC：

```text
random_value = HMAC-SHA256(
  period_secret,
  period_id + user_id + action_id + config_version
)
```

因此：

```text
同一个 Action → 永远相同 random_value → 永远相同 Reward Grant
```

`period_secret` 不写入 GitHub；Period 激活时可保存 `SHA256(period_secret)` 作为 commit hash 供审计。

## 16. Reward Definition

核心表：`pulse_reward_definition`。

首个 Reward 类型：

```text
newapi_quota
```

未来可扩展：

```text
entitlement
model_trial
fixed_benefit
```

Reward 权重与概率配置进入 active 后冻结。

## 17. Reward Budget

核心表：`pulse_reward_budget`。

必须支持：

- Period Total Budget；
- Instant Budget；
- Period Reward Budget；
- Reward Type Budget；
- User Budget；
- Hard Margin Budget。

Reward 创建前在同一事务预占 Budget，不允许事后发现超预算。

任何 Reward 都不得突破 hard cap。

## 18. Reward Grant

核心表：`pulse_reward_grant`。

状态：

```text
pending → settling → settled
                    ↘ failed
settled → reversed
```

Grant 一旦首次产生，其 reward type、amount、random value 不得重新随机。

## 19. Settlement Outbox

Pulse DB 和 new-api DB 不做分布式事务。

Pulse DB 同一事务写入：

```text
ticket_spend
+
reward_grant
+
settlement_outbox
```

Worker 再异步调用 new-api Benefit。

## 20. new-api Internal Benefit API

新增内部接口：

```text
POST /api/internal/pulse/benefits/quota
GET  /api/internal/pulse/benefits/quota/:source_ref
POST /api/internal/pulse/benefits/quota/:source_ref/rollback
```

Grant 示例：

```json
{
  "user_id": 123,
  "source_ref": "reward_grant_01J...",
  "quota_delta": 50000,
  "reason": "pulse_instant_reward"
}
```

固定语义：

```text
source_type = pulse_reward
source_ref  = reward_grant_id
action      = grant
target_type = user_quota
target_id   = user_id
```

使用 new-api 已有 BenefitChangeRecord 去重语义保证同一 Reward 最多到账一次。

发生 timeout 时必须先 `GET Benefit(source_ref)`：存在则标记 settled；不存在才使用原 source_ref 重试。禁止生成新 source_ref 绕过幂等。

## 21. Period Reward

支持：

```text
trigger_type = instant
trigger_type = period_close
```

周期奖励公式参数化，可支持平均分、按 Contribution 权重、按 Ticket Spend 权重、固定档位。

必须满足：

```text
SUM(period rewards) <= period reward budget
```

周期奖励 action id 稳定，例如：

```text
period_reward:{period_id}:{user_id}
```

重复运行不会重复发放。

## 22. Period Close

关闭状态机：

```text
active
 ↓
settling
 ↓
停止新 Pulse
 ↓
确认 Usage Watermark
 ↓
Ledger / Account 对账
 ↓
计算 Period Reward
 ↓
创建 Reward Grants / Outbox
 ↓
Settlement
 ↓
Ticket Expiration
 ↓
Metrics 固化
 ↓
closed
```

必须是可重入 State Machine；中途崩溃后继续，而不是重发。

## 23. Holdout Experiment

确定性分组：

```text
bucket = SHA256(experiment_id + user_id) % 10000
```

按 `holdout_bps` 决定 Treatment / Control。

同一用户在同一个 experiment 中必须稳定属于同一组。

核心指标：

- Ticket 激活率；
- Pulse 使用率；
- 次期参与率；
- 10 日留存；
- 有效付费调用；
- 沉默用户唤醒；
- Reward 成本；
- 活动真实成本 / 贡献毛利。

重点比较 Treatment vs Control。

## 24. 用户身份与 API

浏览器不得自行声明可信 `user_id`。

链路：

```text
Browser
↓
new-api Session
↓
new-api Signed BFF
↓
Meta Pulse
```

建议签名头：

```text
X-Pulse-User-Id
X-Pulse-Role
X-Pulse-Timestamp
X-Pulse-Nonce
X-Pulse-Signature
```

用户 API：

```text
GET  /v1/period/current
GET  /v1/me/summary
GET  /v1/me/ledger
GET  /v1/me/rewards
GET  /v1/me/rewards/:grant_id
POST /v1/me/pulses
GET  /healthz
GET  /readyz
```

## 25. Admin / Operator

内部管理能力：

```text
Period
Economics Rule
Reward Definition
Budget
Settlement
Conflict
Reconciliation
Experiment
Metrics
Audit
```

active Period 核心经济配置只读。所有管理写操作必须带 reason，并写 `pulse_audit_log`。

## 26. 核心数据库表

```text
pulse_period
pulse_economics_rule
pulse_usage_event
pulse_ingest_conflict
pulse_ledger_entry
pulse_account
pulse_reward_definition
pulse_reward_budget
pulse_reward_grant
pulse_settlement_outbox
pulse_idempotency
pulse_worker_cursor
pulse_experiment_assignment
pulse_user_period_stat
pulse_metric_daily
pulse_audit_log
```

重要唯一约束至少包括：

```text
pulse_usage_event: UNIQUE(source_system, source_event_id)
pulse_ledger_entry: UNIQUE(operation, idempotency_key)
pulse_account: UNIQUE(user_id, period_id, asset_type)
pulse_reward_grant: UNIQUE(period_id, user_id, action_id)
pulse_settlement_outbox: UNIQUE(reward_grant_id)
pulse_idempotency: UNIQUE(scope, idempotency_key)
pulse_experiment_assignment: UNIQUE(experiment_id, user_id)
```

## 27. Redis 边界

Redis 可以用于：Cache、Nonce、Rate Limit、短锁、Worker Lease。

Redis 不得成为 Contribution、Ticket、Reward、Budget、Settlement 的最终事实源。

> Redis 全丢，账不能丢。

## 28. 安全边界

Pulse 不保存：

- Password；
- API Key；
- Cookie；
- Token Key；
- 完整 Prompt / Response；
- IP；
- 支付信息。

只使用 new-api `user_id` 作为 opaque principal。

Pulse 对 new-api LOG_DB 使用只读账号，并且无权限直接写 new-api 用户余额表。

## 29. 对账与可观测性

必须能自动检查：

```text
SUM(Ledger) = Account
Ticket Spend = Pulse Action = Reward Grant
Reward settled => new-api Benefit exists
Budget reserved + settled <= cap
accepted UsageEvent => exactly one accounting effect
```

核心指标：

```text
pulse_ingest_lag_seconds
pulse_ingest_duplicate_total
pulse_ingest_conflict_total
pulse_account_reconciliation_errors
pulse_ticket_minted_total
pulse_ticket_spent_total
pulse_reward_pending_total
pulse_reward_failed_total
pulse_reward_settlement_latency_seconds
pulse_budget_used_ratio
pulse_api_latency_seconds
pulse_worker_job_failure_total
```

至少告警：Ingest Lag、Conflict、Ledger Mismatch、Settlement 堆积/Dead、Budget 接近上限、Period Close Failed、API 5xx 激增。

## 30. 故障模型

### Pulse MySQL 不可用

Pulse 不可用，new-api 正常。

### new-api LOG_DB 不可用

Ingest 暂停，恢复后从 Cursor 继续。

### Benefit API 不可用

Reward 保持 pending，Outbox 重试，不重新抽奖、不重复扣券。

### Worker 崩溃

重启后从 durable cursor / outbox / state 继续。

### HTTP Response 丢失

客户端复用同一 Idempotency-Key，返回原业务结果。

### Period Close 中断

State Machine Resume，重复执行不得重复发奖。

## 31. 部署

初期同机部署即可：

```text
Nginx
new-api
meta-pulse-api
meta-pulse-worker
MySQL
Redis
```

但必须满足：

```text
服务独立
数据库独立
权限独立
```

建议：

- Pulse DB 用户：Pulse DB read/write；
- NEWAPI_LOG_DSN 用户：new-api LOG_DB read only；
- Internal Benefit API：内网/localhost + HMAC；
- Secret 仅存服务器 Secret / 密码管理器，不写 GitHub。

## 32. `opensource-loyalty` 复用边界

不 Fork。

借鉴：

- Stable Business Idempotency Key；
- Payload Fingerprint；
- Ledger；
- Reward Lifecycle；
- Deterministic Holdout；
- Resource-derived Event Identity；
- Replay / Conflict Test。

不采用：

- Foodservice Protocol；
- Order / Merchant / Location；
- LIP Reference Engine；
- Identity / Cloud / Wallet；
- 全状态内存加载与全量持久化模式。

若直接复制 Apache-2.0 代码，保留 NOTICE / License Attribution。

## 33. 工程红线

1. Pulse 故障不得影响 new-api 主链路。
2. 同一 Usage 重放 N 次只能记一次账。
3. Ledger 是业务事实源。
4. Ledger 历史金额禁止 UPDATE。
5. 修正只能走 reversal / adjustment。
6. 同一 Pulse Action 永远只有一个随机结果。
7. 1 张 Ticket 最多真正消费一次。
8. 一个 Reward Grant 最多真正到账一次。
9. 不确定 Benefit 是否到账时先 Query。
10. 禁止换 source_ref 绕过幂等。
11. Reward 不得突破 Hard Budget。
12. Period Active 后核心规则不可原地修改。
13. Period Close 必须可重复执行。
14. 浏览器不得自行声明可信用户身份。
15. 金额与贡献值禁止浮点做账。
16. Holdout 必须稳定可复现。
17. 所有人工财务调整必须进入 Audit Log。
18. Pulse 无 new-api 用户余额直接写权限。

## 34. 最终工程范围

```text
Usage Ingestion
+
Economics Engine
+
Contribution Ledger
+
Ticket Engine
+
Reward Engine
+
Budget Engine
+
Settlement Engine
+
Period Engine
+
Experiment Engine
+
Analytics
+
User API
+
Admin API
+
new-api Pulse UI
+
Observability
+
Reconciliation
+
Backfill
+
Backtest
+
Ledger Check
```

项目最终原则：

> **用户真实调用产生平台价值，平台将其中一部分价值转化为用户可感知的权益；所有权益必须可追溯、可重放、可对账、可纠错、可控制成本，同时永远不能威胁元衡 API 主业务的稳定性。**
