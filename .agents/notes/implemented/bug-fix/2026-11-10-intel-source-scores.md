# Agent Note: 保留来源原始评分

Status: implemented

## Problem

本地 v1 min-max 加权分被当作来源排行榜分数，稀疏自报评测也可取得满分。

## Decision

复用导入观测和版本快照，读取 LLM Stats models 的 top_scores 原始分类评分；没有 overall 就不生成总分。原始 benchmark score 使用独立 points 项保存，停止除以 max_score。新方法 source-v2 不回退 v1。

## Alternatives considered

继续 v1 并增加可信度惩罚能改善稀疏数据，但仍加工来源分数，违反用户明确要求。自行重算 TrueSkill 也无法保证与来源一致，故不采用。

## Testing

59.9 保持 59.9，缺失 overall 不合成总分；增删其他模型不改变分数；前端说明同步，历史观测保留。

## Consequences

上游未提供 overall 时总榜暂无评分；接受缺失，不猜测分类映射。既有独立服务提案仅部分重叠，保留其架构讨论，本笔记取代其中自有评分方向。

Intel 单测、go vet、完整社区测试与 Intel/社区构建通过。官方 API 未承诺 overall，缺失时不提供总榜排名；不宣称已复现网站 TrueSkill 总分。旧来源百分比在公开详情排除，仅留数据库审计。

现场复查：v0.2.107 升级跳过 24 小时内同步，且 top_scores 不是 TrueSkill 排行榜评分；修复由[排行榜接入](2026-11-10-intel-ranking-import.md)接管。
