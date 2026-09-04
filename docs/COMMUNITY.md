# Meta Pulse 社区｜论坛与博客架构

本文档定义 Meta Pulse 的社区层：论坛（Apache Answer）、博客（VitePress）以及两者与 Pulse 主线的集成边界。

`docs/ARCHITECTURE.md` 仍是系统基线；本文档只在其之上追加社区相关的边界，不改变任何既有不变量。

## 1. 为什么要社区层

元衡 C 端 API 业务的获客高度依赖价格比较，缺少自有流量入口。社区层解决三件事：

1. **搜索引流**：技术内容被搜索收录，带来非价格驱动的自然流量；
2. **等级落地**：Pulse 等级在论坛中成为可见的社会身份，否则等级只是控制台里的一个数字；
3. **内容资产**：模型评测、故障复盘、接入教程同时是 B 端能力（Provider Health、模型情报）的对外展示面。

## 2. 三个服务，三个数据库

```text
new-api            身份 + 资金事实源
meta-pulse         贡献值 / 券 / 等级事实源
meta-pulse-forum   社区内容（Apache Answer）
```

数据库彼此独立，账号权限独立：

| 数据库 | 归属 | Pulse 的权限 |
|---|---|---|
| `new_api` | new-api | 只读（LOG_DB） |
| `meta_pulse` | Pulse | 读写 |
| `meta_pulse_forum` | Answer | 只读 |

符合 `ARCHITECTURE.md` 第 31 节「服务独立、数据库独立、权限独立」。

## 3. 选型：Apache Answer

