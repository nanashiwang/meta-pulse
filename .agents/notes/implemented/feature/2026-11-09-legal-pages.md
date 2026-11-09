# Agent Note: 服务条款与隐私政策由 METAR 页面承载

Status: implemented

## Problem

页脚的「服务条款」「隐私政策」指向 Answer 原生 `/tos`、`/privacy`，内容取决于 Answer 后台 Legal 设置，仓库没有正文，无法与实际数据处理（绑定、Pulse、经营统计 Cookie、聊天、Turnstile、工具页）一起评审和版本化。用户要求撰写并直接上线。

## Decision

正文写在 `metar-frontend/production/src/seo-pages.json` 的 `/tos`、`/privacy` 条目，作为唯一来源：`seo.py` 生成无脚本/SEO 页面并收入站点地图，`build.py` 从同一条目生成 `legal.js` 按需加载给 METAR 壳渲染。两路由登记在 `metar-frontend/shared/routes.json`，再由 `generate-routes.py` 生成浏览器、论坛插件和 Nginx 的路由白名单；SEO 文档映射与页脚站内路由同步。生成产物不能手改，CI 的 `--check` 必须通过。正文仅提供中文，英文界面显示“以中文版本为准”提示。

内容依据仓库现状核对：Answer 账号与 `visit` 会话、可选一对一绑定只取 new-api 标识/用户名/显示名、社区任务赠券与无现金价值的奖励、`metar_ops` 90 天哈希访客标识及 DNT/GPC、网关日志不含查询参数和 Referer、聊天非端到端加密且不自动清理、账本只追加不删除、工具页本地计算与 DNS 探测内存 90 秒。

## Alternatives considered

在 Answer 后台填写 Legal 设置：无需改代码，但正文不进仓库、无法随功能变更同步评审，且需要生产管理员手工操作。正文另写一份 JS 再复制到 SEO JSON：会产生两份易漂移的文本。提供英文全文：法律文本双语维护成本高、翻译歧义风险大，暂以中文为准。

## Testing

`test_seo.py` 断言两页 SEO HTML、站点地图和 `legal.js` 生成；`router.test.js` 断言 Nginx 白名单与前端路由一致；前端 189 项、插件 57 项 Node 测试与部署脚本回归通过。本地构建产物在浏览器中渲染 `/tos`、`/privacy`，章节完整、页脚链接为站内路由。

## Consequences

修改法律正文只需改 `seo-pages.json` 并重新部署静态资源与网关；Answer 后台 Legal 设置不再被页脚使用。运营主体名称、专用联系邮箱与运营方所在地尚未写入，联系渠道暂为站内聊天和「站务建议」。涉及新增数据处理的功能上线时，应同步更新隐私政策。
