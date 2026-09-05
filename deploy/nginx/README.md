# Meta Pulse 社区公网网关

新社区域名以论坛为主、博客为辅：

```text
https://<社区域名>/       Apache Answer
https://<社区域名>/blog/  VitePress
```

new-api 继续运行在原域名/服务器。社区网关**不代理 new-api 或 Pulse**；`pulse-api:8088` 只允许 new-api Signed BFF 和内部服务通过受控私网访问。

## 上线配置

1. 将 `meta-pulse.conf` 中 `community.yourdomain.com` 替换为正式社区域名；
2. 挂载证书：

   ```text
   /etc/nginx/tls/fullchain.pem
   /etc/nginx/tls/privkey.pem
   ```

3. Answer 站点 URL 设置为 `https://<社区域名>`，开启本地注册/密码登录并配置发信；
4. Answer 插件 `newapi_base_url` 指向现有 new-api HTTPS 根地址；
5. new-api 仅配置现有 SSO Bridge：

   ```env
   PULSE_FORUM_SSO_SECRET=<与插件 sso_hmac_secret 一致>
   PULSE_FORUM_SSO_CALLBACK_URL=https://community.yourdomain.com/api/user-center/login/callback
   ```

6. 使用原编排零停机重建 new-api；无需修改 new-api 源码或在社区服务器部署 new-api；
7. Nginx 必须能通过容器私网访问 `forum:80`，并挂载 VitePress 构建目录到 `/var/www/blog`；
8. 所有业务容器保持无宿主机 `ports`，不得直接暴露 Answer/Pulse/MySQL/Redis。

## 安全边界

- 普通论坛请求只向 Answer 转发 `visit` Cookie；
- 固定 callback 只转发 `meta_pulse_forum_flow` Cookie；
- callback 清除 Authorization；普通请求保留 Answer 前端自己的 Authorization，否则登录后的 Answer API 无法工作；两类请求都清除 Pulse 签名头；
- HTTP/HTTPS 统一使用不含 query/Referer 的 `community_no_query` 日志格式；callback、`/users/auth-landing`、`/users/confirm-email` 额外 `access_log off`，并返回 `Cache-Control: no-store`、`Referrer-Policy: no-referrer`；
- new-api session Cookie 不进入 Answer；
- 网关配置中不得出现 new-api/Pulse upstream 或 proxy_pass；
- callback 内部 rewrite 到 `/answer/api/v1/connector/redirect/pulse_user_center`，query 由 Nginx 保留给插件验签。

现有 new-api Ticket 尚未签名 OAuth state；短期 HttpOnly flow 只是额外门禁。不得取消固定 callback、邮箱确认、单次 nonce 或 Binding Guard 来“简化”流程。

## 配置校验

本机有 Docker 与 OpenSSL 时运行：

```bash
./deploy/nginx/test-config.sh
```

脚本先静态检查 Cookie/header/upstream 边界，再用临时自签证书执行 `nginx -t`，不会启动公网监听或连接业务服务。
