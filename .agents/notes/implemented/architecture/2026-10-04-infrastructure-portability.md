# Agent Note: 单机部署下的基础设施可迁移性

Status: implemented

## Problem

目标是现在继续单机 Docker 部署，未来分别迁出 Pulse MySQL、Forum MySQL、Redis 和附件，无需重写业务。当前领域与身份边界已经分离，主要缺口在部署连接、启动依赖、完整备份与迁移验收。本记录覆盖已实现的部署解耦和恢复工具；生产保持单机，异地目的地由用户明确暂缓。

### 2026-10-04 只读生产快照

源站代码为 `498dc67` / v0.2.34。服务器 2 vCPU、约 1.92 GiB 内存、34 GiB 根盘；磁盘使用率 73%，可用约 8.9 GiB。可用内存约 926 MiB，swap 已使用约 1.28 GiB；随后短时 vmstat 未观察到持续换页，不能只凭 swap 使用量认定当前内存不足。API/Worker readyz 通过，两个 MySQL 和 Redis 容器健康。

| 对象 | 实测状态 | 含义 |
|---|---|---|
| Pulse MySQL | 8.0.46；27 表，表与索引约 2082 MiB；数据目录约 5.7 GiB | 当前数据增长重点 |
| Forum MySQL | 8.0.46；39 表，表与索引约 1.06 MiB | 暂无因论坛数据量立即拆库的证据 |
| Pulse binlog | 约 3384 MiB，过期设置 30 天 | 本地占用较大，但尚未核实完整的时间点恢复链路 |
| 更新备份 | 3 个目录，共约 3 GiB | 同机升级恢复材料，不等于异地容灾 |
| Forum 附件 | 实际配置为 `/data/conf/uploads`，约 32 KiB | 不能误以为 `/data/uploads` 是正在使用的目录 |
| Pulse 冲突表 | 约 354 MiB，估算约 92 万行 | 最近 1000 条均因缺少 verified-paid funding proof；不能据此把所有历史记录判为同一原因或重复数据 |
| Redis | 无密码，未发布宿主端口；AOF 关闭，默认 RDB save 仍开启，使用匿名卷 | 不应把关闭 AOF 误读为完全无持久化，也不能把 RDB 当业务账本 |

表大小及行数来自 information_schema，行数为估算。未导出生产用户资料或数据库备份；未修改配置、重启服务、清理数据或升级数据库。已检查系统 timer、常见 cron 位置和备份工具，未发现项目定时异地备份；不排除服务器提供方或站外系统另有备份。

## Audit baseline and related notes

- `services/pulse/internal/config/config.go` 已读取数据库 DSN、Redis 地址/密码/DB；`docker-compose.yml` 却把 Pulse DSN、Redis 地址和 Forum Guard DSN 固定到本地服务名。
- Pulse、Forum 已经是独立 MySQL 容器、schema、账号和数据卷。new-api LOG_DB 已通过私网只读连接；不需要改变领域、资金或身份事实源。
- Answer 主连接来自容器 `/data/conf/conf/config.yaml`；插件的 `FORUM_BINDING_GUARD_DSN` 同时支撑绑定保护和成长数据访问。迁库必须同步两者，不能只改插件 DSN。Worker 的 `FORUM_DB_DSN` 是独立只读连接，线上目前为空。
- Pulse Redis 客户端只有 Addr/Password/DB，尚无 ACL username、TLS/CA 配置入口；论坛 nonce 客户端使用 `redis.ParseURL`，可以复用其 URL 方式。论坛 nonce 连接仍在插件配置中，不会随 Pulse 地址自动变化。
- `deploy/install.sh` 固定 pull/up/wait 本地三个基础设施服务；Compose depends_on 同样固定。`deploy/lib.sh` 要求四个本地 MySQL 密码，并按容器名执行 root mysqldump；容器不存在会跳过备份。这些不能直接用于外部库。
- 更新备份已有部署锁、配置、两个 SQL dump、API/Worker 分角色私钥和成功后保留策略；未覆盖 Answer 配置与附件。两个在线 dump 也不是跨库原子快照。
- `.github/workflows/ci.yml` 当前只跑 MySQL 8.0 集成测试。Answer 已提供 Storage 插件接口；当前论坛镜像只编入本项目插件，不能宣称已有对象存储可直接启用。
- 搜索活跃笔记仅命中相关的[部署备份保留](../../implemented/process/2026-10-01-deployment-backup-retention.md)：部分重叠，原有保留决定继续成立；异地备份使用独立保留策略，不让更新清理器负责。通用决策工作流与本方案无技术归属重叠。

## Decision

扩展现有 Compose 和部署助手，Pulse DB、Forum DB、Redis 分别选择 local/external，默认 local，保持原项目名、卷和私钥。外部 overlay 禁用对应本地服务，部署、预检和备份统一经过 `deploy/compose.sh` / `deploy/lib.sh`；外部模式不要求本地 root 密码，旧卷不自动退役。

