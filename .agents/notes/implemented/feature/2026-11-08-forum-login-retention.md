# Agent Note: 社区网页登录保持 30 天并按访问续期

Status: implemented

## Problem

Apache Answer 1.7.1 的社区登录由浏览器 `visit` Cookie 和 Redis 中的 access/visit 会话缓存共同决定，默认两者都是 7 天。只延长 Cookie 会造成浏览器仍持有凭证、但服务端会话已过期；只改服务端 TTL 又会留下较短的浏览器 Cookie。

## Decision

在论坛镜像构建时对版本锁定的 Answer 1.7.1 应用本地补丁：将 access token、visit token 映射和 `visit` Cookie 的固定 TTL 统一为 30 天；有效会话被读取时刷新服务端缓存 TTL；成功读取当前用户信息时始终重新下发 30 天 Cookie，使活跃用户按每次有效访问续期。登录、主动退出、服务端撤销和账号状态校验仍沿用 Answer 原生边界。

## Alternatives considered

- 只在 Nginx 增加 `Max-Age=30d`：不能续期 Redis 会话，也无法覆盖 Answer 原生的 token 生命周期。
- 仅在前端定时请求自定义续期接口：会依赖脚本加载和额外路由，不能覆盖原生页面/图片鉴权，且重复实现 Answer 会话逻辑。
- 复制 Answer 到仓库长期维护：可控但扩大 Fork 边界；改为构建时从固定版本源码生成并应用小补丁，保留升级时的明确冲突点。

## Outcome

- 浏览器 `visit` Cookie、access token 缓存和 visit 映射的基础 TTL 均为 30 天。
- 读取有效 access/visit 会话后，服务端对应缓存 TTL 重新回到 30 天。
- 当前用户信息成功响应会重新下发 30 天 Cookie。
- Answer 1.7.1 补丁可无模糊应用并通过受影响包编译/测试。
- 部署离线回归和论坛插件现有检查通过。

## Risks

- Answer 升级可能改变受补丁文件的上下文，导致论坛镜像无法构建；构建时固定补丁失败应阻止发布。
- 滑动会话会让持续活跃用户长期保持登录，退出、撤销、账号状态失效和 Redis 故障仍必须保持即时失效。

## Validation

- Answer 1.7.1 源码补丁以 `--fuzz=0` 干净应用；受影响的 `internal/repo/auth`、`internal/controller`、`internal/base/middleware` 和 `internal/service/auth` 编译测试通过。
- `go test ./services/pulse/... ./services/forum-plugin/user-center-pulse/...`、`go vet ./services/forum-plugin/user-center-pulse/...`、`make test` 和 `python3 .agents/workflow.py verify` 通过。
- Dockerfile 已验证包含固定 Answer 源码替换和补丁入口；论坛镜像构建尝试因 Docker Hub 请求 `alpine:3.20` 元数据超时失败，未观察到源码编译错误。

## Consequences

升级 Answer 版本时必须重新审阅并应用该补丁；该行为不改变 Pulse 身份边界，不会让 new-api Cookie 或 Pulse 凭证进入 Answer。
