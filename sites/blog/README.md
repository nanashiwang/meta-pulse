# Meta Pulse 博客

博客以 VitePress 构建为纯静态文件，由社区 Nginx 挂载到 `/var/www/blog`：

```bash
npm ci
npm run build
```

产物位于 `docs/.vitepress/dist`。生产环境只发布静态产物，禁止对公网运行 `vitepress dev` 或 `vitepress preview`。
