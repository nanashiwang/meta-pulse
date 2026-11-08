# Agent Note: 模型情报独立服务

Status: proposed

## Problem

社区计划新增模型价格模块，并将继续扩展评测、Provider 可用性、模型更新日志等情报能力。价格数据来自外部供应商，需要定时采集、保留历史并支持对比；放错位置会让采集故障牵连论坛登录浏览或 Pulse 权益，或让后续情报能力反复迁移。

## Existing capabilities and impact

- 现有服务：[services/pulse](../../../../services/pulse/)（增长权益与账本）、[forum 插件](../../../../services/forum-plugin/user-center-pulse/)（集市、聊天、活动、经营指标，均以 Answer 为事实源）。
- 仓库中无模型价格相关实现；[README](../../../../README.md) 与 [ARCHITECTURE](../../../../docs/ARCHITECTURE.md) 将模型情报、Provider Health、智能路由列为长期方向。
- 参照 [集市](../../../../docs/VIBE_CODING_MARKETPLACE.md) 的前端路由、nginx 白名单与 Answer 原帖讨论复用方式；管理写入参照插件 [admin_bff.go](../../../../services/forum-plugin/user-center-pulse/admin_bff.go) 的签名转发。
- 检索 `.agents/notes` 无相关旧笔记，新建。
- 受影响入口：`go.work`、`docker-compose.yml`、`deploy/nginx/meta-pulse.conf`、`metar-frontend/shared/routes.json`、production 前端路由与导航、Makefile。

## Proposal

新建 `services/intel`（api / worker / tool），独立数据库 `metar_intel`，采用「目录 + 只追加观测 + 派生评分快照」数据模型；公开只读 API 由 nginx 直连，管理写入经论坛插件 admin BFF 签名转发；前端新增 `/models` 页面族与 `/model/:slug` 详情，并复用 Answer 原帖承载讨论。

产品形态对标 AI 榜单站（llm-stats.com 一类）：首个模块为榜单首页，含维度领先卡片、综合榜预览（综合分、推理、编程、Agent、上下文、速度、价格、许可）、近期新模型与方法说明。功能与信息架构对标，视觉、文案、综合分方法与数据自有。完整设计见 [docs/MODEL_INTEL.md](../../../../docs/MODEL_INTEL.md)。

开放问题：M1 模型范围与冷启动方式、综合分 v1 评测集合与权重、速度自测范围与预算、展示币种与汇率、是否展示元衡售价、是否允许用户提交纠错。

## Alternatives considered

- **放进论坛插件** — 最强论据是集市已证明插件可承载独立模块且直接拿到 Answer 身份。否决：价格是外部数据而非 Answer 内容，定时采集与历史数据放在 Answer 进程内会让故障与升级牵连论坛（AGENTS.md 不变量 21），且每次修改需重建 forum 镜像。
- **放进 Pulse** — 最强论据是已有 Go 服务骨架、money 定点类型与 worker 调度。否决：Pulse 承载账本与预算不变量，混入采集比价扩大故障面，也偏离其增长权益定位；可抽取共享的 money 包代替。
- **完整复刻参考站（页面、文案、数据）** — 最强论据是上线最快、信息架构已被验证。否决：对方品牌、设计、文案与汇总数据属于对方，复制或抓取有侵权与条款风险，且数据无法自主核实与纠错；改为对标功能与信息架构，数据取自原始来源与自测，综合分为本站公开方法。
- **放进 new-api** — 最强论据是元衡售价本就在 new-api。否决：跨厂商比价与历史不是资金事实源，且不能让新功能进入模型调用主服务；Intel 仅只读读取元衡售价。

## Acceptance criteria

- Intel 停止时，论坛登录浏览、Pulse 抽奖与结算、new-api 调用均正常，`/models` 显示不可用。
- 相同采集内容重放不产生新快照；价格变化只追加快照，修正有审计记录。
- 首页领先卡片与榜单来自同一评分快照；每个数值可追溯来源与更新时间，缺失值显示「—」不补零；评分方法调整只新增版本。
- 金额全链路无 float；公开 API 匿名可读且不带 Cookie；管理接口拒绝非管理员与未签名请求。
- `make verify-notes`、Intel 单元与 MySQL 集成测试、前端路由测试、`make deploy-config-test` 通过。

## Risks

- 外部来源改版或限流导致数据过期：按来源记录运行状态并展示「更新于」，不以空值覆盖。
- 抓取条款风险：优先官方/聚合 API，上线前逐个来源核对条款。
- 模型命名不一导致错配：别名人工审核后生效。
- 多一个服务的运维成本：共用 MySQL 实例与部署脚本，独立库与账号。
