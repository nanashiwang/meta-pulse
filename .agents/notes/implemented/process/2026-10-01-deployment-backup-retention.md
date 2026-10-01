# Agent Note: 安全部署备份保留

Status: implemented

## Problem

update.sh 已有 Git flock、配置/数据库/私钥备份与健康检查，但每次执行都留下完整备份，没有成功凭证；摄入验收在完成提示后执行。只按目录时间删除会丢失失败更新恢复现场。

## Decision

复用 update.sh / metar update 入口与全程更新锁，新增 Python 标准库保留助手。默认保留最近 3 份成功更新备份，最少 1 份；0 关闭清理。当前备份、失败/中断、旧版无状态备份及显式 pin 永久保护。预览不需要 Docker 或配置，不更新服务。全部健康、版本及可选摄入验收完成后才记录成功并清理；清理失败单独警告，不伪称部署失败。

固定根为仓库 .data/deploy-backups；拒绝链接、越界名称、特殊文件和跨设备目录。使用目录描述符与 no-follow 操作，避免检查后替换链接导致越界删除。只删本机制明确记录成功的目录。

## Existing capabilities and note audit

远程 main 与本地 692d476 一致；metar update 透传参数，无需独立管理入口。检索 deploy/backup/备份/发布仅命中 capability-review-and-decision-notes 工作流笔记，属通用流程，与本决定无归属重叠。账本、身份、业务状态机不受影响。

## Alternatives considered

- 按 mtime 或 PID 删除旧目录：无迁移成本，但 PID 会复用且无法证明旧部署成功，不能保护失败恢复现场。
- 独立 cron 清理：能定期回收，但与部署验收脱节且新增并发协调入口；复用现有锁和成功出口更小。
- 纯 shell find/rm：依赖少，但链接替换与路径边界难以严谨处理；Python 已用于发布和摄入验收，标准库可提供 dir_fd/no-follow。

## Verification

`make deploy-test` 通过：新增 10 项文件系统回归覆盖 0/1/N、当前/失败/未知/pin、时钟回拨、损坏状态、路径/链接/硬链接/FIFO、链接替换、清理异常与真实 flock。现有 update_test.sh 扩展覆盖备份/构建/迁移/健康/网关/摄入验收失败、TERM、预览无 Docker/配置依赖、真实并发更新/预览互斥，成功验收后才删除。笔记门禁通过。发布仍需复用既有 CI、构建和附件校验，本笔记不把发布等同于生产部署。

## Consequences

失败与旧版备份不受成功数量限制，仍需人工核对；数量策略不是磁盘配额。首次由旧脚本拉取此版本的运行不会追溯获得新机制，后续调用才生效。拥有仓库写权限的管理员属于信任边界。
