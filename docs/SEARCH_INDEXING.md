# METAR 搜索引擎收录

适用域名为 `https://metar.uk/`，HTTP 与 www 继续永久跳转主域名。收录范围为公开首页、问答、标签、知识库、帮助与社区规范。登录、通知、收藏、账号绑定、管理、搜索结果和个人奖励不参与收录；认证边界不变，robots/noindex 不是访问控制。

普通 GET/HEAD 使用 301 永久跳转，兼容百度验证器；其他方法继续使用 308 保留请求方法与请求体。百度实际验证曾返回“未知原因:308”，不能仅以浏览器能打开判定站长平台能跟随跳转。

百度还可使用官方文件验证：网关在 HTTP/HTTPS 的精确地址 `/baidu_verify_codeva-5q1djDn6Ob.html` 返回下载验证文件的原始内容，并设置 noindex。它是公开的域名所有权证明；不开放其他明文站点页面，不接受动态文件路径，也不受静态目录重建影响。

## 网站端

- `/robots.txt` 向所有爬虫公布三个站点地图，不根据 User-Agent 返回不同内容。
- 公开问题列表、问题详情、回答列表和标签列表的只读查询允许爬虫获取，以便渲染页面；接口本身继续返回 noindex。其余账户、通知、管理与权益 API 保留抓取限制及原认证规则。禁止将整个 `/answer/api` 全部拦截而不保留这些精确例外，否则 Google 渲染首页时会因公开数据请求被阻止而得到错误页 noindex。
- `/sitemap.xml` 与 `/sitemap/*` 保留 Answer 原生动态实现，只包含 Answer 允许公开的问题；新问题由 Answer 自动纳入，缓存刷新由上游管理。不要把该 URL 替换成包含嵌套 sitemap index 的静态索引。
- `/sitemap-site.xml` 由正式前端构建生成，覆盖公开入口。
- `/blog/sitemap.xml` 由 VitePress 构建生成，所有文章 URL 保留 `/blog/` 前缀；新文章发布需重新构建。
- 首页和固定公开入口在初始 HTML 中提供说明与普通链接；实时列表与动态正文仍由 Answer 提供。帖子原生 `/questions/:id` 已有服务端正文、canonical 和 QAPage 结构化数据，壳层 `/question/:id` 指向它作为规范地址。公开帖子标题与描述在渲染后更新，失败页标记 noindex。
- 私人入口与 API 通过网关 `X-Robots-Tag` 标记 noindex；壳层站内跳转同步更新 robots/canonical，防止沿用上一页的索引状态。
- 博客不存在的地址返回 404，构建目录 `/blog/metar/` 不作为第二套公开页面。
- Google 与百度 HTML 验证标签来自站长账号的 `https://metar.uk/` 属性，是需要长期公开的验证信息，不是 API 凭据；标签保存在正式首页源码，构建和升级会持续保留。仅部署标签并不等于已验证或已收录。

## 平台提交

1. Google Search Console 添加 `https://metar.uk/`，部署后点击 HTML 标签验证。分别提交上面三个站点地图，并使用网址检查对首页及真实原创内容请求编入索引。平台显示成功提交也不代表已收录。
2. 百度搜索资源平台登录、添加并验证 `https://metar.uk/`。按当前账号实际开放的普通收录/链接提交能力操作；没有 Sitemap 权限时可提交公开 URL，不假定每个新站都有批量权限。
3. Bing Webmaster Tools 添加并验证该站点，可在平台允许时从 Search Console 导入；再提交三个站点地图。其他遵循 robots/sitemap 的爬虫也可以发现这些入口。

不要提交账户、通知、管理或奖励页，不使用包含登录 token、SSO 参数的 URL，不承诺排名或收录时间。站点地图只帮助发现内容，搜索引擎仍独立决定是否收录。当前线上仅观察到两篇 Answer 初始化示例帖；持续发布真实中文问题、可复现教程和原创经验对中文搜索价值更大，不应批量生成低质量占位文章。

## 验收

当百度验证节点与业务源站直连异常时，可使用自有前置反代；保持 DNS-only、HTTPS 回源与业务权限边界。配置、两端证书续期和回滚见 [前置反代说明](../deploy/nginx/RELAY.md)。入口线路改变后必须重新执行平台验证，不能只凭其他网络 curl 成功判断百度可达。

```bash
make test-community build-blog build-community
./deploy/nginx/test-config.sh
python3 deploy/nginx/test-seo.py
```

真实部署后检查首页原始 HTML、三个 XML、Googlebot/Baiduspider 访问、原生帖子正文、博客未知路径 404、私人页面 noindex；最后在站长平台查看验证、提交和索引状态。凭据不可用时明确保留部署/提交为待完成，不能把本地或 CI 成功作为生产证据。

参考：[Google JavaScript SEO](https://developers.google.com/search/docs/crawling-indexing/javascript/javascript-seo-basics)、[Google Sitemap](https://developers.google.com/search/docs/crawling-indexing/sitemaps/build-sitemap)、[百度搜索资源平台](https://ziyuan.baidu.com/)。
