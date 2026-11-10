# Agent Note: 社区导航 PR 集成

Status: implemented

## Problem

PR #1 统一原生社区入口、个人中心和声望排行榜，但旧网关断言导致 CI 失败，旧 Hash 书签与无历史返回还需兼容；模型榜标题也意外回退。

## Decision

首页选择已由[恢复首页共用讨论列表](2026-11-11-restore-community-feed.md)取代，下述原生首页方案保留为历史决定；顶栏、个人导航与声望榜继续适用。

复用 PR 的 Answer `/questions` 社区入口、`/users` 近七天声望榜及 `/me` 专用导航。根路径、latest/discover 重定向保留查询；原生桥接在 `/questions` 恢复旧 Hash 壳层入口，不接管敏感回调。移除公共旧侧栏但保留管理后台导航；管理入口仍检查真实身份与角色。保留模型评分榜标题及已移除近期模型模块的决定。

排行榜仅使用 Answer 原生前 20 条，剔除工作人员后按返回顺序展示，分页不冒充全站 Top500。数据与权限事实源不变。

## Alternatives considered

直接合并可最快交付，但留下失败检查与已确认回归；只修测试不能解决历史链接丢失。另建排行榜后端可扩大人数，但超出 PR 范围，保留当前上游数量限制。

## Related notes

[社区导航暂时收紧](../../implemented/simplification/2026-10-15-community-navigation.md)部分重叠：公共左栏布局由本篇取代，保留撤下帮助/知识库入口的范围。[模型页精简](../../implemented/simplification/2026-11-10-model-pages.md)独立负责模型榜，不回退其标题与入口。

## Verification

完整社区前端测试、Go 模块测试和 vet、部署脚本回归、博客/前端构建通过。真实 Nginx 配置、SEO 与中转容器测试验证查询参数重定向、原生路径归属、Cookie/身份头隔离及 TLS 失败关闭；导航桥测试覆盖旧 Hash、外站/编码路径拒绝及敏感回调不接管。

## Consequences

原生入口使用 Answer 渲染，旧壳层首屏 SEO 不再承担首页；排行榜人数受原生 API 限制。所有组件需配套升级，生产部署单独验收。PR 与发布 CI 状态以实际工作流结果核实，不以本地检查代替。
