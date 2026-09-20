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

### 跨仓库身份链路

系统采用双身份事实源，而不是把社区账号委托给 new-api：

```text
new-api = API 登录、模型调用与资金身份事实源
Answer  = 社区注册、密码、会话、封禁、资料与内容事实源
```

两条登录链路彼此独立：

```text
Pulse 控制台：浏览器 / YuanHeng → new-api session → Signed BFF → Pulse
Pulse 社区权益：浏览器 → Answer 认证会话 → 保护绑定 → community-bff → Pulse
社区浏览：浏览器 → METAR 静态壳层 → 同源 Answer API
社区写入/登录：浏览器 → Answer 原生页面 → Answer session
```

Answer 用户可选通过 new-api 现有 `/api/forum/sso/start` 绑定 API 身份。绑定事实源是 Answer `user_external_login(provider=pulse_user_center)`；数据库约束保证 Answer user ID 与 new-api user ID 一对一且不可静默换绑、转移或普通解绑。未绑定用户仍可使用社区，但不能展示或领取 Pulse 权益。

Answer v1.7.1 在启用 UserCenter 展示能力后会统一发布登录/注册链接，其注册跳转实现还会误读登录地址。公网网关因此归一化 `/answer/api/v1/user-center/agent` 返回的两个公开跳转字段：登录固定接入带浏览器 flow 的 Connector，注册固定留在 Answer `/users/register`；框架 redirect 路由只作为兼容兜底。不得把二者都直接指向 new-api，也不得绕过 Connector 创建 flow。

Cookie 只发给各自服务。YuanHeng 可在隔离 WebView 中打开 new-api 控制台，但不得保存密码、向 Pulse 发送 Cookie，或自行声明可信 `user_id`。社区网关不得把 new-api session 或 Pulse 签名头转发给 Answer；Answer 前端自己的 Authorization 必须保留。METAR 静态壳层读取同源 Answer API，并通过同源社区 BFF 使用本人 Pulse 权益；复用 Answer 已有 `_a_ltk_` token，不得复制 token 到自有存储、外部入口或 Pulse 原始请求。

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

浏览器和桌面端不能直连 Pulse 原始 API。对外仅暴露 new-api BFF 和受 Answer 会话保护的社区 BFF；`meta-pulse-api` 的用户 ID、角色和请求体均来自已验签的服务调用。Pulse 停止时，new-api 的模型请求、登录、计费、充值和余额链路仍必须可用。

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
├── services/pulse/
│   ├── cmd/{api,worker,tool}/
│   ├── internal/
│   │   ├── app/              依赖装配与 HTTP 生命周期
│   │   ├── config/           配置校验
│   │   ├── domain/           纯领域逻辑（后续里程碑补齐）
│   │   ├── ports/            应用接口与 UnitOfWork
│   │   ├── store/mysql/      Pulse DB GORM 实现
│   │   ├── store/redis/      Redis 运行时能力
│   │   ├── transport/http/   Gin 中间件与路由适配
│   │   ├── security/         HMAC、时间窗、Nonce
│   │   └── observability/    结构化日志与 Prometheus
│   └── migrations/           Goose SQL migration
├── services/forum-plugin/    Answer UserCenter 插件
├── sites/blog/               VitePress
├── metar-frontend/
│   ├── src/                  离线视觉/交互原型
│   └── production/           无 mock 的 Answer 生产适配层
└── deploy/                   Docker / Nginx
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

### new-api LOG_DB 映射边界

Pulse 使用 new-api LOG_DB 只读账号，仅读取业务必需字段：

```text
logs.id       → source_event_id（稳定事件 ID）
logs.user_id  → opaque principal
logs.type     → consume / refund 分类
logs.quota    → 用户侧计费额度
```

当前 new-api 中 `LogTypeConsume = 2`、`LogTypeRefund = 6`。`quota` 是总计费额度，不证明资金来自付费充值。当前仅 `other.pulse_funding` 的 verified v1 凭证可证明其中 paid_quota；缺失/未知来源不产券。它仍不等同于 Provider 成本；在成本快照接入前，不能把它宣称为真实毛利。Pulse 使用 `(created_at, id)` 复合游标、UTC+8 的 `source_created_at` 半开周期归属，并在同一 Pulse 事务内提交事件、账本、券、统计和游标。

游标翻页对 LOG_DB 的查询形状是硬约束，不是实现细节。new-api 的 `logs` 索引为 `(created_at, type)`，因此每一页必须满足两点：**按单个 type 分别查询后在 Pulse 侧归并**，以及**带上冗余的 `created_at >= cursor.created_at` 下界**。`type IN (2, 6)` 会让两个 created_at 序列交错，索引无法按序输出，计划退化为全表扫描加 filesort；而只写 `(created_at, id) > (?, ?)` 或等价的 OR 形式同样不可索引 —— `id` 不在该索引中，优化器只能从最早一行开始走完整个索引。两者都会把一次毫秒级翻页变成分钟级全表读取。归并后按 `(created_at, id)` 排序并截断到 batch size，结果与单语句形式一致。

Usage 关联契约如下：

- 普通消费：`logs.request_id` 是请求关联键，`logs.id` 是事件主键；
- 异步任务退款/差额结算：new-api 在任务私有计费快照中保存原始 `request_id`，并将它写回退款/差额日志的 `logs.request_id`；`logs.other.task_id` 保存稳定任务键；
- 差额结算仍使用 `LogTypeConsume`（补扣）或 `LogTypeRefund`（退回），并在 `logs.other` 保存 `pre_consumed_quota`、`actual_quota`、`reason`；不另造不可追溯的 correction 类型；
- 历史或特殊任务若只有 `task_id`、没有原始 `request_id`/明确 `origin_log_id`，不得猜测原消费，必须进入 `manual_review` 与对账冲突；
- `logs.other` 中兼容读取 `origin_log_id`、`original_log_id`、`consume_log_id`、`related_log_id`、`log_id`，明确关联优先于 request 关联。

无法确认关联时不得直接改变贡献值。上述字段均为最小化关联元数据，禁止复制 prompt、response、IP、Token 或 Cookie。

### Provider 成本事实

当前 new-api `LOG_DB` 有用户侧 `quota` 及限定路径的付费来源凭证，但没有可审计的 Provider 实际成本。Pulse 回测和预算在成本快照上线前只能使用“用户收费 × 已配置倍率”的估算值，报告必须明确标注为成本代理值，不得宣称真实毛利。若进入正式 Margin-aware 运营，new-api 需在消费日志写入时保存不可变的整数定点成本快照（金额、币种、Provider/价格版本），退款和差额日志只关联原消费，不重算或覆盖该快照；Pulse 只读消费这些最小字段。

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

