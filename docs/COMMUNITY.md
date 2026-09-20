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

头像使用 Answer 官方外观扩展：默认设为系统头像，并安装 [`avatar-head.html`](../services/forum/customization/avatar-head.html)，将默认/失败头像显示为本地生成的用户名首字符。用户仍通过 Answer「编辑资料 → 头像 → 自定义」上传更换；不会修改头像数据库字段或关闭外部媒体保护。安装、升级与回退见[头像定制说明](../services/forum/customization/README.md)。METAR 静态首页同样支持同源上传头像和首字符回退。

### 3.1 中英文界面

原生论坛和 METAR 静态首页右上角均提供「中文 / English」。用户显式选择后，偏好保存在同源浏览器的 `metar-language`（`zh_CN` / `en_US`），刷新、切换页面、登录或退出后保留；浏览器存储禁用时当页仍可切换。静态首页默认中文，原生论坛未显式选择前沿用 Answer 账号/站点默认语言。该偏好只控制界面，帖子正文、标题、用户名和用户资料保持原文。

原生入口随现有 `pulse_user_center` 插件的 UI 模块构建，插件须启用；使用 Answer 自身的 i18next、官方中英文字典和日期库，不 Fork Answer。模块仅将用户显式选择映射到 Answer 内存中的 `user.language`，让原生界面和 API 提示一致；不写用户资料、会话或站点默认设置，不请求 new-api/Pulse。浏览器偏好优先于账号语言；用户可用右上角控件切换，其他设备不受影响。

UI 资源通过 Go embed 保留到 `go mod vendor`，官方 `answer build` 负责复制私有插件、加载与打包。更新需重建 `forum` 镜像（普通 `metar update` 即可，勿用 `--skip-forum`），无需额外粘贴外观脚本。Answer 升级时需回归 `@/i18n/init`、`loggedUserInfoStore`、`#header > .w-100`、访客/登录用户、语言异步初始化竞争及手机导航布局。

### 3.2 普通路径与列表首页

新旧页面共享 `metar-frontend/shared/routes.json` 的路由归属。Answer 插件会在导航到 METAR 页面后完整切换页面，避免点站名或登录回跳后仍停留在旧首页；编辑器阻止的导航不会触发切换。增加页面时运行 `python3 metar-frontend/shared/generate-routes.py` 同步两端策略和网关，再运行 `make test-community`。

首页 `/` 与 `/latest` 使用紧凑讨论列表，保留活跃、最新、热门、高赞和待回答筛选。`/topics` 是标签目录，`/topic/:slug` 是标签下的讨论，`/question/:id` 是问题只读详情。手机端收拢次要指标，保留标题、标签、回复和活动时间；中英文与深色主题沿用浏览器偏好。

旧 `/#/...` 分享链接由前端转换，刷新与直接访问由 Nginx 白名单处理。原生 Answer 的发帖、问题互动、账号、通知及所有 API/回调路径继续保留；收藏与通知壳层分别使用 `/me/bookmarks` 和 `/me/notifications`。完整边界见架构第 44 节。

### 3.3 统一视觉与主题

`metar-frontend/shared/theme-tokens.css` 是社区、原生 Answer 与知识库的共用配色。`theme-core.js` 管理浏览器主题，修改后运行 `python3 metar-frontend/shared/generate-theme.py` 打包到静态前端和 Go embed 插件；测试检查产物没有漂移。

三个界面共用 `_metar_theme`（`light` / `dark`），显式选择后保持到刷新、跨页面与同源其他标签；未选择时跟随系统，存储禁用时当页仍可用。VitePress 的 `auto` 值按跟随系统处理。该偏好只影响此浏览器，不写账号或站点配置；文章语言仍由内容本身决定。Answer 的异步站点/账号默认主题不能覆盖浏览器当前选择。

启用 `pulse_user_center` 插件后，原生页面使用 METAR 页头、导航和样式。新增导航采用普通链接，保留浏览器及 Answer 的离开页面保护；非编辑页面的站名和后台“返回网站”直接使用 `/latest` 的浏览器文档导航，保留 Answer 的 `beforeunload` 草稿提醒，避免先渲染旧首页再跳一次。其他原生 React 导航仍经过第 3.2 节的提交后切换。菜单和表单的权限、校验、验证码、上传、草稿、保存仍由 Answer 提供。插件关闭时 Answer 可以继续独立工作，但统一外观与导航桥不再加载。

升级 Answer/VitePress 时需回归页头结构、原生主题异步初始化、个人页和编辑器、管理侧栏、320px/390px 窄屏、英文长标签，以及知识库首屏主题脚本。完整进度见 `METAR_UNIFICATION.md`。

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

