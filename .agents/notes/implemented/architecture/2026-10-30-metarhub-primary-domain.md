# Agent Note: METAR 主域名迁移至 metarhub.com

Status: implemented

## Problem

用户要求今后从 metarhub.com 访问社区。现有入口是 metar.uk，经 155.103.118.155 固定 HTTPS 回源 NAN 104.168.114.150；Answer SiteURL 同时影响链接、邮件和插件写请求的同源检查，new-api 有固定 SSO callback，不能只新增 DNS 别名。

## Decision

复用现有双层网关、Answer 实例和静态构建，将正式入口、canonical、sitemap、博客链接与配置示例统一到 metarhub.com。两端独立签发新证书并保持严格回源验证。旧域名与 www 通过保留旧证书的可选兼容虚拟主机导向新站；旧域名 GET/HEAD 保留路径和查询串，写请求返回 409，避免迁移时重放状态变更。旧 SSO callback 丢弃参数，要求在新域名重新发起登录，不能跨域搬运 flow Cookie。

生产先准备 DNS/证书和备份，再切换 Answer SiteURL、new-api 固定回调与两层网关。账号、内容、绑定、奖励规则、业务账本和密钥不迁移。新域名使用新的浏览器会话；原域名本地草稿和偏好仍留在原浏览器存储中。

## Related note audit

- [NAN 迁移](../../implemented/process/2026-10-27-metar-server-migration.md)：部分重叠，沿用服务器和唯一写入端；该记录的历史域名保留。
- [基础设施可迁移性](../../implemented/architecture/2026-10-04-infrastructure-portability.md)：部分重叠，复用现有备份与恢复流程，不引入第二套业务实例。

## Alternatives considered

- 仅在前置机增加域名别名：改动最少，但 Answer 同源写校验、SSO 回调和 SEO 仍使用旧域名，无法完成正式迁移。
- DNS 直接指向 NAN：可少一跳，但无须为了域名变更同时替换既有线路和可信代理边界，继续复用前置机。
- 旧域名所有方法 308：可保留请求体，但会跨域重放写请求且不能搬运 Cookie；只迁移浏览入口，旧写请求明确拒绝。

## Verification

正式前端回归、博客与社区构建、两个真实 Nginx 的 TLS/认证头/ACME/旧域名跳转测试、网关配置与 SEO 回归通过。两端新证书已独立签发；生产切换、续期演练和真实账号流程仍必须按部署步骤另行核验，不以源码测试替代。

## Consequences

换域名需要重新登录，浏览器草稿与偏好不自动迁移。已有 SSO 在途 flow 不能跨域继续，用户必须重新发起。回退只恢复同一实例的入口与站点配置，不能恢复旧服务器业务库或改写账本。