每个 Usage Event 必须保存当时命中的规则、`economics_config_version`、倍率、eligibility、contribution 快照，且规则版本必须与归属 Period 的 `config_version` 一致；后续规则变更不得重算旧事件。事件同时保留 model/channel 与最小化 request/correlation 字段，供退款对账；不得保存 prompt、response、IP、Token 或 Cookie。

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
- `pulse_ledger_entry.payload_hash` 持久化请求指纹，同 key 不同 payload 必须 conflict；
- Ledger 表由数据库 append-only trigger 拒绝 UPDATE/DELETE，应用层只允许 INSERT；
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
POST /v1/internal/me/actions
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

用户 Action 的持久化幂等范围不依赖当前周期或运行时配置：

- 请求范围：`pulse_action_request:{user_id}` + `Idempotency-Key`；
- 动作范围：`pulse_action_identity:{user_id}` + `action_id`，避免换请求 key 后再次扣券；
- 请求指纹仅覆盖规范化的 `user_id + action_id + trigger_type`；周期、配置版本和随机结果在首次成功时绑定并保存在原响应/Grant 中。

单事务固定先锁 Action Identity，再锁 Request Identity（避免 MySQL 重放时的重复键/gap-lock 锁序循环），先返回已保存结果，再为真正的新操作寻找 Active Period。首次请求、双幂等响应、扣券、预算、Grant 和 Outbox 一起提交；周期 closed、跨周期或密钥轮换不影响已提交结果的重放。同 key 改 payload 必须 conflict。新的用户操作必须使用新的 `action_id` 和请求 key，不能按周期复用固定值。

升级兼容保留原 `pulse_action:{period_id}:{user_id}` 记录及其指纹，不覆盖历史。首次读取旧记录时校验原响应、指纹与 Grant 后，在同一事务补建稳定映射；同一旧 key 或 action 对应多笔历史结果时 fail closed，交由人工对账，不猜测、不补发。旧记录按 key 查找、旧 Grant 按用户/action 查找，使用 `00009` 的辅助索引。上线必须排空旧 API 写请求，禁止混跑新旧 Action 实现；回退旧版前必须先封闭 Action 入口并解决新格式兼容，禁止删除幂等记录绕过校验。

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

Grant 一旦首次产生，其 reward type、amount、random value 不得重新随机。状态只能按前述方向推进；`settled → reversed` 必须保留原 `settled_at`，仅追加 `reversed_at`，不得抹去历史结算时间。

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

Worker 再异步调用 new-api Benefit。Outbox 使用 `dead` 表示重试耗尽但仍可 Query/Reconcile 的不确定结果，使用 `conflict` 表示 payload 非法、指纹冲突、已撤销或来源不一致等终态完整性冲突；`conflict` 不得重新进入自动 Reconciliation，只能保留 Grant 与预算预占供人工审计处置。

人工恢复使用 `reward-retry --grant-id <grant_id>` 精确选择一条 `dead` 记录。命令必须先 Query 原 `source_ref`：已到账则直接收敛，只有 `not_found` 才以 attempts fence 原子领取一次额外发送机会；attempts 不清零，`conflict` 永远不得人工重发。命令重复执行不能更换 source_ref，也不能覆盖已完成结果。

## 20. new-api Internal Benefit API

新增内部接口：

```text
POST /api/internal/pulse/benefits/grant
POST /api/internal/pulse/benefits/query
GET  /api/internal/pulse/benefits/query/:source_ref
POST /api/internal/pulse/benefits/rollback
```

Grant 示例：

```json
{
  "grant_id": "reward_grant_01J...",
  "user_id": 123,
  "amount": 50000,
  "transferable_quota": false,
  "source_ref": "reward_grant_01J...",
  "reward_type": "newapi_quota",
  "payload_hash": "<settlement payload sha256>"
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

new-api 的 Benefit Receiver 必须以 `source_ref` 唯一定位，并持久化请求的 payload fingerprint。Grant 请求的 `user_id` 必须与已验签的 `X-Pulse-User-Id` 完全一致，避免签名主体与实际入账用户脱钩；Query/Rollback 只使用服务身份签名，不从浏览器或请求体推导入账用户。Pulse Outbox 的 `payload_hash` 对 JSON 做稳定规范化（对象 key 排序、去除空白后再 hash），避免 MySQL JSON 列读回时的 key 顺序/空白变化造成误判；兼容历史 struct 字段序列化的记录时仍必须完成同一 payload 语义校验：

```text
同 source_ref + 同 payload → 返回第一次结果
同 source_ref + 不同 payload → conflict，不得吞掉 duplicate
```

Pulse 专用接收端已在 `BenefitChangeRecord` / `GrantUserQuotaTx` 基础上实现 payload 比较、审计记录、明确错误码及独立发放限额。同一 Reward 最多到账一次；新奖励强制 `transferable_quota = false`，不能把活动额度再次转移。新发放默认关闭，真实到账仍需部署验收。

Benefit 状态必须显式区分：

```text
applied / already_applied → 奖励当前有效，可收敛为 settled
rolled_back              → 奖励已撤销，applied=false，不得收敛为 settled
not_found                → 尚未找到奖励，可使用原 source_ref 重试
```

`status` 是跨版本判定依据；即使旧 new-api 在 `rolled_back` 响应中遗留 `applied=true`，Pulse 也必须按已撤销处理。Grant 在 rollback 后重放只能返回 `rolled_back`，不得返回 `already_applied` 或再次增加额度。Rollback 仅在确认 `rolled_back` 后才允许本地 Grant/Budget 收敛为 reversed。

发生 timeout 等不确定结果时必须先 Query 原 `source_ref`：只有 `applied / already_applied` 才标记 settled；`not_found` 才使用原 source_ref 重试；`rolled_back` 或 source_ref 不一致进入 terminal conflict，保留原 Grant 与预算预占供人工处置。Benefit Receiver 明确返回 payload conflict 时属于已确定的指纹冲突，必须直接进入 terminal conflict；Query 只返回生命周期状态，不能证明已到账 payload 与当前 Outbox 一致，因此禁止再用 Query 的 `applied` 覆盖该冲突。禁止生成新 source_ref 绕过幂等。

## 21. Period Reward

Reward Grant 的来源类型固定为：

```text
trigger_type = pulse          # 用户开启脉冲，仅由 /me/actions 生成
trigger_type = period_reward  # Period Close 服务端生成
trigger_type = content        # 管理员审核内容后服务端生成
```

用户 Action 只能提交 `pulse`，不得伪装 `period_reward` 或 `content`；持久化层同时校验来源、Reward Definition 与 `loyalty` / `period_reward` / `content_reward` Budget 的固定绑定。

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

浏览器不得自行声明可信 `user_id`。Pulse 用户接口有两条受控链路：

```text
Browser / YuanHeng
  → new-api Session
  → new-api Signed BFF（服务端派生 user_id）
  → Meta Pulse

Community Browser
  → Answer 认证会话、激活及本地状态校验
  → 受保护的一对一绑定
  → Answer 插件 community-bff（独立角色和密钥）
  → Meta Pulse
