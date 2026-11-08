# Agent Note: 自建权威 DNS 泄露探测

Status: implemented

## Problem

现有 DNS 卡片不可用，因为网页不能读取操作系统递归解析器，且仓库没有权威 DNS 查询观测端。普通 DoH 请求只能测到 DoH 提供商，不能替代系统解析器。

## Existing capabilities and impact

复用 METAR 网络自检页面和同源 Nginx API 边界。独立新增无数据库 DNS 探测服务，避免依赖 Answer、Pulse 或 Intel。主站 DNS 由 Cloudflare 管理，生产入口经前置 Nginx 与业务网关两段；业务源站地址见 `deploy/nginx/RELAY.md`。功能需要 DNS 子域 NS 委派及公网 UDP/TCP 53，不能靠代码发布自动生效。检索命中[环境面板笔记](../../implemented/feature/2026-11-08-claude-environment-panel.md)与[工具入口笔记](../../implemented/feature/2026-11-08-tools-network-check.md)：本决定部分取代“DNS 暂不实现”，保留环境评分、导航及隐私边界；其他 DNS 迁移笔记不涉及检测。

## Decision

自建仅权威、不递归的 `dnsprobe.metarhub.com` 区域。浏览器点击扫描后向同源服务申请短时一次性 nonce，再请求 `<nonce>.dnsprobe.metarhub.com` 触发系统/网络递归 DNS。权威服务器只记录处于有效期内 nonce 的查询源 IP；API 仅把临时结果返回申请浏览器，内存保存、短 TTL 清理、限流。返回只展示 resolver IP 与观测数，不猜地区或作“安全/泄露”结论。DNS query/source IP 不进入日志或持久存储。

服务默认关闭；仅 `DNS_PROBE_ENABLED=true` 才启用 profile 与 host UDP/TCP 53 映射。端口绑定显式配置为业务源站的公网 IP，避开本机 loopback resolver。Nginx 用 Docker 内部 resolver 按需代理同源 issue/status API；Content-Security-Policy 仅允许固定 `*.dnsprobe.metarhub.com` 作为探测目标。

DNS 控制台仍需人工创建 DNS-only `ns1.metarhub.com A <业务源站公网 IP>` 和 `dnsprobe.metarhub.com NS ns1.metarhub.com`；防火墙开放该源站 UDP/TCP 53。发布不执行 DNS 修改或部署。只有权威查询实际到达并回读后，UI 才称检测成功；无结果显示委派/网络不可达，不声称无泄露。

## Alternatives considered

- 普通 DoH 查询：无法观测访客系统 resolver，拒绝伪标。
- 第三方 DNS 测试 API：引入外部域名、可用性与隐私依赖；拒绝。
- 仅静态“不可用”说明：不满足用户要求；改为自管小型权威服务。

## Operational limits

公网权威服务暴露 UDP/TCP 53，并需人工 DNS 委派与防火墙变更；服务单实例/内存状态重启会丢弃未完成探测。递归器可能代理、缓存或隐藏客户端地址，因此返回的是权威服务器观察到的递归出口，不一定是客户端配置中的上游品牌或地理位置。

## Consequences

增加独立 Go 模块、可选 Compose profile、Nginx 路由与部署前提，但不加数据库、第三方 API、账号或跨项目权限。DNS 探测结果在 TTL 到期后销毁。

## Testing

覆盖权威区 NS/SOA/NODATA/NXDOMAIN/refused、nonce/TTL/限流/回读/清理、HTTP Origin/请求体/安全头、同源 UI 发起与轮询/取消/超时、DNS-only Compose profile/UDP+TCP 发布、动态 nginx resolver、生产部署配置校验。明确验证无委派时返回 pending/unknown 而不是 clean。

本地 Go race 测试通过真实 UDP/TCP socket 验证 NS/SOA、NXDOMAIN 与拒绝区外递归、临时结果回读、过期、限流和并发；19 项前端测试通过，包含随机域名白名单、回读凭据隔离、取消与状态展示。四项部署配置回归、全套社区测试、部署离线测试、Nginx 容器配置测试、前端构建通过。源站 NAN 只读核对为 104.168.114.150，53 端口未占用；旧 meta-pulse SSH 指向退役机器，不用于上线。

Docker Hub 本地元数据首次请求超时，重试后镜像完整构建通过；本地只读/无 capabilities 容器通过 HTTP 发起、UDP/TCP NXDOMAIN、结果回读验收。新增 CI 同步测试与镜像构建。公网 NS 委派、生产开关和浏览器真实 DNS 回读尚未执行，不能声明线上可用。
