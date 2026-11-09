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

## 5 ⚡️ expectation preset

用户随后要求每券期望 5 ⚡️（2,500,000 quota）并更有吸引力。管理员预设改为九档、总权重 1000（1 点 = 0.1%，回应“权重不必加很多零”）：API 1/3/10/30/100/500 ⚡️ 权重 425/325/100/40/9/1，中奖率 90%，期望恰为 5 ⚡️；EXP 1/4/20 权重 50/30/20。考虑过在原 50% 方案上等比放大金额（期望 5 ⚡️ 但一半抽奖只得 EXP，体感差）与全部集中小额（无大奖话题性），选择高中奖率加 0.1% 的 500 ⚡️ 头奖。成本约原方案 20 倍且无奖池上限；预设不自动生效，须管理员载入保存。取代[50% 预设](2026-10-09-pulse-fifty-percent-preset.md)的参数，其加载与预览机制不变。前端测试按 100000/500000/3 三种换算比例校验整数期望。
