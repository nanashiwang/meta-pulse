# Meta Pulse 服务器部署

正式版本支持 `metar update --release v0.1.0`，完成发布清单验证、完整构建和实际镜像版本核对。发布与恢复步骤见 [版本流程](../docs/RELEASE.md)。

本目录提供 Linux + Docker Compose v2 的首次部署和一键更新脚本：

- `install.sh`：初始化生产配置、生成随机凭据、启动依赖、执行迁移、启动服务并检查 `/readyz`。
- `update.sh`：加锁拉取远程分支、备份配置、构建镜像、执行前向迁移、重建服务并检查 `/readyz`。
- `metar.sh` / `install-cli.sh`：数字菜单与 `metar` 子命令，以及一次性命令注册。
- `meta-pulse.env.example`：生产配置模板。
- `test.sh` / `update_test.sh`：不连接真实 Docker、不执行远程更新的脚本回归。
- `config-test.sh`：用测试凭据渲染实际生产 Compose，验证 API/Worker/Tool 的最小权限配置；不读取真实 `.env`、不连接业务数据库。
- `build-blog.sh`：使用一次性 Node 容器和锁文件构建纯静态博客，不在生产机运行 Vite 开发服务。
- `nginx/renew.sh` / `nginx/install-renewal-timer.sh`：续期 `metar.uk` 证书并安全重载网关。

## 部署边界

脚本只部署 Meta Pulse 自身的 Pulse MySQL、Redis、API、Worker 和 Answer 容器，不会：

- 修改或重启 new-api；
- 写入 new-api 用户余额或 LOG_DB；
- 给 Pulse、Answer、MySQL、Redis 发布宿主机端口；
- 删除 Docker 数据卷或执行 `docker compose down -v`；
- 自动修改 Nginx、TLS、DNS 或公网防火墙。

new-api 必须先独立部署，并提供 Signed BFF、Internal Benefit API、LOG_DB 只读账号及现有 Forum SSO Bridge。new-api 可以位于另一台服务器并独立更新；社区服务器不运行第二套 new-api。公网仅开放新的社区域名，参见 [`deploy/nginx/README.md`](nginx/README.md)。当根目录的宿主机覆盖配置包含 `gateway` 服务时，`update.sh` 会在博客变更后重建静态产物，并校验、启动和热重载网关。

## 社区静态前端

启用 `gateway` 时，部署脚本会依次构建 VitePress 与 METAR 正式前端：

```bash
./deploy/build-blog.sh
./deploy/build-community.sh
```

正式产物位于 `sites/blog/docs/.vitepress/dist/metar/`，复用现有 `/var/www/blog` 只读挂载。Nginx 只在精确 `/` 返回该首页；Answer 的 `/questions`、`/users/*`、`/answer/api/*`、Connector 和 callback 路径不被静态前端接管。生产构建缺失时网关返回 404，不回退到原型或 mock。

## 首次部署

服务器要求：Linux、Docker、Docker Compose v2、Git、OpenSSL、`flock`（通常由 util-linux 提供）。推荐使用专用部署用户，并让该用户加入 `docker` 用户组。

```bash
git clone https://github.com/nanashiwang/meta-pulse.git /opt/meta-pulse
cd /opt/meta-pulse
./deploy/install.sh
```

首次运行会在 `/opt/meta-pulse/.env` 创建配置并自动生成 Pulse/Forum 数据库密码及 HMAC 随机密钥。由于 `NEWAPI_LOG_DSN` 必须来自真实环境，脚本会在缺少该配置时安全退出；编辑配置后再次执行：

```bash
chmod 600 .env
vi .env
./deploy/install.sh
```

也可以在非交互场景通过环境变量注入外部配置（不要把 DSN 放进命令行参数）：

```bash
NEWAPI_LOG_DSN='只读账号 DSN' \
NEWAPI_INTERNAL_BASE_URL='http://<new-api私网地址>:3000' \
./deploy/install.sh
```

