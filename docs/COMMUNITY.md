# Meta Pulse 社区｜论坛、博客与账号绑定架构

本文档定义社区层边界。`docs/ARCHITECTURE.md` 仍是系统基线。

## 1. 产品定位

对外入口采用“**社区为主，Pulse 权益为辅**”：

- Apache Answer：注册、登录、问答、资料、封禁与社区关系；
- VitePress：教程、评测和搜索引流；
- Meta Pulse：真实付费调用之后的等级、券、奖励和内容权益；
- new-api：API 登录、模型调用、计费、充值和余额。

社区账号无需 new-api 即可注册和使用；绑定 new-api 只用于解锁 Pulse 等级与权益，不改变 Pulse 的经济事实源。

## 2. 服务、数据库与事实源

```text
Answer   = 社区身份、密码、会话、封禁、资料、内容事实源
new-api  = API 身份、模型调用、计费与资金事实源
绑定关系 = Answer user_external_login 中 provider=pulse_user_center 的记录
Pulse    = Contribution / Ticket / Reward / Budget / Period 事实源
```

| 数据库 | 归属 | Pulse 权限 |
|---|---|---|
| new-api LOG_DB | new-api | 仅必要日志表只读 |
| `meta_pulse` | Pulse | 读写 |
| `meta_pulse_forum` | Answer | 内容采集账号只读 |

论坛插件使用 Answer 数据库账号安装并检查绑定约束，但该 DSN 只注入 `forum` 容器，不进入 Pulse API/Worker。服务、数据库、账号和更新流程保持独立。

## 3. Apache Answer 选型

使用 Apache Answer v1.7.1：Go/Gin/MySQL/React 技术栈一致，具备 SSR/站点地图、多语言、Connector 与 UserCenter 扩展点。

不 Fork Answer。仓库只维护：

```text
services/forum/                         Answer 构建定义
services/forum-plugin/user-center-pulse Connector + Pulse 徽章插件
```

Go 无动态插件机制，因此 Answer 升级后必须重新编译并做真实 schema、登录、绑定、资料和封禁回归。v2 当前模块路径兼容问题未解决前不升级。

## 4. 双身份与可选绑定

### 4.1 账号原则

- Answer 开启本地注册、密码登录、资料维护和本地封禁；
- new-api 账号与 Answer 账号可以分别存在；
- 绑定是可选操作，不绑定也能浏览、注册、登录和发帖；
- 一个 Answer 用户最多绑定一个 new-api 用户；
- 一个 new-api 用户最多绑定一个 Answer 用户；
- 禁止静默换绑、身份转移和普通自助解绑；
- 纠错只能在备份后进入维护窗口，由授权管理员执行并留下工单、操作者、原因、前后值和时间审计。

Answer 的 `user_external_login` 是绑定事实源。插件为 `provider = 'pulse_user_center'` 增加条件生成列、单列唯一索引以及 INSERT/UPDATE/DELETE 触发器：

```text
new-api external_id  一对一  Answer user_id
```

插件启用和每次可信 callback 前都检查约束是否完整；缺列、缺索引、复合索引冒充、缺触发器或数据库不可用时绑定 fail closed。其他 Connector 不受这些条件约束影响。

### 4.2 为什么不按邮箱自动绑定

new-api 现有 Login Ticket 包含邮箱，但没有证明历史邮箱一定已经验证。Connector 因此向 Answer 返回空 Email/Avatar：

- 不按同名邮箱静默接管已有社区账号；
- 新绑定沿用 Answer 自带的邮箱确认流程；
- 社区昵称、头像和资料仍由 Answer 管理。

### 4.3 本地封禁权

插件配置：

```text
EnabledOriginalUserSystem = true
UserStatusAgentEnabled    = false
RankAgentEnabled          = false
```

new-api 或 Pulse 不覆盖 Answer 的封禁、密码、资料和治理角色。Pulse 等级只能展示，不能兑换社区管理权限。Pulse 不可用时徽章降级为空，不阻断 Answer 本地登录或浏览。

## 5. 绑定与登录链路

不修改 new-api 源码，复用其现有 SSO Bridge：

```text
Answer Connector 入口
  → 写入短期 HttpOnly/Secure/SameSite=Lax 浏览器 flow
  → new-api GET /api/forum/sso/start
  → new-api 从自己的 session 派生用户并签发短期 Login Ticket
  → 302 到社区固定 callback
  → Nginx rewrite 到 Answer Connector receiver
  → 严格解析、验签、检查 Binding Guard
  → Redis 原子消费 flow + ticket nonce
  → Answer 已绑定用户直接登录；未绑定用户走邮箱确认后创建/绑定本地账号
```