```

服务签名头：

```text
X-Pulse-User-Id
X-Pulse-Role
X-Pulse-Timestamp
X-Pulse-Nonce
X-Pulse-Signature
```

签名至少覆盖规范化的 `method + path + user_id + timestamp + nonce + body_hash`；时间窗、Nonce、来源和密钥版本必须校验。new-api BFF 的 `user_id` 只能来自 new-api session；社区 BFF 的 API 身份只能来自真实 Answer 会话对应的受保护绑定；Worker 的 Benefit 用户必须来自不可变 Grant，并由 new-api 校验请求头与 body 一致。

社区身份独立属于 Answer。可选绑定链路为：

```text
Answer browser flow
  → new-api session 签发短期 Login Ticket
  → 固定社区 callback
  → Connector 严格验签并原子消费 flow + nonce
  → Answer 邮箱确认/本地账号绑定
```

Login Ticket callback 在验签前全部不可信。Connector 不使用未证明 verified 的 new-api Email 自动匹配 Answer 账号；`EnabledOriginalUserSystem=true`，并关闭 UserStatus/Rank 代理，避免 new-api/Pulse 覆盖 Answer 密码、资料、封禁和治理角色。现有 Ticket 未签名 `state`，浏览器 flow marker 只是额外门禁，不等价完整 OAuth state；后续 SSO 专项升级应把 state 纳入 Ticket 签名。本次付费来源与 Benefit 升级没有改变该 Ticket 契约。

以下是 Pulse 原始用户 API，只允许内网具有对应签名角色的 new-api BFF 或 Answer 社区 BFF 访问；浏览器和 YuanHeng 不得直接调用：

```text
GET  /v1/internal/me/summary
GET  /v1/internal/me/rewards
GET  /v1/internal/me/rules
POST /v1/internal/me/actions
```

周期信息包含在 summary/rules 中；原动作结果通过 `rewards?action_id=…` 精确恢复，没有单独的公开 Ledger 或 Grant 详情入口。`/healthz`、`/readyz` 是独立的私网健康检查，不属于用户 BFF 契约。

summary 的 `ledger` 仅返回当前周期最新 100 条贡献值与 Ticket 流水，按 ID 升序显示，`ledger_has_more` 标识是否还有更早记录，`ledger_limit=100` 明确投影上限。两类资产分别通过账户索引读取至多 101 条后合并，不在请求内加载全部历史。累计贡献、当期贡献、等级和可用券仍来自完整账户快照；Ledger 事实、全量重建与对账不受此展示上限影响。

运营概览的账实核验通过 `(user_id, period_id, asset_type, amount)` 覆盖索引一次聚合完整 Ledger，再与全部 Account 的余额及版本（流水条数）比较。空账户按零余额、零版本核验；该只读诊断不参与任何经济动作授权。

内部路由按角色最小授权：`new-api` 与独立 `community-bff` 访问本人 summary/action/reward/rules；`forum` 只使用独立 `PULSE_FORUM_HMAC_SECRET` 访问用户等级 Profile；内容奖励管理路由只接受 `admin`。Forum Profile 密钥不得与 Settlement/Worker、BFF、Admin、Reward Random 或 Forum SSO Login Ticket 密钥复用；角色缺失、未知、密钥复用或不匹配时 fail closed，且不得触发查询、记账或结算。

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

M7 扩展表：

```text
pulse_content_candidate
pulse_content_award
pulse_content_award_limit_guard
```

重要唯一约束至少包括：

```text
pulse_usage_event: UNIQUE(source_system, source_event_id)
pulse_ledger_entry: UNIQUE(operation, idempotency_key)，并保存 payload_hash
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

Pulse 对 new-api LOG_DB 使用只读账号，并且无权限直接写 new-api 用户余额表。new-api session Cookie 不得转发给 Pulse 或论坛；YuanHeng 的 Cookie 必须隔离在 new-api WebView / 客户端安全存储边界内。社区使用新的独立 HTTPS 域名：根路径 `/` 以及白名单内的 `/latest`、`/topics`、`/question/:id`、`/topic/:slug`、`/me/*`、`/admin/pulse` 等 History 路径由无 mock 的 METAR 静态壳层提供，`/blog/` 由 VitePress 提供，其余 `/questions`、`/users/*` 和 `/answer/api/*` 仍由 Answer 提供。静态壳层只调用同源 Answer API 与认证社区 BFF，不自行实现身份或写权限判断。社区网关仅按 allowlist 转发 Answer `visit` 或 callback flow Cookie；callback 清除 Authorization，所有代理路由清除 Pulse 签名头，普通路由保留 Answer 自己的 Authorization；网关不直接代理 new-api 或 Pulse。

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

指标采集按进程归属：API `:8088/metrics` 只提供真实 HTTP 指标，路由标签使用模板或固定 `unmatched`，不使用用户 ID/原始路径扩张序列。Worker `:8089/metrics` 在运营快照事务成功提交后更新业务 Gauge，同时暴露周期失败和固定任务名的失败计数；该端点仅供内网 Prometheus 访问。

首次采集前业务 Gauge 为 `NaN`，`meta_pulse_operations_up=0`，最近成功时间为 0。采集失败保留最后成功的数值/时间，设置 up=0 并增加失败计数；需同时监控抓取失败、up=0 和 `time() - meta_pulse_operations_last_success_timestamp_seconds > 120`，不能把数据过期当作零故障。所有指标仍是可重建诊断，不授权任何经济写操作。

## 30. 故障模型

### Pulse MySQL 不可用

Pulse 不可用，new-api 正常。

### new-api LOG_DB 不可用

Ingest 暂停，恢复后从 Cursor 继续。运行中的 Usage、Settlement、Reconciliation、Period Close、运营聚合和可选 Content Ingest 各用独立任务循环与超时；慢日志读取不能消耗结算/对账的时间预算。单个任务自身不重叠执行，停机统一取消并等待在途任务退出；数据库事务和 Outbox fence 仍负责跨实例幂等，调度器不是记账锁。启动时的 LOG_DB 只读权限门禁仍保持 fail closed。

调用外部数据库的任务（Usage Ingest、Content Ingest）在连续失败时按指数退避重试，从 Interval 翻倍至 10 分钟封顶，任何一次成功立即重置。这条约束针对的失败模式是：游标停滞时每次重试都是同一条昂贵查询，固定间隔重试会把一条慢语句变成对外部库的持续压力，使故障无法自愈。只访问 Pulse 自身数据库的任务不退避。除调度节奏外，每页读取还带语句级 `MAX_EXECUTION_TIME` 上限作为内层兜底，使异常执行计划无法长时间占用 LOG_DB 线程；Go context deadline 仍是外层边界。

Usage Worker 默认每页最多 250 条，单轮硬超时仍为 20 秒。为避免正常积压因整页处理超时进入长退避，摄入另设 15 秒处理预算（含取页时间）：只在一条事件及其游标事务成功提交后检查预算，达到预算且仍有剩余事件时返回 `yielded=true`，按正常 30 秒间隔续跑。未处理尾部不推进游标、不缓存为事实源，下一轮从已提交游标重新读取。预算不是单条事件的超时保证；外部读取失败、事务失败、取消或硬超时仍返回错误并保留退避。批次统计只累计已成功提交的事件，不能把事务内已计算但最终回滚的结果当作入账进度。离线工具默认不启用该预算，仍受其自身 context 约束。

