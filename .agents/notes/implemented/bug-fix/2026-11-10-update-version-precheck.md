# Agent Note: 更新前检查远程提交

Status: implemented

## Problem

metar update 透传 deploy/update.sh，原流程在 fetch 前备份，提交相同仍构建和重启，反复生成无用备份。

## Decision

复用部署锁、Git 与备份流程，在备份前 fetch 并固定目标 SHA、检查快进关系。同分支同提交默认退出；--force 用于失败重试、配置变更或手动拉取后的重建。显式 --release 保持完整重建，但提前核验发布元数据。无需新增 UI、接口或数据库。

## Existing capabilities and note audit

能力地图的部署入口与 metar 参数透传覆盖本问题。检索活跃笔记只命中 [备份保留](../../implemented/process/2026-10-01-deployment-backup-retention.md)，部分重叠；原备份保护和清理规则继续有效。

## Alternatives considered

- 仅移动 fetch：仍会重复备份，不能满足重复执行直接退出。
- 只比较 VERSION 或缓存的 origin 分支：不能发现同版本号的新提交或远程变更；采用本次 fetch 的 SHA。
- 要求成功标记才跳过：旧部署和手动部署无统一成功标记，会再次全量备份；采用代码检查并明确 --force 恢复入口，不把代码最新称为服务验收成功。

## Verification

`make deploy-test` 通过。离线真实 Git 仓库验证同提交无备份/构建/迁移/重启，有新提交先备份后合并，fetch 失败及分叉在备份前退出，强制重建与显式版本重建继续生效。

## Consequences

首次仍由旧脚本执行升级，后续调用才生效。代码最新不证明服务已运行新版；同提交重试需要 --force。fetch/发布核验失败在创建备份前退出，不伪造备份失败状态；真实部署仍由运维执行。