生产 new-api 只需增加配置并重建其容器：

```env
PULSE_FORUM_SSO_SECRET=<与 Answer 插件 sso_hmac_secret 一致>
PULSE_FORUM_SSO_CALLBACK_URL=https://metar.uk/api/user-center/login/callback
```

不需要把 new-api 部署到社区服务器，也不需要修改 new-api 数据库或代码。

### 5.1 Answer v1.7.1 跳转适配

插件同时实现 Connector（安全绑定）和 UserCenter（Pulse 徽章展示）。Answer v1.7.1 只要检测到 UserCenter，就会在登录页展示 UserCenter 按钮，并把全站注册链接指向框架跳转端点；其 sign-up handler 还错误读取 `LoginRedirectURL`。如果插件把这两个地址留空，浏览器会落到不存在的 `/answer/api/v1/user-center/login/` 或 `/sign-up/`。

为保持“双身份、可选绑定”：

- 插件声明 UserCenter 登录目标为 `/answer/api/v1/connector/login/pulse_user_center`；
- 插件声明注册目标为 Answer 本地 `/users/register`；
- 公网 Nginx 对 `/answer/api/v1/user-center/agent` 的公开 JSON 响应做定点归一化：`login_redirect_url` 使用相对 Connector 路径，`sign_up_redirect_url` 必须保持字面值 `/users/register`，使 Answer 前端不再重定向本地注册页；代理关闭上游压缩与缓存，且仍执行 Cookie、身份头隔离；
- 网关把框架 `/login/redirect`、`/sign-up/redirect` 以及已生成的 `/login/`、`/sign-up/` 旧链接作为兼容兜底，分别送往 Connector 和本地注册；
- 登录入口不得直接跳到 new-api `/api/forum/sso/start`，否则不会先创建 HttpOnly 浏览器 flow。

该适配只修复 Answer 框架跳转，不改变社区账号、密码、封禁和资料仍由 Answer 管理的事实源。

### 5.2 Login Ticket

```text
payload   = user_id \n username \n display_name \n email \n avatar \n timestamp \n nonce
signature = hex(HMAC-SHA256(PULSE_FORUM_SSO_SECRET, payload))
```

接收端必须：

1. 只接受 GET，限制 query 总长度；
2. 要求八个字段各出现一次，拒绝缺失、重复和额外字段；
3. 拒绝 CR/LF、非规范正整数 `user_id`/`timestamp`、过期和过早 Ticket；
4. 使用常量时间 HMAC 校验；
5. Binding Guard 正常后，才原子消费浏览器 flow 与 Ticket nonce；
6. Redis 不可用、约束不完整或状态不确定时失败，不降级为放行。

SSO 密钥与 Pulse 只读 Profile、Settlement/Worker 服务签名密钥分离且禁止复用；轮换期只允许“当前密钥 + 明确配置的上一密钥”。

### 5.3 已知剩余限制

现有 new-api Ticket 没有签名 `state`。当前浏览器 flow marker 能拒绝未从社区发起的 callback，并防止 flow/nonce 重放，但**不等价于 OAuth 的完整 signed state**。

本轮为了不改 new-api，保留该限制，并依赖短有效期、固定 callback、HttpOnly flow、单次 nonce、Answer 邮箱确认和不可转移绑定共同收敛风险。若未来允许最小 new-api 变更，应将高熵 `state` 写入 Ticket 签名并在 callback 等值校验。

## 6. 网关与 Cookie 边界

社区使用独立产品域名：

```text
https://metar.uk/       Apache Answer
https://metar.uk/blog/  VitePress
https://www.metar.uk/   跳转主域名
```

new-api 继续运行在其现有域名和服务器。社区 Nginx：

- 不代理 new-api 或 Pulse；
- 普通请求只向 Answer 转发 `visit` Cookie；
- callback 只转发短期 flow Cookie；
- callback 清除 Authorization；普通请求保留 Answer 自己的 Authorization，否则本地登录后的 API 会失效；全部路由清除 Pulse 签名头；
- HTTP/HTTPS access log 使用不含 query 与 Referer 的专用格式；callback、`auth-landing`、`confirm-email` 额外关闭 access log，并返回 `Cache-Control: no-store` 与 `Referrer-Policy: no-referrer`；
- Answer/Pulse/MySQL/Redis 容器不直接发布宿主机端口。

这保证社区故障不会进入 new-api 模型调用、计费或充值主链路，也不会把 new-api session Cookie 交给 Answer。

