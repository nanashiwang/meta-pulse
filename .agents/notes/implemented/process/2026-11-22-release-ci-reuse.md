# Agent Note: Release 复用同一提交的主分支 CI

Status: implemented

## Problem

main 推送运行八项 CI，随后标签发布再次运行同一套检查，增加资源与排队成本。独立标签仍须有完整验证，不能只删除 Release verify。

## Decision

复用本仓库 ci.yml、main push、完整相同 SHA 的最新运行成功结果。正在运行时最多等待三十分钟，暂不可见时等待一分钟；缺失、失败、超时、API 异常均回退已有 workflow_call。preflight 始终检查标签元数据，publish 仅在 preflight 成功且复用成功或补跑成功时运行。仅查询任务获得 actions:read；发布写权限仍限于 publish。

## Existing capabilities and note audit

能力入口为 .github/workflows/{ci,release}.yml、release/preflight.py 与 publish.sh。既有 [Release 重入核验](2026-11-08-release-replay-verification.md) 管理附件只读重入，部分相关但决定独立；部署备份/迁移、业务功能 CI 笔记不拥有此次调度决定，保留不动。继续使用 Python 标准库、gh CLI 和现有发布测试入口。

## Alternatives considered

- 保留两遍 CI：独立易懂，但每次正式发布重复占用镜像与数据库构建资源。
- 直接删除发布 CI：最省资源，但标签可绕过主分支成功检查。
- workflow_run 自动发布：可避免轮询，但改变标签发布入口及权限上下文，迁移成本更高。

## Verification

40 项发布回归通过，覆盖相同/不同 SHA、仓库/分支/事件/工作流错配、最新失败覆盖旧成功、运行中到成功、缺失、超时、API 异常与补跑发布条件。actionlint 校验通过，笔记门禁通过；真实只读查询准确复用 88ef1d3 的 CI run 45000755149。正式版本的工作流及附件结果以 GitHub Actions 为准。

## Consequences

查询异常会多跑一次 CI；等待最多增加三十分钟延迟。复用成功测试结果不代表复用构建附件，仍按 tag 隔离构建并回读校验。