### Benefit API 不可用

Reward 保持 pending，Outbox 重试，不重新抽奖、不重复扣券。

### Worker 崩溃

重启后从 durable cursor / outbox / state 继续。

### HTTP Response 丢失

客户端复用同一 Idempotency-Key，返回原业务结果。

### Period Close 中断

State Machine Resume，重复执行不得重复发奖。

## 31. 部署

new-api 与 Meta Pulse/社区允许部署在不同服务器并独立更新。推荐拓扑：

```text
new-api 服务器：现有域名 + new-api + LOG_DB / Benefit API
社区服务器：metar.uk + Nginx + Answer + VitePress + Pulse API/Worker + 独立 MySQL/Redis
```

两端只通过受控的只读 LOG_DB、Internal Benefit API、Signed BFF 与浏览器 SSO Bridge 建立联系。社区服务器不运行第二套 new-api，社区网关也不代理 new-api/Pulse 公网接口。

社区可以增加自有前置反代：`metar.uk → 64.83.9.190 → HTTPS 23.94.111.46`，Cloudflare 仅管理 DNS。前置机使用独立证书并校验回源证书与 `metar.uk` 主机名，不缓存页面/API，不在访问日志中记录 query、Referer 或凭据。源站只信任该前置机精确 IP 的 `X-Real-IP`，供访客限流与日志使用；转发给 Answer 的 X-Forwarded-For 重新生成为可信单值，不接受浏览器声明用户身份。Answer Cookie、Authorization、绑定与 Pulse 权限边界保持不变。两台机器的 ACME webroot 分开，前置机找不到的挑战文件才转发源站，分别验证自动续期。部署与 DNS 切换顺序见 `deploy/nginx/RELAY.md`。

无论同机或跨服务器，都必须满足：

```text
服务独立
数据库独立
权限独立
```

建议：

- Pulse DB 用户：Pulse DB read/write；
- NEWAPI_LOG_DSN 用户：new-api LOG_DB 仅授予 `logs` 表的 SELECT（及必要的 USAGE/SHOW VIEW），由 `access-check` fail closed 验收；
- Internal Benefit API：内网/localhost + HMAC；
- Pulse 原始用户 API：仅内网，由 new-api Signed BFF 或独立 Answer 社区 BFF 代理；
- 对外 `/api/pulse/*`：由 new-api BFF 接管并清除浏览器提交的 Pulse 服务签名头，不把浏览器请求直接转发为 Pulse 原始请求；
- 社区使用新的独立 HTTPS 域名；精确根路径进入 METAR 静态壳层、Answer 原生/API 路径保持代理、`/blog/` 进入 VitePress，网关使用 Cookie allowlist，Login Ticket callback query 不进入边缘 access log；
- 仅开通社区账号与可选绑定可复用现有 SSO Bridge 配置；开放自动奖励还必须升级 new-api 付费来源证明和 Benefit 接收端，并按 `REWARDS_ROLLOUT.md` 验收；
- Secret 仅存服务器 Secret / 密码管理器，不写 GitHub。
- 生产配置按角色校验：API 需要 BFF/Admin/随机密钥，社区角色与受控撤销分别使用独立密钥；API 不持有 LOG_DB 凭据或发奖用的 `PULSE_SERVICE_HMAC_SECRET`，撤销密钥按需配置。Worker 仅持有结算服务/随机密钥及只读 LOG_DB/Benefit 配置，不注入 rollback/BFF/Admin 密钥；迁移和只读工具不因公共配置加载而被迫持有签名密钥，实际操作另行检查所需能力。
- 更新流程先获得部署锁、只读校验并备份原 `.env`，再执行数据库备份、拉取、构建和迁移；只有安装可初始化凭据。更新配置缺失/占位时停止，不能自动重建密码或随机种子。Compose 使用已校验配置文件，不接受继承的应用环境变量隐式覆盖。
- API/Worker 均通过自身 `/readyz` 检查 Pulse MySQL/Redis；Worker 诊断端口为容器内 8089，不能将其公开到宿主机或公网。

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
19. 论坛内容不得产生 contribution 或 ticket。
20. Answer 允许本地独立注册；社区身份与 new-api API/资金身份是双事实源。
21. 可选绑定必须一对一，不得静默换绑、转移或普通解绑。
22. 未绑定用户和绑定前内容不得产生 Pulse 内容奖励。
23. Pulse 故障不得阻断论坛本地登录或浏览。
24. 论坛登录回调参数在验签通过前一律视为攻击者可控。
25. 未证明 verified 的 new-api Email 不得用于 Answer 自动绑定。
26. new-api/Pulse 不得覆盖 Answer 本地封禁、密码、资料和治理角色。

## 34. 社区层

论坛（Apache Answer）与博客（VitePress）是主要产品入口，Pulse 权益是绑定后的增强能力。

```text
Answer                    社区身份、会话、封禁、资料与内容事实源
new-api                   API 身份、模型调用与资金事实源
user_external_login       一对一绑定事实源
Meta Pulse                贡献值 / 券 / 等级 / Reward 事实源
```

- Answer 开启本地注册和密码登录；绑定 new-api 可选，未绑定用户仍可正常使用社区；
- 插件同时实现 Connector 与非权威 UserCenter：Connector 处理绑定，UserCenter 只提供可降级徽章；
- 绑定 guard 使用条件生成列、单列唯一索引和 INSERT/UPDATE/DELETE 触发器，防重复绑定、并发冒领、静默换绑和普通解绑；
- new-api 原有 `/api/forum/sso/start` 从 session 签发短期 Ticket；社区先经过 new-api 同源 bootstrap 页面，规避 `SameSite=Strict` session 在跨站首跳时不发送；插件要求浏览器 flow、严格字段集合、HMAC、时间窗及 Redis 原子 nonce，故障时 fail closed；
- new-api Email 未证明已验证，Connector 不传 Email/Avatar，绑定沿用 Answer 邮箱确认；
- Pulse 对论坛内容库只读；Answer 本地 ID 必须经受保护绑定映射为 new-api ID；
- 只采集绑定后发布的公开 available/closed 问题，未绑定、绑定前、隐藏、待审核或删除内容不进入候选；
- 内容奖励直接生成独立预算 Reward Grant，**不产生 contribution 或 ticket**；
- 社区首页静态壳层读取 Answer API，并通过同源认证社区 BFF 使用本人权益；不保存余额或中奖事实、不复制 Answer token、不直接调用 Pulse。社区网关仅代理 Answer（包含社区 BFF），不直接代理 new-api/Pulse；只转发 allowlist Cookie，callback 清除 Authorization，普通代理路由保留 Answer API Authorization，所有代理路由清除 Pulse 签名头；
- Pulse 故障时等级徽章降级为空，不影响 Answer 本地登录和浏览。

