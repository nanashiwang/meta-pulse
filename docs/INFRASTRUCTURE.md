# 单机运行与基础设施迁移

默认仍为原来的单机 Docker 部署，现有 `.env` 不必新增配置，项目名和所有旧数据卷名称保持不变。生产 MySQL 默认仍为 8.0。本功能允许分别迁出 Pulse DB、Forum DB、Redis；附件沿用 Answer 存储边界。应用领域、账本、绑定与发奖契约不变。

## 连接与部署入口

使用 `metar`、`deploy/install.sh`、`deploy/update.sh` 或 `bash deploy/compose.sh ...`。外部模式由这些入口自动附加对应 Compose overlay，**不能绕过入口直接执行裸 `docker compose up`**，否则基础文件仍会选择本地服务。需要支持可选依赖的 Docker Compose 2.20+。

| 配置 | 默认 | 外部模式要求 |
|---|---|---|
| `PULSE_DB_MODE` | `local` | `external` + `PULSE_DB_DSN` |
| `FORUM_DB_MODE` | `local` | `external` + `FORUM_BINDING_GUARD_DSN`，与 Answer 主连接一致 |
| `PULSE_REDIS_MODE` | `local` | `external` + `PULSE_REDIS_URL` |
| `PULSE_BACKUP_DB_DSN` / `FORUM_BACKUP_DB_DSN` | 本地备份使用各自 root | 外部库必须显式配置同目标的备份账号 |

外部模式不要求对应本地数据库密码，不创建或自动启动对应本地容器。已存在的旧容器和卷不会自动删除；切换验收后再在维护窗口明确退役，不能使用 `down -v` 清理业务数据。

示例（只展示占位符）：

```dotenv
PULSE_DB_MODE=external
PULSE_DB_DSN=pulse_runtime:REPLACE@tcp(db.private:3306)/meta_pulse?charset=utf8mb4&parseTime=true&loc=Asia%2FShanghai&tls=true&timeout=5s&readTimeout=30s&writeTimeout=30s
PULSE_BACKUP_DB_DSN=pulse_backup:REPLACE@tcp(db.private:3306)/meta_pulse?tls=true
PULSE_REDIS_MODE=external
PULSE_REDIS_URL=rediss://pulse:REPLACE@redis.private:6380/0
```

继续使用独立 schema 和账号。多个 schema 可以部署到同一外部实例，但资源与故障域会共享；不得把 new-api 主库写权限交给 Pulse。`FORUM_DB_DSN` 仍是 Worker 可选的只读连接，不是 Forum 插件写连接。

Redis URL 优先于旧地址/密码/DB，支持单节点 Redis、ACL 用户名与 `rediss://`。论坛 SSO 的 `nonce_redis_url` 在 Answer 插件后台独立配置，迁移时必须同步。Redis Cluster 不在此次支持范围；其多 DB 与跨 slot Lua 需单独适配。nonce 有效窗口内不能简单清空或切到空实例；维护窗口需覆盖在途 ticket/flow 的有效期，或保留对应短期状态。Redis 不是账本事实源。

## TLS、超时与连接池

默认挂载公开 CA 目录 `deploy/certs` 到 `/etc/meta-pulse-ca`，可通过 `META_PULSE_CA_DIR` 指定服务器目录；不得在其中放私钥。私有 CA 设置 `SSL_CERT_FILE=/etc/meta-pulse-ca/ca.pem`，文件应包含应用访问所有服务所需的信任根。MySQL DSN 使用 `tls=true`，Redis 使用 `rediss://`；验证失败不降级为明文。MySQL `skip-verify` / `preferred` 不接受。

Pulse 与只读适配器默认连接超时 5 秒、读写超时 30 秒；DSN 可显式调整。Pulse 主库默认最大连接 20、空闲 10，配置为 `PULSE_DB_MAX_OPEN_CONNS` / `PULSE_DB_MAX_IDLE_CONNS`；生命周期与空闲回收通过 `PULSE_DB_CONN_MAX_LIFETIME=30m` / `PULSE_DB_CONN_MAX_IDLE_TIME=5m` 调整。各进程各有自己的连接池，配置预算需乘以实例数量。跨主机使用私网/VPN，或正确验证 TLS，不能直接公开数据库与 Redis 管理端口。

## Answer 主配置与插件一致性

生产常见配置路径是容器 `/data/conf/conf/config.yaml`，未初始化镜像也可能使用 `/data/conf/config.yaml`。新论坛入口会核对 Answer 与插件的网络地址、schema 和账号，并分别验证连接；发现不一致即停止启动。安装前的本地 SQLite 默认配置只用于保留原生初始化流程，外部模式必须先准备 MySQL 配置。

在已完成目标库恢复的维护窗口，更新部署 DSN 后执行：

```bash
bash deploy/compose.sh run --rm --no-deps -T --entrypoint /usr/bin/forum-config forum sync --apply
bash deploy/compose.sh run --rm --no-deps -T --entrypoint /usr/bin/forum-config forum check
```

`sync` 先验证新连接，再只修改 `data.database.connection`，原文件备份为旁边的 `.before-infra`，不覆盖邮件、站点、上传路径等字段。重复同步同一 DSN 不改文件；下一次迁移前需先另存旧 `.before-infra`，工具不会覆盖它。此命令不迁移数据库、不启动新发奖；执行后需按完整迁移步骤更新服务。自定义配置位置可在 Compose override 的 forum 环境中设置 `FORUM_CONFIG_FILE`。

## 完整恢复集

更新前备份继续保留，新增 Answer `/data` 备份；本地库缺失/停止或外部库备份失败时中止更新，不能静默跳过。更新备份依然是在线逐库备份，不能将其理解为跨库原子快照。

