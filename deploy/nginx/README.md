# Meta Pulse 社区公网网关

正式社区入口：

```text
https://metarhub.com/       METAR 正式首页（真实 Answer API 数据）
https://metarhub.com/questions  Apache Answer 原生论坛
https://metarhub.com/blog/  VitePress
https://www.metarhub.com/   308 跳转至 metarhub.com
```

new-api 继续运行在原域名和原服务器。社区网关**不代理 new-api 或 Pulse**；`pulse-api:8088` 只允许 new-api Signed BFF 和内部服务通过受控私网访问。

## 上线配置

先将 `docker-compose.override.example.yml` 复制或合并到仓库根目录 `docker-compose.override.yml`；若已有 WireGuard 的 `pulse-api.ports`，必须保留原配置。然后执行：

```bash
./deploy/build-blog.sh
./deploy/build-community.sh
mkdir -p .data/certbot/.well-known/acme-challenge
chmod 755 .data/certbot .data/certbot/.well-known .data/certbot/.well-known/acme-challenge
```

1. Nginx 必须加入 Meta Pulse Compose 网络，通过容器 DNS 访问 `forum:80`；不得发布 Answer 宿主机端口；
2. 将宿主机完整的 `/etc/letsencrypt` 只读挂载到容器同路径。Nginx 使用：

   ```text
   /etc/letsencrypt/live/metarhub.com/fullchain.pem
   /etc/letsencrypt/live/metarhub.com/privkey.pem
   ```

   不要只挂载 `live/` 中的单个软链接文件，否则续期后容器可能继续读取旧证书。

3. 将 ACME webroot 挂载到 `/var/www/certbot`。首次签发可在 80/443 尚未监听时使用 standalone，后续使用 webroot 自动续期；
4. 将 VitePress 构建目录挂载到 `/var/www/blog`；METAR 正式前端会构建到该目录的 `metar/` 子目录，无需增加新的宿主机挂载；
5. Answer 站点 URL 设置为 `https://metarhub.com`，开启本地注册和密码登录并配置发信；
6. Answer 插件配置：

   ```text
   newapi_base_url=https://meta-api.vip
   pulse_base_url=http://pulse-api:8088
   nonce_redis_url=redis://redis:6379/2
   ```

7. new-api 仅配置现有 SSO Bridge：

   ```env
   PULSE_FORUM_SSO_SECRET=<与插件 sso_hmac_secret 一致>
   PULSE_FORUM_SSO_CALLBACK_URL=https://metarhub.com/api/user-center/login/callback
   ```

   同时在 new-api 原站 Nginx 的 HTTPS `server` 内加入固定同源 bootstrap（可直接使用仓库的 `deploy/newapi/forum-sso-bootstrap.conf`），否则 new-api 的 `SameSite=Strict` session Cookie 在从 `metarhub.com` 跨站首跳时不会发送：

   ```nginx
   location = /api/forum/sso/bootstrap {
       default_type text/html;
       add_header Cache-Control "no-store" always;
       add_header Referrer-Policy "no-referrer" always;
       add_header X-Content-Type-Options nosniff always;
       add_header X-Frame-Options DENY always;
       add_header Content-Security-Policy "default-src 'none'; base-uri 'none'; frame-ancestors 'none'" always;
       return 200 '<!doctype html><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=/api/forum/sso/start"><a href="/api/forum/sso/start">继续</a>';
   }
   ```

8. 使用原编排重建 new-api；无需修改 new-api 业务源码或在社区服务器部署 new-api；Nginx reload 即可加载上述 bootstrap。
9. 所有业务容器保持无宿主机 `ports`，不得直接暴露 Answer、MySQL 或 Redis。Pulse API 仅可绑定受控私网地址；
10. 证书首次签发并启动网关后执行：

    ```bash
    sudo ./deploy/nginx/install-renewal-timer.sh
    sudo ./deploy/nginx/renew.sh --dry-run
    ```

    后续 `./deploy/update.sh` 会在宿主机覆盖中检测 `gateway`：博客或 METAR 前端变化时按正确顺序重建静态产物，并校验后热重载 Nginx。

## 安全边界

