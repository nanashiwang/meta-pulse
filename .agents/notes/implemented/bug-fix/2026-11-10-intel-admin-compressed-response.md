# Agent Note: 模型榜管理压缩响应完整性

Status: implemented

## Problem

模型榜后台已配置数据源且同步成功，管理页仍显示不可用。线上同一次 overview/sources 请求在 Intel 返回 200，而网关记录 upstream prematurely closed connection；此前只看状态码把持续传输故障误判为重启暂态。

## Existing capabilities and impact

- [管理 BFF](../../../../services/forum-plugin/user-center-pulse/intel_admin_bff.go) 已提供管理员实时校验、固定上游签名和 JSON 校验，问题集中在成功响应的 c.Data 写入。
- [图片导入笔记](../../implemented/feature/2026-11-01-external-image-import.md) 已确认同版本 Answer Brotli 与 Gin Data 的 Content-Length 冲突；复用其响应写法与真实 HTTP 测试模式。
- [模型情报提案](../../proposed/architecture/2026-11-08-model-intel-service.md) 讨论服务边界，与本次传输修复部分相关；本次不变更其架构决定。
- 影响 /admin/models 的概览、数据源、导出与管理成功响应。密钥配置、Answer 权限与 Intel 数据事实源不变。

## Decision

成功 JSON 经当前 ResponseWriter 写出，让 HTTP 服务和 Brotli 中间件根据实际字节确定 framing。保留上游 JSON 校验、状态码、Content-Type、no-store 及原有错误投影。

## Alternatives considered

- **网关关闭 Brotli 协商** — 能快速恢复，但直接访问 Answer 或其他代理仍会触发相同缺陷，且需要长期维护路由例外。
- **解析 JSON 后 c.JSON 重新编码** — 可以避免 Data 设置长度，但增加导出大 JSON 的解析分配，并可能改变数字或原始表示；现有数据已校验为合法 JSON，直接写出足够。

## Consequences

- **收益**：管理 BFF 交付经过校验的完整 JSON，不再把未压缩长度暴露给 Answer Brotli 与后续网关。
- **代价与已知上限**：需重新构建 Forum 镜像；未来若更换压缩中间件，真实 HTTP/代理测试仍是传输契约，不可退化为只检查 Recorder 状态码。

## Verification

- `go test ./services/forum-plugin/user-center-pulse/...` 覆盖 identity 与 Brotli、Answer 真实 HTTP 服务及反向代理完整读回；临时恢复旧 `c.Data` 实现时测试以 `unexpected EOF` 失败。
- 发布并重建 Forum 后，以网关与浏览器完整 JSON 响应确认生产修复；代码测试本身不代替生产验收。