独立恢复集入口：

```bash
metar backup create --quiesced --keep-local 3
metar backup verify /opt/meta-pulse/.data/recovery-sets/生成的目录
metar backup restore-drill /opt/meta-pulse/.data/recovery-sets/生成的目录 --image mysql:8.4
```

`--quiesced` 暂停原本正在运行的 Pulse API、Worker、Forum，完成备份后启动同一批现有容器，不重新部署配置；原本停止的容器保持停止。成功、普通失败、INT/TERM 都尝试恢复服务。SIGKILL、断电或 Docker 本身故障无法由进程保证恢复，需人工检查并用 `bash deploy/compose.sh start pulse-api pulse-worker forum` 恢复原容器。其他部署的写入者也必须纳入维护窗口。

省略 `--quiesced` 时清单标记为 `online-per-database`，恢复演练拒绝把它当作一致恢复集。所有备份复用部署管理锁。恢复集包含两个 SQL dump、`.env`、渲染后的 Compose、Answer 全部 `/data`、API/Worker 独立私钥及 SHA-256 清单，目录 700、文件 600。缺少容器、私钥或必要文件、校验失败均报错。

本地默认保留最近 3 个完整成功恢复集，`--keep-local 0` 禁用清理。当前、失败、上传未确认、未知格式和带 `.keep` 的目录保留；这不是磁盘容量上限。此策略与 `.data/deploy-backups` 的更新备份保留独立。

恢复工具仅创建**无网络的一次性 MySQL 容器**，不连接生产库、不运行应用、不发奖或发邮件。它恢复 SQL、重建隔离环境的锁定 DEFINER 账号，核对 Pulse ledger/account 与社区 EXP 余额，并验证附件与私钥文件。此单实例演练要求两套 schema 名称不同；若两个外部实例使用相同 schema 名称，需要分别隔离恢复，工具会明确拒绝合并。它不等于完整生产恢复：还需确认原生页面、实际密钥解密、绑定保护、触发器权限，以及与 new-api 的 Benefit Query/Reconcile。用户历史奖励的 source_ref 不得更换，已到账额度不会随 Pulse 快照一起回退。

备份客户端默认使用 MySQL 8.4，支持 8.0/8.4 的逻辑备份；使用 `--no-tablespaces`、`--set-gtid-purged=OFF`，保留 routines/events/triggers。外部备份账号需具备目标库读取及这些对象的必要权限；托管服务对 DEFINER/触发器可能额外限制，需在演练中验收。该工具不把 raw 数据卷复制当作在线数据库备份。

## 加密仓库与定时执行

目前不自动启用异地任务。准备好独立备份位置后，安装 restic，将 `deploy/offsite.env.example` 复制到仓库外的 600 权限文件；密码文件也需 600，恢复密码另行保管。按 restic 文档显式初始化加密仓库，日常备份不会误初始化到错误目标。

```bash
metar backup create --quiesced --offsite-config /etc/meta-pulse/offsite.env
# 上传已有完整恢复集：
metar backup upload /opt/meta-pulse/.data/recovery-sets/生成的目录 --offsite-config /etc/meta-pulse/offsite.env
# 确认维护时段和目标后，显式安装定时器：
sudo deploy/install-backup-timer.sh --apply /etc/meta-pulse/offsite.env
```

上传后会从加密仓库实际恢复一份到本机临时目录并重新核对清单，回读失败则整次任务失败；不能把“上传命令已运行”当成功。该回读需要额外临时磁盘空间。定时器每天服务器时区 03:30 执行，存在短维护窗口，不自动安装、不自动采购存储。通过 systemd/现有监控告警非零退出和最后成功时间；请实际验证告警收件链路。

远端保留策略独立，推荐日 7 / 周 4 / 月 3；可在专用仓库按 `--tag meta-pulse --group-by host` 管理，避免按每次不同目录路径分组导致快照永不淘汰。工具不自动执行远端 forget/prune。每日全量只提供天级恢复点，分钟级 RPO 还需要连续 binlog 归档和时间点恢复验收；在此之前不要为节省磁盘直接缩短生产 binlog 保留期。

## 附件独立迁移

Answer 已有 Storage 插件接口，本次不替换上传业务，也不预装未经选择的对象存储插件。`forum_data` 仍保存原数据；实际附件路径以 Answer 配置为准，当前生产为 `/data/conf/uploads`。可以先将附件复制到挂载好的独立存储，再通过 `docker-compose.override.yml` 将其挂到同一容器路径，保持已有 URL；配置、缓存与私钥不与公共附件混放。新挂载会遮盖旧目录，必须先复制、校验并停写切换，不能直接挂空卷。

备份覆盖 `/data` 下的附件；如果自定义 `upload_path` 在 `/data` 外或启用对象存储，必须另行纳入恢复集与演练，不能认为本工具已备份外部对象。对象存储插件需要编入论坛镜像，并验收上传、读回、头像、删除与旧链接后再启用。

## 迁移验收顺序

1. 目标环境验证版本、账号、TLS、触发器与备份权限；保留两套 schema/账号隔离。
2. 隔离环境恢复备份，检查原生登录、绑定、社区成长、附件及账实一致。
3. 维护窗口停止所有写入者，完成最终同步，核对两库与运行私钥配套。
4. 修改连接及 Answer 主配置，运行预检，再启动应用；观察健康、摄入游标和奖励对账。
5. 旧实例只读保留。新库已经接受写入后，禁止简单切回旧库；先解决数据反向同步和奖励一致性。

CI 同时验证 MySQL 8.0/8.4 的财务、绑定和成长回归，并执行 8.0 dump → 8.4 恢复及 restic 加密回读。一次性合成数据演练不代替生产备份恢复或对具体托管服务的验收。
