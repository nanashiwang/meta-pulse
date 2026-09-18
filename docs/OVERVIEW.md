# Meta Pulse 项目总览

本文是当前代码的导航，不替代 [架构基线](ARCHITECTURE.md)、[社区边界](COMMUNITY.md) 和 [实施计划](IMPLEMENTATION_PLAN.md)。代码实现、隔离环境验证与生产验收分别记录；不能由前两者推断第三者已完成。

## 系统边界

| 系统 | 负责什么 |
| --- | --- |
| new-api | API 身份、模型调用、计费、余额及最终权益到账 |
| Apache Answer | 社区独立注册、登录、资料、封禁、内容和治理角色 |
| Meta Pulse | 真实付费调用之后的贡献、券、周期、奖励、预算、结算与审计 |
| VitePress / METAR | 知识库与社区展示入口 |

Answer 与 new-api 通过受保护的一对一可选绑定关联。未绑定用户仍可使用社区；绑定前的内容不得产生 Pulse 内容奖励。社区内容不产生贡献值或券。

Pulse 不进入模型请求同步主链路，也没有 new-api 用户余额表写权限。Pulse 故障不能阻断模型调用、计费或论坛本地登录与浏览。

## 已有入口

| 入口 | 实现位置与边界 |
| --- | --- |
| 社区首页 `/` | `metar-frontend/production/`，同源读取真实 Answer API |
| 论坛 `/questions`、`/users/*` | Answer 原生路由，保留登录和内容写权限 |
| 知识库 `/blog/` | `sites/blog/`，VitePress 静态站 |
| 用户权益 `/console/pulse` | new-api 仓库，通过用户 Signed BFF 读取本人数据 |
| 运营概览 `/console/pulse-ops` | new-api 仓库，通过管理员 Signed BFF 读取周期、规则、游标与诊断计数 |
| Pulse 原始接口 | 仅内网，必须经过签名与角色校验；浏览器不能直接访问 |

运营调用链：

```text
new-api 管理员会话
  → /api/pulse/ops/overview
  → 独立 Admin 密钥签名
  → Pulse /v1/internal/admin/operations/overview
```

Answer 管理员身份不自动获得 Pulse 运营权限。METAR 原型中的后台页面仍为设计参考，不能替换为生产后台。已有 new-api 入口无需在社区再暴露一次内部 API。

## 代码地图

| 目录 | 职责 |
| --- | --- |
| `services/pulse/cmd/{api,worker,tool}` | HTTP 服务、异步任务与审计运营命令 |
| `services/pulse/internal/domain` | 无框架依赖的定点数、经济规则、周期、奖励等领域逻辑 |
| `services/pulse/internal/service` | 用例编排及事务边界 |
| `services/pulse/internal/ports` | 仓储和外部服务接口 |
| `services/pulse/internal/store/mysql` | 持久化、幂等约束、事务与派生快照 |
| `services/pulse/internal/adapter` | new-api 日志/Benefit 与 Answer 内容只读适配 |
| `services/forum-plugin/user-center-pulse` | 安全绑定及可降级等级展示 |
| `metar-frontend/src` | 离线交互原型，不可直接部署 |
| `metar-frontend/production` | 无 mock 的社区生产适配层 |
| `deploy` | Compose、安装更新、网关及配置验证 |

## 关键链路

1. **摄入**：只读 LOG_DB → 标准 Usage Event → 单事务写事件、贡献账本/账户、券权益/账本/账户、用户周期统计与游标。相同来源事件重放不重复入账，载荷变化进入冲突。
2. **开脉冲**：同一事务完成幂等、扣券、预算预占、固定随机结果、Reward Grant 和 Outbox。
3. **结算**：Worker 通过幂等 Benefit API 发放。超时先 Query/Reconcile，永远复用原 `source_ref`，不直接写 new-api 余额。
4. **周期**：显式运营命令创建 10 天周期及经济规则；Active 后冻结核心配置；Close 可重入并受水位和对账约束。
5. **内容奖励**：只读采集绑定后的合格内容 → 人工审核 → 独立预算与资格限额 → Grant/Outbox；不产生贡献或券。

Ledger 是事实源，Account 是可重建投影。金额与贡献使用整数定点数；跨服务使用 Outbox、幂等接收和对账，不使用分布式事务。

## 本轮收尾与剩余验收

日志摄入默认批量统一为 250。Worker 在 15 秒处理预算达到后，只于事件事务成功提交处让出批次，30 秒后从 durable cursor 继续；20 秒硬超时和真实故障退避保持有效。日志记录 `elapsed_ms`、`yielded` 与已提交计数，不能把取到的条数当作已入账条数。

旧的服务器临时配置调整不能证明生产积压已追平。上线后应观察多轮游标、水位和成功日志，核对积压是否持续缩小，步骤见 [部署验收](../deploy/README.md#运营入口与摄入验收)。

尚未关闭的范围：

- 真实 LOG_DB/Provider 成本、经济参数回测及生产积压追平验收；
- new-api Benefit 实际到账、超时恢复、撤销与密钥轮换演练；
- Answer SMTP、账号激活、绑定及跨实例 Redis flow/nonce 验收；
- METAR 社区 BFF、社区权益页、统一写页面与完整运营工作台，详见 [前端阶段计划](METAR_FRONTEND_REFACTOR_PLAN.md)。

## 验证与交付

```bash
make test vet deploy-config-test build-pulse build-community
# 专用的一次性测试库，禁止业务数据库：
PULSE_INTEGRATION_DSN='专用测试 DSN' make test-integration
```

仓库根不是 Go module，Go 命令需显式列模块。当前仓库有 CI 与服务器更新脚本，尚无版本号和 GitHub Release 自动发布流程；提交推送不等于服务器已部署。
