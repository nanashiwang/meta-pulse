# Meta Pulse 实施计划

本文档是 `docs/ARCHITECTURE.md`（系统基线）与 `docs/COMMUNITY.md`（社区层）之上的工程执行清单。目标不是先把页面做出来，而是先打通身份、日志、记账、预算和结算边界，再逐步开放奖励。

```text
先定义跨仓库契约 → 再建 Pulse 地基 → 再接真实日志 → 再展示 → 再发奖 → 最后做内容奖励
```

## 1. 当前状态

```text
✅ 架构文档       ARCHITECTURE.md / COMMUNITY.md / AGENTS.md
✅ Monorepo 骨架  services/{pulse,forum,forum-plugin} + sites/blog + deploy
✅ 论坛插件       UserCenter、等级展示、Ticket 验签代码与测试已存在
✅ new-api 调研   已确认登录、2FA、LOG_DB、BenefitChangeRecord、额度发放边界
🟡 P0 接入契约     Pulse 侧签名/Nonce 骨架已落地；new-api SSO / BFF / Benefit API 待实现
🟡 M0 地基         配置、Gin、健康检查、GORM/Redis、Goose migration、指标已落地；真实环境验收待完成
✅ M1 Ledger 记账内核    领域账本、账户快照、幂等冲突、重建与 ledger-check 已落地
⬜ M2-M7          见下文
```

本次已启动 M0 第一批：`services/pulse/` 已从 HTTP 桩升级为可装配的 API/Worker 基础，新增 16 张核心表 migration、依赖健康检查、结构化日志、Prometheus registry、UnitOfWork 事务边界和服务签名校验。`docs/OVERVIEW.md` 为未跟踪文件，本计划不依赖它，也不覆盖或删除它。

## 2. 已确认的跨仓库事实

### 2.1 身份事实源

new-api 的登录入口为：

```text
POST /api/user/login
POST /api/user/login/2fa（启用 2FA 时）
```

登录完成后由 new-api session Cookie 保存用户身份；受保护 API 同时要求 `New-Api-User`，且该 ID 必须与 session 中的用户一致。

YuanHeng Desktop 已采用：

```text
登录 → 捕获 Set-Cookie → 处理 pending 2FA session → 保存正式 session
     → Cookie + New-Api-User 查询用户/模型 → 创建或复用本机 API Token
```

结论：Pulse 不实现登录，不接收密码、Cookie 或客户端自报的可信 `user_id`。

### 2.2 Usage 事实源

new-api 的 `LOG_DB` 中：

```text
LogTypeConsume = 2
LogTypeRefund  = 6
Log.id         = 稳定日志主键
Log.user_id    = 用户 ID
Log.quota      = 用户侧计费额度
```

Pulse 使用独立只读账号读取 `LOG_SQL_DSN` 指向的日志库。`quota` 是第一阶段的 Eligible Paid Usage；Provider 实际成本目前不是日志事实源，必须在回测阶段明确采用“估算毛利”还是扩展 new-api 记录成本快照。

普通消费、异步任务退款、差额结算的关联字段并不完全统一，不能仅凭 `refund` 类型推断因果关系；必须在 M2 前完成 Mapper 契约与异常样本回放。

### 2.3 Benefit 事实边界

new-api 已有 `BenefitChangeRecord` 唯一索引和 `GrantUserQuotaTx`，但现有重复记录处理只吞掉 duplicate，未比较请求 payload。因此 Pulse 专用 Benefit API 必须补齐：

```text
同 key + 同 payload    → 返回第一次结果
同 key + 不同 payload  → conflict
```

Pulse 奖励必须通过 new-api Internal Benefit API 发放，建议 `transferable_quota = 0`，禁止直接写 `users.quota`。

## 3. 跨仓库目标链路

### 3.1 用户控制台 / YuanHeng

```text
浏览器或 YuanHeng 隔离 WebView
        ↓ session Cookie（仅发给 new-api）
new-api Session Auth / Signed BFF
        ↓ 服务端派生 user_id，并签名调用
Meta Pulse 用户 API
```

