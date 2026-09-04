# Meta Pulse｜元衡脉冲计划

**元衡脉冲计划（Meta Pulse）是一套建立在元衡 API 真实付费调用之上的 C 端用户增长与回馈系统。**

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
参与 10 天周期奖励
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

Meta Pulse 负责 Usage Event、贡献值、脉冲券、经济规则、10 天周期、Reward、Budget、Experiment、增长分析和奖励结算状态。

论坛与博客构成社区层，负责搜索引流、等级展示和内容沉淀；论坛身份完全委托给 new-api，详见 [docs/COMMUNITY.md](docs/COMMUNITY.md)。

硬原则：

> **Pulse 故障不得影响 new-api 模型调用、计费、充值和余额。**

## 技术栈

- Go 1.22+
- Gin
- GORM
- MySQL 8.0+
- Redis 7+
- React 18 / Vite / Semi Design（用户页面复用 new-api 前端）
- Apache Answer（论坛）/ VitePress（博客）
- Docker / Docker Compose / Nginx

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
│       └── user-center-pulse/       身份委托 + 等级徽章插件
├── sites/
│   └── blog/                        Track B｜VitePress 博客
├── deploy/nginx/                    HTTPS 分域网关与 Cookie 隔离
└── docs/
```

Apache Answer 源码不进入仓库，通过官方镜像与 Go module 引入。

## 文档

- [完整项目架构](docs/ARCHITECTURE.md)
- [社区架构（论坛 + 博客）](docs/COMMUNITY.md)
- [实施计划（里程碑与出口标准）](docs/IMPLEMENTATION_PLAN.md)
- [工程约束](AGENTS.md)
- [数据库迁移说明](services/pulse/migrations/README.md)
- [服务器一键部署与更新](deploy/README.md)

## 当前状态

**仓库内 P0–M7 已完成｜系统架构已冻结｜当前仅剩真实外部环境验收。**

已落地范围包括：身份与服务签名边界、Usage Ingest、Ledger/Account、等级、确定性 Reward、Hard Budget、Transactional Outbox、Benefit Query/Reconciliation/Rollback、可重入 Period Close、运营审计与指标、论坛 SSO/等级，以及独立预算的内容奖励。

尚未冒充完成的事项只包括真实 LOG_DB 样本与只读权限、Provider 成本快照、new-api Benefit 实际到账与密钥轮换、Answer 真实 schema/公网 SSO、跨实例 Redis Nonce、真实域名和生产灰度。明细见 [实施计划第 8 节](docs/IMPLEMENTATION_PLAN.md#8-当前未冒充完成的外部验收)。

实现不得改变 `docs/ARCHITECTURE.md` 定义的系统边界、事实源和工程红线，以及 `docs/COMMUNITY.md` 定义的社区层边界。里程碑、出口标准和外部验收清单见 `docs/IMPLEMENTATION_PLAN.md`。
