# Agent Note: 勋章等级与获奖上下文

Status: implemented

## Problem

Answer 的 nice/good question 与 answer 铜银勋章中文同名，且获奖弹窗仅显示名称，用户无法区分不同内容获奖、不同等级与重复提示。

## Decision

在 Answer 1.7.1 构建补丁 answer-badge-context.patch 中区分六个默认问题/回答勋章的中文等级。授予成功后复用 ObjService 读取获奖对象标题（回答显示所属问题标题），以纯文本快照加入通知 ExtraInfo，不改变授予、计数、阈值、去重和奖励规则。对象缺失/删除/查询失败时省略标题，不让展示信息影响已完成的授予。

成就通知保存标题和描述翻译键，在读取时翻译获奖条件；弹窗缓存按 notification_id 保存标题，接口返回可选 content_title/description，React 以文本插值显示，禁止 HTML 插入。沿用已有查看勋章详情入口，不根据未经校验的数据生成新链接。旧缓存和旧通知缺少上下文仍可正常读取，旧弹窗可显示当前勋章条件，不回填或重放历史授奖。

## Alternatives considered

- 只改名称：无法知道是哪篇内容触发提示。
- 前端为每个弹窗另查获奖列表：增加请求且排序变化时可能关联错内容。
- 取消重复获奖：改变社区规则，不属于本次授权。

## Verification

在实际 Go module 来源执行零模糊补丁检查通过。叠加既有补丁后的 badge、notification、notification_common 编译与 schema 测试通过；新增缓存往返、同勋章不同通知独立移除、旧缓存兼容及 JSON 转义回归。TSX 与接口格式检查通过。未在本地完成完整 React 类型检查及 Docker 镜像构建，交由现有 CI 的完整论坛构建验证；不能将格式检查视为类型检查。

## Consequences

新通知具备上下文，旧通知不猜测来源。标题是获奖时快照，后续编辑不自动更新，与原通知快照语义一致。需重建部署 forum 并完成 Answer 语言资源更新才影响实际界面；社区静态资源单独更新不足。部署不属于本次改动。
