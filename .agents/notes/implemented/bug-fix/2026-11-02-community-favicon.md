# Agent Note: 统一社区浏览器标签图标

Status: implemented

## Problem

2026-11-02 线上帖子 `/questions/10010000000000102` 与标签页的 HTML 使用 `/favicon.ico`，公开 siteinfo 的 branding 四项均为空。Answer 1.7.1 的 SSR 与 React PageTags 都在未配置图标时回退到此路径；该路径仍返回 Answer 默认蓝色图标。METAR 首页单独使用绿色 SVG，VitePress 未声明图标，因此跨入口品牌不一致。

## Decision

2026-11-05 后台用户页反馈仍显示旧蓝色图标。实时核对发现原生后台首屏 HTML 不声明图标，React PageTags 继续使用未版本化的 `/favicon.ico`，公开 branding 为空。根资源已是 METAR 原图，固定 URL 无法可靠刷新浏览器独立的 favicon 缓存。补充 `native-favicon.js`：插件加载时声明带 ICO 内容摘要的默认资源 URL，监听 head 中图标节点的建立/替换，只归一化同源无参数 `/favicon.ico`，并修正 type 为 image/x-icon。写入幂等且仅观察 head，适用于后台、登录及其他原生页面的 React 导航。管理员配置的其他图标保持原值，迟到的自定义图标会移除插件临时回退；不改品牌数据库。

使用用户随后提供的蓝色渐变原图 `production/src/images/brand-logo.jpg`，保留原图字节与白底。主页、加载占位、原生页头与博客标题统一引用该图片；构建时生成嵌入原图的自包含 SVG，并发布从原图导出的多尺寸 ICO。网关仅精确接管 `/favicon.ico`，返回真实 ICO 字节、正确类型及需重新验证的缓存策略；原生 SSR 与默认 manifest 继续共用根资源；React 默认图标另使用上述版本化路径。博客显式引用同源 SVG。VitePress 标题插槽绕过默认 logo 的 `/blog/` 前缀；原生页头用运行时 CSS 变量引用共享图片，防止 Answer 构建器将网关路径当成源文件解析。管理员已设置的其他图标 URL 继续由 Answer 控制，不改数据库和上游代码。

检索活跃笔记的 favicon/品牌/外观/appearance/avatar/theme，仅命中 taxonomy 抓取修复；它讨论 siteinfo 抓取权限，与图标路由无重叠，保持不变。

## Alternatives considered

- 只在 Answer 后台上传图标：能即时修复已初始化站点，但仓库默认回退仍错误，博客也遗漏；无法随版本交付完整修复。
- 只在插件用 JavaScript 改 head：不能单独覆盖 SSR 与 manifest，故保留根资源接管；针对浏览器旧缓存，补充小范围、幂等的默认链接归一化，并验证 Helmet 替换与自定义图标。
- 将 `.ico` 路径返回 SVG：无需派生产物，但格式、响应类型和 Answer 声明可能冲突；使用真正的多尺寸 ICO 保持兼容。

## Verification

新增回归覆盖 ICO 内容摘要与 URL 一致、后台无图标启动、Helmet 重建/改写、无重复写入、迟到自定义图标与触屏图标不受影响。Playwright 使用真实线上 Answer bundle、模拟管理员只读响应和本地待发布脚本验证后台 users 页，检查版本化链接与图片字节；该模拟不代表线上代码已经部署。

`make test-community`（8 项 Python、180 项 JavaScript）、`make build-blog build-community`、`make deploy-test`、10 项发布回归通过。隔离 Docker 的 Nginx 配置、SEO 与双网关检查覆盖根图标字节、MIME、HEAD、ETag/304、安全头、JPEG 和博客页头；构建测试确认 SVG 内嵌字节与原图一致，提交的 JPEG 与用户上传文件逐字节一致。

Pillow 检查六种 ICO 尺寸；Chrome 对本地 SVG/ICO 进行解码。主页本地构建与知识库在深浅主题检查，知识库 320/390/1280 像素宽度无横向溢出；线上原生页头仅在测试浏览器中覆盖新样式与图片，390 像素宽度无溢出。截图只验证 Logo 与页头显示，不作为内容加载或生产部署验收；生产配置与服务器未改动。

## Consequences

ICO 是用户原图的派生资源，换标时需要同步重新导出。浏览器既有 favicon 专用缓存可能需关闭旧标签重开；源站响应正确不代表所有客户端缓存即时消失。生产 Forum、网关和静态产物需一起更新。原生页头继续依赖已启用的视觉插件；离线原型和历史发布记录不作为生产品牌入口。
