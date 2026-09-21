# Meta Pulse｜元衡脉冲计划

**元衡脉冲计划（Meta Pulse）是以社区为主要产品入口、以真实付费调用权益为增长内核的用户社区与回馈系统。**

社区账号由 Apache Answer 独立管理，可单独注册；社区经验、签到和成长等级独立可用；用户可选绑定元衡 new-api 账号，解锁 Pulse 贡献等级、脉冲券和奖励。Pulse 的经济内核仍只建立在 new-api 的真实付费调用之上。

它把原本单向的“充值 → 调用模型 → 扣费 → 再充值”，变成：

```text
正常使用元衡 API
        ↓
产生真实付费消耗
        ↓
获得贡献值
        ↓
积累脉冲券
        ↓
开启一次脉冲
        ↓
获得即时回馈
        ↓
使用脉冲券参与奖励（新券按获得时间计算额度资格）
        ↓
形成持续使用动力
```

用户看到的是“**调用 → 积累 → 开启 → 获得回馈**”；后台运行的是 **Economics + Ledger + Idempotency + Budget + Reward + Settlement + Experiment**。

## 为什么做

元衡当前 C 端 API 业务天然容易陷入价格、模型数量和通道稳定性的横向竞争。Meta Pulse 的目标不是把元衡做成游戏平台，而是：

- 提高真实付费用户留存；
- 提高有效 API 调用；
- 让历史真实使用形成持续积累；
- 建立不只依赖低价 Token 的产品差异化；
- 沉淀可复用到 Enterprise FinOps、AI Analytics、Provider Health、模型情报和智能路由的数据能力。

战略边界：

> **C 端养现金流，B 端建壁垒。**

## 与 new-api 的关系

```text
new-api = 模型调用与资金事实源
Meta Pulse = 调用之后的增长与权益系统
```

`new-api` 继续负责用户、API Key、模型请求、Provider/Channel、计费、充值、余额、订阅和消费/退款日志。

Meta Pulse 负责 Usage Event、贡献值、脉冲券、经济规则快照、历史周期、Reward、Budget、Experiment、增长分析和奖励结算状态。

管理员在 **Pulse 配置 → 设置兑换比例** 配置贡献倍率、产券门槛、额度资格天数（默认 30 天）、奖项权重和预算，不再手动创建周期。新券满设定天数后仅抽经验；抽奖页面公开当前券的规则。已有券保留原规则，剩余贡献持续累计。详见 [管理配置说明](docs/METAR_ADMIN_SETTINGS.md)。

论坛与博客构成社区层，负责注册登录、搜索引流、内容沉淀和等级展示。Answer 是社区身份事实源；new-api 是 API/资金身份事实源；二者通过一对一、不可静默转移的可选绑定关联，详见 [docs/COMMUNITY.md](docs/COMMUNITY.md)。

硬原则：

> **Pulse 故障不得影响 new-api 模型调用、计费、充值和余额，也不得阻断论坛本地登录或浏览。**

## 技术栈

- Go 1.22+
- Gin
- GORM
- MySQL 8.0+
- Redis 7+
- React 18 / Vite / Semi Design（new-api 控制台）与 METAR 静态社区壳层
- Apache Answer（论坛）/ VitePress（博客）
- Docker / Docker Compose / Nginx

## METAR 前端改造

`metar-frontend/` 保留 43 个页面入口的离线视觉/交互原型；原型仍只使用本地 mock，**不能直接部署**。正式实现位于 [`metar-frontend/production/`](metar-frontend/production/)，已经与原型彻底分离，并接通 Apache Answer 的真实只读能力：

- 发现、问题列表、详情、回答、话题与搜索；
- 当前用户、个人资料、收藏、通知与账号绑定状态；
- 右上角中英文界面切换，与 Answer 原生论坛共享浏览器语言偏好；
- Answer 原生登录、注册、找回、发帖和写操作入口；
- VitePress 知识库入口，以及未登录、未激活、未绑定和服务不可用状态。

正式首页与 `/latest` 使用紧凑讨论列表，支持不带 `#` 的 History 路由；Nginx 按白名单提供深链接，旧 Hash 链接自动转换。`/questions`、`/users/*`、`/answer/api/*` 等路径仍由 Answer 原生 UI/API 负责，因此前端回退不会修改社区数据、会话或权限规则。Pulse 用户权益通过受 Answer 身份保护的社区 BFF 接通总览、奖池、抽奖及奖励记录；默认关闭新抽奖，未完成配置时明确显示不可用状态。后续阶段见 [`docs/METAR_FRONTEND_REFACTOR_PLAN.md`](docs/METAR_FRONTEND_REFACTOR_PLAN.md)。