## 7. Pulse 等级与权益解锁

仅绑定用户可以通过稳定的 new-api `user_id` 查询 Pulse 等级。论坛插件以 `forum` 服务角色和独立 `PULSE_FORUM_HMAC_SECRET` 调用 Pulse 只读 Profile 接口；该密钥不得与 Settlement/Worker 的 `PULSE_SERVICE_HMAC_SECRET` 或 Forum SSO 的 `PULSE_FORUM_SSO_SECRET` 复用，插件配置会拒绝复用。浏览器不能声明身份。

```text
Answer external binding
  → new-api user_id
  → Pulse GET /v1/internal/users/:user_id/profile
  → PersonalBranding 展示等级/贡献值
```

Pulse 失败时返回“无徽章”，Answer 的本地账号、会话和内容仍正常。

## 8. 内容奖励

论坛内容永远不产生 contribution 或 ticket。内容奖励是独立、人工审核、可撤销的 Reward Grant：

```text
Answer 只读公开问题元数据
  → Content Candidate
  → 真实付费门槛 + 人工审核 + 双层限额 + Hard Budget
  → Reward Grant / Outbox
  → new-api Benefit（transferable_quota=false）
```

采集边界：

- 只读 Answer v1.7.1 `question` 与受保护的 `user_external_login`；
- 只采集 `show = 1` 且 `status IN (available, closed)` 的问题；
- Answer 本地 `user_id` 不能直接当作 new-api `user_id`；
- 只采集绑定存在且 `binding.created_at < question.created_at` 的内容；Answer 时间戳为秒级，同秒无法证明先绑定后发布，因此 fail closed；
- 未绑定用户、绑定前/同秒内容、隐藏/待审核/删除内容不进入候选，不追溯补奖；
- 先按原始 question ID 分页，不合格行只推进持久化游标、不创建 Candidate，防止尾部不合格内容反复扫描；
- 只读取 ID、映射后的作者 ID、标题和创建时间，不读取正文。

内容奖励使用独立 `budget_type=content_reward`，不计入贡献毛利分母。稳定动作：

```text
action_id = content_award:{content_type}:{content_id}:{award_version}
```

删除或抄袭只能沿原 Grant/source_ref rollback；禁止换 key 重发或直接改历史金额。

## 9. 博客与冷启动

VitePress 负责长文、教程和 SEO；Answer 负责问答与讨论。上线前准备模型评测、故障复盘、Provider 周报和接入教程等种子内容。博客和论坛内容本身不直接进入 Contribution/Ticket 经济账本。

## 10. 测试与上线门禁

长期保留：

- forged / expired / future / replayed Ticket；
- callback 缺失、重复、额外参数；
- flow Cookie 缺失、伪造、重放；
- Redis/Binding Guard 故障 fail closed；
- 同一 new-api ID 或 Answer ID 的并发重复绑定；
- UPDATE/DELETE/绑定时间篡改、复合索引或布尔分组削弱约束；
- 未验证 Email 不传入 Answer；
- 本地封禁不被 UserCenter 覆盖；
- Answer 本地 ID 不泄漏为 new-api ID；
- 未绑定、绑定前/同秒、隐藏、待审核内容不产生候选，连续不合格行仍推进游标；
- 全局访问日志不记录 query/Referer；callback 和敏感落地页额外关闭日志，不转发 Answer 会话/Authorization；普通 Answer API Authorization 保持可用；
- 社区网关不代理 new-api/Pulse。

真实 MySQL 回归：

```bash
FORUM_INTEGRATION_DSN='.../forum_integration?...' make test-forum-integration
```

测试会重建 Answer 测试表，因此 DSN 的 schema 名必须以 `_integration`、`-integration`、`_test` 或 `-test` 结尾；禁止指向业务库。

## 11. 工程红线

1. Answer 是社区身份事实源，new-api 是 API/资金身份事实源；
2. 论坛允许独立注册，绑定 new-api 必须可选；
3. 绑定必须一对一、不可静默换绑、不可普通解绑；
4. 浏览器 callback 参数验签前一律不可信；
5. 未绑定和绑定前内容不得产生 Pulse 内容奖励；
6. 论坛内容不得产生 contribution 或 ticket；
7. Pulse 故障不得阻断论坛本地登录或浏览；
8. Pulse 对论坛内容库只读，论坛插件的 guard DSN 不得进入 Pulse；
9. 不开启 Rank/UserStatus 代理，不让付费等级控制社区治理；
10. Answer 源码不进入本仓库。