完整定义、剩余 signed-state 限制和上线门禁见 `docs/COMMUNITY.md`。

## 35. 最终工程范围

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

## 36. M6 周期与运营实现

Pulse 的周期关闭由 Worker 异步执行，严格遵循：

```text
draft → active → settling → closed
```

关闭前必须确认 Usage Cursor 的 `watermark_at >= period.ends_at`；随后在同一个 Pulse DB 事务内完成 Ledger/Account 重建校验、未消费 Ticket 过期、周期奖励 Grant/Outbox 写入和最终状态迁移。事务中断会整体回滚，重跑通过稳定 action id `period_reward:{period_id}:{user_id}` 和 `ticket-expire:{period_id}:{user_id}` 收敛，不重新随机、不重复扣券。周期奖励使用独立 `period_reward` Budget，强制 `transferable_quota=0`，并校验不可变 `config_version`。

人工财务调整必须携带操作人、原因和 request id；调整只追加 `contribution_adjustment` / `ticket_adjustment` Ledger 分录，并在同一事务追加 `pulse_audit_log`。历史分录和金额不得 UPDATE。实验分组先由版本化 HMAC 稳定计算，再由 `(experiment_id, user_id)` 唯一记录固化，后续配置变化不得覆盖历史 cohort。

运营指标是可丢失、可重建的诊断投影，不是经济事实源。Worker 从 Pulse 持久化表聚合 ingest lag、开放冲突、Ledger mismatch、Settlement retry/dead 和 Budget reserved/hard cap 到 `pulse_metric_daily`，并输出异常告警日志；Prometheus 由 API 提供 HTTP 指标、Worker 提供周期/结算/预算指标和采集新鲜度。Worker 各任务独立调度，指标聚合失败不得阻断用量记账或结算。

## 37. M7 内容奖励实现边界

内容奖励是社区内容到权益的窄入口，不能改变贡献值经济模型：

```text
Answer 只读元数据
  → pulse_content_candidate（游标 + payload fingerprint）
  → 管理员审核/档位/reason/audit
  → pulse_content_award
  → Reward Grant + Settlement Outbox
  → new-api Benefit（transferable_quota=false）
```

- `services/pulse/internal/adapter/forum` 只读取 Answer v1.7.1 `question` 与受保护的 `user_external_login` 映射：问题 ID、绑定后的 new-api 作者 ID、标题和创建时间；不读取正文、不写论坛库。仅采集绑定后发布且公开的 available/closed 问题；判定使用 `binding.created_at < question.created_at`，因为 Answer 的 `TIMESTAMP` 为秒级，同秒先后关系不明时保守排除。未绑定、绑定前/同秒、隐藏、待审核或删除内容不进入候选。读取先按原始 question ID 分页，不合格行只推进 durable cursor、不创建 Candidate，避免尾部不合格内容被永久重复扫描；Worker 使用独立只读 `FORUM_DB_DSN`，论坛库故障只影响候选采集。
- 内容奖励预算固定使用 `budget_type=content_reward`，与 `loyalty`、`period_reward` 分离；内容奖励不会写 Contribution/Ticket Ledger，也不进入贡献毛利分母。
- 发放必须同时通过真实付费门槛、管理员审核、用户周期上限/全站日上限和 Hard Budget 预占四道闸。限额检查先锁定 `pulse_content_award_limit_guard` 的固定行，跨实例串行化后以 locking current read 重算；全站日界按 Asia/Shanghai 的 `[day_start,next_day_start)`。未满足门槛或限额时只保留可审计的资格结果，不创建 Grant。
- 稳定 action 为 `content_award:{content_type}:{source_content_id}:{award_version}`；相同 action 重放返回原结果，payload 改变返回 conflict；随机值/Grant/source_ref 不因重试变化。
- Content Candidate 创建时固定为 `pending`，首次审核只能原子地从 `pending` 进入 `approved` / `rejected` / `deleted`，审核人、原因和时间不得被后续请求覆盖；追加 `award_version` 可复用已批准候选，但必须新增 Award 与 Audit Log，不能改写首次审核记录。
- Content Award 状态随共享 Grant 结算同步：`pending → settled → reversed`；结算在同一 Pulse 事务内把关联 Award 标记为 `settled`，已结算内容继续计入用户/全站限额，只有 `reversed` 才从活跃额度中排除。
- 内容删除或抄袭只能沿原 `GrantID/source_ref` 执行 Benefit rollback，再把原 Award 标记为 `reversed`；rollback 与本地提交之间发生故障时，重试必须先复用原 source_ref，不能换 key 发补偿奖励或重复释放预算。
- 管理路由只接受已签名且角色为 `admin` 的 Principal，操作人从签名身份派生；浏览器提交的 `user_id`、`actor_id` 等字段一律不可信。`Idempotency-Key` 是审核/撤销请求的必填字段。

Pulse 侧 M7 已具备实现和回归测试；Answer v1.7.1 表结构、绑定时间映射及一对一约束已在隔离 MySQL 8 验证。生产仍需验收只读权限、正式域名/邮件、跨实例 flow/nonce、new-api Benefit rollback/idempotency 及社区降级链路。

## 38. 用户奖励历史只读投影

`GET /v1/internal/me/rewards` 由已验签 Principal 派生用户身份，查询 Pulse 内部 Grant 记录但只返回 `grant_id`、周期、action、奖励类型、额度、状态和创建时间。`budget_type`、`source_ref`、随机值、内部 payload 和管理审计字段不得进入该响应；`user_id` 查询参数即使出现也不会改变查询主体。该接口只读，不改变任何 Ledger、Budget、Grant 或 Settlement 状态。

## 39. 周期开局与游标运营命令

周期只能由运营通过 `cmd/tool` 显式创建，Worker 不会自动开周期。没有 Active Period 时 `usage_ingest` 会直接失败并且**不推进游标**，源行原样保留等待重试，因此"缺周期"是可安全恢复的停摆，不是数据丢失。

```text
tool period-create --key … --starts-at … --multiplier-bps … [--activate]
  [--rewards-file … --reward-budget … --ticket-threshold-milli …]
  → 单事务：检查窗口重叠 → 建 draft 周期 → 写入经济规则及可选奖池/预算 → 完整校验 → 可选 draft→active → pulse_audit_log
```

