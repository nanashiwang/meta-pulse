# Agent Note: 后台禁止徽章弹窗并保留跨页关闭状态

Status: implemented

## Problem

线上 /admin/dashboard 加载的 chunk-mix1.84009624.chunk.js 已包含 v0.2.129 的内存 Set 去重，但每次完整导航会重建组件并再次弹出。此前修复只覆盖当前 Layout，未满足用户跨页操作不被打断的需求。

## Decision

复用原生 BadgeModal 与 useLocation，所有 /admin 及 /admin/* 禁止自动弹窗和动画，也不自动标记通知已读。社区页关闭状态按账号写入 sessionStorage，刷新、完整导航或组件重建继续生效，保留内存降级。其他账号独立，关闭标签页后自然结束；徽章和成就中心原样保留。只改变展示，不更改授予或数据库。

## Alternatives considered

- 仅延续原内存 Set：无额外存储，但已经由线上验证无法覆盖跨页面导航。
- localStorage 永久禁用：跨新标签也有效，但会长久吞掉新徽章提醒；采用会话范围。
- 批量标记所有通知已读：可清除积压，但篡改用户尚未阅读的通知，不采用。

## Verification

后台直接加载、刷新、SPA 进入均不弹窗或启动动画；关闭后重建组件和新通知轮询仍不弹，其他账号正常；存储不可用时不崩溃，后台仍禁止弹窗。Go race、服务与控制器构建以及真实 TSX 转译后的 React 渲染回归通过；完整论坛镜像由 CI 在发布前验证。

## Consequences

浏览器禁用存储时只能在当前组件内保留关闭状态。前端会话偏好不是服务端已读事实，不影响通知权限。

## Related notes

取代[原关闭修复](../../implemented/bug-fix/2026-11-15-badge-dismiss.md)中仅 Layout 存活期去重的决定，原通知数据库校验继续有效。其他 badge/notification 活跃笔记涉及聊天或等级，与此调整无关。
