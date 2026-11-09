# Agent Note: 社区注册接入 Cloudflare Turnstile

Status: implemented

## Problem

2026-11-09 只读查询线上 `/answer/api/v1/captcha/config` 返回空 slug，注册没有人机验证。注册账号能参与社区任务，需阻止直接调用注册 API 绕过网页验证。

## Decision

复用 Answer v1.7.1 的 Captcha 插件、原生注册表单和服务端 email action 校验。新增独立 captcha-turnstile 插件并编入现有 forum 镜像；它与 UserCenter 身份和 Pulse 业务无耦合。管理员配置域名、Site Key、Secret Key 后启用。服务端只向固定 Cloudflare Siteverify 地址发送 token 与 secret，校验 success、hostname、固定 action；缺配置、错误和超时一律拒绝。前端使用 Answer captcha hook，失败、过期、重复提交和卸载时清理 token/widget。

现有能力核对覆盖 `/users/register`、`POST /answer/api/v1/user/register/email`、`/user/action/record`、`/captcha/config` 和管理插件页面。Answer 的 email action 每次验证，密码错误及其他操作沿用上游阈值。没有重写注册 API、身份或邮箱激活；现有 UserCenter 前端槽是 editor，故验证码使用独立插件槽。

## Alternatives considered

只加前端组件可绕过，不能满足接口保护。Cloudflare 网关规则需要代理流量且扩大网站影响范围；当前站点仅 DNS，选择应用内验证。将验证码塞入 UserCenter 会混合 editor 与 captcha 插件槽，独立插件复用上游生命周期更清晰。

## Testing

Go race 测试通过，覆盖失败关闭、请求编码、域名/action、过期重放结果、配置并发及不缓存成功令牌。9 项 JS 测试覆盖一次消费、重试、取消、卸载、脚本失败和超时；Answer 原生 email action 回归通过。Playwright 使用模拟 Cloudflare 回调验证弹窗、失败重试、重复回调仅提交一次和 Escape 清理。forum 镜像构建通过；现有论坛 Go 测试、社区前端回归、部署回归及 31 项发布测试通过。真实生产密钥与 Cloudflare 完整注册验收待运营配置；模拟回调不代表生产验证成功。

## Consequences

发布不自动启用生产验证码。需要真实域名的 Turnstile 密钥，在 Answer 后台保存并启用；不能将测试密钥用于生产验收。启用后 Answer 共用验证码的找回密码和高频写操作也使用 Turnstile；Cloudflare 不可用时相关受保护操作被拒绝，普通浏览和未触发阈值的登录不受影响。验证不能杜绝所有人工或代理批量注册。
