# Agent Note: 管理员用户列表提高信息密度

Status: implemented

## Problem

Answer 1.7.1 的 `/admin/users` 固定每页 10 条，没有跳页、列设置和总数，只显示名称、声望、邮箱、创建时间、状态和角色。九千多名用户时管理员需要翻几百页，也看不到账号是否已绑定 API、最近是否登录。此前插件用 CSS 计数器补序号、额外请求补总数，只能在原生表格外围加装饰，无法改分页或增加列。

## Decision

新增按顺序应用的第三个 Answer 补丁 `services/forum/answer-admin-user-list.patch`，直接改原生列表页及其接口：

- 页面：每页 10/20/50/100（URL `page_size` 优先，其次本地偏好）、跳页、「显示第 a–b 条，共 N 条」、筛选总数、列设置开关（隐藏列存本地）、紧凑模式；新增序号、ID、邮箱复制、提问/回答、API 账号、登录方式、最近登录列；状态、操作列保持原样，操作列仍是最后一列，原生弹窗与权限判断不变。
- 接口：`GetUserPageResp` 增加 `last_login_date`、`question_count`、`answer_count`；服务端将 `page_size` 限制在 100，防止构造请求一次读出整张用户表。
- 绑定列：插件新增只读 `POST /answer/admin/api/metar/accounts/bindings`，最多 100 个规范化 Answer 用户 ID，校验同源、`X-Metar-Request`、管理员会话与实时管理员身份后读 `user_external_login`；`pulse_user_center` 行返回 new-api 用户 ID，其余 provider（排除账号切换审计行）作为登录方式。页面用原生 `fetch` 调用，失败只显示「—」，不触发 Answer 全局 403/50X 错误页。
- 插件 `account-switch.js` 删除 CSS 序号与总数注入；三点菜单按行内首个 `/users/` 链接识别用户，不再依赖它位于第一列。

## Alternatives considered

- 继续由插件在原生表格上注入：无需维护 UI 补丁，但 React 拥有行与分页，插件无法改每页条数，向 React 行插入单元格会在重渲染时错位或被清除；只能做到序号、总数这类外围装饰。不采用。
- 插件自建一套用户管理页并隐藏原生页：完全可控，但要重写封禁、删除、改角色、改密码、激活等操作及其权限判断，形成第二套管理入口。不采用。
- 在 Answer 补丁里直接联查绑定表：少一次请求，但会让上游补丁依赖插件 provider 与绑定校验语义。不采用；绑定语义仍由插件持有。

## Consequences

管理员列表一次最多 100 行，绑定列额外一次查询；插件未启用或数据库不可用时列表与操作照常可用。显示 new-api 用户 ID 不改变一对一绑定、换绑或解绑规则。补丁含 `ui/` 源码，Docker 构建由 `answer build` 重新编译 UI；Answer 升级时必须重新移植三个补丁。`make test-forum-admin-user-list` 按 Dockerfile 顺序无 fuzz 应用补丁，测试分页上限并核对页面使用的 i18n 键；绑定接口有路由测试。

## Related notes

沿用[管理员账号切换](2026-11-06-admin-account-switch.md)与[手动赠券](2026-11-09-admin-ticket-grant.md)的三点菜单注入；[可选邮箱](2026-11-08-admin-create-user-without-email.md)补丁先于本补丁应用。
