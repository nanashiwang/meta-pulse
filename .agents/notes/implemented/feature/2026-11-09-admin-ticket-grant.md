# Agent Note: 管理员在用户管理手动赠送抽奖券

Status: implemented

## Problem

用户要给账号 uno 加 10 张抽奖券以验证新的 5 ⚡️ 期望方案，并要求发券入口放在用户管理界面。此前没有任何发券入口：`AdminAdjustmentService` 未接入路由，且只写账本、不铸造券批次，v3 抽奖从批次选券，因此这样加的券不可抽。直接改库会绕过幂等与审计。

## Existing capabilities and impact

复用 `/admin/users` 行内三点菜单（[账号切换](2026-11-06-admin-account-switch.md)已注入）、插件的管理员会话与 `CheckAdminIdentity`、`CommunityIdentity` 一对一绑定解析、admin HMAC 客户端，以及[社区任务赠券](2026-11-06-community-tickets.md)的「规则锁 → 幂等 → 用户券锁 → ticket_bonus 账本 → 券批次 → 审计」事务。新增仅为一条插件路由 `/answer/admin/api/metar/accounts/tickets`、一条 Pulse 路由 `/v1/internal/admin/tickets/grant` 和 `CommunityTicketService.AdminGrant`。

## Decision

浏览器只提交用户名、数量（1–100）、原因，每次提交生成一个 Idempotency-Key 并在结果不确定时用同一键重试；插件按管理员加前缀，Pulse 回执 payload 含操作人/用户/数量/原因，同键不同内容冲突。一次赠送铸造一个 `issued=count` 批次，有效期按当前持续规则，不受任务日限、不产生 contribution。自己的账号也显示赠券项（便于管理员自测），切换登录项仍排除自己。未绑定 API 账号返回 `binding_required`。

## Alternatives considered

- **接入 `AdminAdjustmentService` 正向调整**：已有审计与幂等，但不铸造批次，券不可抽；为它补批次等于重写一份赠券事务。
- **CLI 工具命令**：无需前端，但用户明确要求放在用户管理界面，且需登录服务器操作。
- **直接 SQL**：最快，但绕过幂等、审计与锁序，违反账本只追加和人工调整可审计的不变量。

## Consequences

管理员可无上限次数地手动发券，成本控制依赖管理员审核与审计日志；单次上限 100 防误输。需同时升级 Pulse API 与 Forum 插件（含静态资源）；旧 Pulse 返回 404 时前端提示失败、不会发券。撤回仍需另行追加负向调整，本次未提供界面。

## Verification

- Go vet/test（pulse、user-center-pulse）通过：路由仅 admin、缺幂等键/超限/多余字段拒绝；插件跨站、非管理员、未绑定、账号不存在均不调用上游，签名身份与前缀键正确。
- 插件 Node 测试 46 项通过，含菜单项、同键重试与未绑定提示；正式前端测试 185 项通过。
- MySQL 100 并发重放只铸造一个 10 张批次、账本 +10、同键改数量冲突的集成测试已编写，本地无 MySQL 未运行；未部署生产。
