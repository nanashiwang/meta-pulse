# Agent Note: 单机部署下的基础设施可迁移性

Status: proposed

## Problem

目标是现在继续单机 Docker 部署，未来分别迁出 Pulse MySQL、Forum MySQL、Redis 和附件，无需重写业务。当前领域与身份边界已经分离，主要缺口在部署连接、启动依赖、完整备份与迁移验收。本记录是审计和待实施方案，不代表已改造部署或配置异地备份。

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

## Existing capabilities and note audit

- `services/pulse/internal/config/config.go` 已读取数据库 DSN、Redis 地址/密码/DB；`docker-compose.yml` 却把 Pulse DSN、Redis 地址和 Forum Guard DSN 固定到本地服务名。
- Pulse、Forum 已经是独立 MySQL 容器、schema、账号和数据卷。new-api LOG_DB 已通过私网只读连接；不需要改变领域、资金或身份事实源。
- Answer 主连接来自容器 `/data/conf/conf/config.yaml`；插件的 `FORUM_BINDING_GUARD_DSN` 同时支撑绑定保护和成长数据访问。迁库必须同步两者，不能只改插件 DSN。Worker 的 `FORUM_DB_DSN` 是独立只读连接，线上目前为空。
- Pulse Redis 客户端只有 Addr/Password/DB，尚无 ACL username、TLS/CA 配置入口；论坛 nonce 客户端使用 `redis.ParseURL`，可以复用其 URL 方式。论坛 nonce 连接仍在插件配置中，不会随 Pulse 地址自动变化。
- `deploy/install.sh` 固定 pull/up/wait 本地三个基础设施服务；Compose depends_on 同样固定。`deploy/lib.sh` 要求四个本地 MySQL 密码，并按容器名执行 root mysqldump；容器不存在会跳过备份。这些不能直接用于外部库。
- 更新备份已有部署锁、配置、两个 SQL dump、API/Worker 分角色私钥和成功后保留策略；未覆盖 Answer 配置与附件。两个在线 dump 也不是跨库原子快照。
- `.github/workflows/ci.yml` 当前只跑 MySQL 8.0 集成测试。Answer 已提供 Storage 插件接口；当前论坛镜像只编入本项目插件，不能宣称已有对象存储可直接启用。
- 搜索活跃笔记仅命中相关的[部署备份保留](../../implemented/process/2026-10-01-deployment-backup-retention.md)：部分重叠，原有保留决定继续成立；异地备份使用独立保留策略，不让更新清理器负责。通用决策工作流与本方案无技术归属重叠。

## Proposal

### 第一批：保持本地默认，解开部署绑定

1. 扩展现有 Compose 和部署助手，分别选择 Pulse DB、Forum DB、Redis 的 local/external 模式；覆盖纯本地、单项外部及全部外部的组合。应用层只依赖连接可用性，local 模式才启动、等待本地容器。
2. 环境配置允许覆盖完整 DSN/Redis URL，保留旧环境文件的本地默认值。外部模式不要求本地 root 密码、不创建本地数据库、不启动闲置容器。迁移保持现有 Compose project 和卷名称，禁止因重组文件意外创建空库或更换运行密钥。
3. 为 Answer 主连接与插件连接增加一致性预检；从同一部署配置派生或明确校验目标。保留现有 Answer 配置中的站点、邮件等字段，禁止整体覆盖；保留 Forum 只读内容连接的独立权限。
4. 补齐 MySQL 连接超时、连接池上限/空闲/生命周期及 TLS/CA 配置；补齐 Pulse Redis URL、ACL username、TLS/CA。URL 不输出到日志。证书验证失败不得自动降级为明文；跨机器用私网或 VPN，服务端口不直接公开。
5. Redis 首先支持独立或托管的单节点兼容端点；DB 编号不作为安全隔离。保留 namespace、nonce 原子消费及 fail-closed。Redis Cluster 的跨 slot Lua 与 DB 限制需要专项适配，不默认为任意托管集群均可用。
6. 安装、更新、健康检查、备份都读取同一份经过验证的配置。外部连接失败应在迁移和停服前拒绝继续；外部库备份不能因为没有本地容器而静默跳过。

### 第二批：从升级备份扩展到可恢复的异地备份

- 保留现有更新前备份；新增可独立运行的备份/验证/恢复演练入口，共用部署锁与清单格式，不依赖恰好发生一次升级。
- 恢复集包括两个数据库、Answer 配置与实际上传目录、部署配置、API/Worker 各自私钥和版本清单。备份加密，恢复凭据另存；密钥与数据库必须配套，不能在恢复中重新生成随机种子或角色私钥。
- 本地与外部库都使用显式备份连接；最小权限覆盖 triggers、routines、events 等必要对象，不假设托管库提供 root/SUPER。恢复处理 DEFINER、GTID、触发器权限以及目标数据库账号。
- 可先以每日全量备份建立异地恢复能力；这只能承诺最多约一天的数据损失窗口，不能视为余额/奖励恢复已经安全。若目标是分钟级恢复，再增加 binlog 持续归档，验证完整链路后才调整本地保留期。
- 建议初始目标 RPO ≤ 15 分钟、RTO ≤ 2 小时，属于待确认目标，不是当前已达能力。保留策略可从日 7 / 周 4 / 月 3 起步，异地空间与加密目标需落实后才能启用。
- 一致性演练安排短维护窗口，暂停所有相关写入（Pulse API、Worker、Forum 写入及后台成长任务）；不能只停止 API 就认为两个数据库一致。在线逐库 dump 需标明其一致性限制。
- 恢复默认在隔离网络禁用对外发奖、邮件等副作用，再核对 ledger/account、ticket/grant、EXP 交付/撤销、绑定约束与附件。Pulse 回退到旧快照后，new-api 已发出的奖励仍可能存在；稳定 source_ref 与 Query/Reconcile 是恢复要求，禁止换 ID 重发。
- 配置定时执行、失败通知、远端可读性校验与周期性实际恢复；只有上传成功而未恢复验证，不能标记容灾验收完成。

