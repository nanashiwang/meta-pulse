# Agent Note: METAR 迁入退役 NAN 主机

Status: implemented

## Problem

用户授权清空 104.168.114.150 上已停用的旧 NewAPI 业务并迁入当前 METAR。现场确认 METAR 源站 23.94.111.46 的根盘使用率 98%，仅余约 747 MiB；目标 96 GiB 根盘尚被旧业务占用 82 GiB。仓库原记载前置机 64.83.9.190；现场 DNS、SSH 与 Nginx 核对确认当前前置机为 155.103.118.155，仍将 metar.uk 回源到旧 METAR，独立 new-api 已运行于 147.135.39.173。

## Decision

目标机已停写的旧业务、配置与恢复材料归档到已有大容量服务器。完整 SHA-1 文件校验通过后，只清理归档清单中的旧业务目录及旧镜像；操作系统、SSH、防火墙和审计记录保留。归档校验排除新创建的 METAR WireGuard 配置和目录 mtime，不能把新业务准备产生的变化误判为旧数据损坏。

迁移复用现有 Compose、恢复集格式、私网连接、域名及前置网关。初次切换保持 v0.2.65 / f0125ba 原运行镜像、MySQL 8.0.46、原始数据、绑定、游标、运行私钥、资金目标和业务开关。最终 dump 流式压缩写到目标，不占用源站剩余空间。源站应用正常停机、数据库只读、Redis 正常保存 RDB 后才复制最终数据；目标逐表验收通过后成为唯一写入端。

WireGuard 内部地址及身份复用，源 wg0 先停用并禁止开机启动，目标以 wg-metar 接管；独立 new-api 仅更新持久和运行 peer endpoint，不重启其业务。目标 Docker 按 WireGuard 之后启动，社区续期 timer 在真实 dry-run 成功后启用。前置机只替换 METAR 固定 HTTPS 回源与 HTTP ACME fallback，其他站点保持原配置。

## Related note audit

- [基础设施可迁移性](../../implemented/architecture/2026-10-04-infrastructure-portability.md)：部分重叠，复用其单机隔离、完整恢复集和迁移后禁止盲目回切的边界；本篇负责实际主机迁移及磁盘退役。
- [部署备份保留](../../implemented/process/2026-10-01-deployment-backup-retention.md)：部分重叠，不伪造旧备份成功状态，不把本次明确授权的主机退役交给普通保留清理器。
- [恢复就绪检查](../../implemented/bug-fix/2026-10-15-restore-drill-readiness.md)：复用正式 TCP 就绪与错误脱敏，不能把镜像启动当作数据恢复成功。

## Alternatives considered

- 清空目标后重新安装：最省操作步骤，但会丢掉旧业务恢复材料；先异机归档并校验。
- 同时升级应用、数据库或拆分外部数据库：可以顺便完成升级，但扩大故障定位和回滚范围；本次首先保持现有部署身份和经济语义。
- 在线直接复制 MySQL 数据目录：传输快，但不是一致恢复点；只可在数据库正常关闭后作为同版本冷副本，并配套逻辑备份与表级核验。
- 改公网 DNS 直连目标：可少一跳，但改变既有入口和证书续期路径；优先仅切换现有前置机的固定回源 IP。

## Verification

- 旧 NAN 归档 `/srv/retired-nan-20261027/rootfs` 位于独立的 147.135.39.173：66,328,532,222 字节逻辑文件，完整内容校验无差异。清理后目标从约 9.4 GiB 可用增至约 77 GiB；随后使用部分空间恢复 METAR 和保存恢复材料。
- 停写快照含 Pulse 27 表、12,204,266 行及 15 个触发器；Forum 50 表、1,734 行及 7 个触发器。恢复后每表 COUNT、CHECKSUM EXTENDED、全部字段元数据、规范化 DDL 与触发器完全一致。Ledger 余额/版本、EXP 余额、绑定唯一性检查均为零差异。
- 原始 SHOW CREATE 文本存在显式 `CHARACTER SET utf8mb4` 与隐式写法差异。完整字段元数据及 schema 默认字符集/排序规则独立一致后，只规范化紧邻相同 utf8mb4 COLLATE 的冗余字符集声明；不能笼统忽略 charset 或所有 DDL 差异。原始审计文件仍保留。
- Answer `/data`、两份运行密钥卷和 Redis 卷通过带删除预览的内容比对，确认无额外目标文件。恢复集沿用 backup.py 格式及校验，包括两个 SQL、配置、附件、密钥和清单；独立服务器 `/srv/metar-migration-recovery-20261027` 再次逐文件 SHA-256 校验成功，配套证书、WireGuard、宿主覆盖配置和审计材料也已归档。
- 目标 API/Worker readyz、Forum 配置一致性、LOG_DB 可读且只读检查通过；初次启动三个业务镜像均为 v0.2.65 / f0125ba，重启计数为零。源站项目容器全部停止并设为 restart=no，旧 WireGuard 与续期任务禁用；迁移临时 SSH 授权撤销。
- 2026-10-27 10:32 UTC 进入维护、10:58 UTC 前置回源切至 NAN。首页、真实话题列表、博客、静态脚本、原生登录与 sitemap 通过；浏览器确认公开帖子正文和上传图片。未登录 Pulse 请求为 401；SSO 发起产生 Secure/HttpOnly/SameSite=Lax flow cookie，错误回调不授予会话。
- 前置和源站 ACME 探针分别成功；NAN Certbot 模拟续期成功并启用 timer。网关仍验证 TLS 信任链，源站只信任当前前置精确地址。
- 本地 `make deploy-test`、`make build`、双 Nginx 回归、网关配置检查及发布回归通过；工作流/版本元数据在提交前单独校验。迁移验收与后续正式版本发布、实际部署分别记录，不用 Release 成功代替生产验收。

## Consequences

社区维护约 26 分钟，独立 new-api 主链路保持运行。目标已产生新写入，旧源站是冷恢复副本，禁止仅改回源地址恢复旧写入端。两个逻辑库、附件与密钥必须配套恢复，已到账 Benefit 不能随 Pulse 数据回退重新发放。

旧库在迁移前已有约十小时摄入积压，游标冻结于 `1793062530:14954808` / version 4090148，迁移后继续前进，不能宣称已经追平。原源站和目标都存在 `period_close: 1 periods failed to close`。旧逐条 ledger-check 在 60 秒超时前观察到两条 ticket operation mismatch；这是不同于汇总余额对账的历史语义检查，尚未完成全量逐条扫描。本次不改写 Ledger，也不据此修改经济规则或重复发奖。

验收没有使用真实账号完成登录、绑定、发帖或抽奖，没有发送真实测试消息或奖励。一次性异机归档与完整恢复集不代表已经配置定时异地备份、PITR 或分钟级 RPO。归档包含敏感数据，父目录仅 root 可访问。
