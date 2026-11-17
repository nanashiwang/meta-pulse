# Agent Note: 个人空间直接签到入口

Status: implemented

## Problem

用户要求先完成签到入口，在 /me 资料卡右上角、公开主页左侧直接签到，完成后提交推送。随机闪与每周活动券仍是设计方案，本次仅接入当前已运行的签到。

## Existing capabilities and impact

复用 [个人空间](../../../../metar-frontend/production/src/app.js) 与 Answer 的 /metar/experience/summary、/metar/experience/checkin。服务端按用户与日期幂等，浏览器只提交空对象。现有 [签到刷新](../../implemented/bug-fix/2026-11-13-checkin-ticket-refresh.md) 保留 Pulse 的赠券查询与恢复；本篇取代其中“个人空间移除签到按钮、仅 Pulse 提供签到”的入口决定。[社区赠券](../../implemented/feature/2026-11-06-community-tickets.md) 继续拥有发券来源、绑定与日限。[券统计与签到同步](../../implemented/bug-fix/2026-11-13-ticket-statistics-and-checkin.md) 的独立交付标志保留。本次无新增后端接口或财务规则。

## Decision

仅活跃账号展示按钮，已签到时禁用；加载、重试和提交均查询服务端状态。提交成功刷新经验卡片；丢失响应后只读核对，尚未确认则可重试。同一页禁止并行提交，切页或身份变化后旧响应不再更新界面。未绑定账号使用同一经验签到接口，无 Pulse 依赖。

## Alternatives considered

- 链接到 /pulse：改动少且可复用整页，但需要离开个人空间才能签到，不符合本次直接入口的使用预期。
- 直接复用 Pulse 任务面板：有赠券查询能力，但需读取绑定和 Pulse 状态；个人空间只需确认经验签到，不应为按钮加载整套任务面板。

## Verification

新增 6 项签到回归覆盖已签到、首次签到、重复点击、接口故障、丢响应及切页/身份变化；合入远程最新 main 后正式前端 278 项测试通过，构建成功；Python 构建/SEO 检查 8 项通过、1 项 POSIX umask 用例因 Windows 不支持而跳过。项目笔记检查通过。隔离本地浏览器验证按钮在公开主页左侧、签到后经验从 155 更新到 165、320px 无横向溢出；使用模拟接口，不代表生产实际签到。

## Consequences

当前签到仍按既有经验和已绑定用户每日一券规则执行，不宣称已上线随机闪。前端推送后需更新静态资源才会出现在生产；Pulse 页面继续展示异步赠券进度。

## 签到实际明细（2026-11-18）

[签到实际到账投影](2026-11-18-checkin-receipt.md) 补充两个入口的当日签到账本净经验与明确闪状态；移除用户端每券期望说明，不改变既有经验、签到赠券或抽奖奖池。