可选账号绑定复用 new-api 已有 SSO Bridge；该绑定流程本身不要求修改 new-api 业务源码。自动抽奖另需升级付费来源证明与 Benefit 接收端，见第 12 节。原站 Nginx 增加固定的同源 bootstrap 页面，解决其 `SameSite=Strict` session Cookie 在 metar.uk 跨站跳转时不发送的问题：

```nginx
location = /api/forum/sso/bootstrap {
    default_type text/html;
    add_header Cache-Control "no-store" always;
    add_header Referrer-Policy "no-referrer" always;
    add_header X-Content-Type-Options nosniff always;
    add_header X-Frame-Options DENY always;
    add_header Content-Security-Policy "default-src 'none'; base-uri 'none'; frame-ancestors 'none'" always;
    return 200 '<!doctype html><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=/api/forum/sso/start"><a href="/api/forum/sso/start">继续</a>';
}
```

该页面只在 new-api 域名下提交一次文档，再导航到固定 SSO 路径；不接收用户输入、不携带凭据、不构造开放重定向。


```text
Answer Connector 入口
  → 写入短期 HttpOnly/Secure/SameSite=Lax 浏览器 flow
  → new-api 同源 SSO bootstrap 页面
  → 同源 GET /api/forum/sso/start（携带现有 new-api session）
  → new-api 从自己的 session 派生用户并签发短期 Login Ticket
  → 302 到社区固定 callback
  → Nginx rewrite 到 Answer Connector receiver
  → 严格解析、验签、检查 Binding Guard
  → Redis 原子消费 flow + ticket nonce
  → Answer 已绑定用户直接登录；未绑定用户走邮箱确认后创建/绑定本地账号
```

仅开通可选账号绑定时，new-api 增加以下配置并重建其容器：

```env
PULSE_FORUM_SSO_SECRET=<与 Answer 插件 sso_hmac_secret 一致>
PULSE_FORUM_SSO_CALLBACK_URL=https://metar.uk/api/user-center/login/callback
```

new-api 继续在原服务器独立运行。以上是 SSO 绑定的配置要求，不包含自动奖励所需的钱包核算迁移、接收端升级和额度上限。

### 5.1 Answer v1.7.1 跳转适配

插件同时实现 Connector（安全绑定）和 UserCenter（Pulse 徽章展示）。Answer v1.7.1 只要检测到 UserCenter，就会在登录页展示 UserCenter 按钮，并把全站注册链接指向框架跳转端点；其 sign-up handler 还错误读取 `LoginRedirectURL`。如果插件把这两个地址留空，浏览器会落到不存在的 `/answer/api/v1/user-center/login/` 或 `/sign-up/`。

为保持“双身份、可选绑定”：

- 插件声明 UserCenter 登录目标为 `/answer/api/v1/connector/login/pulse_user_center`；
- 插件声明注册目标为 Answer 本地 `/users/register`；
- 公网 Nginx 对 `/answer/api/v1/user-center/agent` 的公开 JSON 响应做定点归一化：`login_redirect_url` 使用相对 Connector 路径，`sign_up_redirect_url` 必须保持字面值 `/users/register`，使 Answer 前端不再重定向本地注册页；代理关闭上游压缩与缓存，且仍执行 Cookie、身份头隔离；
- 网关把框架 `/login/redirect`、`/sign-up/redirect` 以及已生成的 `/login/`、`/sign-up/` 旧链接作为兼容兜底，分别送往 Connector 和本地注册；
- 登录入口不得直接跳到 new-api `/api/forum/sso/start`，必须先经过 `/api/forum/sso/bootstrap`，否则浏览器可能因 `SameSite=Strict` 不发送已有 new-api session。

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

当前 SSO Ticket 契约仍保留该限制；自动奖励升级没有修改此契约。短有效期、固定 callback、HttpOnly flow、单次 nonce、Answer 邮箱确认和不可转移绑定共同收敛风险。后续 SSO 专项升级仍需将高熵 `state` 写入 Ticket 签名并在 callback 等值校验。

## 6. 网关与 Cookie 边界

社区使用独立产品域名：

