# Agent Note: 论坛前端资源完整性

Status: implemented

## Problem

论坛入口引用 account-menu 与 password-visibility，但 embed 清单遗漏三个文件；Turnstile 完全没有 embed 声明，vendor 后可能不再被识别为 UI 插件。线上抽样同时存在帖子无 script src、样式 href 为空和加载动画不消失；尚未证明线上实际镜像与本地遗漏的因果关系。

## Existing capabilities and impact

复用 [论坛 Dockerfile](../../../../services/forum/Dockerfile) 的 answer build 与 Go vendor 路径，补齐两个现有插件而不重写详情页。导航集成和 Turnstile 既有笔记部分重叠；本篇负责打包完整性及验证边界。

## Decision

补齐准确资源清单；构建前检查入口递归相对导入是否被嵌入；使用真实 Go embed/vendor 验证文件保留。提供部署后只读页面验收，拒绝主脚本缺失或空样式地址，不以 API 200 代替前端可用。

## Alternatives considered

- 在最终目录手动复制遗漏文件：可暂时修复，但不覆盖下次增加 import 的情况。
- embed 整个源码目录：简单，但会捎带测试与无关文件；保留明确清单并验证依赖闭包。
- 立即改写帖子为首页 SPA：可能减少导航开销，但不能修复缺失部署资源，且扩大身份与编辑器风险。

## Verification

5 项 Python 回归通过，覆盖递归 JS/CSS 引用、遗漏 package.json、删除导入文件、缺主脚本/空样式页面，以及两个插件真实 Go vendor 保留的逐字节比较。Turnstile 9 项 JS 回归通过，METAR 正式静态前端构建通过。线上目标帖子只读验收明确报告主脚本和样式缺失，说明未部署前仍不可用。

## Consequences

本地源码修复不等于线上镜像修复，生产验收独立；本机无 Docker，完整镜像构建需 GitHub CI。
