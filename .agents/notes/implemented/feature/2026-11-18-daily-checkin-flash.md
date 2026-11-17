# Agent Note: 服务器生成每日签到闪并复用Pulse结算

Status: implemented

## Problem

用户明确要求服务器先生成并保存具体签到闪金额，再提交现有Pulse闪到账流程。此前仅显示0.00/not_issued，没有发行逻辑。采用已确认0.01–1.50闪150档均匀随机，绑定前不补发；恢复2026-11-18起已签到缺记录。

## Existing capabilities and impact

复用成长签到事实、社区保护绑定、community-bff签名、Pulse RewardGrant/预算/SettlementOutbox/Benefit接收端和原source_ref重试。已有[签到回执](2026-11-18-checkin-receipt.md)仅负责显示；[周一周五赠券](2026-11-17-scheduled-checkin-rewards.md)仍独立运行。

## Decision

Forum后台根据真实签到事件建立独立持久待发记录，crypto/rand.Int无偏生成1–150分闪，事务提交后才能发送签名请求；同用户/日期与事件唯一，不重抽。Pulse按API用户/日期锁幂等，固定金额入账并在同一事务创建Grant、预算预留、结算Outbox及审计。独立内部draft快照存150档定义和不限量预算，不激活为普通抽奖奖池，不扣券、不改原活动规则；保留内部key前缀，不允许管理员/CLI发布为普通活动，普通活动窗口重叠检查排除该内部快照；后续请求只读定义。金额换算要求quota_per_unit可被100整除，拒绝舍入。GET状态只读，客户端不提交金额或身份。后台交付不依赖页面，重试保持固定金额及Grant；接收端限额继续生效。

## Alternatives considered

- 客户端随机再上传：无法信任且可刷金额，拒绝。
- 请求内直接调用new-api：跳过现有结算恢复边界，拒绝。
- 使用普通奖池某一奖项承载签到：混淆奖项定义与金额且会影响抽奖概率，使用独立内部快照；不激活，不进入普通抽奖选择。

## Verification

相关Pulse服务/数据库/HTTP/API/Worker及Forum主包/成长/社区赠券Go测试与vet通过，API/Worker构建通过；295项正式前端测试与构建通过。覆盖100次固定金额重放、payload变化冲突、整数范围及不可整除换算拒绝、Outbox失败回滚、只读归属、绑定变化不联系上游、原Worker结算及丢响应按原source_ref查询恢复、发放中到已到账不重新POST。新增真实MySQL 100路并发发行、三表一致性与来源隔离、Forum随机金额固定/跨日/非资格/迟到状态回归，本地无隔离MySQL未执行，待CI。浏览器以模拟服务器0.30闪回执验证390px深色发放中到已到账、经验2EXP保留，无横向溢出或脚本错误；不是生产实际发放验收。

## Consequences

新增Answer持久表随Forum库备份，完整升级Forum和Pulse后交付；不绕过new-api接收端限额。不在本次开发中直接修改生产余额。

## CI测试隔离修复

首次推送的MySQL8.0/8.4集成检查中，新签到测试残留9999年结束的内部快照。两个旧测试直接取整表MAX(ends_at)，导致后续活动开始年份超出允许范围，均报invalid period。现已在普通活动测试日期查询中排除内部签到快照，签到并发用例使用独立换算单位500100并清理所属Outbox、Grant、定义、预算、规则、幂等及审计；不修改生产经济校验。当地Go服务测试通过，完整MySQL矩阵以本次CI结果为准。
