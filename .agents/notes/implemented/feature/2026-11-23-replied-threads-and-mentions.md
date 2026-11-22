# Agent Note: 找到自己的回复和评论提及用户

Status: implemented

## Problem

成员难以找回参与过的帖子，原生评论选人偏向工作人员且回复他人时会提前跳过提及通知。

## Existing capabilities and impact

个人空间复用社区壳层；评论和通知沿用 Answer 原生机制及已支持的评论定位链接。

## Decision

按本人当前公开回答和评论聚合主题。原生评论选人先显示当前讨论成员，搜索匹配用户名或昵称。通知从已发布的正文提及生成，不信任客户端名单。

## Alternatives considered

不另建活动索引或通知系统，不新增 AI 功能。

## Verification

隔离 MySQL 验证同帖合并、归属、标题搜索和已隐藏/删除内容过滤；浏览器验证分页及回复定位链接、候选选择、中文昵称查询、键盘与鼠标操作。原生 Answer Go 包、TypeScript、ESLint 和正式前端构建通过。

## Consequences

列表查询 Answer 的现有内容，不新增记录表。提及只限评论与回复，单条最多提醒五位，用户自身不发提醒。
