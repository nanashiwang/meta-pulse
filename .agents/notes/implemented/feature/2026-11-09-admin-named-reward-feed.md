# Agent Note: 管理员查看最近百条中奖用户名

Status: implemented

## Problem

抽奖页当前所有用户都只能看到匿名中奖动态，而且只读取近 30 天最多 20 条；社区管理员排查与运营时无法识别中奖成员，较早记录也无法查看。

## Decision

保留现有 Answer 会话和 community-bff 的匿名三字段接口，扩为全局最近 100 条。新增独立 Answer 管理员路由，按实时 Answer 管理员身份复核后，使用独立 Admin HMAC 读取带 Pulse user ID 的同一批奖励，并仅在论坛服务端将其映射为 Answer 用户名。该响应不复用普通投影，也不接受浏览器查询过滤。抽奖页以当前用户实时角色选择匿名或管理员数据，并在前后端都限制 100 条。

修改入口限于 `/pulse` 动态、Answer 管理路由、Pulse 管理员只读投影、Answer 用户绑定映射和能力文档。账本、发奖、结算及用户本人奖励历史保持不变。

## Alternatives considered

- 在同一普通接口无条件返回 username/user_id：代码最少，但突破现有所有已登录用户的匿名边界，故不用。
- 浏览器拿匿名列表再调公开用户查询：会扩大身份枚举接口并形成可拼接关联数据；改由服务端管理员路由和已验证 Answer 映射完成。
- 给管理员完整 Grant 或历史分页：可提供更多审计字段，但超过界面需求且扩大敏感数据面；只返回最近 100 条的最小奖励字段和 Pulse user ID，映射后只输出 username。

## Verification

Go race 回归覆盖独立 admin 角色、匿名白名单、管理员降权拒绝及名称映射；真实 MySQL 8.0 隔离库验证最新 100 条排序、撤销移除、管理投影及 external_id 与 Answer id 不同情况下的绑定映射。前端六项专项测试覆盖上限、恶意用户名转义、普通投影剥离名称、权限/网络失败清空管理数据及身份变化。社区完整测试、前端构建和 Pulse 构建通过；发布和生产部署分别验收。

## Consequences

管理员视图揭示用户名与中奖信息的关联，权限限制在 Answer 管理员并禁用缓存；用户名不是账本身份，不作经济或审计主键。无映射用户名时显示明确占位而不泄露 ID。普通用户仍只看到匿名信息。

## Existing notes

[匿名动态](../../implemented/feature/2026-10-15-anonymous-reward-feed.md)部分被本决定取代，普通匿名边界与独立刷新保留；[礼盒舞台](../../implemented/feature/2026-10-15-pulse-reward-stage.md)及抽奖结算规则不变。