### 第三批：迁移演练与附件接口准备

- CI 加入 MySQL 8.0 / 8.4 矩阵，先保留生产 8.0。新实例安装、旧版本 dump 恢复、托管权限、TLS、触发器和 Answer 原生流程分别验证。
- 数据库迁移采用：外部预检 → 配套备份 → 短维护窗口停止写入 → 最终同步 → 校验 → 切连接 → 验证 → 开放写入。旧实例只读保留；新库接受写入之后不能简单切回旧库，必须处理反向同步或重新迁移。
- 两个 schema/账号/授权继续独立；未来可放在同一外部实例降低成本，但故障域和资源竞争会合并，应保留分开配置的能力。当前不为节约两个容器而先合并数据库。
- 附件保持本地默认，优先复用 Answer Storage 插件接口，不另写业务上传系统。对象存储需要编入并验证兼容插件；历史附件 URL 保持可读或建立映射。上传、读回、删除、头像、旧链接和备份恢复均验收后再切换。
- 保留定期增长观测：Pulse 数据/索引、各原因冲突量、binlog、备份、磁盘与查询耗时。冲突记录先按原因识别再设计归档，禁止直接删 ledger、幂等记录或为省空间跳过财务审计。

## Alternatives considered

- 立即上独立数据库或托管数据库：能获得独立资源与部分运维能力，但现阶段论坛数据很小，仍需先解决备份和脚本假设，并新增费用与网络依赖。先做可迁移接口。
- 只开放 DSN 配置：改动最小，但启动依赖、Answer 主连接、root 备份与外部失败处理仍不成立，无法证明可迁移。
- 建独立基础设施平台或引入编排集群：便于更大规模管理，但增加当前单机项目的维护成本；扩展现有 Compose、脚本和健康检查即可覆盖本阶段。
- 立即合并两套 MySQL：可能降低部分内存开销，但额外改变故障域且需要搬数据；当前观测不足以证明收益值得这次迁移。
- 仅复制 Docker 数据卷：对同版本本机恢复直观，但不能作为在线一致备份或跨版本/托管数据库迁移通用方案；使用受支持的数据库备份与恢复流程。

## Acceptance criteria

- 旧 `.env` 的本地默认行为与卷身份不变；纯本地、混合、全部外部组合均通过 Compose 渲染、预检、启动与备份测试。
- 外部模式无本地库/root 凭据依赖；错误 DSN、权限不足、不可达 Redis 和证书错误可解释地失败，不漏备份、不修改生产数据。
- Pulse/Forum 不共享业务账号；LOG_DB 和 Forum 内容只读权限保留；API/Worker 分角色密钥边界不变。
- 隔离环境从备份恢复后，账实一致、绑定触发器完整、奖励不重复、EXP 可对账、附件可读；记录实际 RPO/RTO 与已知窗口。
- 8.0/8.4 CI、生产恢复演练与 Answer 原生浏览器验收分别有证据；不能互相代替。

### 本次已完成的兼容性检查

本机启动一次性 MySQL **8.4.11**（镜像 digest `sha256:6ea90827b1100f8f2ae306a539f86d2c264a26ed435a2a9f75551dd5c3aeb242`），仅绑定本机随机端口，使用独立 `pulse_audit_test`、`forum_audit_test`、`growth_audit_test`。开启 `log_bin_trust_function_creators=1`，执行项目原有测试，无生产数据导入：

- `make test-integration`：Pulse service 与 MySQL store 两包通过。
- `make test-forum-integration`：论坛绑定/管理员身份与内容读取两包通过。
- `make test-growth-integration`：社区成长与经验交付两包通过。

六包均使用 `-count=1 -race`，全部通过。该结果覆盖核心迁移、事务、幂等及插件数据库契约；不证明完整 Answer 应用、生产旧库升级、托管最小权限或备份恢复已验收。当前生产两个 MySQL 的账号使用 caching_sha2_password；MySQL 8.4 默认禁用 mysql_native_password 的兼容点仍纳入未来迁移预检，不能以启用旧认证代替适配。

官方参考：[8.4 升级变化](https://dev.mysql.com/doc/refman/8.4/en/upgrading-from-previous-series.html)、[旧认证插件限制](https://dev.mysql.com/doc/refman/8.4/en/native-pluggable-authentication.html)、[Upgrade Checker](https://dev.mysql.com/doc/mysql-shell/8.4/en/mysql-shell-utilities-upgrade.html)。

## Risks

异地目的地、保留空间、恢复密钥保管和维护窗口尚未确定；不能宣称异地备份已启用。迁移期间的双库一致性、已到账奖励与恢复快照之间的时间差、nonce 重放窗口、托管触发器权限和历史附件 URL 都必须单独验收。该方案不授权生产升级、删除 binlog/账本或采购新服务。
