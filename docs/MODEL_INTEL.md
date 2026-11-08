# 模型情报模块（Model Intel）设计

日期：2026-11-08。状态：**M1 部分实现，未部署**（见文末「M1 实现状态」）。决策记录见 [proposed note](../.agents/notes/proposed/architecture/2026-11-08-model-intel-service.md)。

模型情报是 METAR 社区内的公开 AI 模型榜单与数据中心：综合排行、按用途榜单、模型详情、价格对比、评测、速度、模型动态，并附带社区讨论。产品形态对标 llm-stats.com 一类的 AI 榜单站，**功能与信息架构对标，视觉、文案、评分方法与数据均为自有**：

- 不复制对方品牌、页面设计、文案、FAQ 与综合分名称；页面沿用 METAR 生产前端的视觉体系。
- 不抓取或转载对方汇总的数据；数据来自厂商官方公开信息、评测原始发布方（论文/官方榜单，需核对许可）与本站自测。
- 每个数值标注来源与更新时间；综合分为本站方法，公开计算口径与版本。

命名用 `intel` 而非 `pricing` / `leaderboard`，以容纳后续评测、竞技场、Provider 可用性和智能路由数据。

## 系统边界

```text
new-api   = 模型调用与资金事实源（含元衡自身售价）
Answer    = 社区身份与内容事实源
Pulse     = 社区任务与增长权益系统
Intel     = 模型目录、评测、价格、速度等公开数据的采集、版本化、评分与查询
```

硬原则：

1. **Intel 故障不得影响论坛登录/浏览、Pulse 权益或 new-api 调用与计费。** 榜单页只显示不可用状态。
2. Intel 对 new-api 只读：读取公开价格作为「元衡售价」来源；速度测量作为普通 API 客户端用专用测量 Key 调用，不进入 relay 链路、不读写余额。
3. Intel 不产生 Contribution / Ticket / Reward。
4. 金额、分数禁止 float 存储：金额用整数微单位，分数/百分比用整数基点或定点。
5. 观测数据（价格、评测结果、速度样本、综合分）只追加；纠错通过新记录 + 修正审计完成。
6. 浏览器不能自报 user_id；管理员身份由 Answer 实时校验后经签名 BFF 传入。

## 放置位置

新建独立服务 `services/intel`，加入 `go.work`，与 `services/pulse` 同技术栈（Go / Gin / GORM / MySQL / Redis）。

| 候选位置 | 结论 |
|---|---|
| 论坛插件（集市所在） | 否。集市的事实源是 Answer 帖子；榜单是外部数据 + 定时采集 + 评分计算，放进 Answer 进程会让故障与升级牵连论坛（AGENTS.md 不变量 21），且每次改动需重建 forum 镜像。 |
| `services/pulse` | 否。Pulse 带账本/预算不变量，混入采集评分会扩大故障面。 |
| new-api | 否。跨厂商榜单与历史不属于资金事实源，也不应让新功能进入调用主服务。 |
| 独立服务 `services/intel` | **采用。** 故障隔离、独立部署；榜单、价格、评测、速度共享同一模型目录。 |

## 第一个模块：AI 榜单首页（M1）

### 页面与路由

模块根路径为 `/models`，详情使用单数前缀，与现有 `question/:id`、`marketplace/item/:id` 的 detailPrefixes 约定一致，避免 slug 与子页面冲突。

| 路径 | 内容 | 期 |
|---|---|---|
| `/models` | 榜单首页 | M1 |
| `/models/leaderboard` | 完整综合榜（全部模型、筛选） | M1 |
| `/model/:slug` | 模型详情 | M1（基础版） |
| `/models/methodology` | 综合分计算口径与版本 | M1 |
| `/models/open` | 开放权重榜 | M2 |
| `/models/best/:usecase` | 按用途榜单（编程、写作、数学、研究、长上下文、工具调用、推理） | M2 |
| `/models/compare` | 多模型对比 | M2 |
| `/models/pricing` | 价格对比 | M2 |
| `/models/benchmarks`、`/models/benchmark/:slug` | 评测目录与单项评测榜 | M2 |
| `/models/updates` | 模型发布/改价/弃用动态 | M2 |
| `/models/provider/:slug` | 厂商页 | M3 |
| `/admin/models` | 管理后台 | M1 |

