# Agent Note: 已发布 Release 的只读重入核验

Status: implemented

## Problem

手动发布与同一 tag 的 GitHub Actions 可能重复执行。原脚本把已公开 Release 一律视为不可覆盖错误，导致附件已完整发布后仍产生失败告警；直接吞掉该错误又会掩盖缺件、篡改或来源冲突。

## Decision

已发布 Release 只读下载并核对远程 tag 解析到的 commit、manifest、精确附件集合、GitHub API 提供的 digest/size、归档实际 SHA-256 与 `SHA256SUMS`。全部一致才返回成功；任一缺失、额外件、摘要/大小不一致、manifest 冲突或 tag commit 冲突都失败。draft 仅补传缺失附件且禁止 clobber，已有附件必须与本地一致。已公开版本无需本地构建，不要求本地重建压缩包与远端逐字节相同。

## Alternatives considered

- 直接忽略“已发布”错误：能消除告警，但会把缺件或错误来源伪装成成功。
- 重新上传并使用 `--clobber`：能统一产物，但违反已发布资产不可覆盖且无法保证重复执行的语义。
- 比较本地与远端压缩包字节：不同构建环境的 gzip 元数据可能不同，错误拒绝合法发布。

## Verification

30 项发布回归覆盖重复核验、不同 gzip 字节、缺件、下载损坏、manifest/tag 来源冲突、只读失败路径、draft 补传与并发恢复。`./deploy/test.sh`、shell/Python 语法检查、能力地图与决策笔记门禁通过。真实下载 `v0.2.89` 六件附件后，远端标签提交、manifest、asset digest/size 和 `SHA256SUMS` 全部核验通过。GitHub Actions 运行 `42404170536` 的失败 job `128499034540` 确认落在旧脚本“已发布版本不可覆盖”分支。

## Consequences

重跑同一已完成 Release 不再产生失败告警，也不会改动 GitHub 资产。真实缺件或冲突仍需新版本或人工修复，不能由发布脚本覆盖 immutable Release。

## Release validation follow-up

v0.2.90 全套 CI 和归档构建通过；首次真实草稿创建证明按 tag 查询返回 404，草稿仅在授权列表可见。因此 404 后必须成功读完分页集合才能认定不存在；重复项、列表权限或网络失败均停止。回归中的 CLI 替身按此真实行为返回草稿 404 和多页结果。该版本空草稿与标签保留，使用 v0.2.91 验证并发布修复，最终结果以 GitHub 工作流及资产回读为准。

独立笔记工作流在 Ubuntu 26.04 runner 安装 Python 3.9 时失败，尚未运行笔记检查；固定 ubuntu-24.04 以保持原 Python 版本，避免同时改变工具运行语义。自动工具哈希不变。

## Existing capabilities and note audit

检索发布/Release 命中部署备份保留、基础设施迁移与各功能发布验收笔记；均不拥有 Release 重入决定，无需替代或归档。复用既有 manifest 校验器、构建脚本、CI 与发布权限，不新增生产入口。
