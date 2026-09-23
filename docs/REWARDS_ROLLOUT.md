# 社区抽奖与 new-api 自动到账

代码已接通社区入口 `https://metar.uk/#/pulse`，新抽奖和 new-api 新到账默认关闭。部署验收并启用后，额度奖励自动发给服务端确认的一对一绑定账号，不分发普通兑换码。中奖记录和到账状态分别展示；发放延迟保留原结果，不重新抽奖。

## 开放前条件

1. 先完成 new-api 付费来源证明版本升级。该版本变更本地钱包核算：必须排空旧实例的在途请求和旧余额批量更新，再整体切换，不能新旧版本混跑。按 new-api `docs/PULSE_FUNDING_PROVENANCE.md` 验收，不给未知历史余额回填付费资格。
2. 备份 Pulse 数据库并应用 `00010`、`00011`；重建 API/Worker/Answer 插件，更新社区 Nginx。旧周期保持 legacy，不能把旧券直接用于新发奖；保留原账本与历史奖励。
3. 创建新的有奖周期，同时冻结经济规则、产券门槛、奖项、权重、预算与资金策略。用量摄入只对 new-api 已结算付费证明中的 paid 部分产券。
4. 配置两端独立签名密钥、接收端金额上限、展示换算单位；验证真实绑定账号的小额到账、重放、超时查询和受控撤销后再开放。

首版来源覆盖升级后的 Stripe/Creem/易支付真实支付回调入账，以及随后普通同步钱包结算。历史余额来源保持未知、不回填资格；赠送、手动加额和兑换码不提供付费资格；订阅、独立套餐令牌、异步任务、Realtime、强制预扣图像和未归因路径先不产券。混合来源凭证仅 paid 部分产券，未知部分不能视作已支付。Pulse 故障不进入 new-api relay 的同步网络依赖。

## 配置与最小权限

新版 METAR 可在 `/#/admin/pulse` 配置下表中 Pulse 侧的运行开关、额度换算和签名密钥；new-api 接收端仍在 new-api 后台设置。首次管理通道配对、只写密钥和角色私钥备份见 [METAR 管理员配置](METAR_ADMIN_SETTINGS.md)。网页已保存的覆盖值优先于 `.env`；以下环境变量名称同时用于识别两端应对应的字段。

| 配置 | 所在进程 | 语义 |
|---|---|---|
| `PULSE_ACTIONS_ENABLED` | Pulse API | 默认 false；只控制新抽奖，原请求恢复继续有效 |
| `PULSE_REWARD_SHADOW_MODE` | Pulse API/Worker | 默认 true；社区不允许把模拟中奖当作真实奖励 |
| `PULSE_QUOTA_PER_UNIT` | Pulse API | 必须与 new-api QuotaPerUnit 一致的正整数，未配置不能开放抽奖 |
| `PULSE_COMMUNITY_BFF_HMAC_SECRET` | Pulse API/Answer 插件 | 独立 community-bff 角色；插件字段为 `community_bff_hmac_secret` |
| `PULSE_SERVICE_HMAC_SECRET` | Pulse Worker/new-api | settlement 角色，只允许发奖与查询 |
| `PULSE_ROLLBACK_HMAC_SECRET` | Pulse 管理 API/new-api | 独立 rollback 角色，只允许查询与撤销，Worker 不得持有 |
| `PULSE_BENEFIT_ENABLED` | new-api | 默认 false；暂停时历史重放、查询和受控撤销继续可用 |
| `PULSE_BENEFIT_MAX_GRANT_QUOTA` | new-api | 新奖励单笔整数额度上限 |
| `PULSE_BENEFIT_USER_DAILY_QUOTA` | new-api | 单用户每日累计发放上限 |
| `PULSE_BENEFIT_DAILY_QUOTA` | new-api | 全站每日累计发放上限 |

每个签名角色的密钥必须不同，不能复用 SSO、forum profile、BFF、admin 或随机密钥。轮换使用相应 `_PREVIOUS` 配置。部署更新不会自动改写已有密钥；安装可生成初始独立密钥。接收端三个额度上限全部有效才允许新发奖，按 Asia/Shanghai 自然日累计毛发放额，撤销不返还每日上限。限制在数据库事务中检查，Redis 不是额度事实源。

Pulse API 不注入发奖用的 `PULSE_SERVICE_HMAC_SECRET`，仅按需配置独立撤销密钥；Worker 持有发奖密钥，不持有撤销密钥。缺少撤销配置只使受控撤销不可用，不能转用发奖密钥。

API 额度展示不声明人民币或提现价值。数值换算只用于显示；后端预算、凭证、余额都用整数记账。