选定 [Apache Answer](https://answer.apache.org/)（ASF 顶级项目，Apache-2.0）。

| 诉求 | Answer 的能力 |
|---|---|
| 技术栈一致 | Go + Gin + MySQL + React，与 meta-pulse 相同 |
| SEO | `internal/router/template_router.go` 提供真实服务端渲染：`/questions/:id/:title`、`/tags/:tag`、`/users/:username`、`/sitemap.xml`、`/sitemap/:page`、`/robots.txt`、`/opensearch.xml` |
| 等级展示 | `plugin/user_center.go` 的 `PersonalBranding(externalID)` 接口 |
| 中文 | 内置 `zh_CN.yaml` / `zh_TW.yaml` |

未选用的方案：

- **Discourse**：Rails + PostgreSQL + Sidekiq，引入第二套技术栈，运维最重；
- **Flarum**：2.0 长期停留在 RC，核心 SEO 依赖扩展，无限滚动不利于长帖收录；
- **NodeBB**：额外承担 MongoDB 运维。

### 不 Fork

Answer 源码不进入本仓库。仓库只维护 **UserCenter 插件 + 构建定义 + 部署编排**，Answer 本体通过官方镜像与 Go module 引入。

理由与 `ARCHITECTURE.md` 第 32 节对 `opensource-loyalty` 的处理一致：Fork 一个上游活跃项目意味着永久承担合并成本，而我们需要的只是一个插件。

### 已知成本

Go 没有动态插件机制，Answer 官方采用重编译方式集成插件。因此：

- 插件必须通过 `answer build --with <module>` 编译进二进制；
- 需要自建镜像与 CI；
- **Answer 每次升级都要重新构建镜像**。这是持续性运维负担，不是一次性成本。

### 版本约束

Answer v2.0.2 的 `go.mod` 仍声明 v1 模块路径（`github.com/apache/answer`，无 `/v2` 后缀），Go 工具链无法将其解析为 v2 模块。

**插件当前编译against v1.7.1。** 升级到 v2.x 需等待上游修正模块路径，或改用 vendor 方式。

## 4. 身份边界

论坛不拥有身份。全部身份委托给 new-api；插件的登录入口必须走 SSO Bridge，不得把浏览器直接送到一个可由参数声明用户的登录接口：

```text
Answer
  → new-api /api/forum/sso/start
  → 未登录：/login?next=/api/forum/sso/start
  → 已登录：new-api 从 session 读取用户并签发短期单次 Login Ticket
  → 302 到固定 Answer callback
  → 插件验签、原子消费 nonce、建立论坛会话
  → external_id = new-api user_id
  → Answer 调 Pulse 只读接口取等级
  → PersonalBranding 徽章渲染
```

`next` 只能指向固定 SSO 路由；callback 固定为 `https://forum.yourdomain.com/api/user-center/login/callback`（正式环境替换域名），禁止开放重定向。浏览器和 YuanHeng 的 new-api session Cookie 不得转发给 Answer；论坛固定使用独立子域，网关只向 Answer 转发其 `visit` Cookie。

### Login Ticket 验签

`LoginCallback` 由**浏览器重定向**到达，因此 query 参数在验签通过前全部是攻击者可控的。若直接信任 `user_id`，任何人访问 `?user_id=1&username=admin` 即可登录为任意用户——这正是 AGENTS.md 第 13 条禁止的情况。

new-api 签发、插件校验的 ticket。Ticket 中的用户字段只能来自 new-api 服务端 session，不能由浏览器提交后直接签名：

```text
payload   = user_id \n username \n display_name \n email \n avatar \n timestamp \n nonce
signature = hex(HMAC-SHA256(shared_secret, payload))
```

登录 Ticket 使用独立的 `PULSE_FORUM_SSO_SECRET`；论坛读取等级时则以 `forum` 角色、完整 canonical request 和 `PULSE_SERVICE_HMAC_SECRET` 调用 Pulse。两个密钥分开配置，不向浏览器暴露，也不复用。

四项约束：

| 约束 | 防御的攻击 |
|---|---|
| 字段以换行连接，且签发端/验签端都拒绝字段中的 `CR/LF` | 分隔符注入造成的字段边界歧义与签名移植 |
| TTL 2 分钟，双向校验 | 陈旧 ticket 重放；伪造的未来时间戳 |
| nonce 单次有效，验签成功后由共享 Redis `SET NX` 原子消费；Redis 不可用时 fail closed | ticket 落在浏览器历史/Referer/代理日志中被重放；多实例各自放行；以及攻击者抢先烧掉他人 nonce 的骚扰 |
| `user_id` 必须是大于 0 的规范十进制整数 | `0`、负数、前导零或带符号身份绕过 |
| secret 为空时拒绝 | 未配置时 fail closed——空密钥的 HMAC 仍是合法 HMAC |

new-api 已提供签发入口，插件使用共享 Redis 原子消费 nonce；固定 HTTPS callback 与网关 Cookie allowlist 已落地。论坛公网开放前仍须在正式域名执行端到端登录、重放和降级验收。

插件 `Description()` 中的两个关键开关：

| 开关 | 取值 | 原因 |
|---|---|---|
| `EnabledOriginalUserSystem` | `false` | 论坛无独立注册通道，浏览器无法自行声明 `user_id`（AGENTS.md 第 13 条） |
| `RankAgentEnabled` | `false` | Answer 内部 rank 同时管控投票/编辑/关帖权限。接管它等于让付费额度直接兑换社区治理权——充值大户不应自动获得删帖权 |

两套声望各自独立生长，仅在个人页并列展示。

Pulse 侧只需新增一个只读接口：

```text
GET /v1/internal/users/:user_id/profile
→ { user_id, level: { key, name }, lifetime_contribution_milli }
```

`level` 使用稳定的嵌套对象契约；插件展示 `level.name`，业务判断可使用 `level.key`。Pulse 当前不提供账户封禁事实，论坛 `UserStatus` 固定保持可用；后续如需同步封禁，应从身份事实源 new-api 单独定义契约，不得由 Pulse 推断。

**Pulse 不可用时论坛必须继续工作。** 插件的 `PersonalBranding` 在 Pulse 请求失败时降级为「无徽章」，绝不阻断登录或锁定账号。论坛登录本身只依赖 new-api SSO，不同步依赖 Pulse。

## 5. 等级系统

等级基于**终身累计净贡献**，而非单周期贡献：

```text
level = f(lifetime_net_contribution_milli)
```

周期贡献是活动指标、会归零；等级是身份，必须只涨不掉，否则用户不会在意它。

展示位置：

- new-api 控制台：等级 + 进度条；
- 论坛个人页：`PersonalBranding` 徽章；
- 论坛楼层：头衔；
- 内容奖励档位：同等质量下高等级用户档位更高（等级唯一的经济效力）。

## 6. 内容奖励

### 数据流

内容奖励与脉冲奖励**共用后半段，绝不共用前半段**：

```text
Usage Event → Contribution → Ticket → Pulse Action ─┐
                                                     ├→ Reward Grant → Outbox → Benefit
Content Candidate → 人工审核 → Content Award ────────┘
```

两条路径在 `pulse_reward_grant` 汇合，复用同一套 Budget / Settlement / Reconciliation / 状态机，因此只有一条到账路径、一套对账口径。

### 硬约束：内容不产生 contribution 或 ticket

`Contribution = Eligible Paid Usage × Contribution Multiplier` 这一定义承载了整个 margin-aware 经济基础。若发帖能产生 contribution：

- 贡献值不再对应真实毛利；
- `活动成本 / 贡献毛利` 指标失真；
- 等级退化为灌水排行榜。

因此内容奖励直接生成 Reward Grant，**跳过账本前半段**。

### 独立预算线

```text
pulse_reward_budget.budget_type = content_reward
```

内容奖励在会计上属于**市场获客成本**，不是忠诚度返还成本，必须排除在 8%–12% 毛利占比的分母之外。

### 防刷四道闸

人工审核只能判断内容质量，无法判断账号是否为规模化注册的小号。因此四道闸缺一不可：

| 闸 | 规则 | 落点 |
|---|---|---|
| 付费门槛 | 累计真实付费消耗 ≥ 门槛才有兑换资格；未达标内容仍可获荣誉，但不生成 Grant | 审核时校验 |
| 人工审核 | 运营显式 approve + 选择档位 + 填写 reason | 写 `pulse_audit_log` |
| 双层限额 | 单用户单周期上限 + 全站单日上限 | Budget 引擎 |
| 可撤销 | 内容删除/抄袭 → `settled → reversed`，走 Benefit rollback | 复用 Grant 状态机 |

第一道闸保留了「权益只来自真实付费使用」这条原则：**内容质量决定能不能拿，付费历史决定拿不拿得到。**

### 幂等

```text
action_id = content_award:{content_type}:{content_id}:{award_version}
```

稳定且可重放。`award_version` 用于「追加奖励」场景（如后期评为精华）。

### 采集方式

不用插件推送，用只读游标拉取，与现有 Usage Ingest 完全同构：

```text
meta-pulse-worker
  ├─ 只读 NEWAPI_LOG_DSN  → Usage Ingest
  └─ 只读 FORUM_DB_DSN    → Content Candidate Ingest
```

复用 `pulse_worker_cursor` 与整套 job 基础设施；Answer 无需知道 Pulse 存在；任一侧故障不影响另一侧。

审核动作在 Pulse Admin 完成（预算与审计在 Pulse 侧），Answer 只负责内容本身。

### 新增表

```text
pulse_content_candidate
pulse_content_award
pulse_content_award_limit_guard
```

`pulse_content_award_limit_guard` 只保存固定并发串行化行，不保存金额；等级由 Pulse Ledger/Account 实时派生，不维护独立等级事实表。

## 7. 博客

Answer 至今没有长文/文章类型（已核对 1.4 / 1.5 / 1.6 / 2.0 release notes）。用置顶问题模拟博客语义不对，也无法输出 `Article` 结构化数据。

因此博客独立为 VitePress 静态站：内容是营销资产、更新频率低，SSG 在 SEO、首屏与成本上均优于动态渲染。

## 8. 域名与路由

公网固定采用论坛独立子域，不再使用同 hostname 反向代理 Answer：

```text
https://yourdomain.com/                new-api 控制台
https://yourdomain.com/blog/           VitePress 静态站
https://yourdomain.com/console/pulse   Meta Pulse UI（由 new-api 提供）
https://forum.yourdomain.com/          Apache Answer
```

`yourdomain.com/forum/*` 只做 308 跳转。论坛虚拟主机采用 Cookie allowlist，只向 Answer 转发其 `visit` Cookie，即使 new-api 被误配为父域 Cookie，`session` 也不会进入论坛上游。`/api/pulse/*` 明确代理到 new-api BFF，并清除浏览器提交的 Pulse 服务签名头；部署配置中不存在 Pulse 公网上游，Compose 也不发布 Pulse/Answer 宿主机端口。Login Ticket callback query 不写入边缘 access log，并返回 `Referrer-Policy: no-referrer`。详见 `deploy/nginx/`。

## 9. 引流闭环

社区不是附属品，而是漏斗入口：

```text
搜索/分享 → 博客文章（含可直接运行的调用示例）
              ↓
          注册送额度
              ↓
          真实付费调用
              ↓
          贡献值 → 等级
              ↓
          论坛徽章/头衔 → 内容产出 → 更多搜索流量
```

## 10. 冷启动

新论坛的主要失败原因是无内容可收录。上线前需准备 30–50 篇种子内容，博客与论坛共用选题：

- 模型能力/价格横评；
- 各模型踩坑实录；
- Provider 稳定性周报；
- API 接入教程。

## 11. 依赖关系

```text
Track A｜Pulse 主线   M0 → M1 → M2 → M2.5 → M3 → M4 → M5 → M6
Track B｜博客         立即启动：VitePress + 种子内容
Track C｜论坛         立即启动：Answer 部署 + zh-CN
                          └─ UserCenter 插件（依赖 M2：需真实贡献值数据）
                               └─ 内容奖励（依赖 M4/M5：需 Reward + Settlement）
Track D｜new-api      Signed BFF → Internal Benefit API → /console/pulse
```

关键依赖点两个：

1. **UserCenter 插件依赖 M2** — 需要真实贡献值才有等级可展示；
2. **内容兑换依赖 M5** — Settlement 打通后才能真正发放额度。

在 M5 之前，**论坛以纯荣誉模式运行**（徽章、头衔、精华，不涉及 quota）。这同时是观察窗口：先看真实内容质量与灌水比例，再确定兑换档位。

## 12. 工程红线（社区层追加）

在 `ARCHITECTURE.md` 第 33 节基础上追加：

1. 论坛不得拥有独立注册通道，身份一律来自 new-api；
2. 论坛内容不得产生 contribution 或 ticket；
3. 内容奖励使用独立 budget type，不计入贡献毛利占比分母；
4. 内容奖励必须经人工审核并写入 `pulse_audit_log`；
5. Pulse 故障不得阻断论坛登录或浏览；
6. 不开启 `RankAgentEnabled`，付费等级不得兑换社区治理权限；
7. Pulse 对论坛数据库只读；
8. Answer 源码不进入本仓库。
