# Meta Pulse 实施计划

本文档是 `docs/ARCHITECTURE.md`（系统基线）与 `docs/COMMUNITY.md`（社区层）之上的**工程执行计划**：目录职责、里程碑出口标准、跨仓库依赖与待决策清单。

架构文档回答「系统边界是什么」，本文档回答「按什么顺序建、每步做完的标志是什么」。

## 1. 当前状态

```text
✅ 架构冻结          ARCHITECTURE.md / COMMUNITY.md / AGENTS.md
✅ Monorepo 骨架     services/{pulse,forum,forum-plugin} + sites/blog + deploy
✅ UserCenter 插件   身份委托 + 等级徽章 + 签名校验（41 项测试）
⬜ M0 地基           migration + 建表 + GORM/Redis 接入 + 装配
⬜ M1..M6            见第 4 节
```

`services/pulse/` 目前只有 `config.go` 与三个 main 空壳；`go.mod` 无依赖。**M0 是所有后续工作的前置。**

## 2. 目录职责

架构文档给出了目录树，本节补充两个它未定义、但决定代码可测性的抽象。

```text
services/pulse/internal/
├── app/              手工依赖装配：app.NewAPI(cfg) / app.NewWorker(cfg)
├── config/           环境变量加载（已存在）
├── domain/           纯 Go，零框架依赖，全部可单测
│   ├── money/          Milli / Bps 定点类型
│   ├── period/         draft→active→settling→closed 状态机
│   ├── economics/      规则匹配 + 贡献值计算
│   ├── ledger/         Entry / Operation / Asset 与记账规则
│   ├── ticket/         entitlement = floor(net/threshold) + debt
│   ├── reward/         权重表 + random_value → 选奖（纯函数）
│   ├── budget/         预占 / 释放 / hard cap
│   ├── experiment/     SHA256 bucket 分组
│   ├── level/          lifetime_contribution → 等级
│   ├── idem/           幂等 key 构造 + payload fingerprint
│   └── random/         版本化 HMAC 派生
├── ports/            ★ service 依赖的接口
├── service/          事务编排：ingest / pulse / settlement / periodclose / query / admin / content
├── store/mysql/      GORM 实现 ports，不含业务判断
├── adapter/newapi/
│   ├── logsource/      只读 LOG_DB 游标 + Mapper
│   └── benefit/        Internal Benefit API 客户端
├── adapter/forum/    只读论坛 DB 游标（Content Candidate）
├── transport/http/   router + middleware + handler + dto
├── job/              worker jobs + Redis lease 调度
├── security/         HMAC 校验、nonce
└── observability/    metrics / log / trace
```

### 两个补充抽象

**`ports/` 包.** 架构文档定义了依赖方向 `service → domain → interfaces`，但未指定接口定义位置。放在 `ports/` 使 service 完全不依赖 GORM，测试可用内存实现驱动——这是 AGENTS.md 第 11 节那批回归测试能否写得动的前提。

**`UnitOfWork` 抽象.** 事务边界必须由 service 控制：Ingest 与 Pulse Action 都要求「一个事务跨 6 张表」。若 store 层自行开事务，必然出现半记账状态。

```go
type UnitOfWork interface {
    Do(ctx context.Context, fn func(Repos) error) error
}
```

### `domain/money` 使用 distinct type

```go
type Milli int64  // 1 contribution = 1000 milli
type Bps   int32  // 10000 = 1.00x
```

不用裸 `int64`。这是把 AGENTS.md 第 14 条（禁止浮点做账）交给编译器执行，而非依赖 code review。

## 3. 技术选型

| 项 | 选择 | 理由 |
|---|---|---|
| Migration | **goose** | 纯 SQL 文件、可嵌入二进制、支持 up/down；Pulse 需要审计友好的 DDL，ORM automigrate 不可接受 |
| ORM | GORM | 架构文档已定 |
| HTTP | Gin | 架构文档已定；与 Answer 一致 |
| 测试 | 标准库 + testcontainers | Ledger 不变量必须对真实 MySQL 验证，唯一约束在内存实现里测不出来 |
| Metrics | prometheus/client_golang | 架构文档第 29 节指标均为 Prometheus 风格 |

## 4. 里程碑

推进原则：**先把账算对并可见，再开始发钱。**

### M0｜地基

- goose 接入 + 16 张核心表 DDL（含全部唯一约束）
- GORM / Redis 连接与健康检查
- `app/` 装配、`ports/` 接口骨架、`UnitOfWork` 实现
- Gin 路由骨架替换标准库 mux
- 结构化日志 + Prometheus registry
- `.env.example` 与 `config.go` 对齐校验（缺失必填项启动即失败）

**出口**：`docker compose up` 全部服务健康；16 张表建成；`/healthz` 与 `/readyz` 反映真实依赖状态（DB/Redis 断开时 readyz 失败）。

