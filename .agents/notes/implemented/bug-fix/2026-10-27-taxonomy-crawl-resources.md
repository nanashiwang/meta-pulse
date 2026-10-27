# Agent Note: 公开类别目录的抓取依赖

Status: implemented

## Problem

2026-10-27 Search Console 的 noindex 报告列出七个 URL。`/users/login`、`/users/register`、`/users/account-recovery`、`/pulse`、`/status` 符合既有排除策略；`/topics` 与 `/topics?view=tags` 是公开目录。Google 实时测试复现 `/topics` HTTP 200、初始 HTML 无 noindex，但渲染后出现 `noindex, follow` 且 canonical 被移除。资源面板明确显示 `/answer/api/v1/siteinfo`、`/answer/api/v1/tags?...` 被 robots.txt 拦截；账户 `/user/info` 被拦截符合预期。

目录依赖 siteinfo 的推荐标签；失败进入 app.js 的错误页和 SEO noindex。当前白名单只覆盖旧问题/回答/标签列表，未随类别能力扩展。类别介绍还依赖公开 `/tag?name=…`，侧栏常用标签依赖 `/tags?tags=…`。

## Decision

扩展已有 robots.txt 精确 Allow 规则，补齐 `siteinfo`、`tag`、`tags` 的端点本身和查询串，保持子路径默认禁止。复用 Answer 1.7.1 既有匿名 GET、taxonomy 模块与错误处理，不新增渲染服务或公开 API；JSON 的 X-Robots-Tag 仍为 noindex，原生写操作仍需认证。加入实际目录/侧栏请求经过 robots 规则的回归，避免只检验 HTTP 200 或路由名。

## Existing capabilities and note review

已核对能力地图、taxonomy.directory/catalog、Answer 原生路由、seo.js、seo.py、网关与三份 sitemap。类别数据仍来自 Answer；sitemap-site.xml 保留公开 `/topics`，私有入口不进入 sitemap。

- [类别治理](../../implemented/feature/2026-10-09-community-taxonomy.md)：部分重叠，继续负责数据源与申请；本篇记录抓取资源契约。
- [初始加载骨架](../../implemented/bug-fix/2026-10-05-community-loading-shell.md)：部分重叠，保留首屏、缓存和错误展示决定。
- [聊天](../../implemented/feature/2026-10-10-community-chat.md)、[成员侧栏](../../implemented/feature/2026-10-15-member-sidebar.md)：无关，私人消息与草稿不进入公开投影。

## Alternatives considered

- 只移除前端错误页 noindex：能消除报告表象，却让爬虫索引加载失败内容，真实目录仍不可读；不采用。
- 放开整个 Answer API：无需维护逐项白名单，但扩大无关账户、管理与写入资源的抓取范围；使用精确公开依赖。
- 新增 SSR 或静态复制类别目录：可减少渲染依赖，但引入第二套内容同步与故障恢复，现有匿名接口已满足需求，本次不采用。

## Verification

新增目录实际请求回归和 Python robots 回归在修复前均因 siteinfo 禁止抓取失败，修复后通过。覆盖类别、标签两种视图、分页、真实介绍与侧栏；相似前缀、子路径、身份/通知/管理/权益接口仍禁止。`make test-community` 通过 8 项 Python、137 项前端、6 项外观及 27 项插件 JavaScript 检查；博客/前端构建、隔离 Nginx 配置与 SEO、10 项发布回归通过。网关检查覆盖公开 sitemap 页与标签视图无 noindex、五个有意排除的报告 URL 和公开 JSON 仍 noindex。发布附件、生产版本和部署后的 Google 渲染终态分别验收，不能以本地结果代替。

## Consequences

公开目录沿用同一实时内容源，爬虫可获得实际类别与标签，不再因本站资源规则进入错误页。新增公开页面依赖时需要同步审查抓取白名单；目录请求回归只覆盖受影响模块，不能代替其他新页面的依赖核对。

robots Allow 不能按 HTTP 动词区分；安全依赖 Answer 原有认证，不能将抓取规则当访问控制。Google 对 robots 和索引存在独立缓存，部署或请求收录成功不等于已经收录。
