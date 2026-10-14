# Agent Note: 恢复演练等待正式 MySQL 实例并输出安全诊断

Status: implemented

## Problem

CI run 38619454464 在隔离恢复演练的 CREATE DATABASE 阶段失败，同一提交的 Release CI 却通过。现有脚本用默认 Unix socket 的 SELECT 1 判断就绪；官方 MySQL 镜像初始化时会先运行 `--skip-networking` 临时实例，再关闭并启动正式实例。本地延长官方初始化阶段后，旧 socket 查询在 @@port=0 时成功，正式 TCP 查询同时返回 ERROR 2003；证实就绪误判，下一条恢复 SQL 可以遇到关闭窗口。统一隐藏 subprocess 错误还抹去了失败阶段和 MySQL 错误码。

## Decision

复用 `deploy/backup.py` 的恢复入口，将就绪、建库、导入和对账统一为容器内 127.0.0.1 TCP 连接。保留 `--network none`、无宿主端口、一次性数据卷与 finally 清理；用限时认证 SELECT 1 等待正式服务，超过期限明确失败。只重试只读就绪探测，不重放可能已部分执行的恢复 SQL。

诊断保留固定阶段、退出码和格式校验后的 MySQL 数字错误码；不输出原始 SQL、stdout/stderr、凭据、DSN 或 Docker 日志。真实集成回归延长初始化阶段，明确验证恢复从正式实例开始，并继续覆盖本地/外部备份及加密回读。

## Related note audit

- [基础设施可迁移性](../../implemented/architecture/2026-10-04-infrastructure-portability.md)：部分重叠，保留原有完整恢复集与隔离边界，本记录补充启动就绪和错误诊断。
- [部署备份保留](../../implemented/process/2026-10-01-deployment-backup-retention.md)：无关，本次不修改备份筛选或清理规则。
- 其他社区恢复笔记涉及网页/业务幂等，与 MySQL 容器启动无关。

## Alternatives considered

- 成功探测后固定 sleep：实现简单，但机器负载和初始化时间不固定，仍可能误判，且每次正常执行都要额外等待。
- 对 CREATE/导入重试：能掩盖短暂断连，但 SQL 可能部分生效，重试无法保证恢复语义；只允许在进入恢复写入前等待正式实例。
- 输出完整 Docker/MySQL 日志：调查最直接，但通用恢复工具面对真实备份和密钥，使用固定阶段与数字错误码避免泄露。

## Testing

- `METAR_TEST_COMPOSE=1 python3 deploy/test_infrastructure.py`：14 项通过，覆盖临时 socket、就绪截止、单次挂起、超时清理、不重试部分导入、错误码与 traceback 脱敏，以及八种 Compose 模式。
- `METAR_BACKUP_INTEGRATION=1 python3 deploy/test_backup_integration.py`：真实 Docker 全流程通过（110.9 秒），本地 8.0 导出、延迟初始化的 8.4 恢复、外部只读备份账号、账实对账、错误密码拒绝与 restic 加密回读；建库前断言正式端口为 3306。
- 同一真实集成回归切换为修改前 `backup.py`，在建库前稳定捕获 `@@port=0 != 3306`（53.0 秒）；修改后通过，证明回归能识别本次竞态。
- `make deploy-test`、Python 编译、发布回归和决策笔记校验通过。CI 与 Release 使用同一集成入口，发布结果以对应工作流和附件核验为准。

## Consequences

正式实例开始监听之前会等待，比临时 socket 早返回更可靠；代价是初始化耗时计入就绪预算。没有 TCP listener 的自定义镜像将明确超时；支持目标仍为官方 MySQL 8.0/8.4。故障发生在正式服务运行后仍使演练失败，不能把启动修复当作任意 SQL 自动恢复。生产升级与生产历史数据恢复另行验收。
