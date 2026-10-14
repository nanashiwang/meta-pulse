# Agent Note: Pulse 礼盒舞台与近期奖励

Status: implemented

## Problem

用户提供大幅礼盒背景、奖励卡片、双抽奖按钮和下方记录的参考图，希望优化现有抽奖界面。当前紧凑舞台较小，记录只在弹窗内，无法同时浏览最近奖励。

## Decision

复用 PulseCore 的 Canvas、Timeline、单抽/五连抽和原 Action 恢复逻辑。使用独立的静态礼盒背景，扩大舞台与卡片，右上角唯一券数入口打开原权益弹窗。下方展示本人的最近五条奖励及权益，完整编号与记录保留在原生 dialog。刷新只更新数据节点，不打断演出、关闭弹窗或丢失已确认记录。

没有公开中奖接口，暂用现有本人权益组成第二栏；不以示例用户或本人记录冒充社区中奖动态，不改变公开数据边界。装饰图不承载文案、金额或操作；无图片、无 Canvas 或演出模块加载失败时仍可使用原有操作。额度和经验继续按接口实际单位显示。

## Existing capabilities and note review

- [紧凑工具](../../implemented/simplification/2026-10-09-pulse-compact-tools.md)：部分取代隐藏所有历史、右上角小字工具的展示决策；原生弹窗、私人身份边界和固定翡翠演出继续有效。
- [紧凑历史](../../implemented/simplification/2026-10-09-pulse-compact-history.md)：部分重叠，近期历史恢复可见，不恢复概率列表与冗长说明。
- [结果恢复](../../implemented/bug-fix/2026-10-09-pulse-result-recovery.md)：继续沿用历史独立刷新、已有结果保留与原 Action 恢复。
- 当前奖励规则、五连抽和奖池预设：无关，不改变概率、单位、预算、券或结算。

## Alternatives considered

- 只改颜色与边框：范围更小，但不能呈现参考图中舞台与记录并列的层次。
- 新增公开中奖接口：更接近参考图，但需要确定公开字段、匿名化和权限，超出已有本人奖励展示，先复用本人权益。
- 将图中卡片和记录直接作为初始内容：视觉完整但会伪造奖项和中奖事实，因此未抽奖时保留原待开启核心。

## Verification

正式前端 131 项 JS、8 项构建、6 项壳层与 27 项插件 JS 回归通过；博客与社区构建通过。实际浏览器覆盖中文浅色、中文和英文深色、320/390px 窄屏：五连抽生成五个不同 Action，单抽与记录弹窗并行刷新保持焦点，历史 503 保留近期记录并不锁定按钮；不确定结果沿用原 Action 恢复。减少动画、无 Canvas、演出脚本失败的基础抽奖与完整记录入口可用。截图在 `output/playwright/pulse-redesign-five-desktop.png` 和 `output/playwright/pulse-redesign-mobile-dark-viewport.png`。只使用本地合成奖励验证抽奖，不消耗生产券。

## Consequences

礼盒背景由内置图片生成工具创建，压缩为 48 KB WebP，仅由按需加载的 Pulse 样式引用。与参考图相比，第二栏使用本人权益，公开中奖动态仍未实现；更多可见记录增加了同步节点，资格刷新与历史失败均保留既有记录，只有独立历史读取成功才替换数据。