浏览器和桌面端都不直接访问 Pulse 原始 API，也不直接向 Pulse 传 session Cookie。

### 3.2 论坛登录

```text
Answer
  → new-api /forum/sso/start
  → 未登录：/login?next=/forum/sso/start
  → 已登录：new-api 从 session 读取用户并签发短期单次 Login Ticket
  → 302 到固定 Answer callback
  → 插件验签、消费 nonce、建立论坛会话
```

Ticket 的用户字段必须由 new-api 服务端读取，不能由浏览器提交后直接签名。Callback 地址使用固定配置或严格 allowlist，禁止开放重定向。Nonce 必须使用跨实例可共享的原子存储。

### 3.3 奖励结算

```text
Pulse DB 事务：Ticket Spend + Budget Reservation + Reward Grant + Outbox
        ↓
Pulse Worker 调用 new-api Benefit API
        ↓ timeout 时先 Query(source_ref)，不得换 source_ref
new-api 在自己的事务内变更用户额度并写 BenefitChangeRecord
```

## 4. 目录职责

```text
services/pulse/internal/
├── app/              手工依赖装配：API / Worker / Tool
├── config/           环境变量加载与启动校验
├── domain/           纯 Go 领域逻辑，零 Gin/GORM/Redis/new-api 依赖
│   ├── money/        Milli / Bps 定点类型
│   ├── period/       draft → active → settling → closed
│   ├── economics/    规则匹配与贡献计算
│   ├── ledger/       append-only 分录、冲正、调整
│   ├── ticket/       产券、欠券、消费、过期
│   ├── reward/       权重表与确定性选奖
│   ├── budget/       预占、释放、hard cap
│   ├── experiment/   稳定分组
│   ├── level/        终身等级
│   ├── idem/         幂等键与 payload fingerprint
│   └── random/       版本化 HMAC 随机
├── ports/            service 所依赖的接口
├── service/          事务编排，不直接处理 HTTP
├── store/mysql/      GORM 持久化，不复制业务判断
├── adapter/newapi/   LOG_DB 只读游标与 Benefit API 客户端
├── adapter/forum/    论坛 DB 只读游标
├── transport/http/   Gin 路由、鉴权、handler、DTO
├── job/              Worker 任务与租约
├── security/         HMAC、时间窗、Nonce、allowlist
└── observability/    日志、指标、追踪
```

事务边界由 service 控制，统一使用：

```go
type UnitOfWork interface {
    Do(ctx context.Context, fn func(Repos) error) error
}
```

## 5. 里程碑与出口标准

### P0｜跨仓库接入契约与安全门禁

**负责人：new-api + Meta Pulse + 论坛插件 + 部署配置。M0 前置。**

- [ ] new-api 实现 `/forum/sso/start`，使用 `next` 语义，禁止开放重定向；
- [ ] new-api 从 session 读取用户资料并签发 Login Ticket；
- [ ] Ticket 具备 TTL、未来时间拒绝、HMAC 验签、单次 nonce、callback allowlist；
- [ ] 论坛插件将登录入口切换到 SSO Bridge；
- [ ] Nonce 改为 Redis 或数据库原子消费，不能只依赖进程内存；
- [ ] 确定论坛域名与 Cookie 隔离方案；同域临时方案必须在 Nginx 剥离 new-api session；
- [ ] new-api 实现 Pulse Signed BFF，浏览器访问 new-api，用户 ID 由 session 派生；
- [ ] 更新 Nginx/网关：对外 `/api/pulse/*` 只进入 new-api BFF，Pulse 原始 API 不暴露；
- [ ] 统一服务签名覆盖 method、path、user、timestamp、nonce、body hash；
- [ ] 定义 Benefit API 的 grant/query/rollback、payload fingerprint、conflict 和错误码；
- [ ] 定义 Usage Mapper：consume、refund、correction、异步 task 退款、差额结算；
- [ ] 生产密钥不进入 Git，支持轮换和 fail closed；
- [x] Pulse 侧实现规范化 `method/path/user/timestamp/nonce/body_hash` 验签、时间窗和重放拒绝，生产 Nonce 适配 Redis 原子 `SETNX`。

