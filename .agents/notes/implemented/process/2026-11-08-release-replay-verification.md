# Agent Note: 已发布 Release 的只读重入核验

Status: implemented

## Problem

手动发布与同一 tag 的 GitHub Actions 可能重复执行。原脚本把已公开 Release 一律视为不可覆盖错误，导致附件已完整发布后仍产生失败告警；直接吞掉该错误又会掩盖缺件、篡改或来源冲突。

## Decision

已发布 Release 只读下载并核对远程 tag 解析到的 commit、manifest、精确附件集合、GitHub API 提供的 digest/size、归档实际 SHA-256 与 `SHA256SUMS`。全部一致才返回成功；任一缺失、额外件、摘要/大小不一致、manifest 冲突或 tag commit 冲突都失败。draft 仅补传缺失附件且禁止 clobber，已有附件必须与本地一致，且不要求本地重建压缩包与远端逐字节相同。

## Alternatives considered

- 直接忽略“已发布”错误：能消除告警，但会把缺件或错误来源伪装成成功。
- 重新上传并使用 `--clobber`：能统一产物，但违反已发布资产不可覆盖且无法保证重复执行的语义。
- 比较本地与远端压缩包字节：不同构建环境的 gzip 元数据可能不同，错误拒绝合法发布。

## Verification

29 项发布回归覆盖重复核验、不同 gzip 字节、缺件、下载损坏、manifest/tag 来源冲突、只读失败路径、draft 补传与并发恢复。`./deploy/test.sh`、shell/Python 语法检查、能力地图与决策笔记门禁通过。真实下载 `v0.2.89` 六件附件后，远端标签提交、manifest、asset digest/size 和 `SHA256SUMS` 全部核验通过。GitHub Actions 运行 `42404170536` 的失败 job `128499034540` 确认落在旧脚本“已发布版本不可覆盖”分支。

## Consequences

重跑同一已完成 Release 不再产生失败告警，也不会改动 GitHub 资产。真实缺件或冲突仍需新版本或人工修复，不能由发布脚本覆盖 immutable Release。
