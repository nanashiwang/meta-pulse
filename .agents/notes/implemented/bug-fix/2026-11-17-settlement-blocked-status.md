# Agent Note: 结算受阻状态与管理员告警

Status: implemented

## Problem

额度奖励的 Outbox 重试耗尽后保留 Grant pending 供原单恢复，但用户记录只投影 Grant 状态，仍显示发放中。接收端单笔和日限额独立于奖池，限额拒绝会累积为 dead。

## Decision

保留原资金状态机，历史及原 Action 查询额外读取 Outbox 状态，仅将 pending + dead/conflict 投影为 settlement_dead。后台配置页通过既有管理员运营快照展示受阻数量；查询失败明确显示未知，不冒充零异常。沿用管理员实时权限、固定路径签名、白名单投影与 no-store；不开放网页重发、不暴露错误原文。运营恢复使用既有 reward-retry，Query 原 source_ref 后按原单执行，不拆单、不清重试次数。

## Existing capabilities and note review

能力地图的本人权益、额度结算、运营概览和管理配置覆盖现有入口。复用 RewardHistory、Outbox、operations overview 与 Answer 管理员 BFF。无需新资金模型。与[结果恢复](../../implemented/bug-fix/2026-10-09-pulse-result-recovery.md)部分重叠，保留其抽奖与到账解耦；与[紧凑历史](../../implemented/simplification/2026-10-09-pulse-compact-history.md)部分重叠，保留记录入口；其余命中为无关功能或部署背景，无过时提案需迁移。

## Alternatives considered

- 将 Grant pending 改为 settlement_dead：可直接使用旧前端状态，但会破坏 RetryDead、Reconcile 对 pending 的恢复前提，且需要历史数据迁移；仅改变展示投影更窄。
- 浏览器按等待时间猜失败：实现简单，但网络慢与真实 dead 不等价，不能据此判断到账。
- 新建消息推送系统：可主动触达，但增加通知渠道、去重和投递状态；本次复用管理员配置页告警和已有监控指标。

## Verification

Go 回归覆盖 dead/conflict、正常 pending、已到账、经验奖励及恢复后状态；本人历史与 Action 查询一致，数据库原状态不改。隔离 MySQL 8.4 验证批量读取、用户隔离和不写原 Grant，原结算/对账回归通过。make test、make vet、build-pulse、build-community 和网关检查通过；管理员正常/受阻/不可用三态、撤权及安全投影回归通过。浏览器合成数据在桌面与 390px 检查告警及表单布局；生产部署单独核验。

## Consequences

受阻不等于奖励取消；必须通过原单恢复。页面告警在管理员访问配置页时呈现，不等于已接通邮件或站内信推送。接收端真实限额独立运营修改，不能为发大奖擅自提高用户指定上限。

## Operational recovery

2026-11-17 经用户授权，通过 new-api 已有管理接口将每人每日接收上限从 100 调为 200，保留单笔 100、全站每日 2000。原单 Query/Retry 恢复四笔合计 107 额度，接收端逐单凭证确认；另两笔各 500 按用户明确指示保留待处理，不拆分、不提高限额。此记录不含用户身份或凭据，发布本身不重新应用运营配置。
