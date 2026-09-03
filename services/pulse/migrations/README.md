# Database Migrations

Meta Pulse 使用独立 MySQL 8.0+ 逻辑库。SQL 文件采用 Goose 注释格式，并按数字顺序执行。

首个 migration `00001_initial_schema.sql` 创建 16 张核心表；`00002_ledger_payload_hash.sql` 补充账本 payload 指纹和 append-only 数据库保护；`00003_usage_correlation.sql` 补充最小退款关联字段与冲突唯一键：

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

## Migration 规则

1. 不直接修改 new-api 数据库 schema；Meta Pulse migration 只管理 Pulse 自己的数据库。
2. Pulse 对 new-api LOG_DB 只读，不能通过 migration 给 Pulse 增加 new-api 用户表写权限。
3. Ledger 表设计必须 append-only；不得设计“修改历史金额”的业务迁移。
4. 所有金额、贡献值、余额、Budget 使用整数/fixed-point 类型，不使用 FLOAT/DOUBLE 做业务账。
5. 唯一约束必须承载幂等语义，不只依靠应用层判断。
6. 任何 destructive migration 必须先有备份、回滚和数据校验方案。
7. active Period 的历史经济配置不得通过 migration 被批量覆盖。
8. `00002_ledger_payload_hash.sql` 创建 append-only trigger；启用 MySQL binary logging 时，DBA 须在迁移前配置 `log_bin_trust_function_creators=1`，或使用具备等效迁移权限的受控发布流程。

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