- 根路径 `/` 与白名单 History 路径（如 `/latest`、`/topics`、`/question/:id`）提供无 mock 的静态壳层；其脚本只访问同源 Answer API，Pulse 尚未接 BFF 时不展示虚构权益；
- `/questions`、`/users/*`、`/answer/api/*` 等普通论坛请求仍由 Answer 处理，并只向 Answer 转发 `visit` Cookie；
- Answer v1.7.1 的 `/answer/api/v1/user-center/agent` 响应在网关定点归一化：登录固定进入 `pulse_user_center` Connector，注册固定返回 `/users/register`；该代理关闭压缩与缓存，避免本地注册被 UserCenter 接管；
- UserCenter 的 `/login/redirect`、`/sign-up/redirect` 及其旧空跳转父路径仅作兼容兜底，不承载独立认证逻辑；
- 固定 callback 只转发 `meta_pulse_forum_flow` Cookie；
- callback 清除 Authorization；普通请求保留 Answer 前端自己的 Authorization；两类请求都清除 new-api 身份头与 Pulse 签名头；
- HTTP/HTTPS 使用不含 query/Referer 的 `community_no_query` 日志格式并写入 stdout；Compose 示例限制日志大小，callback、`/users/auth-landing`、`/users/confirm-email` 额外关闭访问日志；
- new-api session Cookie 不进入 Answer；
- 网关配置中不得出现 new-api/Pulse upstream 或 proxy_pass；
- callback 内部 rewrite 到 `/answer/api/v1/connector/redirect/pulse_user_center`，query 只交给插件验签；
- `www.metarhub.com` 只做主域名跳转，不承载独立会话。

现有 new-api Ticket 尚未签名 OAuth state；短期 HttpOnly flow 只是额外门禁。不得取消固定 callback、邮箱确认、单次 nonce或 Binding Guard 来“简化”流程。

## 配置校验

本机有 Docker 与 OpenSSL 时运行：

```bash
./deploy/nginx/test-config.sh
```

脚本先静态检查 Cookie、Header、回调和 upstream 边界，再用临时自签证书执行 `nginx -t`，不会启动公网监听或连接业务服务。


## 从 metar.uk 迁移至 metarhub.com

复用现有前置机和 NAN，两个域名只连接同一个 Answer/Pulse 实例。先完成以下准备，再切换旧入口：

1. `metarhub.com` 的 DNS-only A 指向 `155.103.118.155`，`www` CNAME 指向主域名。前置机与源站各自签发包含两个新主机名的独立证书，禁止复制私钥或关闭回源 TLS 校验。
2. 备份两端网关、Answer `site_info` 的 general 配置及 new-api 的固定回调。源站按正式发布流程构建博客、METAR 和服务；切换前核对新静态资源的 canonical/robots/sitemap。
3. Answer「站点设置 → 通用」的站点 URL 改为 `https://metarhub.com`；new-api「系统设置 → Meta Pulse」的 `PulseForumSSOCallbackURL` 改为 `https://metarhub.com/api/user-center/login/callback`。仅修改该字段，不清空其他配置或密钥。Answer URL 同时用于插件的同源写校验，不可仅在前置机改 Host。
4. 加载新的 `meta-pulse.conf` 与 `metar-relay.conf`。已有站点另加载可选的 `metar-legacy-redirect.conf`：源站 Compose 为 gateway 增加只读挂载至 `/etc/nginx/conf.d/metar-legacy-redirect.conf`，前置机将其放在独立 enabled 配置内。首次安装没有旧证书时不要加载该兼容配置。
5. 旧域名 HTTP/HTTPS 的 GET/HEAD 保留路径与查询串 301 到新域名；旧写请求返回 409，客户端必须在新域名重新登录后操作。旧 SSO callback 丢弃 Ticket 参数并 302 到新站登录页，不能跨域复用旧 flow Cookie。旧证书继续通过本机主站 ACME handler 续期，两端的挑战目录仍各自独立。
6. 核验新站首页、公开帖子/图片、博客、原生登录注册、SSO 发起/回调拒绝、未登录权益接口拒绝、两个站点地图，以及新旧域名的 ACME；执行两端 Certbot dry-run。真实账号登录、绑定与奖励另行验收，不能以匿名探针代替。

浏览器会话、草稿、界面偏好属于原域名存储，不自动迁移；用户需要重新登录。已经发出的邮件链接经旧域名跳转进入新站。搜索站长工具需重新验证新域名，旧域名验证文件不代表新域名已验证或收录。

回退只恢复同一实例的网关、Answer URL 与固定回调并重新构建旧域名静态文件；不回退数据库或使用已退役的旧服务器。新域名证书和旧证书应保留，直到切换与兼容窗口结束。
