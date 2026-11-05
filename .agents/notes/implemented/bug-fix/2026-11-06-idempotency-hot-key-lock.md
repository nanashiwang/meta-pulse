# Agent Note: 规则守卫预建并直接锁定记录

Status: implemented

## Problem

停止消费获券的 v0.2.82 发布回归在 MySQL 8.0/8.4 均出现 community ticket 100 路重放死锁。日志定位到 period_create_lock/global 的 INSERT ON DUPLICATE KEY UPDATE；持锁事务还需写入社区回执，其他等待者的重复键插入锁使新增回执遭遇 next-key/插入意向锁环路。本地初次测试未复现，不能把仅本地通过视为发布成功。

## Decision

复用既有 IdempotencyRepository 和 period_create_lock/global 身份：迁移 00018 幂等补齐永久守卫行，不修改已存在的 payload/身份；运行时对这一预建守卫使用唯一键 FOR UPDATE，其他业务幂等键沿用原 upsert。守卫缺失则失败关闭，先迁移再启动新 API/Worker。Down 保留守卫，旧代码可复用。不引入新表或跨事务重试，不改变冲突、不可变首次响应和全局串行化顺序。

## Alternatives considered

- 仅重跑 CI：可能掩盖竞争，不能修复实际请求失败。
- 所有 UnitOfWork 统一重试：可恢复死锁，但多个 callback 修改外部结果计数，直接重试可能把未提交结果重复累加；超出本次修复范围。
- 先普通读 ID 再主键锁：可以消除热键插入锁，但普通读会提前建立 Repeatable Read 快照；本地完整回归发现周期重叠竞争可双成功，弃用。必须先守卫锁，后业务快照。
- 专用全局锁新表：可固定锁对象，但需迁移和新的端口；现有幂等守卫已经稳定存在，直接锁主键足够。

## Validation

MySQL 8.0 与 8.4 的完整 Pulse 金融/仓储 race 回归通过；8.4 追加 5 轮社区 100 路重放与周期重叠竞争通过。仓储回归确认 Repeatable Read 旧快照下获取幂等记录仍返回已提交首次响应及原 payload，不能覆盖历史。迁移数量检查更新到 18。云端发布重新执行完整矩阵，不将本地通过视为 Release 完成。

## Consequences

运行时不再写已有规则守卫，但其他新幂等键仍沿用原创建路径。必须先应用 00018，不能手动删除全局守卫；回滚迁移也不删除它。v0.2.82 发布失败，不覆盖标签，使用后续版本发布。
