# Agent Note: 接入真实排行榜与升级同步

Status: implemented

## Problem

v0.2.107 未核对真实上游响应：top_scores 不是 TrueSkill 分数、agent 应为 agents、overall 分类不存在。升级后 24 小时同步间隔又导致新字段尚未入库就发布空快照。生产 443 个模型排名全部为空。

## Decision

复用现有客户端、观测和快照；获取 general/reasoning/code/agents 的 rankings，直接保存 conservative_rating，general 映射综合榜。source-v3 隔离错误旧观测；每分类只使用最新一批排名。升级缺少新版本快照时先强制同步。API 每分类上限 50，页面说明范围，不为缺失模型合成分数。

## Alternatives considered

仅触发同步已在生产验证只能恢复 top_scores，不能恢复正确榜单。恢复 v1 会重新加工评分，违反用户要求。采用 rankings.score 会显示 Opus 119，而真实 TrueSkill conservative_rating 为 59.16，故直接使用后者。

## Testing

真实上游分类与字段回归；上线同步后总榜有排名、Opus 与同批上游一致；旧排名掉出前 50 后不残留。

## Consequences

上游 API 限制无法覆盖全部 443 个模型；明确显示范围。原始评分不缩放，不自行推算。