### M1｜记账内核

- `domain/money` 定点类型
- `domain/ledger` 记账规则（append-only、reversal/adjustment）
- `pulse_ledger_entry` / `pulse_account` 读写
- `tools/ledger-check` 对账工具

**出口**：`SUM(ledger.amount) = account.balance` 不变量测试通过；账本可从 entry 完整重建 account；历史金额 UPDATE 被 store 层拒绝。

### M2｜Ingest 链路

- LOG_DB 只读游标 + `pulse_worker_cursor`
- Usage Mapper（consume / refund / correction）
- `domain/economics` 规则匹配与贡献值计算
- 单事务：UsageEvent → Contribution Ledger → Account → Ticket Entitlement → Ticket Ledger → Ticket Account → UserPeriodStat
- `pulse_ingest_conflict` 冲突落库
- `tools/backfill` 复用同一 Mapper
- `domain/level` 等级计算 + `pulse_user_level`

**出口**：同一 usage 重放 100 次只记一次账；同 ID 不同 payload 进入 conflict 表且不静默覆盖；历史数据可全量回放；`GET /v1/internal/users/:id/profile` 可用（论坛插件依赖此接口）。

### M2.5｜Backtest

- `tools/backtest`：用真实历史日志跑经济模型
- 标定贡献倍率、Ticket 阈值、Budget 上限

**出口**：得到真实的「活动成本 / 贡献毛利」占比数字。**M4 的经济参数必须以此为依据，而非拍脑袋。**

### M3｜只读 API + UI

- Signed BFF 鉴权中间件（HMAC + nonce + 时间窗）
- `GET /v1/period/current`、`/v1/me/summary`、`/v1/me/ledger`
- new-api `/console/pulse` 只读页面

**出口**：可灰度上线。用户能看到贡献值、券数与等级，但尚不能开启脉冲。此时经济模型已用线上真实数据二次验证。

### M4｜Reward 内核

- `pulse_idempotency` + Idempotency-Key 中间件
- `domain/random` 版本化 HMAC 确定性随机
- `domain/budget` 预占与 hard cap
- Pulse Action 单事务：Idempotency → Ticket Spend → Budget Reservation → Reward Grant → Settlement Outbox
- Grant 停留在 `pending`，暂不结算

**出口**：同一 Action 重放 100 次只产生一个 Grant 与一个随机值；并发 Pulse 不超扣券、不超预算；DB commit 后 HTTP 响应丢失可用同 key 恢复。

### M5｜Settlement

- new-api Internal Benefit API（**跨仓库，见第 5 节**）
- Outbox worker + 指数退避重试
- timeout 时先 `GET Benefit(source_ref)` 再决定重试
- Benefit Reconciliation job

**出口**：同一 Benefit 重放 100 次只增加一次额度；Benefit 成功但 Pulse timeout 可通过 Query 自愈；禁止换 source_ref 的行为有测试守护。

### M6｜周期与运营

- Period Close 可重入状态机
- Period Reward（`period_reward:{period_id}:{user_id}`）
- `domain/experiment` Holdout 分组
- Admin API + `pulse_audit_log`
- `pulse_metric_daily` 固化 + 告警接入

**出口**：同一 Period Close 重跑不重复发放；中途崩溃可续跑；Holdout 分组稳定可复现。

### M7｜内容奖励（依赖 M5）

- 论坛 DB 只读游标 → `pulse_content_candidate`
- Admin 审核界面（approve + 档位 + reason → audit log）
- `budget_type = content_reward` 独立预算线
- 付费门槛校验 + 双层限额
- `content_award:{type}:{id}:{version}` 幂等
- 撤销路径：`settled → reversed` + Benefit rollback

**出口**：防刷四道闸全部生效；内容奖励不产生 contribution/ticket；不计入贡献毛利占比分母。

## 5. 跨仓库依赖（new-api）

以下改动在 new-api 仓库完成，**是 Pulse 的关键路径**：

| 改动 | 被谁依赖 | 优先级 |
|---|---|---|
| Signed BFF 转发（HMAC 头） | M3 | 高 |
| **Login Ticket 签发** | 论坛插件 | **高，见下** |
| Internal Benefit API（grant/query/rollback） | M5 | 高 |
| `/console/pulse` 前端页面 | M3 | 中 |
| BenefitChangeRecord 去重语义 | M5 | 高 |

### Login Ticket 签发（P0）

论坛插件的 `LoginCallback` 通过**浏览器重定向**到达，query 参数完全由攻击者控制。new-api 必须签发单次有效的签名 ticket：

```text
GET /forum/answer/api/v1/user-center/login/callback
    ?user_id=<id>
    &username=<name>
    &display_name=<name>
    &email=<email>
    &avatar=<url>
    &timestamp=<unix>
    &nonce=<random>
    &signature=<hmac>
```