**出口：**登录、2FA、论坛 SSO、Ticket 重放、Cookie 隔离、BFF 越权和签名重放测试通过；P0 未完成前论坛不得开放公网，Pulse 奖励不得上线。

### M0｜Pulse 地基

- [x] Goose SQL migration 与 16 张核心表；
- [x] MySQL、Redis 连接与健康检查；
- [x] GORM store、ports、UnitOfWork、手工依赖装配；
- [x] Gin 路由骨架；
- [x] 结构化日志、Prometheus registry；
- [x] `.env.example` 与 `config.go` 一致，必填项缺失时启动失败；
- [ ] 验证 LOG_DB 使用只读账号、Pulse 无 new-api 主库写权限；
- [ ] 明确 Period 时间边界、时区、日志归属时间和水位线策略；
- [ ] 使用真实 MySQL/Redis Compose 完成迁移与 `/readyz` 验收。

**出口：**Compose 服务健康；16 张表迁移完成；DB/Redis 断开时 `readyz` 失败；服务重启不影响 new-api。

### M1｜Ledger 记账内核

- [x] `domain/money` 使用 `Milli` / `Bps`，禁止 float 做账；
- [x] Contribution / Ticket Ledger append-only；
- [x] reversal / adjustment 只能追加，历史金额禁止 UPDATE；
- [x] Account 作为派生快照；
- [x] 账本重建与 `ledger-check` 工具；
- [x] Ledger、Account、幂等键的数据库唯一约束。

**出口：**`SUM(ledger.amount) = account.balance`；账户可由分录完整重建；历史金额 UPDATE 被拒绝。

### M2｜Usage Ingest 与等级

- [ ] LOG_DB 只读 cursor、批量读取、断点续跑；
- [ ] 共用 Mapper 实现实时 Ingest 和 Backfill；
- [ ] `source_system + source_event_id` 幂等；
- [ ] payload hash 不一致写 `pulse_ingest_conflict`，不静默覆盖；
- [ ] 单事务完成 UsageEvent、Contribution Ledger、Account、Ticket Entitlement、Ticket Ledger、UserPeriodStat；
- [ ] Economics Rule 保存命中规则、倍率、eligibility、版本快照；
- [ ] 处理异步任务退款与差额结算；不确定关联时进入人工对账队列；
- [ ] 终身净贡献等级与只读 profile API；
- [ ] `tools/backfill` 支持 dry-run、范围、报告和重复执行。

**出口：**同一 Usage 重放 100 次只产生一次业务记账；同 ID 不同 payload 进入 conflict；历史日志可回放；论坛 profile API 可用。

### M2.5｜真实日志回测

- [ ] 用真实 LOG_DB 样本回放消费、退款、task、结算失败数据；
- [ ] 输出用户覆盖率、贡献值、券产出、异常率和数据缺口；
- [ ] 对比人工倍率、模型倍率、渠道倍率的结果；
- [ ] 标定 Ticket Threshold、Reward 权重、预算上限；
- [ ] 决定采用估算毛利，还是让 new-api 增加不可变成本快照。

**出口：**得到可审计的活动成本 / 贡献毛利数字；M4 参数必须引用回测报告。

### M3｜只读产品入口

- [ ] new-api `/api/pulse/*` BFF；
- [ ] new-api `/console/pulse` 页面与路由；
- [ ] 当前 Period、贡献值、券、等级、Ledger、奖励历史；
- [ ] YuanHeng 通过隔离 WebView 打开 `/console/pulse`；
- [ ] YuanHeng session/API Token 改用 OS 安全存储，用户名密码不落盘；
- [ ] 论坛展示 Pulse 等级，Pulse 故障时降级；
- [ ] 只读灰度、无 Pulse Action。

