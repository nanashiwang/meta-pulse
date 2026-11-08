# Agent Note: 抽奖纯按概率，去掉额度与经验奖池预算

Status: implemented

## Problem

用户要求 Pulse 抽奖去掉“奖励池预算”和“经验池预算”，纯按概率发放，不看预算；奖项编号、类型、数量、权重放在同一行方便查看；修改原因、确认勾选与“保存统一规则”保留。此前额度池已可显式不限总量，但经验池按 AGENTS 不变量 10 必须有硬预算，数据库 CHECK 也只允许 `loyalty` 不限。

## Existing capabilities and impact

复用 [period_create.go](../../../../services/pulse/internal/service/period_create.go) 的不可变规则快照、`pulse_reward_budget.unlimited` 与 [CanReserve](../../../../services/pulse/internal/ports/reward.go) 的整数边界；入口为 `/admin/pulse` 的 [admin-periods.js](../../../../metar-frontend/production/src/admin-periods.js)，经社区插件 [admin_periods.go](../../../../services/forum-plugin/user-center-pulse/admin_periods.go) 字段白名单转发。[未用券统一最新规则](2026-10-09-pulse-current-reward-rule.md)的归属与恢复语义不变。抽奖、规则查询、结算、撤销路径无需改动：不限预算行照常锁定、预留、结算。

## Decision

新增 `experience_budget_unlimited`，与 `quota_budget_unlimited` 同形：仅持续规则、cap 必须为 0、进入审计和幂等指纹；`CanReserve` 允许 `community_exp` 不限。迁移 00019 放宽预算 CHECK，Down 遇不限经验行拒绝。网页删除不限勾选与两个预算输入，始终发送两类不限标记（无额度奖项时不发额度标记），经验单奖项上限 1,000,000 EXP 保留；后端用 `unlimited_experience_supported` 声明能力，旧后端禁用编辑器。奖项改为单行五列网格，窄屏两列。API 仍接受有限预算以兼容旧调用；历史有限预算不改写。

## Alternatives considered

- **删除预算行或跳过预留**：最少代码，但会丢失预留/结算统计并改变 Ledger/Outbox 事务形状，拒绝。
- **给经验池伪造极大 cap**：无需迁移，但零 cap/巨大 cap 语义含混，且违背“不伪造 cap”的既有约定，拒绝。

## Consequences

EXP 发放总量不再有硬上限，成本只由券量、数量与权重控制；社区券仍有日限。需整体升级 Pulse API（先迁移）、社区插件与前端；回退前须关闭 Action 且处理不限经验行。

## Verification

- `go vet`、`go test`（pulse 与 user-center-pulse）通过，含新增纯概率规则校验、EXP 单项上限与 `CanReserve` 用例。
- 前端 `node --test tests/` 180 项通过，表单不再出现预算输入并保留原因/确认/保存。
- MySQL 集成测试已更新为额度与经验均不限，本地无 MySQL 未运行；未部署生产。
