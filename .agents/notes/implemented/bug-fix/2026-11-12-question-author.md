# Agent Note: 帖子列表显示发布者

Status: implemented

## Problem

最近活跃列表的 operator 是最近编辑/回答者，前端在 user_info 缺失时用其作为作者，导致头像、昵称、个人链接和等级显示成他人。

## Decision

扩展 Answer 的 QuestionPageResp，独立添加公开 user_info，作者 ID 仅用于服务端装配且不序列化。FormatQuestionsPage 复用已有作者 ID 批量用户查询，无新增逐帖请求。operator 原语义与排序不变，前端仅在缺少 user_info 且 operation_type 明确为 asked 时，使用原始发布事件的 operator 兼容旧接口；modified/answered 等活动者绝不作为作者。个人帖子和收藏的既有 user_info 保留使用。此决定扩展社区导航集成的列表身份显示，不改变 Answer 身份事实源。

## Alternatives considered

- 前端逐帖读取详情：可获得作者，但每页新增大量请求，放大当前延迟。
- 覆盖 operator 为发布者：改动少，但破坏 Answer 原生最近活动语义。

## Verification

3 项作者渲染回归覆盖不同活动者、缺失资料与多个活动类型；叠加现有补丁后 Answer schema 测试和 question_common 编译通过。

## Consequences

需同时部署 forum 与社区静态资源。旧服务未返回 user_info 时，asked 记录仍显示正确发布者；其他活动记录显示未知作者而不冒用活动者；后端不增加敏感用户字段或独立查询。
