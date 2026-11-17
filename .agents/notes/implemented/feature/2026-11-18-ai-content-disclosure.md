# Agent Note: AI 生成内容自报标注

Status: implemented

## Problem

读者无法区分作者主动声明使用 AI 生成的内容。发布时需要低摩擦的自报入口；不能把未勾选或旧内容当成人工鉴定。

## Existing capabilities and impact

发布入口和原生权限由 Answer 管理，METAR 的讨论列表复用公开问题投影。评选有独立资格和审核条件，本次不修改。

## Decision

原生帖子、回答、评论和评论回复提供可选复选框和发布前提示。布尔字段随原生写入与作者编辑保存，旧客户端省略字段不会清标。仅勾选时在原生详情与 METAR 列表显示。问答联发表单分别保存声明；原有帖子、回答草稿可恢复勾选。已部署数据库由 Answer 启动时的原生版本升级增加字段，不重写正文或标注历史内容。

## Alternatives considered

不强制全体用户每次选择，不用正文前缀或标签占用分类。两者都不能可靠覆盖评论与作者后续更正。

## Consequences

标注表明作者声明，不能推断未勾选内容的来源。只有作者可更正其声明，仍受原生编辑权限和期限约束。需要同时升级 Answer 镜像和 METAR 静态资源。

## Verification

Answer 补丁随 Docker 构建执行 SQLite 升级回归：老数据不标注、布尔值可设置和清除、旧客户端省略字段保留。用隔离 MySQL 8.0 重跑同一场景，另验证 Answer Go、TypeScript 与 METAR 正式前端构建。列表测试拒绝非布尔值的声明。
