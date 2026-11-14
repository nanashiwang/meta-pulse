# Agent Note: 两行讨论列表与参与者头像

Status: implemented

## Problem

列表底部作者时间与最后回复占据第三行，无法快速看到讨论参与者和浏览量。

## Decision

复用 questionRow；标题后放主分类，第二行保留摘要。统计前展示作者及最近公开回答者，按用户去重，合计最多五人。Answer 列表批量查询每帖最近五位不同回答者，再与作者合并，复用用户资料批量查询。辅助查询失败时保留作者，不阻断列表。只读公开回答，不把编辑者当作参与者，不读取正文或私人字段。

## Alternatives considered

逐帖查询回答可以复用现有接口，但会放大每页请求量；仅使用 last_reply 无法展示多位参与者。采用有界批量投影，增加一个兼容字段。

## Related notes

现有 question-author 笔记的作者事实源继续适用，旧第三行布局由本篇取代；restore-community-feed 的路由和列表复用决定不变，其他导航笔记无关。

## Verification

前端回归 make test-community、正式静态构建通过；Answer 叠加补丁编译与 SQLite 实际查询测试覆盖公开状态过滤、用户去重、每帖五人上限、跨帖子隔离、空列表和数据库错误。完整现有补丁叠加测试通过。桌面与手机使用本地资源和线上匿名数据做浏览器验收。

## Consequences

需升级 forum 才能获取完整参与者；SQL 窗口投影依赖项目规定的 MySQL 8。不改变排序、权限、奖励或账本。
