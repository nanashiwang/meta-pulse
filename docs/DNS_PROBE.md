# DNS 递归出口探测

`/tools/network` 的 DNS 检测使用独立 `services/dnsprobe`：同源 API 创建 128 位随机查询名与独立 128 位回读凭据，浏览器访问该随机 HTTPS 名称触发 DNS。权威服务器对该名称返回 NXDOMAIN（不需要证书或 HTTP 服务），同时记录真实递归查询源 IP。网页通过同源 POST 轮询取回结果。一次 DNS 查询并不代表所有应用的解析路径。

结果只含递归出口 IP；未接入地理位置数据库，因此不能据此显示国内/国外或安全结论。浏览器 DoH、系统 DNS、VPN/代理可能各自改变解析路径；此探测反映该浏览器当次路径。仅观测 A/AAAA/HTTPS，拒绝区外查询，不提供递归，不接收 ECS 客户端地址为结果。

## 首次启用

现网源站按 `deploy/nginx/RELAY.md` 为 NAN `104.168.114.150`；先核对实际网卡和端口，不能使用已退役的 `meta-pulse` SSH 别名，也不能把 HTTP 前置机 `155.103.118.155` 当作 DNS 源站。

在 Cloudflare 为 **metarhub.com** 添加以下记录，不修改主域名或现有 NS：

| 类型 | 名称 | 值 | 模式 |
|---|---|---|---|
| A | ns1 | 104.168.114.150 | DNS only |
| NS | dnsprobe | ns1.metarhub.com | DNS delegation |

不要为 `*.dnsprobe` 建 A/CNAME 通配解析，不能开启 Cloudflare HTTP 橙云代理；子域不能留有冲突 DS。此单实例探测服务不是业务 DNS，故障只令检测无结果。NS A 地址若改变必须同时更新源站监听配置。

源站 `.env` 设置：

```dotenv
DNS_PROBE_ENABLED=true
DNS_PROBE_BIND_IP=104.168.114.150
```

确认这个 IPv4 在本机网卡上、53 端口未占用，再在宿主机和云防火墙允许该地址 UDP/TCP 53。不得清空现有防火墙规则。只绑定具体公网地址，避免冲突本地 `127.0.0.53` stub resolver。HTTP 8091 仅在 Compose 内部暴露，不映射公网。

使用既有 `metar update --release <已发布版本>`，或首次安装流程，自动构建/启动可选 dnsprobe profile。主站 Nginx 对同源 `/metar/api/tools/dns/start|result` 动态解析 dnsprobe，未启用时返回 502，不阻断主站启动。网关重建与静态资源发布均沿用现有更新流程。前置机转发这两个路径时须保留 Origin 与 X-Metar-DNS-Probe（现有默认转发会保留），不要记录请求体。

## 验收

```bash
dig @104.168.114.150 dnsprobe.metarhub.com NS +norecurse
dig @104.168.114.150 dnsprobe.metarhub.com SOA +tcp +norecurse
dig +trace dnsprobe.metarhub.com NS
```

直接 NS/SOA 成功只证明服务监听，必须继续在浏览器点完整扫描，确认随机域名查询真实到达并返回解析器地址。公网递归器回读、浏览器代理/DoH 路径分别验收。未出现地址不能称“无泄露”。API `/readyz` 仅是本地进程/监听就绪，不证明公网委派有效。

## 隐私、容量与恢复

独立服务无数据库、无用户绑定、不读取 Cookie/Authorization。Nginx 关闭这两个路径的访问日志；应用不记录查询名、凭据、resolver IP。结果仅内存保存最多 90 秒，每秒清理，单轮最多 32 个 IP。发行最多 120 次/分钟，全局最多 2048 轮；DNS 响应上限 300 次/秒，网关 API 限流 5 次/秒及小突发。不可作公开递归 DNS 使用；可在上游防火墙追加 DNS 流量限速。

重启会丢失未完成会话，浏览器重扫即可，不做持久恢复。禁用时先停止 `dnsprobe`（把开关改 false 不自动删除旧容器），再移除本次 NS 委派与 ns1 A（确认没有其他服务引用），关闭本次防火墙放行；不得撤销主站 DNS、证书、业务端口。
