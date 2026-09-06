# Meta Pulse 服务器部署

本目录提供 Linux + Docker Compose v2 的首次部署和一键更新脚本：

- `install.sh`：初始化生产配置、生成随机凭据、启动依赖、执行迁移、启动服务并检查 `/readyz`。
- `update.sh`：加锁拉取远程分支、备份配置、构建镜像、执行前向迁移、重建服务并检查 `/readyz`。
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

首次启动后，先通过新社区域名完成 Answer 初始化，并在后台确认：

1. 开启本地注册和密码登录，配置站点 URL、发信服务与管理员；
2. 启用 `Meta Pulse` 插件；Compose 已仅向 forum 容器注入 `FORUM_BINDING_GUARD_DSN`；
3. 插件配置：

   ```text
   newapi_base_url       https://<现有 new-api 域名>
   pulse_base_url        http://pulse-api:8088
   sso_hmac_secret       与 new-api PULSE_FORUM_SSO_SECRET 相同
   sso_hmac_secret_previous  仅密钥轮换窗口使用
   pulse_hmac_secret     与 PULSE_FORUM_HMAC_SECRET 相同（只读 Profile 专用）
   nonce_redis_url       redis://redis:6379/2
   level_badge_enabled   true
   ```

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

更新顺序是：加锁 → 只读校验配置 → 备份原 `.env` 和 Compose → 备份运行中的 Pulse/Forum 数据库 → 拉取代码 → 校验 Compose → 构建新镜像 → 排空/停止旧 Pulse API → `migrate-up` → 重建服务 → API/Worker `/readyz`。迁移只前进，不执行 down；失败时会输出容器日志、原 commit 和备份位置，不会伪造成功。

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