### 首页结构（M1）

自上而下：

1. **榜单子导航**：综合榜 / 开放权重榜 / 按用途榜单入口。M1 未上线的入口不显示，不放空链接。
2. **标题区**：一句话说明收录范围与「数据更新于」时间，链接完整榜单与计算口径。
3. **各维度领先卡片（6 张）**：推理最强、编程最强、前十中最便宜、输出最快、上下文最长、开放权重最强。每张卡显示维度、模型、单一指标值和来源，链接 `/model/:slug`。卡片由同一份综合榜快照计算，不单独手工维护。
4. **模态切换**：LLM（M1 唯一启用）；图像/视频生成、TTS、STT、Embedding 在数据就绪前不显示。
5. **榜单预览表（前 15 名，分页）**：
   - 列：排名、模型（图标、名称、NEW/未发布标记）、厂商、综合分、推理、编程、Agent、上下文长度、输出速度（tok/s）、混合价格（$/1M tokens）、许可（专有/开放权重及许可证名）。
   - 时间窗口：全部 / 近 90 天 / 近 30 天发布。
   - 价格口径切换：混合价（默认 3:1 输入:输出，口径写入方法页）/ 输入 / 输出。
   - 缺失值显示「—」，不补零、不估算；悬浮显示来源与测量时间。
   - 分页 15 行/页，URL 带查询参数便于分享与 SEO。
6. **侧栏**：近期新模型（15 天内，链接动态页）、综合分说明、榜单导航卡。
7. **常见问题**：自写问答（当前第一、最便宜的前十模型、最长上下文、最快输出、最强开放模型、综合分口径），答案由快照数据动态生成，同时输出结构化数据供 SEO。
8. **社区讨论入口**：链接本模块的 Answer 讨论类别与热门讨论（替代对方的邮件订阅，不收集邮箱）。

页头搜索沿用现有 ⌘K 搜索，增加「模型/厂商」结果分组（M2）。

### 综合分（本站方法）

- 输入：推理、编程、Agent 三个维度的公开评测结果；各维度权重与评测集合构成一个**方法版本**。v1 只衡量能力，价格与速度单独成列、不计入综合分，避免用不透明的权衡把便宜模型排到更强模型前面。
- v1 权重：推理 40、编程 35、Agent 25；覆盖权重不足 60% 不计分。
- 计算：每个评测在全榜内 min-max 标准化到 0–10000 基点（越低越好则反转）；只有一个模型报告或全部相同的评测无排名信息，跳过；维度内取平均，维度间按权重加权；缺失维度不以 0 计，记录覆盖率，覆盖率不足的模型标记「数据不足」并排在有分模型之后；同分并列（1、1、3）。全程整数运算，可复现。
- 输出：`intel_score_snapshot` 按方法版本 + 计算时间追加；页面始终展示最新快照，方法页列出版本、权重、评测清单与变更记录。
- 方法调整只能新增版本，不改写历史快照。

## 服务结构

```text
services/intel/
├── cmd/
│   ├── api/          公开只读 API + 内部管理 API
│   ├── worker/       采集、归一化、速度测量、评分计算、变更检测
│   └── tool/         回填、重放采集、重算评分、数据核对
├── internal/
│   ├── app/
│   ├── config/
│   ├── domain/
│   │   ├── catalog/    Provider / Model / Alias / Offering
│   │   ├── price/      价格快照、单位换算、混合价
│   │   ├── benchmark/  评测定义与结果
│   │   ├── speed/      速度样本与窗口汇总
│   │   ├── score/      综合分方法版本与计算
│   │   └── money/      整数定点金额（可从 pulse 抽出共享）
│   ├── source/       数据源适配器，每个来源一个实现
│   ├── ports/
│   ├── store/mysql/
│   ├── store/redis/  公开查询缓存
│   ├── transport/http/
│   ├── security/     内部签名校验（复用 pulse 的 HMAC 方案）
│   └── observability/
└── migrations/       Goose SQL
```

