# Agent Note: 论坛前端资源完整性

Status: implemented

## Problem

论坛入口引用 account-menu 与 password-visibility，但 embed 清单遗漏三个文件；Turnstile 完全没有 embed 声明，vendor 后可能不再被识别为 UI 插件。线上抽样同时存在帖子无 script src、样式 href 为空和加载动画不消失；生产日志确认缺少 build/index.html；后续定位 CI 日志表明 request.ts 的 Prettier 错误使 React 构建不产出入口，但上游构建命令仍返回零。插件 embed 修复不足以修复这一独立原因。

## Existing capabilities and impact

复用 [论坛 Dockerfile](../../../../services/forum/Dockerfile) 的 answer build 与 Go vendor 路径，补齐两个现有插件而不重写详情页。导航集成和 Turnstile 既有笔记部分重叠；本篇负责打包完整性及验证边界。

## Decision

补齐准确资源清单；构建前检查入口递归相对导入是否被嵌入；使用真实 Go embed/vendor 验证文件保留。修正邮件错误 UI 补丁的 Prettier 格式；最终二进制在隔离临时目录启动安装 HTTP 服务，实际读取内嵌入口及全部首屏 JS/CSS，缺失即中断镜像构建。该检查只 GET，不连接业务数据库。提供部署后只读页面验收，拒绝主脚本缺失或空样式地址，不以 API 200 代替前端可用。

## Alternatives considered

- 在最终目录手动复制遗漏文件：可暂时修复，但不覆盖下次增加 import 的情况。
- embed 整个源码目录：简单，但会捎带测试与无关文件；保留明确清单并验证依赖闭包。
- 立即改写帖子为首页 SPA：可能减少导航开销，但不能修复缺失部署资源，且扩大身份与编辑器风险。

## Verification

6 项 Python 回归通过，覆盖递归 JS/CSS 引用、遗漏 package.json、删除导入文件、缺主脚本/空样式页面，以及两个插件真实 Go vendor 保留的逐字节比较。Turnstile 9 项 JS 回归通过，METAR 正式静态前端构建通过。线上目标帖子只读验收明确报告主脚本和样式缺失，说明未部署前仍不可用。

## Consequences

本地源码修复不等于线上镜像修复，生产验收独立。最终二进制检查覆盖缺入口和资源返回 HTML 的失败场景；邮件补丁测试、社区回归及静态构建通过。完整镜像通过远程隔离构建和 GitHub CI 验证。
