# Agent Note: 管理员后台跟随社区登录

Status: implemented

## Problem

社区会话按30天滑动续期，而Answer管理员独立缓存固定7天。用户可能仍登录社区、角色仍为管理员，但后台凭据缺失后返回403。用户要求取消这个单独限制。

## Decision

新增answer-admin-session.patch，在既有session-retention补丁之后应用。GetAdminUserCacheInfo以当前有效社区bearer为身份基础，再实时查询用户状态、邮箱状态及唯一管理员角色关系。普通会话不存在、过期或已撤销时不使用旧admin缓存恢复。admin专用缓存不再作为访问门槛，其兼容写入TTL与UserTokenCacheTime一致。

不把管理员权限缓存为永久授权。每次访问要求账号正常、邮箱已验证、唯一role_id=2；取消身份、封禁或异常数据库均拒绝。退出/改密码沿用现有bearer与索引撤销路径，Pulse不修改。

## Alternatives considered

将管理员TTL简单改为30天仍会在社区持续续期后出现时间错位；设置永久admin缓存会使角色变更和会话撤销难以统一。使用有效社区会话并核验当前角色，可取消独立时间门槛而保留认证边界，代价是每个后台请求增加用户与角色读取。

## Consequences

已有社区会话有效但admin缓存已过期的管理员，部署后无需等待重新签发admin缓存。普通社区登录的30天不活动期限保留；本次取消的是管理员独立7天限制，不是允许匿名或已退出账号访问后台。需要重建部署forum。

## Verification

新增时钟缓存与SQLite回归：缺失旧admin缓存、连续29天间隔使用超过87天仍可访问；普通会话到期不恢复；降权、版主、缺失/重复角色、封禁、删除、邮箱未验证均拒绝；退出、改密码、缺失bearer、数据库错误与并发退出不恢复会话。与既有续期测试一起运行通过。完整15个论坛补丁零模糊应用通过；test-session.sh纳入CI race检查。