### 数据模型

「目录 + 只追加观测 + 派生快照」三层：

| 表 | 层 | 说明 |
|---|---|---|
| `intel_provider` | 目录 | 厂商/云平台/聚合平台/元衡自身；名称、图标、官网 |
| `intel_model` | 目录 | 稳定 slug、厂商、家族、模态、发布日期、状态（未发布/可用/弃用）、上下文长度、最大输出、许可类型与许可证、是否开放权重 |
| `intel_model_alias` | 目录 | 各来源命名 → 规范模型；人工确认后生效 |
| `intel_offering` | 目录 | Provider × Model 售卖条目（区域、batch/priority 档位） |
| `intel_benchmark` | 目录 | 评测定义：slug、维度（推理/编程/Agent…）、指标、方向、原始来源与许可 |
| `intel_price_snapshot` | 观测 | offering、价格维度（输入/输出/缓存读写/按次）、单位、整数微单位金额、币种、生效时间、来源、抓取时间、内容指纹 |
| `intel_benchmark_result` | 观测 | 模型 × 评测：定点得分、设置（shots、工具等）、来源 URL、发布日期、是否自测 |
| `intel_speed_sample` | 观测 | offering、输出 tok/s、首 token 延迟、成功与否、测量时间 |
| `intel_score_method` | 派生 | 方法版本：权重、评测集合、标准化方式、生效时间 |
| `intel_score_snapshot` | 派生 | 方法版本 × 计算批次 × 模型：综合分、各维度分、覆盖率 |
| `intel_correction` | 审计 | 被修正记录、新记录、原因、操作人 |
| `intel_source_run` | 运维 | 每次采集/测量/计算的状态、耗时、条数、错误码 |
| `intel_discussion_link` | 关联 | 规范模型 ↔ Answer 原帖 ID |

首页与榜单读取派生快照与最新观测的聚合视图，结果写入 Redis，按快照批次失效，不在请求路径上实时计算评分。

### 数据来源

| 数据 | 来源 | 说明 |
|---|---|---|
| 模型目录、上下文、许可、发布日期 | 厂商官方文档/模型卡、Hugging Face 模型卡 | 首期可人工录入 + 管理后台维护 |
| 价格 | 厂商官方价格页/API；元衡售价来自 new-api 只读价格信息 | 逐个来源核对条款与 robots |
| 评测结果 | 评测原始发布方（论文、官方榜单、厂商技术报告） | 记录来源 URL 与设置；仅使用许可允许的数据 |
| 速度 | 本站自测：worker 通过各 Provider API 用固定提示词定时测量 | 测量 Key 独立于用户；预算与频率可配置 |

不使用其他榜单站汇总的数据作为来源。

采集规则：

- `Source` 接口：`Fetch(ctx) → []RawObservation`；单个来源失败不影响其他来源。
- 内容指纹相同不写新记录；变化则追加并生成模型动态事件（M2 展示）。
- 失败时保留最后一次成功数据并展示该来源的「更新于」，不以空值覆盖。
- 未匹配的命名进入别名待审队列，不自动合并。

## 接口与路由

| 路径 | 走向 | 说明 |
|---|---|---|
| `/metar/api/intel/v1/home`、`/leaderboard`、`/models/:slug`、`/methodology` | nginx → `intel-api` | 匿名只读，Redis + HTTP 缓存，无 Cookie；M2 增加 compare/pricing/benchmarks/updates |
| `/metar/api/admin/intel/*` | nginx → forum 插件 admin BFF → `intel-api` 内部接口 | 复用 [admin_bff.go](../services/forum-plugin/user-center-pulse/admin_bff.go) 模式：Answer 实时管理员校验，HMAC 签名转发 |

管理功能（M1）：模型/厂商录入与编辑、别名审核、价格与评测结果录入/修正、来源启停与手动触发、评分方法版本发布、关联讨论帖。