**出口：**用户能看到数据；浏览器、桌面端、论坛均不能越过 new-api 身份边界；Pulse 故障不影响登录、模型调用、充值和论坛浏览。

### M4｜Reward 内核与 Shadow Mode

- [ ] API mutation 必须使用 Idempotency-Key；
- [ ] 版本化 HMAC 确定性随机，保存 random value 和 config version；
- [ ] 一个 Action 只能消费一张 Ticket、生成一个 Grant 和一个随机结果；
- [ ] Budget reservation 与 hard cap；
- [ ] 单事务写入 Ticket Spend、Budget Reservation、Reward Grant、Settlement Outbox；
- [ ] 先只生成 `pending` Grant，不真实发额度；
- [ ] 并发、响应丢失、DB 重启测试。

**出口：**同一 Action 重放 100 次只有一个 Grant；并发不超扣券、不超预算；网络失败不重新随机。

### M5｜Settlement 与 Benefit API

- [ ] new-api Grant / Query / Rollback 内部接口；
- [ ] HMAC 服务认证、时间窗、nonce、来源绑定；
- [ ] Benefit payload fingerprint 和 conflict；
- [ ] 使用 `GrantUserQuotaTx`，奖励额度 `transferable_quota=0`；
- [ ] Outbox 指数退避、dead 状态、人工重试；
- [ ] timeout 必须先 Query 原 `source_ref`；
- [ ] Benefit Reconciliation；
- [ ] rollback 只追加 reversal，行为可审计。

**出口：**同一 Benefit 重放 100 次只到账一次；不同 payload 进入 conflict；new-api 成功但 Pulse 超时可恢复；禁止换 source_ref 有测试保护。

### M6｜周期与运营

- [ ] Period Close：active → settling → closed，可重入；
- [ ] Watermark 确认、Ledger/Account 对账、周期奖励、券过期；
- [ ] Period Reward 使用稳定 action ID；
- [ ] Holdout 稳定分组；
- [ ] Admin、Audit Log、冲突处理、人工财务调整；
- [ ] 日指标、告警、Settlement 堆积和预算预警；
- [ ] close 中断后的恢复测试。

**出口：**Period Close 重跑不重复发放；中途崩溃可继续；预算、账本、奖励和 Benefit 可对账。

### M7｜内容奖励

- [ ] 论坛 DB 只读游标与 Content Candidate；
- [ ] 人工审核、档位、reason、Audit Log；
- [ ] 独立 `content_reward` budget；
- [ ] 付费门槛、单用户上限、全站日上限；
- [ ] `content_award:{type}:{id}:{version}` 幂等；
- [ ] 删除/抄袭后的 settled → reversed；
- [ ] 内容不产生 contribution 或 ticket。

**出口：**四道防刷闸全部生效；内容奖励与忠诚度预算、账本、贡献值完全隔离。

## 6. 跨仓库任务清单

### new-api

- [ ] Forum SSO Bridge 与 Login Ticket；
- [ ] Pulse BFF，服务端派生 user ID；
- [ ] Pulse Internal Benefit API；
- [ ] Benefit 重复请求 payload 比较与 conflict；
- [ ] `/console/pulse` 路由、页面和导航入口；
- [ ] 明确 `LOG_CONSUME_ENABLED` 的生产门禁；
- [ ] 明确 refund/task/correction 关联字段；
- [ ] 评估增加 Provider 成本快照；
- [ ] Benefit 与 SSO 服务密钥轮换、审计和限流。

### YuanHeng Desktop

- [ ] 复用 new-api 登录、2FA、session、`New-Api-User` 方式；
- [ ] 通过隔离 WebView 打开控制台 Pulse 页面；
- [ ] 不直接访问 Pulse、不存 Pulse 密钥；
- [ ] session Cookie、API Token 迁移 OS 安全存储；
- [ ] session 过期、重新登录、退出登录状态联动；
- [ ] 不持久化用户名密码。

### Meta Pulse