签名算法（与 `services/forum-plugin/user-center-pulse/ticket.go` 一致）：

```text
payload   = user_id \n username \n display_name \n email \n avatar \n timestamp \n nonce
signature = hex(HMAC-SHA256(shared_secret, payload))
```

约束：

- 字段以换行连接，防止字段边界歧义导致签名移植；
- TTL 2 分钟，双向校验（过期与未来时间戳均拒绝）；
- nonce 单次有效，插件侧维护重放缓存；
- `shared_secret` 与 `PULSE_SERVICE_HMAC_SECRET` 同源，不进仓库。

**未实现此接口前，论坛不得开放公网访问。**

## 6. 待决策事项

以下问题影响表结构或经济模型，需在对应里程碑前确认。

### D1｜贡献毛利的事实源（影响 M0 表结构）

new-api `logs` 表只有对用户收费的 `quota`，**没有上游供应商成本**。因此「贡献毛利」在系统内无事实源，只能由人工维护的倍率表近似。

含义：

- `pulse_economics_rule` 实质是**人工维护的毛利率表**，必须版本化 + 可审计；
- 「活动成本/贡献毛利 8%–12%」在 M2.5 回放之前是估计值。

**待定**：是否在 new-api 侧补充渠道成本字段。若补充，Economics 可基于真实毛利；若不补充，需接受近似并在文档中明示。

### D2｜Period 时间边界（影响 M0 表结构）

未定义：

- 全局统一周期，还是按用户注册时间滚动？
- 起止时区？
- 跨周期 usage 归属按 log `created_at` 还是 ingest 时间？

直接决定 `pulse_user_period_stat` 与 watermark 设计。**建议：全局统一周期 + UTC+8 + 按 log `created_at` 归属**，理由是运营口径简单、水位线易于推进。

### D3｜Ticket Debt 的产品表述（影响 M3 UI）

技术方案已定（欠券、新券先抵债、显示 `max(balance, 0)`），但用户看到券数不涨会产生客服问询。需产品侧决定展示方式。

### D4｜内容奖励档位（影响 M7，依赖 M5 观察窗口）

兑换档位、付费门槛、单周期与单日限额的具体数值，应在论坛以纯荣誉模式运行一段时间、观察真实内容质量与灌水比例后确定。

## 7. 测试策略

AGENTS.md 第 11 节列出的回归场景必须长期保留。落位如下：

| 场景 | 里程碑 | 方式 |
|---|---|---|
| 同一 Usage 重放 100 次只记一次账 | M2 | testcontainers + 真实唯一约束 |
| 同一 Action 重放 100 次只产生一个 Reward | M4 | testcontainers |
| 同一 Benefit 重放 100 次只加一次额度 | M5 | fake Benefit server + 幂等断言 |
| 同一 Period Close 重跑不重复发放 | M6 | testcontainers |
| 并发 Pulse 不超扣券、不超 Budget | M4 | 并发测试 + 行锁 |
| DB commit 后响应丢失可恢复 | M4 | 注入式故障测试 |
| Benefit 成功但 Pulse timeout 可恢复 | M5 | fake server 延迟注入 |
| Ledger 与 Account 可重建可对账 | M1 | 属性测试 |

**唯一约束必须对真实 MySQL 验证。** 内存实现测不出 `UNIQUE(source_system, source_event_id)` 是否真的建了。

## 8. 并行推进

```text
Track A｜Pulse    M0 → M1 → M2 → M2.5 → M3 → M4 → M5 → M6 → M7
Track B｜博客     立即：VitePress + 种子内容（不阻塞任何人）
Track C｜论坛     立即：Answer 部署 + zh-CN
                     └─ 插件接入（依赖 M2 的 profile 接口 + new-api Login Ticket）
                          └─ 内容奖励（依赖 M5 + M7）
Track D｜new-api  Login Ticket（P0）→ Signed BFF → Benefit API → /console/pulse
```

论坛在 M5 之前以**纯荣誉模式**运行：徽章、头衔、精华，不涉及 quota。这既规避了未验证经济模型下的成本风险，也提供了观察窗口用于确定 D4 的档位。

## 9. 工程约束提醒

- 仓库根不是 Go module，`./...` 无法跨模块解析；所有 Go 命令须显式列模块路径（见 Makefile）；
- 插件当前编译 against Answer v1.7.1（v2.0.2 的 `go.mod` 模块路径未加 `/v2` 后缀，Go 工具链无法解析）；
- Answer 每次升级需重新构建镜像（Go 静态插件机制）；
- 任何改变系统边界、事实源、幂等语义、状态机或认证边界的改动，必须同步更新 `ARCHITECTURE.md`（AGENTS.md 第 12 节）。
