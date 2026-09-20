# METAR 自有前置反代

拓扑：浏览器/搜索爬虫 → `64.83.9.190`（metar.uk，独立 HTTPS 证书）→ `23.94.111.46:443`（业务网关）。Cloudflare 保持仅 DNS，www CNAME 指向 metar.uk。前置机和业务机可能都在美国；增加这一跳只是更换访问入口和线路，是否改善抓取必须以百度真实验证为准。

## 配置与切换

1. 备份前置机 Nginx 配置。该机器已有其他站点，只新增 `sites-available/metar.uk` 与对应启用链接，内容来自 `metar-relay.conf`，不覆盖 nginx.conf 或其他虚拟主机。
2. 在前置机独立申请 metar.uk / www.metar.uk 证书。DNS 切换前可通过一次 DNS-01 验证签发；不要复制业务机私钥。验证 TXT 只创建本次所需记录，签发后删除本次记录，保留其他 TXT。
3. 创建 `/var/www/metar-acme/.well-known/acme-challenge/`。源站仍使用自己的 `/var/www/certbot` webroot；前置机本地不存在的 ACME 文件经 HTTP 转发到源站。两处路径分别用探针验证。
4. 更新源站正式网关配置，精确信任 `64.83.9.190/32` 的真实访客 IP。它只用于日志/限流；不能替代 Answer 身份认证。源站直连仍可用，任意客户端伪造 X-Real-IP/X-Forwarded-For 不得生效。
5. 两端 nginx -t 通过后热加载。先以 `curl --resolve metar.uk:443:64.83.9.190 https://metar.uk/` 验证首页、公开帖子、API/noindex、百度文件、重定向和证书，再将 A 记录切换到前置机。回源地址必须固定为业务 IP，不能解析 metar.uk，否则切换 DNS 后会形成代理循环。
6. DNS 生效后，将前置机 Certbot 续期方式改为 webroot，例如 `certbot reconfigure --cert-name metar.uk --webroot -w /var/www/metar-acme --preferred-challenges http --deploy-hook 'nginx -t && systemctl reload nginx' --run-deploy-hooks`；确认 staging 续期测试成功并保存配置，不能保留需要人工 TXT 的续期方式。沿用前置机已有 certbot.timer。源站另运行 `deploy/nginx/renew.sh --dry-run` 验证其原续期 timer。
7. 最后在百度站长完成验证与链接提交。网络可达、验证成功、提交成功、实际收录分别确认，不能互相替代。

## 转发规则

- 两段 HTTPS，回源开启 SNI、证书信任链与主机名验证；不设置 `proxy_ssl_verify off`。
- 前置机重写真实 IP 和转发链，源站再生成可信单值 X-Forwarded-For；不信任宽泛网段或任意入站头。
- 认证、Cookie、请求方法与响应 Cookie 交由业务网关按原规则处理。前置机不缓存页面或 API，保留 no-store/noindex 和 Cookie 安全属性，支持 WebSocket 与流式响应。
- HTTP 普通 GET/HEAD 301 到 HTTPS，其他方法 308；百度固定验证文件仍可通过 HTTP/HTTPS读取。
- 访问日志使用不含 query/Referer 的专用格式，敏感登录落地与回调关闭访问日志。前置机错误日志仅记录 critical，避免上游常规错误携带完整请求查询串。
- 回滚入口时恢复 A 记录为业务 IP、保持仅 DNS；源站访问与原证书不依赖前置机。不要删除两端有效证书或更改业务数据。

验证：`python3 deploy/nginx/test-relay.py` 使用两个真实 Nginx 和独立测试网络，覆盖可信 IP/伪造头、Answer Cookie 与 Authorization、私有 no-store/noindex、两端 ACME、跳转及回源证书不可信时的 502。
