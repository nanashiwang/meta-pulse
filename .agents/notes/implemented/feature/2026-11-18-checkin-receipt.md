# Agent Note: 签到实际到账明细

Status: implemented

## Problem

用户要求删除获取券区域的每券2.5闪期望说明，并显示本次签到实际到账的经验与闪金额。当前签到接口返回成长总览，checkin_amount是下次可领取额度，已签到后为0，不能用它推断本次经验。当前签到只发经验与签到券，没有直接闪发放，不能把抽奖期望或抽奖奖励当成签到到账。

## Existing capabilities and impact

复用 [成长账本](../../../../services/forum-plugin/user-center-pulse/growth/store.go) 的 metar_exp_event/metar_exp_ledger 和已认证 summary/checkin 路由；复用 [个人空间签到](../../implemented/feature/2026-11-17-profile-checkin.md) 与 [Pulse签到面板](../../implemented/feature/2026-11-17-scheduled-checkin-rewards.md)。这两篇继续拥有入口和赠券规则，本篇补充实际到账投影。不新增随机闪发行、金额规则或结算路径。

## Decision

Summary返回可选checkin_receipt，按当前上海自然日从本人签到事件的账本净额投影experience；day用于标识回执。flash_amount为0.00且flash_status=not_issued，明确没有直接签到闪。已签到重访与丢响应恢复都读同一账本，不能按余额差、总经验、现有规则金额或抽奖记录猜金额。两个入口显示服务器回执；旧服务/读失败时显示明细暂不可用，不显示假零。

## Alternatives considered

- 使用checkin_amount或签到前后余额差：请求少，但前者签到后为0且规则可能变化，后者会混入同时到账的其他经验。
- 把抽奖闪金额合并到签到：能得到非零显示，但来源不同，会把抽奖所得误称为签到发放。

## Verification

292项正式前端测试通过，Forum主包/成长/社区赠券Go测试与vet通过，正式前端构建成功。新增重复签到、规则变更、其他奖励和跨日隔离、日上限零到账的MySQL回归用例；本地无隔离MySQL，待CI执行。前端覆盖两入口金额、已签到重访、丢响应恢复、未绑定不调用Pulse，以及缺失/非法回执不假报0。隔离浏览器以模拟回执验证320px深色下期望说明消失、实际2 EXP与0.00闪（未发放）可见且无横向溢出，不代表生产真实签到。

## Consequences

需同时升级Forum与静态资源才有完整明细；本次不启用先前讨论的每日0.01–1.50随机闪。