- [ ] M0-M6 主线；
- [ ] new-api LOG_DB 只读适配器；
- [ ] new-api Benefit 客户端；
- [ ] BFF 签名校验；
- [ ] 论坛 profile 与 content 只读适配器；
- [ ] 对账、回放、回测、指标和告警。

### 论坛与博客

- [ ] Answer 继续使用插件，不 Fork 上游；
- [ ] 论坛切换到 new-api SSO；
- [ ] Nonce 使用共享原子存储；
- [ ] Pulse 不可用时论坛降级；
- [ ] M5 前仅展示等级/徽章，不发内容额度；
- [ ] 博客独立使用 VitePress，内容不接入经济账本。

## 7. 待决策事项

### D1｜毛利事实源（M2.5 前）

当前 new-api 日志只有用户收费 `quota`，没有 Provider 成本。建议先以估算倍率完成回测，同时预留成本快照字段；若正式目标是 Margin-aware，最终应补充不可变成本快照。

### D2｜Period 口径（M0 前）

建议：全局统一周期、UTC+8、按日志 `created_at` 归属，Ingest 时间只用于水位线和延迟指标。

### D3｜Refund / Correction 口径（M2 前）

必须确认：退款是否有稳定原消费关联；没有关联时如何进入 correction、人工对账和后续冲正，禁止把无法确认的退款直接计入用户贡献。

### D4｜Cookie 拓扑（P0 前）

建议论坛使用独立子域；若暂时沿用路径路由，必须在边缘层阻止 new-api session Cookie 进入论坛。

### D5｜Ticket Debt 展示（M3 前）

技术上保留 debt，产品上决定显示“欠券/抵扣中”还是只显示可用券，避免用户误认为系统漏发。

### D6｜内容奖励档位（M7 前）

论坛先以纯荣誉模式观察内容质量和灌水率，再确定额度档位、付费门槛和限额。

## 8. 测试底线

以下测试必须长期保留：

| 场景 | 里程碑 | 要求 |
|---|---|---|
| Usage 重放 100 次 | M2 | 真实 MySQL 唯一约束 |
| Action 重放 100 次 | M4 | 一个 Grant、一个随机结果 |
| Benefit 重放 100 次 | M5 | 只到账一次 |
| Period Close 重跑 | M6 | 不重复发放 |
| 并发 Pulse | M4 | 不超扣 Ticket、不超 Budget |
| DB commit 后响应丢失 | M4 | 同 key 可恢复 |
| Benefit 成功但 Pulse timeout | M5 | Query/Reconciliation 恢复 |
| Ledger/Account 重建 | M1 | 可重建、可对账 |
| Login Ticket 重放 | P0 | 单次 nonce、过期拒绝 |
| BFF 越权/签名重放 | P0 | 浏览器不能伪造身份 |

内存 fake 不能替代真实 MySQL 唯一约束测试；所有人工调整、冲正、rollback 都必须有审计断言。

## 9. 推进顺序

```text
P0 跨仓库身份/服务契约与 Cookie 安全
  ↓
M0 Pulse 地基
  ↓
M1 Ledger
  ↓
M2 Usage Ingest
  ↓
M2.5 真实回测
  ↓
M3 new-api UI + YuanHeng 入口 + 论坛等级
  ↓
M4 Reward Shadow Mode
  ↓
M5 真实 Benefit Settlement
  ↓
M6 Period / Admin / Experiment
  ↓
M7 内容奖励
```

博客和 Answer 基础部署可以并行，但论坛公网开放必须等待 P0；内容奖励必须等待 M5，并经过纯荣誉观察期。

## 10. 工程约束

- 仓库根不是 Go module，Go 命令必须显式列出模块路径；
- domain 不依赖 Gin、GORM、Redis 或 new-api SDK；
- Redis 只做缓存、nonce、限流、短锁和租约，不能成为事实源；
- 不用浮点做金额、贡献值、余额、预算或券账；
- Period active 后冻结核心经济配置；
- 任何改变系统边界、事实源、幂等、结算、身份或状态机的实现，都必须同步更新架构文档。