- 周期窗口固定 10 天、半开区间 `[starts_at, ends_at)`，`ends_at` 由服务端推导，不接受调用方传入。
- 创建时必须至少写入一条经济规则，且规则与周期在**同一事务**内落库。这是不变量 #11 的直接结果：周期 Active 后规则不可原地修改，而无匹配规则的事件会被记为 `eligible=false / contribution=0` —— 那是被记录的不合格事件，不是隐式的默认奖励规则，事后无法补救。
- 重叠检查覆盖全部状态而非仅 Active。两个周期共享任一时刻都会让事件的周期归属产生歧义，即使它们都已 closed。窗口首尾相接不算重叠，与 `period.Contains` 的半开语义一致。
- 激活前调用 `economics.ValidateRules`，走与 ingest 热路径相同的校验，避免周期带着"每批都会失败"的规则上线。
- 带奖池时必须同时提供严格 JSON 奖项、正整数 quota 预算和产券阈值，资金策略强制 `verified-paid-v1`；奖项、权重、loyalty 预算及审计与周期同事务落库。未提供奖池的旧调用保留 legacy 摄入用途，不能开放真实抽奖。Active 后数据库冻结奖项与预算核心配置，预占/结算计数仍可推进；格式与操作步骤见 `REWARDS_ROLLOUT.md`。

游标前移是独立的、不可逆的运营动作：

```text
tool cursor-seek --skip-before … --reason … --confirm
  → 单事务：锁游标 → 断言只能前进 → 写入 "<skip_before-1>:<max_id>" 与 watermark → pulse_audit_log
```

- 被跳过的源行**永久放弃记账**：它们不会再产生 usage event、贡献、券或奖励。命令要求显式 `--confirm`。
- 游标只能前进。后退虽然对 `source_event_id` 幂等，却会把 period close 依赖的 `watermark_at` 一并回退。
- 游标值是 `"<unix 秒>:<id>"`，比较必须逐段按数值进行；字符串序会把 `"999:1"` 排在 `"1000:1"` 之后，从而放过一次跨位数的回退。

两条命令都强制携带操作人与原因，并在同一事务追加 `pulse_audit_log`（不变量 #16）。

## 40. 运营只读概览接口

`GET /v1/internal/admin/operations/overview` 是运营控制台的唯一数据源。它只读，且**没有注册任何写动词**：周期创建、规则写入和游标前移全部留在 `cmd/tool` 的审计路径上，控制台看得到状态，改不了状态。

```text
admin Principal（签名派生，不信任请求体）
  → ListPeriods（含规则数、用户数、事件数、券数）
  → ListRules（仅 draft/active 周期）
  → ListCursors（游标位置 + watermark 滞后）
  → OperationalSnapshot（可失败，失败不影响其余投影）
```

- 路由限定 `admin` 角色，并要求 Principal 带有非零 user_id。投影包含周期、游标和券计数，非管理员不可读，即使它不写入任何状态。
- 响应带 `Cache-Control: no-store`。控制台靠轮询判断摄入是否停摆，缓存页面会把停滞的游标显示成健康。
- 规则只为 `draft` 和 `active` 周期查询。closed 周期的规则是历史，不会再变，不值得每次刷新都付一次查询。
- `health` 区分两种停摆，因为运营动作不同：**没有覆盖当前时刻的 active 周期**（需要建周期），与 **active 周期没有任何经济规则**（每个事件都会记为 `eligible=false / contribution=0`，且不变量 #11 使其无法原地修复，只能关闭后重建）。后者比前者危险，因为它表面上看起来正常。
- 错误响应不回传底层错误文本。该文本会包含内部表名和列名，控制台只需要知道投影不可用。
- 投影不包含任何 Ledger 金额。控制台不应成为账本事实源的第二份、更弱的渲染。

已有运营概览位于 new-api `/console/pulse-ops`，通过同域 `/api/pulse/ops/overview` 访问本接口；new-api 从管理员会话派生用户及角色，并使用独立 `PULSE_ADMIN_HMAC_SECRET` 代签。Answer 角色不自动获得 Pulse 运营权限：新增 METAR 运行配置页仅在插件显式配对管理凭据后开放第 42 节的设置接口，不能据此修改本接口的周期、经济规则或账本。


## 41. 自动到账与付费证明安全边界

社区认证 BFF 已落地：Answer 原生认证与激活校验后，插件从真实会话读取本地身份，实时核验本地状态及保护绑定，使用独立 community-bff 密钥访问 `/v1/internal/me/{summary,rewards,rules,actions}`。公网 `/metar/api/pulse/*` 仅别名转发 Answer 插件，不能透传为 Pulse 原始 API。POST 固定同源 Origin、X-Metar-Request、严格JSON和Idempotency-Key；只允许 action_id，trigger_type由服务端固定pulse。summary不暴露内部身份/账本source，action不暴露随机值、配置内部字段或审计数据。查询结果no-store。

新奖池周期持久化 funding_policy=verified-paid-v1 和 ticket_threshold_milli，创建时与经济规则、奖项、loyalty预算、审计同事务写入。历史周期默认legacy，不能原地升级发奖或降回draft。Active后的核心周期配置、奖项与预算上限由数据库冻结，运行计数继续允许更新。产券使用周期固化门槛。API默认不允许新Action；Shadow状态也不能从社区扣券。已成功Action的跨周期恢复先于开关与周期校验。

new-api 新增本地付费资格账本与钱包结算凭证。首版只覆盖升级后的 Stripe/Creem/易支付真实支付回调入账，以及随后普通同步钱包的预扣、绝对金额结算和退款；使用稳定 request_id 与用户锁，新支付成功与资格入账同事务。历史余额来源保持未知、不回填资格；赠送、手动加额、普通/钱包兑换码、签到、邀请和 Pulse 奖励不增加付费资格。订阅、独立套餐令牌、异步任务、Realtime、强制预扣图像及其他未归因扣费路径暂不产券；不支持的扣费/转码/手工修改保守失效资格。该实现不依赖 Pulse 网络；上线须排空旧实例在途请求与旧余额 batch 后整体切换，不能混跑。

LOG_DB 最小事实为 `other.pulse_funding={version:1,status:"verified",paid_quota,non_paid_quota,unknown_quota,proof_ref}`，非负整数合计必须等于日志 quota。Pulse 只以 paid_quota 形成 QuotaDelta/贡献；同一 verified 凭证中的 non_paid_quota 和 unknown_quota 不计入贡献。凭证缺失、状态 unknown 或未支持的退款进入 manual_review；同步预扣失败退款在消费日志前完成，异步初次与后续日志暂不产券。proof_ref 与 Usage/Ledger/游标同事务使用 `paid_funding:{source_system}` 幂等记录永久占用，另一 log.id 重复携带同 proof 不可再次记贡献。老 Usage 和账本不重写；新周期服务层也拒绝无证明事件。付款追回冻结 new-api 新奖励资格，已发奖励保留原 Grant 审计，不自动重复或超额追扣。

Pulse使用事务预算预占；当余量不足最大单奖，暂停整个奖池以维持公开权重语义。new-api另以持久化锁和每日计数，在自己的事务中限制单笔、用户日、平台日毛发放额，固定Asia/Shanghai日界，撤销不返还当日限额。账户禁用/删除/支付风险冻结拒绝新Grant；现存同source_ref同payload先恢复，开关/限额不阻断重放。新奖励类型固定newapi_quota且不可转让。

