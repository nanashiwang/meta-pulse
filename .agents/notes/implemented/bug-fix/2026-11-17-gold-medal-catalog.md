# Agent Note: 金色评选勋章目录可见性

Status: implemented

## Problem

用户仍看不到编辑者和学者金色勋章。当前勋章页和荣誉页只渲染已获得的条目，未获奖用户看不到这两枚勋章及获得条件。

## Existing capabilities and impact

复用 [荣誉 API](../../../../services/forum-plugin/user-center-pulse/awards/honors.go) 的有效获奖计数与 [荣誉页](../../../../metar-frontend/production/src/award-honors.js)，不改授奖事实源。原生 Answer 行为勋章保留。[内容评选](../../implemented/feature/2026-11-16-content-awards-selection.md) 部分重叠并继续拥有授奖规则；原生勋章获奖提示与关闭笔记不拥有本次目录展示。

## Decision

两个入口使用共享目录渲染器，固定展示两枚金色图形、获得条件及未获得/已获得次数。加载失败显示状态不可用，不误报未获得。获奖记录和头衔选择保持现有行为。

## Alternatives considered

- 自动发给所有用户：会让目录可见，但伪造获奖事实，违反评选规则。
- 仅增加另一条导航：入口已存在，不能解决空数据隐藏目录的问题。

## Verification

共享目录回归覆盖无获奖、单类获奖、次数、失败状态及非法计数。282项前端测试及构建通过；隔离浏览器以模拟数据验证勋章页未获奖两枚可见、荣誉页编辑者两次与学者未获得、390px深色无横向溢出，无页面脚本错误。不是生产授奖验收。

## Consequences

目录展示不代表已授予；需更新静态资源。
