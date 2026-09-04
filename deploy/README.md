# Meta Pulse 服务器部署

本目录提供 Linux + Docker Compose v2 的首次部署和一键更新脚本：

- `install.sh`：初始化生产配置、生成随机凭据、启动依赖、执行迁移、启动服务并检查 `/readyz`。
- `update.sh`：加锁拉取远程分支、备份配置、构建镜像、执行前向迁移、重建服务并检查 `/readyz`。
- `meta-pulse.env.example`：生产配置模板。
- `test.sh`：不连接 Docker 的脚本和配置离线测试。

## 部署边界

脚本只部署 Meta Pulse 自身的 Pulse MySQL、Redis、API、Worker 和 Answer 容器，不会：

- 修改或重启 new-api；
- 写入 new-api 用户余额或 LOG_DB；
- 给 Pulse、Answer、MySQL、Redis 发布宿主机端口；
- 删除 Docker 数据卷或执行 `docker compose down -v`；
- 自动修改 Nginx、TLS、DNS 或公网防火墙。

new-api 必须先独立部署，并完成 Signed BFF、Internal Benefit API、LOG_DB 只读账号和对应密钥配置。公网访问仍由外部 Nginx 通过 Docker 内网转发，参见 [`deploy/nginx/README.md`](nginx/README.md)。

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
NEWAPI_INTERNAL_BASE_URL='http://new-api:3000' \
./deploy/install.sh
```

注意：脚本生成的 `PULSE_SERVICE_HMAC_SECRET`、`PULSE_USER_BFF_HMAC_SECRET`、`PULSE_ADMIN_HMAC_SECRET` 需要同步配置到 new-api / 论坛插件的对应服务端；配置不一致时应保持服务不可用，不要降低验签要求。

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

默认更新当前分支。脚本要求 tracked 工作区干净，保留 `.env`，并使用 Git lock 防止并发更新：

```bash
cd /opt/meta-pulse
./deploy/update.sh
```

常用选项：

```bash
./deploy/update.sh --ref main       # 指定远程分支
./deploy/update.sh --skip-forum    # 只更新 Pulse，不重建 Answer
./deploy/update.sh --no-build      # 仅使用已有镜像（仅适合已预构建场景）
```

更新顺序是：加锁 → 备份运行中的 Pulse/Forum 数据库 → 拉取代码 → 校验 Compose → 构建新镜像 → `migrate-up` → 重建服务 → `/readyz`。迁移只前进，不执行 down；失败时会输出容器日志、原 commit 和备份位置，不会伪造成功。

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

正式关闭 `PULSE_REWARD_SHADOW_MODE` 前，仍需完成 new-api Benefit 实际到账、Query/Reconciliation、密钥轮换、限流和跨实例 Redis Nonce 等外部验收；本脚本不会替代这些验收。
