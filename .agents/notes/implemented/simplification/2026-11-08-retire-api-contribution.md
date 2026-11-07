# Agent Note: 退役 API 消费摄入与贡献权益

Status: implemented

## Problem

消费赠券已取消，但 Worker 仍依赖 LOG_DB 并持续写入贡献，资料、内容资格和运营告警仍引用消费。用户要求彻底取消消费贡献，保留社区赠券与奖励交付。

## Decision

移除生产消费摄入和补录/回测入口及日志库依赖，历史算法只作为历史账本兼容测试夹具保留；当前 profile/summary 返回中性兼容字段并明确贡献已退役，不展示消费等级。内容奖励不再检查付费贡献；旧周期关闭不等待消费水位、不创建消费贡献奖励。既有券、Grant、Outbox、账本与规则快照不改写，社区经验独立。

## Alternatives considered

只隐藏页面或断开 LOG_DB 会遗留补录路径或让整个 Worker 停机，影响奖励交付。增加开关会让旧配置恢复已取消的机制。删除历史表会破坏审计、重建及既有奖励关联，因此保留历史事实而移除运行入口。

## Verification

无 LOG_DB 时 Worker 配置可用，旧消费命令在连接数据库前明确拒绝；生产构建不包含摄入算法。历史贡献不参与当前权益。完整 make test、Go vet、Pulse/博客/正式前端构建、生产 Compose 配置回归通过。隔离 MySQL 8.0 的 service/store race 回归和社区任务赠券集成回归通过；两模块测试 DSN 分别沿用 CI 的上海时区与 UTC 约定，首次本地时区误配的失败不能计作业务验收。发布附件由既有 Release 工作流构建并回读校验，隔离 MySQL/Redis 实启 Worker，故意提供无效旧 LOG_DB 与消费参数，readyz 和 operations 正常且 Usage/贡献/消费游标均零新增；旧 CLI 对无效 Pulse DSN 也在连接前拒绝。生产部署另行验收。

## Consequences

旧进程仍会写入，API/Worker/工具与论坛资源需同时升级；发布不等同部署，回滚旧版会恢复消费摄入。Meta API 配套入口由用户在另一项目交接。

## Related notes audit

[停止消费赠券](../../implemented/simplification/2026-11-06-stop-usage-tickets.md)部分取代：保留其历史券不追溯修改的理由，取消继续记贡献的决定。[社区赠券](../../implemented/feature/2026-11-06-community-tickets.md)与[当前抽奖规则](../../implemented/feature/2026-10-09-pulse-current-reward-rule.md)部分重叠，社区任务和奖池语义继续成立。compact-tools 与历史清零是独立运维决策；账号切换、登录保持、集市与本次无关。
