# Agent Note: 社区图片上传网关限制

Status: implemented

## Problem

Answer 原生粘贴、拖放、图片工具栏与外链导入都使用 `/answer/api/v1/file`。编辑器默认支持 4 MiB 图片与 8 MiB 附件，网关却继承 Nginx 默认 1 MiB，真实网关回归中 2 MiB 请求返回 413，根本未到 Answer。

## Decision

只在精确上传路径设置 9 MiB 请求体上限，为默认 8 MiB 附件与 multipart 留余量；沿用 Answer 认证、文件类型、大小、Storage 及文件记录。其他接口保持原限制。同步网关回归和能力地图。

## Existing note review

[外链图片导入笔记](../feature/2026-11-01-external-image-import.md)部分重叠：下载后复用原生上传的选择仍成立，本笔记仅负责公共上传网关限制；头像裁剪和前端打包不变。

## Alternatives considered

- 全站提高限制：配置更短，但无关写接口也扩大请求体范围。
- 自动压缩粘贴图片至 1 MiB：可减少流量，但改变原图且无法修复附件和外链导入；后端既有容量仍被网关错误限制。

## Verification

真实 Nginx 回归先在 2 MiB 请求复现 413；修复后小文件、2 MiB、8 MiB 加 multipart 余量通过，10 MiB 与普通接口 2 MiB 拒绝。会话头保留、无关 Cookie 和伪造身份头剥离通过。`make test-community`、`make build-blog build-community`、`deploy/nginx/test-config.sh`、`python3 deploy/nginx/test-seo.py` 通过。部署与网关两处静态代理入口计数同步为 14；新增入口继续剥离 Pulse 签名和外部身份。旧静态产物读取超时经重新构建消除，未跳过完整网关检查。

## Consequences

管理员自定义超过 8 MiB 文件仍需协调网关配置；发布不代表生产已部署。公网探测遇到 TLS 连接失败，不能据此宣称生产已恢复。
