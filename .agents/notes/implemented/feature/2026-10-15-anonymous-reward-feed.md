# Agent Note: 匿名社区中奖动态

Status: implemented

## Problem

用户确认本次新增后端接口，在礼盒舞台右下方展示真实社区中奖动态。现有奖励接口只适用于本人记录，不能将完整 Grant 或用户身份交给浏览器再做遮罩。

## Decision

新增独立只读投影，从 Pulse Grant 读取最近 30 天最多 20 条已到账、未撤销、正金额的 pulse 抽奖奖励。SQL 只选择类型、整数金额和中奖时间，接口将时间取整到分钟；所有行统一显示匿名用户，不读取/透传用户名、头像、用户 ID、Grant/Action ID 或稳定化名。额度换算使用当前运行配置的 quota_per_unit。

外部 `/metar/api/pulse/winners` 沿用 Answer 激活、实时状态与保护绑定校验；内部 `/v1/internal/community/rewards` 仅允许独立 community-bff 签名角色。只接受固定无参数 GET，无分页档案查询。接口 no-store，使用组合索引控制读取范围，不新增账本或发奖写入。

前端动态独立加载，主面板显示五条、查看更多打开原生弹窗显示最多二十条。可见页面每分钟刷新，隐藏或离开页面停止；失败保留原数据并明确提示，权限失效清空，迟到响应不能覆盖其他账号/语言/页面；抽奖不等待动态。我的权益仍在券数按钮的弹窗内。

## Existing capabilities and note review

- [礼盒舞台](../../implemented/feature/2026-10-15-pulse-reward-stage.md)：部分取代第二栏用本人权益临时替代的选择；舞台与本人记录保持。
- [紧凑工具](../../implemented/simplification/2026-10-09-pulse-compact-tools.md)、[结果恢复](../../implemented/bug-fix/2026-10-09-pulse-result-recovery.md)：沿用弹窗、焦点和原抽奖恢复；不加入该抽奖依赖链。
- 当前规则和经济预设：无关，不改变中奖、预算、券或结算语义。

## Alternatives considered

- BFF 读取完整 Grant 再遮罩名字：更接近参考图，但新增身份关联和意外泄漏风险；选用没有身份字段的专用 SQL 投影。
- 真名首尾字符或稳定哈希化名：便于区分参与者，但可长期关联同一用户，匿名展示不需要这类标识。
- 公网免登录接口和完整分页：传播更方便，但当前抽奖页受登录绑定保护，没有必要扩大访问与历史枚举范围。
- 在主页面加载时等待动态：实现简单，但社区动态故障会拖住抽奖，改为独立请求及错误区域。

## Verification

MySQL 8.0 与 8.4 隔离库实测迁移、状态/触发类型/期限/正金额过滤、同时间 ID 顺序及撤销后的移除。Go race 测试覆盖服务窗口和安全整数、角色拒绝、固定无参数查询、BFF 激活/绑定验证与字段剥离。前端回归覆盖预览上限、身份字段剥离、失败保留、权限清空、隐藏停止和销毁后的迟到响应。

实际浏览器验证三秒慢动态期间仍可五连抽、503 保留原记录、403 清空两个表、弹窗与刷新焦点、空结果、导航迟到响应隔离、动态脚本失败及重试恢复，以及 320/390px 中英文深色。刷新按钮用 aria-disabled 保持焦点并以 in-flight guard 防重复请求。截图为 `output/playwright/pulse-feed-desktop.png`、`output/playwright/pulse-feed-mobile-english.png`，使用本地合成数据，不代表生产抽奖。

## Consequences

匿名记录仍展示奖项和分钟级时间，因此不提供用户关联线索或公开抓取入口。全局读取新增索引迁移；页面轮询限定可见状态，数据库继续是事实源。
