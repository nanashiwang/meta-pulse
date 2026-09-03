# Meta Pulse 实施计划

本文档是 `docs/ARCHITECTURE.md`（系统基线）与 `docs/COMMUNITY.md`（社区层）之上的工程执行清单。目标不是先把页面做出来，而是先打通身份、日志、记账、预算和结算边界，再逐步开放奖励。

```text
先定义跨仓库契约 → 再建 Pulse 地基 → 再接真实日志 → 再展示 → 再发奖 → 最后做内容奖励
```

## 1. 当前状态

```text
✅ 架构文档       ARCHITECTURE.md / COMMUNITY.md / AGENTS.md
✅ Monorepo 骨架  services/{pulse,forum,forum-plugin} + sites/blog + deploy
✅ 论坛插件       UserCenter、等级展示、SSO Ticket 验签、共享 Nonce 已落地
✅ new-api 调研   已确认登录、2FA、LOG_DB、BenefitChangeRecord、额度发放边界
🟡 P0 接入契约     new-api SSO / BFF / Benefit API、Pulse 验签与轮换已落地；真实部署验收待完成
🟡 M0 地基         配置、Gin、健康检查、GORM/Redis、Goose migration、指标已落地；真实环境验收待完成
✅ M1 Ledger 记账内核    领域账本、账户快照、幂等冲突、重建与 ledger-check 已落地
✅ M2 Usage Ingest 与等级  只读日志游标、统一 Mapper、事务记账、退款复核、等级 Profile 已落地
🟡 M2.5 回测框架  只读回放、范围过滤、异常/覆盖率、倍率对比已落地；真实 LOG_DB 样本待环境接入
✅ M3 只读入口  new-api BFF/UI、YuanHeng 隔离 WebView、论坛等级降级与 Pulse 只读接口已落地；发布灰度待环境验收
🟡 M4 Shadow Mode  Action、幂等、确定性随机、Ticket/Budget/Grant/Outbox 已落地；真实 MySQL 恢复验收待 Compose
🟡 M5 Settlement  Pulse Benefit Client、Outbox 退避、Query/Reconcile/Rollback 与 new-api 接收端已落地；真实额度到账验收待完成
✅ M6 周期与运营       Period Close、稳定周期奖励、Holdout 固化、人工调整审计、日指标聚合已落地；真实 MySQL 中断恢复待 Compose 验收
✅ M7 内容奖励         Pulse 侧候选采集、人工审核、独立预算、限额、幂等、结算与撤销已落地；Answer 真实 schema/SSO 与 Benefit 端到端验收待完成
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

- [x] new-api 实现 `/forum/sso/start`，使用固定登录回跳，禁止开放重定向；
- [x] new-api 从 session 读取用户资料并签发 Login Ticket；
- [x] Ticket 具备 TTL、未来时间拒绝、HMAC 验签、单次 nonce、固定 callback allowlist；
- [x] 论坛插件将登录入口切换到 SSO Bridge；
- [x] Nonce 使用 Redis 原子消费，进程内存实现仅保留给测试；
- [x] 论坛固定使用独立 HTTPS 子域；Nginx 仅向 Answer 转发 `visit` Cookie，旧 `/forum/*` 只重定向；
- [x] new-api 实现 Pulse Signed BFF，浏览器访问 new-api，用户 ID 由 session 派生；
- [x] 更新 Nginx/网关：对外 `/api/pulse/*` 只进入 new-api BFF，Pulse/Answer 均不发布宿主机端口；
- [x] 统一服务签名覆盖 method、path、user、timestamp、nonce、body hash；
- [x] 定义 Benefit API 的 grant/query/rollback、payload fingerprint、conflict 和错误码；
- [x] 定义 Usage Mapper：consume、refund、correction、异步 task 退款、差额结算；不确定关联进入人工复核；
- [x] 生产密钥不进入 Git，支持 current/previous 平滑轮换和 fail closed；审计、限流及轮换演练待部署验收；
- [x] Pulse 侧实现规范化 `method/path/user/timestamp/nonce/body_hash` 验签、时间窗和重放拒绝，生产 Nonce 适配 Redis 原子 `SETNX`。

**代码出口：**登录、2FA、论坛 SSO、Ticket 重放、Cookie 隔离、BFF 越权和签名重放回归测试通过。真实域名、跨实例、轮换演练和公网门禁属于部署验收；P0 外部验收完成前论坛不得开放公网，Pulse 奖励不得上线。

### M0｜Pulse 地基

- [x] Goose SQL migration 与 16 张核心表；
- [x] MySQL、Redis 连接与健康检查；
- [x] GORM store、ports、UnitOfWork、手工依赖装配；
- [x] Gin 路由骨架；
- [x] 结构化日志、Prometheus registry；
- [x] `.env.example` 与 `config.go` 一致，必填项缺失时启动失败；
- [ ] 验证 LOG_DB 使用只读账号、Pulse 无 new-api 主库写权限；
- [x] 明确 Period 使用半开区间 `[start,end)`、Asia/Shanghai（UTC+8），按日志 `created_at` 归属，Ingest 时间仅用于水位线；
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

- [x] LOG_DB 只读 cursor、批量读取、断点续跑；
- [x] 共用 Mapper 实现实时 Ingest 和 Backfill；
- [x] `source_system + source_event_id` 幂等；
- [x] payload hash 不一致写 `pulse_ingest_conflict`，不静默覆盖；
- [x] 单事务完成 UsageEvent、Contribution Ledger、Account、Ticket Entitlement、Ticket Ledger、UserPeriodStat；
- [x] Economics Rule 保存命中规则、倍率、eligibility、版本快照；
- [x] 处理异步任务退款与差额结算；不确定关联时进入人工对账队列；
- [x] 终身净贡献等级与只读 profile API；
- [x] `tools/backfill` 支持 dry-run、范围、报告和重复执行。

**出口：**同一 Usage 重放 100 次只产生一次业务记账；同 ID 不同 payload 进入 conflict；历史日志可回放；论坛 profile API 可用。

### M2.5｜真实日志回测

- [ ] 用真实 LOG_DB 样本回放消费、退款、task、结算失败数据（当前仓库已提供只读回放命令，待配置只读 LOG_DB 后执行）；
- [x] 输出用户覆盖率、贡献值、券产出、异常率和数据缺口；
- [x] 对比人工倍率、模型倍率、渠道倍率的结果；
- [ ] 基于真实报告标定 Ticket Threshold、Reward 权重、预算上限；
- [ ] 决定采用估算毛利，还是让 new-api 增加不可变成本快照。

当前实现：`meta-pulse-tool backtest --from ... --to ...` 使用与 Ingest 相同的 LOG_DB Mapper，范围按 `[from,to)`，只读 Pulse DB，不推进生产游标、不写 Ledger/Account。由于当前环境未提供可验证的真实 LOG_DB 样本，真实回放与参数定标不能伪造完成。

**出口：**得到可审计的活动成本 / 贡献毛利数字；M4 参数必须引用回测报告。

### M3｜只读产品入口

- [x] new-api `/api/pulse/*` BFF；
- [x] new-api `/console/pulse` 页面与路由；
- [x] Pulse 提供只读 `/v1/internal/me/summary`，用户由已验签 Principal 派生，不接受浏览器 user_id；
- [x] Pulse profile/summary 返回当前 Period、贡献值、可用券、等级和当前周期 Ledger；
- [x] Pulse 奖励历史只读投影 `/v1/internal/me/rewards`，仅返回当前 Principal 的安全字段；
- [x] YuanHeng 通过隔离 WebView 打开 `/console/pulse`；
- [x] YuanHeng session/API Token 改用 OS 安全存储，用户名密码不落盘；
- [x] 论坛展示 Pulse 等级，Pulse 故障时降级；
- [x] 只读灰度入口保持无 Pulse Action；正式灰度仍需部署验收。

**出口：**用户能看到数据；浏览器、桌面端、论坛均不能越过 new-api 身份边界；Pulse 故障不影响登录、模型调用、充值和论坛浏览。

### M4｜Reward 内核与 Shadow Mode

- [x] API mutation 必须使用 Idempotency-Key；
- [x] 版本化 HMAC 确定性随机，保存 random value 和 config version；
- [x] 一个 Action 只能消费一张 Ticket、生成一个 Grant 和一个随机结果；
- [x] Budget reservation 与 hard cap；
- [x] 单事务写入 Ticket Spend、Budget Reservation、Reward Grant、Settlement Outbox；
- [x] 先只生成 `pending` Grant，不真实发额度；Shadow outbox 不进入结算发送；
- [x] 同一 Action/Idempotency-Key 重放 100 次及串行化并发测试；
- [ ] 真实 MySQL 并发、commit 后响应丢失、DB 重启恢复测试（需 Compose 验收）。

**出口：**同一 Action 重放 100 次只有一个 Grant；并发不超扣券、不超预算；网络失败不重新随机。

### M5｜Settlement 与 Benefit API

- [x] new-api Grant / Query / Rollback 内部接口；
- [x] Pulse Benefit Client 使用 HMAC 服务认证、时间窗、nonce、来源绑定；
- [x] Benefit payload fingerprint 和 conflict 错误已进入统一契约；
- [x] 使用 `GrantUserQuotaTx`，奖励额度 `transferable_quota=0`；
- [x] Outbox 指数退避、dead 状态、`reward-retry` 人工重试入口；
- [x] timeout 必须先 Query 原 `source_ref`，禁止更换 source_ref；
- [x] Benefit Reconciliation 可重复查询并收敛状态；
- [x] rollback 调用原 source_ref，Pulse 侧只更新可审计状态；new-api 侧必须以 reversal 记录落账。

当前实现不会把 `shadow` Outbox 发送到 new-api；只有关闭 `PULSE_REWARD_SHADOW_MODE` 且 new-api 接收端完成验收后才会处理 pending Outbox。

**出口：**同一 Benefit 重放 100 次只到账一次；不同 payload 进入 conflict；new-api 成功但 Pulse 超时可恢复；禁止换 source_ref 有测试保护。

### M6｜周期与运营

- [x] Period Close：active → settling → closed，可重入；
- [x] Watermark 确认、Ledger/Account 对账、周期奖励、券过期；
- [x] Period Reward 使用稳定 action ID；
- [x] Holdout 稳定分组并持久化首次 assignment；
- [x] Admin、Audit Log、冲突处理、人工财务调整；
- [x] 日指标、告警、Settlement 堆积和预算预警；
- [x] close 中断后的恢复测试（内存回归；真实 MySQL/Compose 仍需环境验收）。

当前实现：`PeriodCloseService` 在 watermark 到达后执行 Ledger/Account 对账、Ticket 过期、周期奖励和状态迁移；`AdminAdjustmentService` 只追加 adjustment 分录并同步 Audit Log；`ExperimentService` 固化 HMAC cohort；`MetricsAggregationService` 将运营快照写入 `pulse_metric_daily`，Worker 已接入周期关闭与指标聚合。

**出口：**Period Close 重跑不重复发放；中途崩溃可继续；预算、账本、奖励和 Benefit 可对账。真实 MySQL 唯一约束、事务回滚和故障恢复仍须在 Compose 环境完成。

### M7｜内容奖励 ✅（Pulse 侧完成）

- [x] 论坛 DB 只读游标与 Content Candidate：Worker 通过可选 `FORUM_DB_DSN` 读取 Answer 问题元数据，只复制必要字段，不复制正文；论坛库不可用时仅停用内容采集；
- [x] 人工审核、档位、reason、Audit Log：管理员请求必须使用已签名 `admin` Principal，actor 不从 JSON 读取；
- [x] 独立 `content_reward` budget：内容奖励汇入通用 Reward Grant/Settlement，但预算线与 loyalty、period_reward 隔离；
- [x] 付费门槛、单用户上限、全站日上限；未达标/超限只写审核与资格结果，不生成 Grant；
- [x] `content_award:{type}:{id}:{version}` 幂等，并校验同 action 的 payload 冲突；
- [x] 删除/抄袭后的 settled → reversed：撤销复用原 Grant/source_ref，Benefit rollback 与本地状态更新均可重试；
- [x] 内容不产生 contribution 或 ticket。

**出口：**四道防刷闸全部生效；内容奖励与忠诚度预算、账本、贡献值完全隔离。Pulse 侧代码、迁移、管理路由和回归测试已完成；正式 Answer 数据库只读账号、真实 schema、SSO 及 new-api Benefit 接收端属于跨仓库部署验收，不在本仓库内冒充完成。

## 6. 跨仓库任务清单

### new-api

- [x] Forum SSO Bridge 与 Login Ticket；
- [x] Pulse BFF，服务端派生 user ID；
- [x] Pulse Internal Benefit API；
- [x] Benefit 重复请求 payload 比较与 conflict；
- [x] `/console/pulse` 路由、页面和导航入口；
- [ ] 明确 `LOG_CONSUME_ENABLED` 的生产门禁；
- [ ] 明确 refund/task/correction 关联字段；
- [ ] 评估增加 Provider 成本快照；
- [ ] Benefit 与 SSO 服务密钥轮换、审计和限流。

### YuanHeng Desktop

- [x] 复用 new-api 登录、2FA、session、`New-Api-User` 方式；
- [x] 通过隔离 WebView 打开控制台 Pulse 页面；
- [x] 不直接访问 Pulse、不存 Pulse 密钥；
- [x] session Cookie、API Token 迁移 OS 安全存储；
- [x] session 过期、重新登录、退出登录状态联动；
- [x] 不持久化用户名密码。

### Meta Pulse

- [x] M0-M6 主线代码、迁移和回归测试；真实 MySQL/Compose 验收仍待完成；
- [x] new-api LOG_DB 只读适配器；
- [x] new-api Benefit 客户端；
- [x] BFF 签名校验；
- [x] 论坛 profile 与 content 只读适配器；
- [x] 对账、回放、回测、指标和告警；真实样本与数据库权限验收仍待完成。

### 论坛与博客

- [x] Answer 继续使用插件，不 Fork 上游；
- [x] 论坛切换到 new-api SSO；
- [x] Nonce 使用共享 Redis 原子存储；
- [x] Pulse 不可用时论坛降级；
- [x] M5 前仅展示等级/徽章，不发内容额度；
- [x] 博客独立使用 VitePress，内容不接入经济账本。

## 7. 待决策事项

### D1｜毛利事实源（M2.5 前）

当前 new-api 日志只有用户收费 `quota`，没有 Provider 成本。建议先以估算倍率完成回测，同时预留成本快照字段；若正式目标是 Margin-aware，最终应补充不可变成本快照。

### D2｜Period 口径（已决策）

全局统一周期使用半开区间 `[start,end)`、Asia/Shanghai（UTC+8）；按日志 `created_at` 归属，Ingest 时间只用于水位线和延迟指标。实现与测试已对齐。

### D3｜Refund / Correction 口径（M2 前）

必须确认：退款是否有稳定原消费关联；没有关联时如何进入 correction、人工对账和后续冲正，禁止把无法确认的退款直接计入用户贡献。

### D4｜Cookie 拓扑（已决策）

论坛固定使用 `forum.<主域>` 独立 HTTPS 子域；旧 `/forum/*` 仅做 308 跳转。网关以 Cookie allowlist 方式只向 Answer 转发 `visit`，并禁止 Pulse/Answer 容器直接发布宿主机端口。

### D5｜Ticket Debt 展示（M3 前）

技术上保留 debt，产品上决定显示“欠券/抵扣中”还是只显示可用券，避免用户误认为系统漏发。

### D6｜内容奖励档位（M7 前）

论坛先以纯荣誉模式观察内容质量和灌水率，再确定额度档位、付费门槛和限额。

## 8. 当前未冒充完成的外部验收

以下项目已有代码、测试或验收脚本，但必须接入真实部署配置后才能勾选完成：

- 真实 MySQL/Redis Compose：迁移、`/readyz`、重启、唯一约束、事务回滚和并发锁；
- new-api `LOG_DB` 只读账号、Pulse 独立数据库及无主库写权限；
- 真实 LOG_DB 回放、参数定标和 Provider 成本快照决策；
- Answer 真实 schema、SSO、等级降级和跨实例 Nonce；
- new-api Benefit 真实到账、重放 100 次、timeout Query、rollback 及密钥轮换演练。

本机当前 Docker Hub 返回镜像授权服务不可用，不能以失败的拉取结果或内存 fake 代替上述验收。

## 9. 测试底线

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

## 10. 推进顺序

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

## 11. 工程约束

- 仓库根不是 Go module，Go 命令必须显式列出模块路径；
- domain 不依赖 Gin、GORM、Redis 或 new-api SDK；
- Redis 只做缓存、nonce、限流、短锁和租约，不能成为事实源；
- 不用浮点做金额、贡献值、余额、预算或券账；
- Period active 后冻结核心经济配置；
- 任何改变系统边界、事实源、幂等、结算、身份或状态机的实现，都必须同步更新架构文档。