注意：脚本生成的 `PULSE_SERVICE_HMAC_SECRET`、`PULSE_FORUM_HMAC_SECRET`、`PULSE_USER_BFF_HMAC_SECRET`、`PULSE_ADMIN_HMAC_SECRET` 需要同步到对应服务。`PULSE_FORUM_HMAC_SECRET` 只用于 Answer → Pulse 的只读 Profile 请求，必须复制到插件 `pulse_hmac_secret`，不得复用可访问 new-api Benefit 的 `PULSE_SERVICE_HMAC_SECRET`。Forum SSO 另用独立密钥，可执行 `openssl rand -hex 32` 生成；它只配置到 new-api 的 `PULSE_FORUM_SSO_SECRET` 与 Answer 插件 `sso_hmac_secret`，且不得与插件 `pulse_hmac_secret` 复用。配置不一致或密钥复用时应保持功能不可用，不要降低验签要求。

跨服务器部署时，可以创建仓库根目录下忽略提交的 `docker-compose.override.yml`，安装、更新、备份和健康检查会自动合并该文件。它只适合保存宿主机专属的端口绑定等配置，禁止写入密钥。例如只将 Pulse API 发布到 WireGuard 地址：

```yaml
services:
  pulse-api:
    ports:
      - "10.77.0.2:8088:8088"
```

也可以通过 `META_PULSE_COMPOSE_OVERRIDE_FILE=/绝对路径/compose.yml` 指定其他覆盖文件。生产环境不得将 `8088` 无限制发布到公网。

## Answer 初始化与账号绑定

升级后的 Pulse 参数和签名密钥可通过 METAR 管理员 `/#/admin/pulse` 配置，首次需在插件设置填入 `admin_hmac_secret` 与 Pulse 运营密钥配对。新迁移 `00012` 与 API/Worker 私钥持久卷必须一起保留，部署更新会备份已有私钥卷；详见 [管理员配置与恢复](../docs/METAR_ADMIN_SETTINGS.md)。原 `.env` 作为部署基线保留，网页覆盖优先，既有部署的业务密钥不自动轮换。

首次启动后，先通过新社区域名完成 Answer 初始化，并在后台确认：

1. 开启本地注册和密码登录，配置站点 URL、发信服务与管理员；
2. 启用 `Meta Pulse` 插件；Compose 已仅向 forum 容器注入 `FORUM_BINDING_GUARD_DSN`；
3. 插件配置：

   ```text
   newapi_base_url       https://meta-api.vip
   pulse_base_url        http://pulse-api:8088
   sso_hmac_secret       与 new-api PULSE_FORUM_SSO_SECRET 相同
   sso_hmac_secret_previous  仅密钥轮换窗口使用
   pulse_hmac_secret     与 PULSE_FORUM_HMAC_SECRET 相同（只读 Profile 专用）
   nonce_redis_url       redis://redis:6379/2
   level_badge_enabled   true
   ```

已部署站点的“登录元衡用户”跳转地址来自 Answer 后台 Meta Pulse 插件的 `newapi_base_url`，不是静态前端配置。请将该字段设置为 `https://meta-api.vip` 并保存；更新仓库不会覆盖数据库中已保存的插件配置。

4. 在 new-api 线上只增加：

   ```env
   PULSE_FORUM_SSO_SECRET=<论坛 SSO 独立密钥>
   PULSE_FORUM_SSO_CALLBACK_URL=https://<新社区域名>/api/user-center/login/callback
   ```

5. 使用原 Compose/编排做零停机重建并验证；不改 new-api 源码、数据库或现有业务路由。

绑定是可选的。Answer 本地账号、密码、资料和封禁保持权威；同一 Answer/new-api 账号只能一对一绑定，普通解绑和换绑会被数据库拒绝。身份纠错只能在备份、维护窗口和审计工单下处理。

部署成功后查看：