结算与撤销权限分离：pulse-settlement只能grant/query；pulse-rollback只能query/rollback，独立密钥且禁止交给Worker。查询/撤销沿用服务主体+source_ref，不从浏览器派生受益人；grant仍必须签名主体等于请求user_id。余额不足时撤销明确拒绝，不能扣成负余额；Pulse保留原记录/预算等待人工审计。暂停新发奖后Query/Reconcile继续运行。`PULSE_ACTIONS_ENABLED` 与 `PULSE_BENEFIT_ENABLED` 默认 false，Shadow Mode 默认 true；Answer 插件缺少独立社区密钥时只关闭该 BFF，不阻断本地登录。代码实现与本地测试不代表生产已开放，完整部署步骤与限定支持范围见 `REWARDS_ROLLOUT.md`。

## 42. METAR 运行配置管理

`/admin/pulse` 通过同源 `/metar/api/admin/pulse/{settings,secret}` 访问 Answer 管理员 BFF。原生管理员认证之后仍需实时读取 Answer `user` 与 `user_role_rel`，核验唯一管理员角色、可用状态和邮箱激活；不依赖 new-api 绑定，不把普通用户或版主提升为 Pulse 管理员。插件须显式配置 `admin_hmac_secret` 配对 Pulse 的运营管理角色，不能复用用户社区 BFF/SSO/Profile/发奖密钥。浏览器没有直接 Pulse 地址/签名/actor 控制能力。

内部接口为 `GET/PUT /v1/internal/admin/settings` 和 `POST /v1/internal/admin/settings/secret`，只允许签名 `admin` Principal。管理 BFF 固定路径、拒绝 query token、限制请求/响应大小、严格 JSON 白名单并验证同源写请求；不转发 Answer token、浏览器指定的 Pulse 头或任意目标 URL。首次显式配对授予设置管理权限，不改变 Answer 的本地治理事实源。

新增 `pulse_runtime_config`、`pulse_runtime_role`、`pulse_runtime_secret` 与 `pulse_runtime_change`。数据库保存公共参数和角色加密密文，是网页覆盖配置的事实源；没有覆盖值的字段使用已登记的部署基线。Redis 不保存配置事实。单例行锁串行化角色登记、版本 CAS、幂等、设置和审计；同 actor+Idempotency-Key+相同规范请求返回第一次安全响应，改 payload/旧版本冲突。配置值、秘密明文、密文及指纹均不进入审计，审计只记录字段名称和安全状态；GET 不回显密钥。

API/Worker 分别持有独立卷中的 X25519 私钥，数据库仅登记公钥。配置密钥以临时 X25519 ECDH、HKDF-SHA256、AES-GCM 封装并绑定字段名/收件公钥；API 能封装提交的 Worker 发奖密钥，但不能解密已保存的 Worker 密钥。角色 SELECT 排除另一角色密文，运行时投影也删除另一角色的业务秘密。跨用途 current/previous 与随机种子通过内部指纹拒绝重复；随机种子不开放编辑，API/Worker 必须一致。首次生成配置加密私钥不修改任何已有业务 HMAC 或随机种子。

API 每次请求、Worker 每轮相关任务读取一个一致配置快照；缓存的路由仅在有效配置改变时重建，在途操作保留原快照。数据库/解密/校验失败时 fail closed，不使用旧快照继续放行；历史奖励、随机结果、Outbox 与 source_ref 均不因配置变化改写。财务工具使用 Worker 同一配置读取路径。新 API/Worker 均须完成迁移与升级，不能以旧版 Worker 的静态环境代替新配置。

网页仅管理运行开关、展示换算和对接密钥。Period 经济参数、Budget、随机种子、数据源连接与主计费仍保持原边界。Worker 首次登记把有效 new-api 目标固化到公共配置，登记后或出现任何 Grant 后，普通设置表单不可改变资金目标；更改 `.env` 也不会绕过已固化目标。不确定奖励的目标迁移必须经专门维护方案，不能用配置热更新切换资金事实源。

设置迁移为 `00012`。更新备份包括数据库、原部署配置及两个私钥卷；缺失原私钥且存在密文时拒绝启动/解密，不生成新业务密钥或清空密文自愈。同角色多实例共享原私钥和环境基线；API/Worker 之间不得共享私钥。恢复时数据库和密钥必须配套，具体步骤见 `METAR_ADMIN_SETTINGS.md`。

## 43. 正式版本与部署身份

博客重建前将已有静态目录移入 `.data/static-builds/` 保留原 inode 与内容，运行中的网关可继续读取旧版本；构建失败不删除仍可能被挂载的静态目录。新产物与服务准备好后，升级脚本重建网关并检查可读性和健康状态。旧静态目录只能在确认没有容器使用后清理，不能因下一次构建或失败自动删除。

`VERSION`、Git 标签、Release 清单及三个运行镜像的版本/提交共同确定部署身份。标签发布先复用完整 CI，再从指定提交构建并下载校验全部附件，最后公开 Release；发布流程不接触生产凭据。指定版本升级必须核对已发布清单、快进到完全相同的提交、备份数据库及两个私钥卷，并在健康检查后验证 API、Worker、Forum 的实际镜像身份。失败不自动回滚数据库或轮换密钥；运行配置与业务状态不随版本发布改变。流程与恢复要求见 `RELEASE.md`。


## 44. 社区普通路径与讨论列表

METAR 使用 History 路由，首页与 `/latest` 提供同一个真实讨论列表；旧 `/#/discover`、`/#/questions` 链接转换到 `/latest`，其余已登记 Hash 路径转换到对应普通路径并保留 query。转换使用 replaceState，站内导航使用 pushState，浏览器前进后退重新读取当前路径。只拦截带内部路由标记的同源普通点击，修饰键、新窗口、下载和原生 Answer 链接保留浏览器行为。

Nginx 只对白名单中的壳层页面返回静态首页，不使用全站 SPA fallback。原生 `/questions`、`/questions/ask`、`/questions/:id`、`/notifications`、`/users/*`、`/answer/*`、SSO callback、博客与静态资源仍归各自处理器。壳层收藏与通知使用 `/me/bookmarks`、`/me/notifications`，避免与原生路径相互覆盖。缺少静态构建仍返回 404；认证、Cookie 过滤、HMAC 签名、CSP 和同源写保护不因路由改变而放宽。

讨论行只投影 Answer 返回的标题、标签、作者、最近参与者、回答数、浏览量和活动时间。筛选与分页由 Answer API 完成，不推断未读数或伪造参与者；没有内容、错误与访客状态仍显式展示。首页移除介绍统计卡和右侧推荐卡，不更改帖子、绑定、奖励或账本。

路由归属唯一配置为 `metar-frontend/shared/routes.json`，生成 METAR/Answer 两端路由策略与 Nginx 白名单；`make test-community` 拒绝未同步产物。Answer 插件在原生 History 导航提交后识别 METAR 页面，并用完整页面加载接入其处理器（根路径规范到 `/latest`）。同样覆盖登录回跳、前进后退与 bfcache 恢复；不拦截提交前的点击，不绕过编辑器未保存提醒，不修改原生 history state，不接管 API、callback、发帖及尚未迁移的账号页面。query/fragment 随跳转保留，重载使用 replace 避免添加无用历史项。