本地验证：

```bash
make test-community
make build-community  # 产物写入现有博客静态卷的 metar/ 子目录
```

## 快速开始

正式版本、下载附件及指定版本升级见 [发布与部署](docs/RELEASE.md)。发布页为 [GitHub Releases](https://github.com/nanashiwang/meta-pulse/releases)，已有服务器使用 `metar update --release v0.2.0`。

### 服务器首次部署

Meta Pulse 使用独立的 Pulse MySQL、Redis、API、Worker 和 Answer 容器；new-api 继续在原服务器独立运行。正式社区首页为 `https://metar.uk/`，Answer 原生论坛路由继续位于同域名，博客位于 `https://metar.uk/blog/`，`www.metar.uk` 跳转主域名。推荐在 Linux 服务器执行：

```bash
git clone https://github.com/nanashiwang/meta-pulse.git /opt/meta-pulse
cd /opt/meta-pulse
./deploy/install.sh
```

首次执行会创建 `/opt/meta-pulse/.env` 并生成随机凭据。填写真实的 `NEWAPI_LOG_DSN`（只读账号）、`NEWAPI_INTERNAL_BASE_URL` 和可选的 `FORUM_DB_DSN` 后再次执行。论坛启用后，在 Answer 后台配置插件。本地论坛和可选账号绑定复用 new-api 已有 SSO Bridge，配置 `PULSE_FORUM_SSO_SECRET` 与固定 callback 即可；自动抽奖还必须升级独立运行的 new-api 付费来源证明及 Benefit 接收端，完成限额和实际到账验收，不能仅靠 SSO 配置开放。无需在社区服务器再部署一套 new-api。脚本在配置不完整时会安全退出，不会删除数据卷。

### 一键更新

已部署的服务器可以一次性安装 `metar` 管理命令（默认写入 `/usr/local/bin`，需要相应权限）：

```bash
cd /opt/meta-pulse
./deploy/update.sh --ref main
bash deploy/install-cli.sh
metar
```

`metar` 打开数字菜单；也可直接执行 `metar update`、`metar restart`、`metar stop`、`metar start`、`metar status`、`metar logs pulse-worker -f`。`metar uninstall` 需输入确认词，只移除容器和网络，保留数据卷、配置与源码。停止/重启/卸载覆盖整套 METAR 服务，包括社区和数据库；不操作独立的 new-api。

`metar update` 复用下方更新脚本，支持 `--ref main --skip-forum`。首次安装命令只是注册快捷入口，不会部署或升级服务；注册后无需手动 `git pull`，后续直接使用 `metar update`。自定义配置与普通用户安装见 [管理命令说明](deploy/README.md#metar-快捷管理命令)。

```bash
cd /opt/meta-pulse
./deploy/update.sh
```

更新脚本会先加锁、只读校验已有配置，再备份数据库和原配置、fast-forward 拉取代码、执行迁移、重建服务并检查 API/Worker 的 `/readyz`。更新不会生成或轮换凭据；配置缺失时必须先恢复原配置。详细参数、日志、回滚和外部依赖见 [`deploy/README.md`](deploy/README.md)。

Pulse API 不持有发奖用的 `PULSE_SERVICE_HMAC_SECRET`，该密钥只交给 Worker 与 new-api 接收端；API 按需配置独立 `PULSE_ROLLBACK_HMAC_SECRET`，仅用于受控查询和撤销。Worker 不持有撤销、BFF 或 Admin 密钥。

更新后自动摄入验收：`./deploy/update.sh --ref main --accept-ingest`。只读观察三个以上 Worker 批次及游标前后变化；无流量时输出“证据不足”，不宣称追平。详见 [验收命令](deploy/README.md#自动摄入验收只读)。

### 本地验证

```bash
make test       # Go 测试 + 部署脚本 + METAR 正式前端测试
make test-community
make vet
make deploy-test
make deploy-config-test  # 生产 Compose 配置 + API/Worker/Tool 最小权限校验
# FORUM_INTEGRATION_DSN=.../forum_integration?... make test-forum-integration
```

### 运行监控与数据库回归

- API 的 `:8088/metrics` 提供 HTTP 指标；Worker 的 `:8089/metrics` 提供账本、结算、预算、周期失败与任务失败指标。两者均只允许内网采集，不发布宿主机端口。
- 业务指标必须同时检查 `meta_pulse_operations_up` 和最近成功采集时间，不能把未采集/过期数据当作正常。
- `make test-integration` 必须显式提供专用测试库的 `PULSE_INTEGRATION_DSN`，执行真实 MySQL 事务、100 并发/重放、跨周期和旧版幂等恢复测试；**禁止指向业务数据库**。CI 自动创建隔离 MySQL。
- `make test-forum-integration` 验证 Answer v1.7.1 表结构、一对一不可变绑定及绑定后内容映射；测试会重建表，schema 名必须以 `_integration`、`-integration`、`_test` 或 `-test` 结尾。
- 新的开启操作必须使用新的 `action_id` 和 `Idempotency-Key`；响应丢失时复用原值，即使周期已经结束，也返回首次结果。

## 仓库结构

Monorepo，三条 track 并行推进：

```text
meta-pulse/
├── go.work                          Go 工作区（pulse + forum plugin）
├── services/
│   ├── pulse/                       Track A｜Pulse 主线
│   │   ├── cmd/{api,worker,tool}/
│   │   ├── internal/
│   │   ├── migrations/
│   │   └── Dockerfile
│   ├── forum/                       Track C｜论坛构建定义
│   │   └── Dockerfile               重编译 Answer + 插件
│   └── forum-plugin/
│       └── user-center-pulse/       可选账号绑定 + 等级徽章插件
├── sites/
│   └── blog/                        Track B｜VitePress 博客
├── deploy/nginx/                    HTTPS 分域网关与 Cookie 隔离
└── docs/
```

Apache Answer 源码不进入仓库，通过官方镜像与 Go module 引入。

## 文档

- [项目总览与已实现入口](docs/OVERVIEW.md)
- [完整项目架构](docs/ARCHITECTURE.md)
- [社区架构（论坛 + 博客）](docs/COMMUNITY.md)
- [实施计划（里程碑与出口标准）](docs/IMPLEMENTATION_PLAN.md)
- [工程约束](AGENTS.md)
- [数据库迁移说明](services/pulse/migrations/README.md)
- [服务器一键部署与更新](deploy/README.md)

## 社区抽奖与自动到账

METAR 管理员可在 `/admin/pulse` 配置额度换算、抽奖开关、影子模式与各用途的 HMAC 密钥，并通过「设置兑换比例」创建带贡献倍率、每券贡献度和奖池的新周期；已启用周期保持只读。首次在 Answer 插件后台完成管理通道配对，之后无需为这些参数反复修改 `.env` 或重启；密钥按 API/Worker 分角色加密，保存有版本校验、幂等与审计。启用、备份和恢复步骤见 [METAR 管理员配置](docs/METAR_ADMIN_SETTINGS.md)。

代码已接通绑定本人账号的自动到账链路、Pulse 预算与 new-api 独立金额上限、付费来源证明、操作恢复和独立撤销权限。新抽奖与新到账默认关闭，旧周期不直接开放发奖；新奖池需审计创建与真实环境验收。首版只认升级后支持的在线支付与普通同步钱包结算凭证中的付费部分；历史余额、赠送和未覆盖的计费路径不自动产券，也不回填付费资格。配置和发布步骤见 [REWARDS_ROLLOUT.md](docs/REWARDS_ROLLOUT.md)。

## 当前状态

**P0–M7 功能里程碑已落地；社区身份已调整为“Answer 独立账号 + 可选绑定 new-api”，并补齐数据库约束、重放防护、内容映射和网关隔离。正式上线仍需真实外部环境验收。**

已落地范围包括：Usage Ingest、Ledger/Account、等级、确定性 Reward、Hard Budget、Transactional Outbox、Benefit Query/Reconciliation/Rollback、可重入 Period Close、运营审计与指标，以及论坛本地注册、一对一不可变绑定、Pulse 徽章和独立预算的内容奖励。

运营只读页面已位于 **new-api `/console/pulse-ops`**，由管理员 BFF 接通 Pulse 周期、经济规则和摄入游标；它不属于 METAR 社区原型后台。部署时需核对两端独立的 Admin 密钥与私网地址，见 [运营入口与摄入验收](deploy/README.md#运营入口与摄入验收)。Usage Worker 默认批量 250，并在 15 秒处理预算后从已提交事件处续跑，避免正常积压反复触发 20 秒硬超时和长退避。

仍需完成的外部验收包括真实 LOG_DB 样本与只读权限、Provider 成本快照、new-api Benefit 实际到账与密钥轮换、社区正式域名、Answer 初始化/邮件发送、跨实例 Redis flow/nonce 和生产灰度。明细见 [实施计划第 8 节](docs/IMPLEMENTATION_PLAN.md#8-当前未冒充完成的外部验收)。

实现不得改变 `docs/ARCHITECTURE.md` 定义的系统边界、事实源和工程红线，以及 `docs/COMMUNITY.md` 定义的社区层边界。里程碑、出口标准和外部验收清单见 `docs/IMPLEMENTATION_PLAN.md`。