```bash
docker compose --env-file .env -f docker-compose.yml ps
docker compose --env-file .env -f docker-compose.yml logs -f pulse-api pulse-worker
```

若只需临时启动 API 验收，可显式使用 `--skip-worker`；这不是完整生产部署，不能作为正式上线状态：

```bash
./deploy/install.sh --skip-worker
```

## 一键更新

已注册快捷命令时，可直接使用 `metar update`；参数与下方脚本一致。

默认更新当前分支。脚本要求 tracked 工作区干净，先取得 Git lock，再只读校验已有 `.env`。配置缺失、空密钥或占位密码会直接停止，必须从备份恢复原凭据；更新绝不会自动生成新密码/随机种子。安装支持的 shell 配置注入不适用于更新，Compose 也不会使用继承的 `PULSE_*`、`NEWAPI_*`、`FORUM_*` 或 `COMPOSE_PROJECT_NAME` 覆盖已校验文件：

```bash
cd /opt/meta-pulse
./deploy/update.sh
```

常用选项：

```bash
./deploy/update.sh --ref main       # 指定远程分支
./deploy/update.sh --skip-forum    # 只更新 Pulse，不重建 Answer；new-api 始终独立更新
./deploy/update.sh --no-build      # 仅使用已有镜像（仅适合已预构建场景）
```

更新顺序是：加锁 → 只读校验配置 → 备份原 `.env` 和 Compose → 备份运行中的 Pulse/Forum 数据库 → 拉取代码 → 校验 Compose → 构建新镜像 → 排空/停止旧 Pulse API → `migrate-up` → 重建服务 → API/Worker `/readyz`。迁移只前进，不执行 down；失败时会输出容器日志、原 commit 和备份位置，不会伪造成功。若 `deploy/nginx/meta-pulse.conf` 发生变更，脚本会强制重建网关容器，避免直接文件挂载因 inode 未更新而继续使用旧配置；无网关配置变更时仅执行常规启动与 reload。

每次更新的 `.env`、更新前 Compose 配置和数据库 dump 位于：

```text
.data/deploy-backups/<UTC 时间>-<旧 commit>/
```

## 状态、迁移和日志

```bash
# 服务状态
docker compose --env-file .env -f docker-compose.yml ps

# 迁移状态
docker compose --env-file .env -f docker-compose.yml run --rm --no-deps \
  --entrypoint meta-pulse-tool pulse-api migrate-status

# 全部日志
docker compose --env-file .env -f docker-compose.yml logs -f
```

## 回滚原则

代码回退不能等同于数据库回滚。更新失败时先查看脚本提示和备份，再确认新迁移对旧版本兼容；不要直接执行 destructive migration 或删除数据卷。代码回退示例：

```bash
git checkout main
git reset --hard <旧 commit>
docker compose --env-file .env -f docker-compose.yml build pulse-api pulse-worker forum
docker compose --env-file .env -f docker-compose.yml up -d pulse-api pulse-worker forum
```

如果新版本已经写入不可逆 schema，必须按数据库备份/恢复方案处理，禁止用 `docker compose down -v`“解决”问题。

正式关闭 `PULSE_REWARD_SHADOW_MODE` 前，仍需完成 new-api Benefit 实际到账、Query/Reconciliation、密钥轮换、限流，以及 Answer 本地注册/邮件确认、绑定和跨实例 Redis flow/nonce 外部验收；本脚本不会替代这些验收。


## 监控与最小权限

API 使用服务/BFF/Admin/随机密钥；Worker 只需要服务/随机密钥、只读 LOG_DB 和 Benefit 地址，不能为了通过启动校验给它额外分发 BFF/Admin 密钥。迁移工具不需要签名密钥。可用 `meta-pulse-tool config-check --role worker` 校验对应环境（角色也支持 `api`、`tool`）；命令不连接数据库、不输出密钥。

Prometheus 必须能访问 Docker 私网，可配置两组抓取目标（不要发布宿主机端口）：