## 前端

在 [production](../metar-frontend/production/) 按集市方式接入：

- 新增 `src/models.js`（首页、榜单、详情、方法页）与对应测试；表格与卡片沿用现有样式变量、深浅主题与移动布局（窄屏榜单横向滚动，固定排名与模型列）。
- 更新 [shared/routes.json](../metar-frontend/shared/routes.json)（pages 与 detailPrefixes 增加 `model`）、[route-policy.js](../metar-frontend/production/src/route-policy.js)、[nginx 页面白名单](../deploy/nginx/meta-pulse.conf) 与 SEO 页面配置。
- 左侧类别固定入口与顶部导航入口；管理员头像菜单入口；i18n 中英文。
- 模型详情的评论、点赞、收藏复用关联 Answer 原帖，不新建互动系统。

## 部署

- `docker-compose.yml` 新增 `intel-api`、`intel-worker`；MySQL 可共用实例，但使用独立数据库 `metar_intel` 与独立账号。
- 纳入 `deploy/update.sh` 迁移、健康检查与备份；Makefile 增加 `build-intel`、`test-intel`、`test-intel-integration`。
- Intel 停止时 nginx 对公开 API 返回 503，前端展示不可用状态；论坛与 Pulse 不受影响。

## 分期

1. **M1 榜单首页**：目录与管理后台、价格/评测/速度三类观测、综合分方法 v1、`/models` 首页、完整综合榜、模型详情基础版、方法页。
2. **M2 榜单扩展**：开放权重榜、按用途榜单、对比、价格对比、评测目录与单项榜、模型动态、搜索分组、讨论帖关联。
3. **M3 多模态与厂商**：图像/视频/TTS/STT/Embedding 榜单、厂商页、Provider 可用性。
4. **M4 数据输出**：开放数据 API、为 new-api 智能路由/FinOps 提供只读数据（另立项，需单独评审边界）。

## 待确认

- M1 收录的模型范围（数量、厂商）与冷启动方式（人工录入为主还是先接自动来源）。
- 综合分 v1 的评测集合与权重。
- 速度自测的 Provider 范围、频率和每月测量预算。
- 价格展示币种（USD/CNY）与汇率来源；是否展示元衡售价。
- 是否允许用户提交数据纠错（若允许，走审核队列）。

## M1 实现状态（2026-11-08）

已实现，本地验证通过（未部署、未接真实数据库）：

| 部分 | 位置 | 验证 |
|---|---|---|
| 定点金额、综合分 v1、导入校验、榜单快照/筛选/排序/分页/领先卡片 | [services/intel/internal](../services/intel/internal/) | `go test ./services/intel/...` |
| MySQL 存储与迁移（目录 upsert、观测按指纹 `INSERT IGNORE`、快照按 方法×指纹 去重） | [store/mysql](../services/intel/internal/store/mysql/store.go)、[migrations](../services/intel/migrations/) | **未在 MySQL 上运行**；需 Docker 后补集成测试 |
| 只读公开 API（home / leaderboard / models/:slug / methodology） | [transport/http](../services/intel/internal/transport/http/public.go) | 路由、参数白名单、错误映射、缓存头单测 |
| `metar-intel-tool`：`migrate up\|status`、`import [--dry-run]`、`score`；worker 定时重算 | [cmd](../services/intel/cmd/) | dry-run 用[虚构示例](../services/intel/testdata/example-import.json)验证 |
| 前端 `/models`、`/models/leaderboard`、`/model/:slug`、`/models/methodology`，顶部导航「模型榜」 | [models.js](../metar-frontend/production/src/models.js) | `make test-community`；本地预览检查桌面/手机、深浅色 |
| 网关 `/metar/api/intel/` 只放行 GET/HEAD 与四个公开路径；compose `--profile intel` 三个服务 | [nginx](../deploy/nginx/meta-pulse.conf)、[compose](../docker-compose.yml) | `make deploy-test` 通过；`deploy-config-test` / `nginx -t` **未运行**（本机无 Docker） |