## 创建奖池

也可在 METAR `/admin/pulse` 点击「设置兑换比例」，填写贡献倍率、每券贡献度、新周期时间及完整奖池配置，核对后保存。网页与下面的工具复用同一创建事务；详情见 [管理员配置](METAR_ADMIN_SETTINGS.md#配置贡献倍率与脉冲券比例)。

管理页提供「载入 1% 多级奖池方案」预设：每 5 contribution 产 1 张券，API 额度奖项权重为 8000/2000/500/100，对应 0.25/0.5/2/10 个 API 额度单位；API 额度中奖概率合计 10.6%，单张券 API 额度数学期望为 0.05 个 API 额度单位。经验奖项默认使用 60000/20000/9400 权重，对应 10/25/100 EXP。预设会根据当前 `quota_per_unit` 将 API 额度单位转换为整数 quota；保存前仍须核对实际成本、经验预算和 quota_per_unit。该预设只填充新规则表单，不会绕过管理员确认或自动开启抽奖。

准备不含凭据的 JSON 文件，例如：

```json
[
  {"key":"daily-reward","amount":50000,"weight":80},
  {"key":"lucky-reward","amount":250000,"weight":20}
]
```

这里的数字只是命令格式示例，不是经营预算建议。实际额度、权重、产券门槛和预算须按真实成本报告确定；Provider 成本尚未成为不可变事实时，不宣称预算来自精确毛利。

文件只接受 1–50 个 `{key,amount,weight}` 对象，不接受额外字段、重复字段或尾随 JSON。key 唯一且匹配 `^[a-z0-9][a-z0-9_-]{0,63}$`；amount、weight 为正整数，每项额度不得超过总预算，额度及总权重均不得超过 `2^53-1`。预算和产券阈值也必须为正整数。

```bash
./bin/meta-pulse-tool period-create \
  --key rewards-2026-01 --starts-at 2026-10-01T00:00:00+08:00 \
  --config-version rewards-paid-v1 --multiplier-bps 10000 \
  --rewards-file /secure/operations/rewards.json \
  --reward-budget 5000000 --ticket-threshold-milli 1000000 \
  --actor-id operator-name --reason '已复核成本和小额到账验收' --activate
```

带奖池的周期强制 `verified-paid-v1`，所有配置和审计在一个事务完成；Active 后数据库拒绝奖项插入/修改/删除和预算核心字段修改，预算预占/结算计数仍正常推进。剩余预算不足最大单奖时暂停整个奖池，避免只剩小奖导致公布概率失真。不会扣券后再报预算不足。

## 身份、重放与恢复

`/metar/api/pulse/*` 只代理同源 Answer 插件，不代理 Pulse 原始服务。Answer 认证且已激活用户、实时本地状态、受保护一对一绑定共同决定接收账号。POST 要求固定同源 Origin、自定义请求头、严格 JSON 和幂等键；浏览器没有指定 user_id/amount/reward/role 的能力。

浏览器保存按当前账号隔离的操作编号和幂等键以恢复丢失响应，不保存余额或中奖事实。重复点击、刷新、超时均沿用原编号；按 action_id 精确查询可跨奖励历史页恢复。收到明确未提交的券不足/预算不足后才结束该请求。无法确定到账时不补兑换码、不换 source_ref。停用活动也能返回原抽奖结果。

new-api 对禁用、删除或支付风险冻结账号拒绝新奖励。支付权益追回设置独立奖励冻结，不改变 Answer 治理角色；解除需核对付款与已发奖励并通过审计操作，不能由普通用户修改。撤销使用原 source_ref；余额不足时拒绝自动扣成负数，保留原奖励和预算，交人工核查。

## 运行验收与停用

- 独立测试库：`make test-integration`、`make test-forum-integration`；new-api 接收端另有 SQLite/MySQL/PostgreSQL 跨连接并发测试。
- 重放同一 Usage/付费凭证/Action/Benefit 各 100 次：贡献、券、Grant、到账都只能发生一次。
- 验证 DB 已提交但响应丢失、账号封禁、日限额耗尽、预算不足、不同 payload、支付撤销冻结与余额不足撤奖。
- 检查两个账本、Grant/outbox/new-api receipt、实际账户余额；页面提示、CI 或 Release 成功都不能代替真实到账证据。
- 紧急停用：先关闭 Pulse 新抽奖，必要时关闭 new-api 新发奖；查询/对账保留，已预占预算不释放、不重抽。不要直接清除凭证、幂等表或历史金额。

本轮交付代码和可复现本地测试。生产配置、真实付款与小额到账须在受控部署后逐项验收；默认开关不会因升级自动开放。
