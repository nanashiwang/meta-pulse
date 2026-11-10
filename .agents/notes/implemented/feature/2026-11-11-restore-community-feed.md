# Agent Note: 恢复首页共用讨论列表

Status: implemented

## Problem

用户希望恢复首页和 latest 的紧凑讨论列表，保留 PR 新顶栏、个人导航与声望榜。

## Decision

`/` 与 `/latest` 共用原有 questionsPage 和 Answer API，社区导航统一指向 `/latest`。精确 `/questions` 和 `/discover` 提供相同壳层后由路由 replaceState 规范到 latest，保留查询和旧 Hash；不做反向 HTTP 重定向，避免已缓存的 v0.2.117 首页 301 形成循环。帖子详情、回答、编辑和发帖仍归 Answer。保留 PR 顶栏、个人导航、声望榜及后台权限。

## Alternatives considered

整包回退 PR 最简单但会删除用户要求保留的功能。把 questions 直接重定向到 latest 可节省一次渲染，但可能与旧永久重定向缓存循环，采用同壳层兼容入口。首页与 latest 独立实现会重复维护列表逻辑，因此复用旧页面及筛选分页。

## Related notes

[社区导航 PR 集成](2026-11-11-community-navigation-integration.md)的首页选择被本篇取代，顶栏与排行榜决定继续适用；既有模型榜决定无关。

## Consequences

首页和 latest 复用既有列表、筛选与分页，保留新版顶栏、个人中心、声望榜和权限边界。旧列表地址使用同壳层兼容而非 HTTP 反向跳转，增加一个别名但避免缓存永久重定向形成循环。帖子详情和发帖仍由 Answer 处理。需配套升级静态资源、Forum 插件与网关；发布不代表生产部署完成。

## Testing

前端与插件 Node 测试、Python 构建测试、make test-community、make deploy-test、博客和社区构建通过。真实 Nginx 容器验证首页和别名 GET/HEAD、SEO、原生帖子与发帖转发，以及代理安全边界。Playwright 使用本地构建读取线上匿名讨论，核对桌面与手机布局及分页；登录态功能沿用既有权限回归测试。笔记与工作流检查随本次提交执行。