```yaml
scrape_configs:
  - job_name: pulse-api
    static_configs:
      - targets: ['pulse-api:8088']
  - job_name: pulse-worker
    static_configs:
      - targets: ['pulse-worker:8089']
```

- API `/metrics`：HTTP 计数，按路由模板分组。
- Worker `/metrics`：真实账本差异、结算 retry/dead、预算、周期/任务失败、采集新鲜度。
- 需要同时告警抓取失败、`meta_pulse_operations_up == 0` 和 `time() - meta_pulse_operations_last_success_timestamp_seconds > 120`。首次采集前业务值为 NaN；失败时保留旧值但将 up 设为 0，不能把陈旧数据当作正常。
- Worker `/readyz` 只检查 Pulse MySQL/Redis；日志源只读权限仍在启动时 fail closed 验收。启动后各任务独立超时，慢日志/论坛查询不消耗结算和对账的任务预算。

## 幂等升级与回归

首次从旧部署脚本升级到本版时，先备份已有 `.env`，在 tracked 工作区干净的前提下用 `git pull --ff-only` 获取新脚本，再执行 `./deploy/update.sh`；已经运行的旧脚本不会自动获得本版的保护。旧 `.env` 还没有 `PULSE_FORUM_HMAC_SECRET` 时，先用 `openssl rand -hex 32` 生成并补入 `.env`，更新 Pulse API 后再把同值写入 Answer 插件 `pulse_hmac_secret`；过渡期只影响等级徽章，不影响 Answer 本地登录、论坛内容或 new-api 业务。不得继续让论坛复用 `PULSE_SERVICE_HMAC_SECRET`。

本次新增 `00009_action_replay_indexes.sql`，只添加历史请求/动作查询索引，不改写任何账本、金额或旧幂等记录。更新脚本在迁移前通过 `compose stop -t 15 pulse-api` 排空/停止旧 API 的在途写请求，不能混跑新旧 Action 实现；这会短暂停用 Pulse API，但不会停止 new-api 的模型、计费或登录服务。新操作需要新的 action id/key；网络重试复用原值，跨周期也返回原结果。历史同 key/action 已有多笔结果时返回 conflict，必须人工核对，禁止删除记录后重发。

若需回退到不认识稳定幂等范围的旧版本，应先在 new-api BFF 封闭 Action 入口并解决回放兼容，优先采用向前修复；不能只回退二进制后继续发奖，更不能恢复旧库抹掉已经提交/到账的奖励。

```bash
make test                # Go + 更新脚本离线回归
make deploy-config-test  # 实际 Compose 渲染 + 生产角色校验
make test-integration          # 需要专用测试库 PULSE_INTEGRATION_DSN
make test-forum-integration    # 需要专用测试库 FORUM_INTEGRATION_DSN
```

集成 DSN 必须指向可丢弃的独立 MySQL 8 测试库，禁止指向现有业务库。论坛集成测试会 DROP/重建 Answer 测试表，因此 schema 名必须以 `_integration`、`-integration`、`_test` 或 `-test` 结尾。MySQL 开启 binary logging 时需允许创建账本保护触发器（`log_bin_trust_function_creators=1`）；CI 自动创建隔离服务并设置该项。真实服务器的安装、更新、备份恢复和公网身份链路仍需另外验收。


## 运营入口与摄入验收

运营只读页面在 **new-api 的 `/console/pulse-ops`**，不是社区域名的 `/admin`。已有代码链路为管理员会话 → `/api/pulse/ops/overview` → Pulse `/v1/internal/admin/operations/overview`。new-api 需配置 Pulse 私网地址 `PULSE_INTERNAL_URL` 与独立 `PULSE_ADMIN_HMAC_SECRET`，后者与 Pulse API 同名配置一致；new-api 控制台中已保存的对应选项优先于环境变量。不得向浏览器或社区静态配置分发密钥。

