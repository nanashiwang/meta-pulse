# Meta Pulse 社区公网网关

正式社区入口：

```text
https://metar.uk/       Apache Answer（主入口）
https://metar.uk/blog/  VitePress
https://www.metar.uk/   308 跳转至 metar.uk
```

new-api 继续运行在原域名和原服务器。社区网关**不代理 new-api 或 Pulse**；`pulse-api:8088` 只允许 new-api Signed BFF 和内部服务通过受控私网访问。

## 上线配置

先将 `docker-compose.override.example.yml` 复制或合并到仓库根目录 `docker-compose.override.yml`；若已有 WireGuard 的 `pulse-api.ports`，必须保留原配置。然后执行：

```bash
./deploy/build-blog.sh
mkdir -p .data/certbot/.well-known/acme-challenge
chmod 755 .data/certbot .data/certbot/.well-known .data/certbot/.well-known/acme-challenge
```

1. Nginx 必须加入 Meta Pulse Compose 网络，通过容器 DNS 访问 `forum:80`；不得发布 Answer 宿主机端口；
2. 将宿主机完整的 `/etc/letsencrypt` 只读挂载到容器同路径。Nginx 使用：

   ```text
   /etc/letsencrypt/live/metar.uk/fullchain.pem
   /etc/letsencrypt/live/metar.uk/privkey.pem
   ```

   不要只挂载 `live/` 中的单个软链接文件，否则续期后容器可能继续读取旧证书。

3. 将 ACME webroot 挂载到 `/var/www/certbot`。首次签发可在 80/443 尚未监听时使用 standalone，后续使用 webroot 自动续期；
4. 将 VitePress 构建目录挂载到 `/var/www/blog`；
5. Answer 站点 URL 设置为 `https://metar.uk`，开启本地注册和密码登录并配置发信；
6. Answer 插件配置：

   ```text
   newapi_base_url=https://cn.meta-api.vip
   pulse_base_url=http://pulse-api:8088
   nonce_redis_url=redis://redis:6379/2
   ```

7. new-api 仅配置现有 SSO Bridge：

   ```env
   PULSE_FORUM_SSO_SECRET=<与插件 sso_hmac_secret 一致>
   PULSE_FORUM_SSO_CALLBACK_URL=https://metar.uk/api/user-center/login/callback
   ```

8. 使用原编排重建 new-api；无需修改源码或在社区服务器部署 new-api；
9. 所有业务容器保持无宿主机 `ports`，不得直接暴露 Answer、MySQL 或 Redis。Pulse API 仅可绑定受控私网地址；
10. 证书首次签发并启动网关后执行：

    ```bash
    sudo ./deploy/nginx/install-renewal-timer.sh
    sudo ./deploy/nginx/renew.sh --dry-run
    ```

    后续 `./deploy/update.sh` 会在宿主机覆盖中检测 `gateway`，自动更新博客并热重载 Nginx。

## 安全边界

- 普通论坛请求只向 Answer 转发 `visit` Cookie；
- 固定 callback 只转发 `meta_pulse_forum_flow` Cookie；
- callback 清除 Authorization；普通请求保留 Answer 前端自己的 Authorization；两类请求都清除 new-api 身份头与 Pulse 签名头；
- HTTP/HTTPS 使用不含 query/Referer 的 `community_no_query` 日志格式并写入 stdout；Compose 示例限制日志大小，callback、`/users/auth-landing`、`/users/confirm-email` 额外关闭访问日志；
- new-api session Cookie 不进入 Answer；
- 网关配置中不得出现 new-api/Pulse upstream 或 proxy_pass；
- callback 内部 rewrite 到 `/answer/api/v1/connector/redirect/pulse_user_center`，query 只交给插件验签；
- `www.metar.uk` 只做主域名跳转，不承载独立会话。

现有 new-api Ticket 尚未签名 OAuth state；短期 HttpOnly flow 只是额外门禁。不得取消固定 callback、邮箱确认、单次 nonce或 Binding Guard 来“简化”流程。

## 配置校验

本机有 Docker 与 OpenSSL 时运行：

```bash
./deploy/nginx/test-config.sh
```

脚本先静态检查 Cookie、Header、回调和 upstream 边界，再用临时自签证书执行 `nginx -t`，不会启动公网监听或连接业务服务。
