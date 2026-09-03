# Meta Pulse 公网网关

公网固定采用分域拓扑：

```text
https://yourdomain.com                 new-api
https://yourdomain.com/blog/           VitePress
https://forum.yourdomain.com           Apache Answer
```

`pulse-api:8088` 仅存在于容器私网；公网 `/api/pulse/*` 必须进入 new-api Signed BFF，不能代理到 Pulse。

## 上线配置

1. 将 `meta-pulse.conf` 中的 `yourdomain.com` 替换为正式域名；
2. 挂载覆盖主域和论坛子域的证书：
   - `/etc/nginx/tls/fullchain.pem`
   - `/etc/nginx/tls/privkey.pem`
3. new-api 固定配置：

   ```text
   PULSE_FORUM_SSO_CALLBACK_URL=https://forum.yourdomain.com/api/user-center/login/callback
   ```

4. Answer 站点 URL 必须是 `https://forum.yourdomain.com`，插件的 `newapi_base_url` 必须是 `https://yourdomain.com`；
5. new-api、Answer 和 Pulse 服务不得直接发布宿主机端口，只由 Nginx 所在私网访问；
6. new-api 的 `session` Cookie 不得设置父域 `Domain=.yourdomain.com`。网关仍采用 Cookie allowlist，仅向 Answer 转发其 `visit` Cookie；
7. Login Ticket callback 不记录 query access log，并返回 `Referrer-Policy: no-referrer`。

历史 `/forum/*` 只做 308 跳转，不再反向代理 Answer，因此 new-api Session Cookie 不会进入论坛上游。

## 配置校验

本机有 Docker 与 OpenSSL 时运行：

```bash
./deploy/nginx/test-config.sh
```

脚本会先检查安全边界，再用临时自签证书执行 `nginx -t`，不会启动公网监听。
