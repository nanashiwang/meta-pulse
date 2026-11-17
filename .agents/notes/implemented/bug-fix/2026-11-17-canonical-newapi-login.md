# Agent Note: 连接元衡统一登录主域

Status: implemented

## Problem

线上 Connector 仍返回 meta-api.vip 的 bootstrap，元衡统一登录主域已经是 meta-api.com；不同域名无法共享浏览器会话。只改配置示例无法修复数据库中已有旧配置。

## Decision

复用 Connector、bootstrap 与签名回调，将浏览器入口中精确匹配的 HTTPS 元衡旧域名（meta-api.vip、cn.meta-api.vip、nan.meta-api.vip）统一到 https://meta-api.com。同步品牌与控制台入口，保留自定义部署地址及原始存储配置，不迁移 Cookie、身份或绑定。

## Related note audit

[社区主域迁移](../../implemented/architecture/2026-10-30-metarhub-primary-domain.md)部分重叠：其社区回调与 flow 边界仍成立，本次仅调整元衡登录起点。[社区会话保持](../../implemented/feature/2026-11-08-forum-login-retention.md)与本次原因无关，保留其 Answer 会话期限决定。

## Alternatives considered

- 仅人工修改线上插件配置：改动最小，但现存安装升级后仍可能使用旧域名，因此在浏览器 URL 生成处提供兼容。
- 强制全部配置使用 .com：简单但破坏自托管与测试地址；只精确映射已知 HTTPS 根地址，不匹配子串、路径或任意端口。

## Verification

插件全模块 Go 测试、构建和 vet 通过；入口回归覆盖三个旧域名、主域、自定义地址、相似恶意主机名、端口与路径，并检查 flow 创建、控制台/品牌/配置入口一致性及不修改原始配置。笔记校验通过。

## Consequences

升级后已有旧根地址配置直接进入主域，避免先到旧域名要求登录。需部署新版 forum 后生效。只有 .com 上有效会话可以复用，不跨域搬运旧会话，不将代码验证当作生产登录验收。自定义地址继续由运营者负责配置。
