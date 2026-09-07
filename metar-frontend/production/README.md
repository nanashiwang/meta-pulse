# METAR 正式前端

此目录是 `metar-frontend/` 视觉原型对应的**生产适配层**，与原型的本地 mock 完全分离。

当前接通：

- Apache Answer 实时问题列表、详情、回答、话题和搜索；
- Answer 当前用户、个人资料、收藏、通知和 Connector 绑定状态；
- Answer 原生登录、注册、账号找回、邮箱激活、发布与写操作入口；
- VitePress 知识库入口；
- 未登录、未激活、未绑定、服务不可用及 Pulse 尚未接 BFF 等真实状态。

当前不伪造：Pulse 等级、余额、券、奖励、运营权限、工单和审核结果。它们必须在对应服务端接口完成后再开放。

构建：

```bash
python3 build.py
```

部署脚本会把产物构建到 `sites/blog/docs/.vitepress/dist/metar/`，由现有社区网关静态托管。公开配置来自 `config.production.json`，只允许同源路径或无凭据的 HTTPS 外部地址，禁止写入任何密钥。稳定资源路径强制浏览器重新验证，避免更新后首页与旧版 JS/CSS 混用。
