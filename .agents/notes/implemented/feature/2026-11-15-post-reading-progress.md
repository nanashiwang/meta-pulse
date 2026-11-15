# Agent Note: 帖子正文阅读进度

Status: implemented

## Problem

用户要求参考截图增加顶部细阅读进度线，长帖阅读时可看到当前位置。

## Decision

通过既有 Answer 插件加载 reading-progress.js，正文限定为 questionDetailPage 中标题所属容器的 article.fmt，排除回答、评论、侧栏和编辑页。3px 蓝线固定在实际 header 底边，无指针交互，不占正文布局；深色模式提高亮度。正文顶部抵达导航下方为0，正文底部进入视口为100，短正文完整可见时为100。仅代表可见阅读位置，不代表用户实际读懂、不触发任何阅读奖励或统计上报。

scroll 使用被动监听与 requestAnimationFrame 合并；ResizeObserver 跟踪正文与导航，load/字体加载/窗口变化重新计算；MutationObserver 处理 Answer SPA 进出与正文替换，缺少正文或非详情路径隐藏。属性写入只在变化时执行，避免自触发循环；progressbar 提供中英文标签和百分比，无持续动画或 aria-live 播报。

## Alternatives considered

- 按整个页面滚动高度计算：回答和评论会把正文进度稀释，不符合正文阅读用途。
- 在静态社区壳层添加：实际帖子详情属于 Answer，无法覆盖目标页面。
- 新增滚动持久记录或奖励：不属于用户要求，增加状态和权限风险。

## Verification

83项论坛前端回归通过，资源闭包检查通过。新增纯计算回归覆盖起止、短帖、无效几何、图片增长；Chromium 模拟 Answer 正文结构验证中途约51%、正文末尾100%、图片撑高后重新计算、375px无横向溢出、离开帖子隐藏。未部署生产或完成完整Docker镜像构建。

## Consequences

需要重建论坛以包含插件模块与样式；不修改文章内容、数据库或用户权益。进度随图片高度变化而调整，长帖直接打开回答锚点可能显示正文已完成，这是当前位置语义。