Pulse MySQL/只读适配器统一超时与验证 TLS，主库连接池可配置。Redis URL 支持 ACL 和 verified TLS，保留旧地址入口。CA 通过只读目录和 SSL_CERT_FILE 传入。拒绝 MySQL skip-verify/preferred；Redis Cluster 需另行适配。

Answer YAML 主连接仍是其事实源。`forum-config check` 在启动和迁移前验证它与插件的目标、账号和可达性；`sync --apply` 先验证新库，只替换连接字段并保护已有迁移备份。保留本地首次初始化；Worker Forum 只读连接与插件写账号仍分开。

更新备份补齐 Answer `/data`，数据库缺失或停止不再跳过。独立 `metar backup` 恢复集包括两个逻辑 dump、配置、附件和两份私钥，以清单校验并复用更新锁。quiesced 模式暂停原本运行的三个应用，finally 启动同一批现有容器；不重建，也不改变原本停止的状态。在线逐库备份不承诺跨库一致。

外部备份显式提供同目标账号，不能降低 TLS。保留 routines/events/triggers，去掉 tablespace 和 GTID 元数据依赖。恢复只在无网络的一次性 MySQL 中执行，重建锁定 DEFINER 并核对 Pulse 余额/版本和 EXP 余额；合成演练不冒充完整生产恢复。

restic 上传后实际回读并校验，任一步失败则任务失败。本地默认保留三个完整成功恢复集，保护当前、失败、未知、固定及不安全目录。远端保留独立管理，不自动 forget。用户已明确“暂时没有，先完善工具与演练”，因此真实异地目标和定时器均未启用，没有承诺分钟级 RPO。

附件保留 Answer Storage 接口与现有容器路径，可迁到独立挂载，不新增上传业务。对象存储插件、旧 URL 和完整页面在实际迁移时验收。当前恢复集覆盖 `/data`，外部对象必须另行备份。

## Alternatives considered

- 立即上独立数据库或托管数据库：能获得独立资源与部分运维能力，但现阶段论坛数据很小，仍需先解决备份和脚本假设，并新增费用与网络依赖。先做可迁移接口。
- 只开放 DSN 配置：改动最小，但启动依赖、Answer 主连接、root 备份与外部失败处理仍不成立，无法证明可迁移。
- 建独立基础设施平台或引入编排集群：便于更大规模管理，但增加当前单机项目的维护成本；扩展现有 Compose、脚本和健康检查即可覆盖本阶段。
- 立即合并两套 MySQL：可能降低部分内存开销，但额外改变故障域且需要搬数据；当前观测不足以证明收益值得这次迁移。
- 仅复制 Docker 数据卷：对同版本本机恢复直观，但不能作为在线一致备份或跨版本/托管数据库迁移通用方案；使用受支持的数据库备份与恢复流程。

## Testing

Go 模块测试、构建、vet 和部署角色配置检查；八种组合真实 Compose 渲染覆盖原卷身份和无本地 root 模式。独立 MySQL 8.4.11 上六个财务、绑定、内容与成长测试包通过 `-count=1 -race`；CI 同时验证 8.0/8.4。测试 DSN 保留项目的 Asia/Shanghai 时间解释，不能用省略 loc 的测试配置误判业务回归。

实际二进制预检已验证 MySQL/Redis 连接成功、错误 Redis 密码与不可信 MySQL TLS 被拒绝，以及 Answer 同步失败不改原文件。

真实 Docker 合成演练覆盖 8.0 导出、暂停/恢复容器、8.4 隔离恢复、账实核对及 restic 加密回读；外部模式使用独立备份账号，错误密码不产生完成文件。单测覆盖 TLS 降级、凭据脱敏、Answer 修改失败保护、损坏/缺失备份、失败重启和保留策略。

## Consequences

逐项迁出基础设施无需改变领域、资金、绑定和发奖语义；代价是外部模式必须使用统一 Compose 入口，并维护 Answer 与插件一致。新库接受写入后不可简单切回旧库，必须反向同步或重新迁移。

本次不修改生产服务、数据库版本或 binlog 保留期，不删除业务数据。完整生产历史 dump、Answer 页面、密钥实际解密、托管 TLS/权限及 new-api Benefit Query/Reconcile 仍需按 [迁移指南](../../../../docs/INFRASTRUCTURE.md) 验收，不能用镜像构建或合成恢复代替。

一致备份有短维护窗口，其他写入者也需停写。INT/TERM 尝试恢复服务，SIGKILL、断电、Docker 故障需人工恢复。保留失败恢复集可能继续占盘；远端回读还需要临时空间。每日全量是天级恢复点，连续 binlog 归档、分钟级 RPO、完整生产 RTO 未建立。异地目标、告警收件链路和密码异地保管在用户准备资源后启用。

官方参考：[8.4 升级变化](https://dev.mysql.com/doc/refman/8.4/en/upgrading-from-previous-series.html)、[旧认证插件限制](https://dev.mysql.com/doc/refman/8.4/en/native-pluggable-authentication.html)、[Upgrade Checker](https://dev.mysql.com/doc/mysql-shell/8.4/en/mysql-shell-utilities-upgrade.html)。生产 caching_sha2_password 无需通过启用旧认证绕过升级检查。