上线后先确认管理员能读取周期与游标、普通用户被拒绝、Pulse 不可用时页面明确降级。页面中的活动周期提示只说明当前时刻存在周期和规则，不能单独证明 Worker 正在运行、历史积压已有对应周期或游标已追平。

### 批量与时间预算

Go、Compose 与生产模板的 `PULSE_INGEST_BATCH_SIZE` 默认值均为 **250**，允许显式配置 1–5000。已有 `.env` 中的显式值不会被更新脚本改写；此前已调成 250 的配置会保留。

Usage Worker 单轮保留 20 秒硬超时，并在 15 秒处理预算后从最近已提交事件处让出批次。正常让出返回 `yielded=true`，30 秒后继续，不触发错误退避；数据库错误、事务失败、取消和硬超时仍退避。该预算在事件之间检查，不保证任意慢单条事务都能在 20 秒内完成。

更新并检查 `/readyz` 后，观察至少三个连续批次：

```bash
docker compose --env-file .env -f docker-compose.yml logs --since 10m pulse-worker
```

- `elapsed_ms` 是实际处理耗时，`fetched` 是读取条数，`accepted` 等结果只计成功提交；
- `yielded=true` 表示本页未处理尾部将在下一轮重新读取，不代表丢数据或跳过记录；
- 核对运营概览中的 `value`、`version` 和 `watermark_at` 持续前进，积压期间 `lag_seconds` 持续缩小；
- 无新调用时水位可能不前进，不能只用水位年龄宣称摄入失败；同时看 Worker 成功日志与实际源流量；
- 若仍出现 `context deadline exceeded`，核对单批耗时与数据库延迟，再按实测降低批量；不要关闭故障退避或通过 `cursor-seek` 跳过积压来制造“追平”。

仓库测试和 `/readyz` 不能替代上述真实摄入验收。没有真实日志与水位证据时，应保留“积压是否追平待验收”。

## metar 快捷管理命令

已有部署先用原更新脚本获取新命令并完成构建，再注册一次入口。默认位置为 `/usr/local/bin/metar`；不要先手动拉取再升级，以免丢失静态资源变更检测所需的旧 commit：

```bash
cd /opt/meta-pulse
./deploy/update.sh --ref main
bash deploy/install-cli.sh
metar
```

若当前用户没有 `/usr/local/bin` 写权限，可用 `sudo bash deploy/install-cli.sh` 安装。普通用户也可选择个人目录：

```bash
bash deploy/install-cli.sh --bin-dir "$HOME/.local/bin"
export PATH="$HOME/.local/bin:$PATH"
```

把 PATH 设置加入自己的 shell 配置后永久生效。命令安装不授予 Docker 或仓库写权限；运行时仍使用当前用户已有权限。安装器不会覆盖同名非 METAR 命令。仓库被移动后，需要在新目录重新运行安装器。

输入 `metar` 显示：

```text
1. 升级
2. 重启全部服务
3. 停止全部服务
4. 启动全部服务
5. 查看状态
6. 查看最近日志
7. 卸载服务（保留数据）
0. 退出
```

也支持直接调用：

| 命令 | 行为 |
| --- | --- |
| `metar update` | 复用升级脚本：锁、备份、Git 快进、构建、迁移、健康检查 |
| `metar update --ref main --skip-forum` | 指定分支升级，跳过论坛重建 |
| `metar start` | 用现有镜像启动整套服务；也可恢复保留数据的卸载 |
| `metar restart` | 重启整套服务，随后检查各容器和 Pulse `/readyz` |
| `metar stop` | 停止整套服务，保留容器及数据 |
| `metar status` | 显示容器状态 |
| `metar logs pulse-worker -f` | 跟踪 Worker 日志；不加 `-f` 只显示最近 100 行 |
| `metar uninstall` | 输入 `UNINSTALL` 后移除容器与网络，保留数据卷、镜像、配置、源码和快捷命令 |
| `metar uninstall --yes` | 脚本环境显式确认上述保留数据的卸载 |
| `metar help` | 查看命令帮助 |