与上文设计的差异：

- **不用 Redis**：API 进程内缓存最新快照（`INTEL_CACHE_TTL`，默认 30s），刷新失败继续返回上一份；网关与浏览器按 `max-age=60` 缓存。
- **独立 MySQL 实例**：与论坛一致使用 `intel-mysql` 容器和 `intel` 账号，而非共用实例。
- **数据库访问**用 `database/sql` 显式 SQL（窗口函数取最新观测），未引入 GORM。
- **纠错**：同一生效时间的新记录以更大 id 覆盖展示，暂未单独建 `intel_correction` 表；导入记录在 `intel_import_run`。
- **按配置启用**：`.env` 填写真实 `INTEL_DB_PASSWORD` 后，`metar update` 自动带上 `intel` profile：更新前备份 `metar_intel`（`intel.sql`），代码快进后构建 `intel-api`/`intel-worker`、启动 `intel-mysql`、执行前向迁移、重建并检查健康。模型榜任一步失败只告警，论坛与 Pulse 照常完成更新；已有 `intel-mysql` 无法备份时本次跳过模型榜。未填写时更新流程不触碰 Intel。

管理后台（2026-11-08 补充）：

- `/admin/models`：概览计数与当前榜单、最近 20 次导入（含操作人 Answer ID）、上传/粘贴 JSON → 校验 → 导入、导出当前数据（可修改后重新导入）、立即重算。
- 链路：浏览器 → `/answer/admin/api/metar/intel/{overview,export,validate,import,recompute}`（论坛插件 [intel_admin_bff.go](../services/forum-plugin/user-center-pulse/intel_admin_bff.go)，复用 Answer 管理员会话 + 数据库实时角色核对 + 同源检查）→ HMAC 签名 → `intel-api` 的 `/v1/internal/admin/*`。网关不转发 Intel 内部路径。
- 签名格式与 Pulse 内部契约一致（方法、路径、用户、角色、时间、nonce、正文摘要），头为 `X-Intel-*`，仅接受 `admin` 角色，±2 分钟窗口，nonce 存 `intel_admin_nonce` 防重放；支持 `INTEL_ADMIN_HMAC_SECRET_PREVIOUS` 轮换。未配置密钥时 Intel 不注册任何管理路由。
- 导入需 UUID `Idempotency-Key`，与正文摘要一起记入 `intel_import_run`；同键同内容返回原结果，同键不同内容 409。浏览器按管理员暂存未确认的导入，重载后可「重试原操作」。
- 浏览器导入上限 1 MiB（与网关论坛位置的默认请求体上限一致），更大的文件用 `metar-intel-tool`。
- 配置：`INTEL_ADMIN_HMAC_SECRET`（≥32 字符）同时注入 `intel-api` 与 `forum`（后者读作 `METAR_INTEL_ADMIN_HMAC_SECRET`，地址 `METAR_INTEL_ADMIN_URL` 默认 `http://intel-api:8090`）。

尚未实现（M1 剩余）：

- 逐条表单编辑与别名审核（当前通过导出 → 修改 → 导入完成编辑）。
- 速度自测采集器（当前速度样本只能导入）。
- `intel_discussion_link` 与模型详情关联原帖（当前为发帖/搜索入口）。
- MySQL 集成测试（`make test-intel-integration`）、`update.sh` 接入与生产验收。

### 本地运行

```sh
docker compose --profile intel up -d intel-mysql
docker compose --profile intel run --rm --entrypoint metar-intel-tool intel-api migrate up
docker compose --profile intel run --rm -v "$PWD/data:/data:ro" --entrypoint metar-intel-tool intel-api import /data/models.json
docker compose --profile intel up -d intel-api intel-worker
```

导入文件格式见[示例](../services/intel/testdata/example-import.json)（虚构数据，禁止导入生产）。价格写美元/百万 token 的十进制字符串，评测 `percent` 单位最多两位小数，第三方评测结果必须带 `source_url`。
