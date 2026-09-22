# Database Migrations

Meta Pulse 使用独立 MySQL 8.0+ 逻辑库。SQL 文件采用 Goose 注释格式，并按数字顺序执行。

`00015_unlimited_quota_budget.sql` 为额度预算增加显式 unlimited 模式，旧记录默认 false。不限总量仍完整记录预留与结算，且受整数表示边界保护。经验预算不能使用此模式。存在不限总量记录时 Down 会拒绝，需配套版本与备份恢复。

`00014_continuous_tickets.sql` 增加持续规则标记、额度资格天数、券发行批次、追加式消费分配和用户事务锁。既有周期默认非持续，原券不迁移、不改变期限。升级须同时更新 API、Worker、Answer 插件和前端；备份后向前迁移。产生新券后不能直接降级旧服务或执行 Down，否则会丢失券龄与消费分配；恢复需使用配套数据库备份。

首个 migration `00001_initial_schema.sql` 创建 16 张核心表；`00002_ledger_payload_hash.sql` 补充账本 payload 指纹和 append-only 数据库保护；`00003_usage_correlation.sql` 补充最小退款关联字段与冲突唯一键；`00004`—`00006` 依次补充预算类型、内容奖励表和内容限额并发 guard；`00007` 持久化 Usage 命中的 economics config version 快照；`00008` 将终态 Settlement 完整性冲突从可对账的 `dead` 状态中分离；`00009` 为跨周期请求/Action 的旧记录恢复添加 key-first 与 user/action 查询索引：

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
pulse_content_candidate
pulse_content_award
pulse_content_award_limit_guard
```

## Migration 规则

1. 不直接修改 new-api 数据库 schema；Meta Pulse migration 只管理 Pulse 自己的数据库。
2. Pulse 对 new-api LOG_DB 只读，不能通过 migration 给 Pulse 增加 new-api 用户表写权限。
3. Ledger 表设计必须 append-only；不得设计“修改历史金额”的业务迁移。
4. 所有金额、贡献值、余额、Budget 使用整数/fixed-point 类型，不使用 FLOAT/DOUBLE 做业务账。
5. 唯一约束必须承载幂等语义，不只依靠应用层判断。
6. 任何 destructive migration 必须先有备份、回滚和数据校验方案。
7. active Period 的历史经济配置不得通过 migration 被批量覆盖。
8. `00002_ledger_payload_hash.sql` 创建 append-only trigger；启用 MySQL binary logging 时，DBA 须在迁移前配置 `log_bin_trust_function_creators=1`，或使用具备等效迁移权限的受控发布流程。
9. `00008_settlement_terminal_conflict.sql` 的终态分类不可逆；Down 只能安全 no-op，禁止把全部 `conflict` 降回可自动对账的 `dead`。

## 最低唯一约束

```text
pulse_usage_event:
  UNIQUE(source_system, source_event_id)

pulse_ledger_entry:
  UNIQUE(operation, idempotency_key)
  payload_hash：用于同 key 不同 payload 的 conflict 判断

pulse_account:
  UNIQUE(user_id, period_id, asset_type)

pulse_reward_grant:
  UNIQUE(period_id, user_id, action_id)

pulse_settlement_outbox:
  UNIQUE(reward_grant_id)

pulse_idempotency:
  UNIQUE(scope, idempotency_key)

pulse_experiment_assignment:
  UNIQUE(experiment_id, user_id)
```

## 对账要求

Migration 上线后必须能够支持以下 invariant：

```text
SUM(Ledger) = Account
Reward settled => new-api Benefit exists
Budget reserved + settled <= cap
accepted UsageEvent => exactly one accounting effect
```


## `00009` 幂等升级

仅添加 `pulse_idempotency(idempotency_key, scope)` 与 `pulse_reward_grant(user_id, action_id, trigger_type)` 非唯一辅助索引，不重写 Ledger、Grant 或旧请求响应。新 API 在同一业务事务内建立不依赖周期的 action/request 双映射，旧记录按原指纹核验后恢复；存在歧义时冲突退出。

执行发布前备份并排空旧 API 写请求，禁止同时运行新旧 Action 写入路径。Down 只移除辅助索引，不会撤销已经建立的稳定幂等语义；旧版本不认识新请求范围，不能以执行 Down/删幂等记录的方式直接恢复发奖。

## `00013` 账本对账覆盖索引

`00013_ledger_reconciliation_index.sql` 为全量余额与记录数对账增加 `(user_id, period_id, asset_type, amount)` 覆盖索引。运营概览一次聚合全部 Ledger 后与 Account 比较，避免每个账户重复扫描和逐行回表。原账户流水索引保留，继续支持摘要最近流水和全量重建。迁移仅新增索引，不改历史账目或经济规则；使用在线索引构建，回退仅删除新增索引。

## `00012` 管理员运行配置

`00012_runtime_settings.sql` 新增单例配置版本、API/Worker 公钥与环境指纹登记、按角色加密的业务密钥、持久化配置请求回执。保存采用同事务版本检查、幂等回执和 `pulse_audit_log`；审计只保存修改字段和配置状态，不保存密钥或指纹。Worker 首次登记时固定 new-api 接收地址，后续环境变量改变不会切换资金事实源。

升级前备份数据库；启用后还必须分别备份 API、Worker 专用密钥卷中的 `api.key`、`worker.key`。业务密钥从数据库读取时只有本角色私钥可以解密，私钥丢失必须恢复原卷，不能重新生成替代。首次登记缺必需密钥或提供无效密钥会失败并回滚登记，修复环境后可以重试；成功登记后的环境签名密钥基线应保持一致，日常修改通过管理员页面完成。

Down 保留配置、密文和幂等回执，重新 Up 不会覆盖现有值。回退旧二进制前先关闭奖励写入并制定配置回迁方案；旧版本只读环境变量，不能把保留数据库表误认为旧版本仍会使用新配置。
