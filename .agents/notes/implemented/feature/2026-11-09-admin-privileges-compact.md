# Agent Note: 管理员特权页紧凑布局

Status: implemented

## Problem

Answer 1.7.1 的 `/admin/privileges` 把级别选择和 22 项声望门槛逐行堆叠，每项标签与输入框上下排列，页面右侧还保留一条空白栏。桌面上需要滚动约 1800px 才能看完一组数值，难以横向比较各项门槛。

## Decision

新增一个在其他 Answer 补丁之后应用的补丁 `services/forum/answer-admin-privileges-compact.patch`，只改 UI：

- 把 `privileges` 加入后台全宽页面列表，去掉空白右栏；
- 页面包一层 `metar-privileges`，通过 SchemaForm 原生 `field_class_name` 给级别和各门槛加类名；
- 新增 `index.scss`：级别独占一行，门槛按 `minmax(min(280px, 100%), 1fr)` 网格排列（桌面 3 栏、窄屏单栏），标签与输入同行、输入框缩窄右对齐，校验错误换行显示。

读取/保存接口、级别切换逻辑、自定义级别校验和权限均未改动。

## Alternatives considered

- 在 Answer「外观 → 自定义」里写 CSS：无需重编译 UI，但配置存在数据库、不随仓库版本走，且无法去掉右侧空栏（由布局组件按路由决定）。不采用。
- 插件注入样式：同样改不了路由级布局，还要依赖 Bootstrap 生成的 `mb-3` 等不稳定类名。不采用。

## Consequences

Docker 构建时多应用一个仅含 UI 的补丁，Answer 升级时需重新移植。已手动核对本补丁按 Dockerfile 顺序在既有补丁之后无 fuzz 应用。线上效果需管理员登录后在 `/admin/privileges` 确认。
