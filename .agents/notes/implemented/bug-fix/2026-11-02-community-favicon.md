# Agent Note: 统一社区浏览器标签图标

Status: implemented

## Problem

2026-11-02 线上帖子 `/questions/10010000000000102` 与标签页的 HTML 使用 `/favicon.ico`，公开 siteinfo 的 branding 四项均为空。Answer 1.7.1 的 SSR 与 React PageTags 都在未配置图标时回退到此路径；该路径仍返回 Answer 默认蓝色图标。METAR 首页单独使用绿色 SVG，VitePress 未声明图标，因此跨入口品牌不一致。

## Decision

复用首页现有 `production/src/favicon.svg`，导出多尺寸 ICO 并随现有静态构建发布。网关仅精确接管 `/favicon.ico`，返回真实 ICO 字节、正确类型及需重新验证的缓存策略；原生 SSR、React 导航和默认 manifest 无需改动即可共用。博客显式引用同源现有 SVG。管理员已设置的其他图标 URL 继续由 Answer 控制，不改数据库和上游代码。

检索活跃笔记的 favicon/品牌/外观/appearance/avatar/theme，仅命中 taxonomy 抓取修复；它讨论 siteinfo 抓取权限，与图标路由无重叠，保持不变。

## Alternatives considered

- 只在 Answer 后台上传图标：能即时修复已初始化站点，但仓库默认回退仍错误，博客也遗漏；无法随版本交付完整修复。
- 在插件用 JavaScript 改 head：方便复用 UI 扩展，但 SSR 首屏仍返回旧图标，也需处理 Helmet 的后续覆盖。固定资源路径可以同时覆盖两种渲染。
- 将 `.ico` 路径返回 SVG：无需派生产物，但格式、响应类型和 Answer 声明可能冲突；使用真正的多尺寸 ICO 保持兼容。

## Verification

`make test-community`（8 项 Python、180 项 JavaScript）、`make build-blog build-community`、`make deploy-test`、10 项发布回归通过。隔离 Docker 的 Nginx 配置、SEO 与双网关检查通过，覆盖根图标真实字节、MIME、HEAD、ETag/304、安全头和全部博客 HTML。Pillow 检查六种尺寸；Chrome 直接通过本地 HTTP 解码 ICO 为 256×256。线上只读核对原生页面的图标引用；拦截线上请求替换文件的预览失败，不计为生产或浏览器替换验收。

## Consequences

ICO 是 SVG 的派生资源，换标时需要同步重新导出。浏览器既有 favicon 专用缓存可能需关闭旧标签重开；源站响应正确不代表所有客户端缓存即时消失。生产网关和静态产物需一起更新。
