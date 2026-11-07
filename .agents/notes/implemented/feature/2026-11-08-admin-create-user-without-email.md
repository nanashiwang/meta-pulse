# Agent Note: 管理员创建用户时邮箱可选

Status: implemented

## Problem

Answer 1.7.1 的 `/admin/users` 批量创建表单按“姓名、邮箱、密码”输入，服务端将邮箱设为必填。社区账号与 API 账号独立，管理员创建社区账号时不应被迫提供邮箱；空邮箱也不应伪造成外部可用地址。

## Decision

复用 Answer 原生批量用户表单和创建服务。`AddUserReq.Email` 允许为空；空邮箱跳过格式、批内重复和数据库重复查询，仍要求姓名与密码。真实邮箱继续校验格式与重复。无邮箱沿用 Answer 原生管理员创建流程和可用状态，数据库使用空字符串，插件不增加建号 API。

## Alternatives considered

- 插件新增用户管理 API：会复制 Answer 的账号创建、密码散列、用户名和权限逻辑，形成另一套事实入口；不采用。
- 为无邮箱账号生成伪邮箱：会造成伪造联系地址、重置密码和邮箱身份歧义；不采用。
- 仅放宽前端表单：Answer 服务端仍拒绝空邮箱，提交必然失败；不采用。

## Testing

`make test-forum-optional-email` 覆盖两个空邮箱的批量解析/校验以及非空无效邮箱拒绝；`make test-forum-session`、完整 `make test`、`make vet`、`make build-forum` 与笔记校验均通过。最终镜像构建无 fuzz 顺序应用会话与可选邮箱补丁。

## Consequences

无邮箱账号不会显示成未激活，但 Answer 密码登录以邮箱为登录名，因此该账号不能通过邮箱密码登录或邮件找回；启用管理员账号切换时管理员可切换到该账号。需要用户自主邮箱登录的账号仍须提供真实邮箱。Answer 升级后必须重新审阅并移植补丁。

## Related notes

沿用[管理员账号切换](2026-11-06-admin-account-switch.md)记录的 `/admin/users` 原生用户管理入口；该笔记仍拥有账号切换流程，本记录只定义邮箱可选语义。