管理范围是本仓库 Compose 及其宿主机覆盖配置中的服务，包含社区、Pulse、数据库和网关。停止/重启/卸载会造成社区和 Pulse 暂时不可用，但不会操作独立部署的 new-api。命令不提供清空数据卷选项，卸载也不清除证书、定时任务或系统 Docker。

自定义环境文件可用 `metar --env-file /path/to/production.env status`；覆盖配置继续使用 `META_PULSE_COMPOSE_OVERRIDE_FILE`。命令复用 `lib.sh` 的配置隔离，不允许 shell 中的应用凭据隐式覆盖 `.env`。升级、启动、重启、停止、卸载使用同一把部署锁，不允许并发执行；状态与日志查询不占锁。

启动前应已完成首次部署和数据库迁移；`metar start` 不构建镜像、不执行迁移，代码升级应使用 `metar update`。启动器指向仓库中的脚本，后续 Git 更新会自动更新命令实现，无需重复安装快捷命令。


### 自动摄入验收（只读）

服务器更新并验收（Python 3 标准库，无需安装 pip 包）：

```bash
./deploy/update.sh --ref main --skip-forum --accept-ingest
```

已有新版部署可单独运行：

```bash
python3 deploy/accept-ingest.py --seconds 180
# 自定义配置沿用部署入口，不能用 shell PULSE_* 覆盖 .env：
META_PULSE_ENV_FILE=/opt/meta-pulse/.env python3 deploy/accept-ingest.py
```

验收核对干净工作区 HEAD、运行中 Worker 镜像的 OCI revision、容器实际
`PULSE_INGEST_BATCH_SIZE`（不是模板值），打印容器/image ID 与启动时间。
部署 helper 自动把当前 HEAD 注入镜像构建参数；旧镜像没有 revision 时必须重建，
`--no-build` 不会把旧镜像伪装成新 commit。手动 Compose 构建需显式设置
`META_PULSE_REVISION=$(git rev-parse HEAD)`，且只在干净工作区构建。

采集前后通过运行中 Worker 的 `meta-pulse-tool ingest-snapshot` 只读查询
`new-api-usage/new-api-log` 游标，输出 value/version/watermark_at/lag_seconds
及 version、lag 差值。快照复用 Worker 实际 Pulse DSN 和时区，不依赖运营指标缓存，
不连接 new-api。观察窗口默认 180 秒，最少 90 秒；只统计首次快照之后的日志，
至少需要连续三个成功批次，输出 fetched/accepted/replayed/conflicts/manual_review/
yielded/elapsed_ms。窗口内任一 ingest 失败均保留为失败，不被后续成功掩盖。

退出码：

- `0`：至少三个成功批次且 value/version/watermark 有前进，证明观察窗口内正常续跑；
  **不代表已追平**，本工具未独立读取源端尾部，也不证明新流量持续到达。
- `2`：**证据不足**，包括空批次（没有新源流量或可摄入源行）、日志不足、
  游标未动、watermark 同秒未变、观察中容器重启/替换。无流量时 lag 自然增长，
  不能据此报摄入失败或已追平；延长窗口或等真实流量后重跑。
- `1`：配置/版本/采集失败、窗口内 ingest 失败或游标回退。命令不打印底层错误和凭据。

lag 是最后已提交源事件的年龄，不是剩余行数；忙碌源端下 lag 增长也不能单独认定故障。
缺游标时 value 为空、version 为 0、watermark 为 null，lag 的 0 不表示追平。
验收不会触发 backfill、cursor-seek、记账、源端流量或服务重启，也不会操作 new-api。
`--accept-ingest` 不能与 `--skip-worker` 同用；验收退出 1/2 时更新本身可能已完成，
不会自动回滚或修改游标。原始批次错误字段不输出，需在服务器受控环境另查日志。
生产验收仍需实际执行，离线测试和 CI 不替代线上证据。