社区、Answer 插件和 VitePress 共用 `shared/theme-tokens.css` 配色与 `_metar_theme` 浏览器偏好。原生页头扩展只提供品牌、导航与主题控件，保留原生页面、表单、权限菜单和编辑器；该层不写用户/站点配置、不创建会话，也不改变 API 或内容事实源。视觉一致与功能迁移分别验收，不通过重定向隐藏尚未覆盖的原生能力。

## 45. 搜索抓取与公开内容投影

公开静态入口构建基础 HTML；Answer 仍是问答可见性与正文事实源，原生详情 SSR、canonical 和动态 sitemap 不被静态快照替代。壳层详情通过 canonical 指向原生帖子，网页错误标记 noindex。站点入口与博客 sitemap 分开生成并由 robots 声明，博客未知地址返回 404。网关按原始请求地址给个人、通知、绑定、管理、奖励和 API 路由加 noindex，不能在 try_files 改写 URI 后误判。认证、Cookie/HMAC 边界和私有数据权限保持不变；不向爬虫绕过权限，不依赖 User-Agent 区分内容。

## 46. 统一入口与本地注册适配

内容互动、登录注册、密码找回、资料设置和社区管理继续复用 Answer 原生页面，静态壳层的旧入口只做同源导航交接。公开资料与当前会话的私人入口分开，不接受 URL 中的 user_id 作为可信身份。插件对 Answer 1.7.1 UserCenter 注册 URL 的前端适配仅在 Meta Pulse 启用且本地账号模式开启时生效；不改后端注册开关、验证、身份、角色或会话。此兼容层不得推广为绕过原生权限守卫的通用重定向机制。


### 管理员网页创建经济周期

METAR `/admin/pulse` 提供贡献倍率、每张券贡献度与完整奖池的创建表单。浏览器仅访问固定同源 `/metar/api/admin/pulse/periods`（GET / PUT），经 Answer admin session、正常/激活/管理员身份实时复核和同源校验，再由独立 admin 签名调用 Pulse `/v1/internal/admin/periods`；actor 来自签名身份，禁止浏览器声明。新周期固定 10 天、Asia/Shanghai、verified-paid-v1，创建即冻结；不提供编辑已启用周期的接口，也不变更运行/发奖开关。

网页创建复用 `PeriodCreateService`，`period_create:{actor_type}:{actor_id}` + Idempotency-Key 对规范化完整命令做 SHA-256：同 key 同内容返回首次结果，不同内容冲突。Period、Economics、Reward Definition、Budget、Audit 和幂等响应在同一事务提交。CLI 与网页创建均先锁定 `pulse_idempotency` 的永久 `period_create_lock/global` 行后检查周期重叠，防止不同 key 并发创建相交时间段；此行仅作为数据库事务互斥，不作为账本事实。创建审计包含规则倍率、门槛、奖池、预算、操作者、原因与请求编号。失败回滚，响应丢失复用原 key 恢复；无历史账本重写。

## 社区经验（EXP）事实边界

社区成长由 Answer 插件独立维护，详见 [COMMUNITY_EXPERIENCE.md](COMMUNITY_EXPERIENCE.md)。Answer 本地账号是唯一社区身份；EXP 不产生 Pulse contribution/ticket/new-api 余额，不替代 Answer reputation 或治理角色。等级阈值为 0/150/600/1750/4500/10000/22500/45000/90000。

Answer 库中的 `metar_exp_ledger` 是 EXP 事实源、`metar_exp_account` 是快照；事件、规则版本、通知、采集游标和审计存于插件自有表。每次加减与事件状态、账户快照、升级通知在一个 MySQL 事务内提交，账户行锁串行化并发额度检查。历史仅追加，撤销采用负流水；每日限额按正向原始流水累计，不被撤销返还。

幂等身份：签到 `user+Asia/Shanghai日期`，问题/回答/采纳 `kind+object`，点赞 `object+actor`，精选 `object`，脉冲经验 `grant_id`；管理员请求 `actor+request_key`，同 key 不同 payload 冲突。撤销墓碑禁止迟到成功响应恢复奖励。来源记录在首次采集时冻结规则，后续版本变更不能增加该来源奖励。

经验路由使用 Answer 官方认证组，从服务器会话读取本人身份，禁止 query token、客户端指定领取身份或金额；写请求校验同源。成长管理额外实时读取本地角色和账号状态。公开投影仅含等级与装扮，不暴露本人账本。经验池独立初始化和降级，Pulse 或 EXP 故障不得阻断论坛本地功能。

### Pulse → Answer 经验奖励交付

新增非货币奖项与独立预算类型 `community_exp`，不改动 contribution/ticket 的产生条件。额度奖项继续使用 loyalty 预算和 new-api Benefit 结算；经验预算不参与金额合计。抽奖按固定顺序锁定 loyalty、community_exp 预算，任一启用奖项所属预算不足以覆盖其最大单次金额时拒绝抽奖，不改变冻结的随机分布。

复用每个 grant 唯一的 settlement outbox，但经验使用 `community_pending → community_delivered` 状态，额度 Worker 不领取；Shadow Mode 仍为 shadow，不交付经验。GET `/v1/internal/me/experience` 和 POST `/v1/internal/me/experience/ack` 仅允许独立 community-bff 角色，身份来自验签 principal。查询返回至多 20 条未确认交付，不用自增 ID 游标，以免跳过晚提交的事务。

Answer 验证受保护的一对一绑定后，先提交 EXP 账本，再 ACK。ACK 在 Pulse 同一事务内锁 grant、outbox、预算，把 pending grant 置 settled、预留转已发、outbox 置 delivered；所有状态读取为锁定当前读，重复确认不重复结算。丢响应和进程退出由下次本人访问重试相同 grant 恢复，经验事实仍在 Answer。单次请求最多三批，成长页限时两秒；Pulse 不可用不阻断本地经验。

独立 admin 角色 POST `/v1/internal/admin/experience/reverse`，幂等范围 `experience_reverse:actor`，同 key 同 payload 返回原结果，不同 payload 冲突。只对 community_exp 允许 pending/settled → reversed；同事务释放对应 EXP 预算、重置待交付、追加审计。Answer 下次同步追加 reversal 或墓碑，再确认 reversed，迟到的 pending ACK 冲突。该流程不返券、不调用 new-api 撤销接口，不保证离线账号即时扣回。

无需新增 Pulse 表；仅扩展既有奖项/预算类型和 outbox 状态。升级需同时更新 Pulse API/Worker、Answer 插件与静态资源；启用经验奖池后不可把 API/Worker 回退到不认识经验类型的旧版，需先暂停新抽奖并清理待交付，再执行经过验证的整体回退。