```text
https://metar.uk/       METAR 静态首页；其他社区原生/API 路径由 Answer 提供
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


## 12. 社区抽奖入口

METAR `/pulse` 通过同源 `/metar/api/pulse/*` 访问 Answer 认证插件，后者从原生会话、实时账号状态和保护绑定派生 new-api 身份，使用独立 community-bff 签名。普通 forum profile 密钥仍只读等级，不获得抽奖权限。社区 BFF 代码已接通；插件未配置独立密钥时权益接口不可用，`PULSE_ACTIONS_ENABLED` 与 new-api 新发奖开关默认关闭。插件扩展采用 Answer 的认证路由，禁止绕过原生认证或查询字符串 token。缺配置和 Pulse 故障只影响权益页，不阻断社区登录/浏览。

页面展示本人券、奖池权重、中奖与到账记录。浏览器只提交随机操作编号和幂等键；同源写请求防跨站，前后端均不允许自报 user_id/amount/reward。丢失响应时查询原 action_id 或用原 key 重试，不重新抽奖。首版只认升级后受支持在线支付和普通同步钱包结算凭证中的 `paid_quota`。旧余额、赠送、兑换码以及未覆盖的计费路径不自动产券；不会从历史余额反推付费资格。具体支持范围、配置、发奖与撤销隔离见 [REWARDS_ROLLOUT.md](REWARDS_ROLLOUT.md)。

## 13. 社区管理员运行配置

METAR `/admin/pulse` 是独立于用户权益的管理员配置入口。只允许 Answer 当前真实管理员，实时复核 `user`、`user_role_rel`、激活与封禁；已有管理员 token 不能绕过降权。不要求 new-api 绑定，也不会将 Answer ID 当作发奖受益人。

首次在原生插件后台配置独立用途的 `admin_hmac_secret`，对应 Pulse 已有运营密钥。后续管理请求固定签名为 admin，公网别名仅代理 Answer 插件的两个固定 settings/secret 路径，不直接代理 Pulse。开关和密钥配置通过版本 CAS、幂等与审计保存；原有用户 BFF、Profile 与 SSO 权限不扩张。管理配置故障不影响本地登录/浏览。

新设置页不回显既有秘密，不在浏览器存储秘密或草稿。SSO、邮件、站点网址仍在 Answer 原生管理页配置；资金接收上限仍在 new-api。首次配对、角色私钥备份及生效规则见 [METAR 管理员配置](METAR_ADMIN_SETTINGS.md)。

## 14. 公开内容搜索收录

公开入口提供初始 HTML 与普通链接，论坛帖子继续使用 Answer 服务端正文与动态站点地图，博客站点地图保留 `/blog/` 前缀。私人入口通过网关及前端 noindex 排除；不改变认证、发布可见性或内容事实源。网站检查、Google/百度/Bing 提交及生产验收见 [SEARCH_INDEXING.md](SEARCH_INDEXING.md)。

### 统一后的内容入口

METAR 列表、搜索与收藏直接打开已应用共享主题的 Answer 详情，搜索回答保留回答定位。旧 `/question/:id` 与 `/me/notifications` 页面只做兼容跳转，查询参数与锚点保留；不再展示功能不全的重复只读副本。公开资料由 `/users/:username` 承接，私人身份与绑定仍由当前 Answer 会话校验。收藏列表支持分页，通知使用原生完整中心；互动数据和权限判断仍全部归 Answer。

### 本地注册兼容

Answer 1.7.1 的 `/user-center/agent` 固定输出插件注册跳转接口，前端 `getSignUpUrl` 即使启用本地用户系统仍选择它，注册守卫会离开本地表单。`local-registration.js` 仅修正 Meta Pulse 自己启用且保留本地账号时的前端注册路径，并同步后续插件信息刷新。原生注册开关、验证码、邮箱验证和服务端注册接口不变；其他 UserCenter 插件和禁用本地账号的模式不受影响。升级 Answer 时必须重新核验这条适配。

中英文插件 YAML 必须随 `i18n` Go 子包嵌入，才能由 `answer build` 的 vendoring/合并步骤保留。已有数据卷中的语言文件不会由 `answer init` 覆盖；升级时按 Answer 原生升级流程，在备份后运行 `answer upgrade -C <实际数据根目录>` 刷新语言并执行原生迁移，再重启 forum。不能把镜像构建成功等同于既有卷内语言资源已更新，数据根目录需以现有部署配置为准。

### 通知页导航一致性

通知中心继续位于 `/users/notifications/inbox`，成就位于 `/users/notifications/achievement`；这是同一社区的原生功能页，不是外站。插件在这两类页面为桌面侧栏与手机抽屉提供 METAR 社区分组导航，并将通知中心标记为当前页。筛选、分页、已读操作和权限仍由 Answer 负责。管理/审核入口只在 Answer 自身已渲染对应入口时展示；离开通知页后恢复原生侧栏，不改动管理后台菜单。

富文本编辑器/多行编辑表单中的站名点击保留原生 React 路由确认；用户确认后才由既有导航桥交接。直接首页导航仅用于非编辑页面。

## 社区成长等级

社区另设独立的累计经验和签到体系，未绑定成员同样可以参与；与本节之前的 Pulse 付费贡献等级、Answer 声望分别展示。等级门槛、资格、防刷、装扮和运营规则见 [社区经验与成长等级](COMMUNITY_EXPERIENCE.md)。入口为 `/me/growth`，管理员入口为 `/admin/growth`。
