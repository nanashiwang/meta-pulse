# Agent Note: 停止 API 消费兑换抽奖券

Status: implemented

## Problem

用户要求取消 Meta API 消费额度获券，只保留已授权的社区签到与互动来源。当前 Worker 和 backfill 共用 UsageIngestService，根据贡献度兑换券；前台和规则管理仍宣传消费兑换。

## Decision

从共享摄入服务移除券发行、批次和 entitlement 计算，所有新处理的消费（含历史补录）只写用量、付费证明、贡献账本/快照和游标。消费退款继续关联和冲正贡献，不再自动增减任何券；已存在的券账本、批次、entitled_tickets 历史统计保留，重放不追溯改账。管理调整仍可审计处理历史异常。

沿用现有社区 ticket_bonus、保护绑定、抽奖与奖励交付，不新增配置开关或发券实现。管理页去掉兑换门槛输入，兼容 API 保留冻结历史 threshold 字段，新规则沿用当前值（无当前规则时用既有默认值）；这些字段不再开启消费获券。同步用户入口与文档，不原地修改 Active 经济配置。

## Alternatives considered

- 只关闭 Worker 或隐藏入口：改动少，但会停掉必要用量统计，且补录仍可发券。
- 增加可选关闭开关：适合保留两套经营模式，但用户要求删除渠道，默认值或旧配置可能重新开启。
- 仅钳制正向 ticketDelta：保留旧退款路径，但新消费不曾发券，后续退款按总贡献计算会误扣历史或社区券；完全脱离 usage 券变更更符合保留已有券的边界。

## Validation

本地 make test、vet、Pulse/博客/正式前端构建通过。隔离 MySQL 8.0 的完整 Pulse service/store race 回归及社区任务采集回归通过：消费 100 次重放、跨规则不加券、失败回滚/游标恢复、旧券及社区券正常抽奖与账本重建。内存回归覆盖旧定期/持续规则下退款和补录不触碰两类券，管理表单保留历史契约且移除门槛。浏览器用合成数据验证获取方式与签到仍可操作，无消费兑换承诺；不作为生产领取验收。

## Consequences

所有采集 Worker / CLI 需升级才生效，旧进程仍可执行旧代码。历史字段和离线 backtest 的经济模拟保留，不表示当前可兑换。回滚到旧版会恢复消费发券，应避免混跑。发布不代表生产已部署。

相关：[社区赠券](../../implemented/feature/2026-11-06-community-tickets.md)、[当前抽奖规则](../../implemented/feature/2026-10-09-pulse-current-reward-rule.md)、[历史运营清零](../../implemented/process/2026-11-01-ticket-reset.md)。

2026-11-08 的[消费贡献退役决定](2026-11-08-retire-api-contribution.md)取代本篇“继续写贡献与消费游标”的部分；本篇历史券不追溯修改及独立社区赠券边界继续成立。
