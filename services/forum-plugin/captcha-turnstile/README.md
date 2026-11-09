# 社区 Cloudflare Turnstile 验证

插件编入 forum 镜像，复用 Answer 原生 Captcha 槽和服务端校验。发布安装包不会自动启用生产验证。

## 启用

1. 在 Cloudflare Turnstile 创建 Managed 类型站点，配置实际注册域名（例如 `metarhub.com`；使用 www 时也添加 `www.metarhub.com`），取得 Site Key 和 Secret Key。
2. 部署包含本插件的 forum 镜像，在 Answer 管理后台 → 插件找到 **Cloudflare Turnstile**。填写 Site Key、Secret Key 和逗号分隔的允许域名。域名必须精确匹配，不含协议、路径或通配符。
3. 保存配置并启用插件。Answer 同一时间只允许启用一个验证码插件。Secret Key 只在管理员配置和服务端保存；不要写入 Git、截图、更新说明或聊天。
4. 用未登录浏览器访问 `/users/register` 并提交有效字段：弹出人机验证后才能注册。验证失败、过期或加载失败时可重新验证或取消。用正常邮箱完成注册和激活，确认已有登录与找回密码流程。
5. `GET /answer/api/v1/captcha/config` 应返回 `slug_name: metar_turnstile`，`config` 仅含公开 `key` 与 `action`。匿名调用 `POST /answer/api/v1/user/register/email`，缺失或伪造 `captcha_code` 应返回验证码错误，不能创建账号；在隔离环境验证有效 token 的重复请求被拒绝。

## 边界

- 注册每次通过 Answer 的 email action 验证；找回密码、更换邮箱及原生高频写操作共用验证码。未触发阈值的普通登录和浏览保持原生行为。
- 后端固定访问 Cloudflare Siteverify，8 秒超时，不跟随重定向；只有 `success=true`、精确允许 hostname 和 `answer_captcha` action 同时满足才通过。Cloudflare 负责 token 的五分钟有效期与一次性消费；不缓存成功结果、不重试验证请求。
- 启用但未配置时拒绝验证；关闭插件会恢复 Answer 无验证码行为，因此不能把“已发布”当作“已保护”。必须完成上述部署和启用验收。
- Cloudflare 故障会使受保护操作失败；不会成为 Pulse、new-api relay、资金账本或普通浏览的依赖。
- 人机验证降低自动化注册风险，不能保证杜绝所有批量账号。管理员创建账号沿用 Answer 上游权限语义。

## 验证

`go test -race ./services/forum-plugin/captcha-turnstile/...`、`node --test services/forum-plugin/captcha-turnstile/*.test.js`、`make test-forum-captcha`、`make build-forum`。
