# Agent Note: 独立磁盘维护与共享缓存回收验收

Status: implemented

## Problem

生产五分钟 cron 正常触发，但 34.53 GB 构建缓存中 27.82 GB 为 shared，原容量回收命令返回成功后仍剩 34.53 GB。手动无容量限制回收释放该缓存。备份维护只随成功更新运行；更新配置或锁也会影响维护机会。

## Decision

保留更新内 8 GB 常规 GC，随后读取 buildx du 总量（含 shared）；超过 16 GB 或任一路径空闲不足 30 GiB 时，完整回收未使用缓存。复查总量与 20 GiB 安全线，未达标明确失败。独立 systemd timer 每 15 分钟复用更新锁、原备份保留脚本和缓存助手，不依赖版本变化、网络或业务配置，不重启 Docker。锁忙记录跳过并下次重试。部署仍由原 cron 执行。

## Related note audit

- [原缓存预算](../../implemented/process/2026-11-13-build-cache-budget.md)：部分替代“不新增定时器”和仅按容量参数判断的决定，保留常规 GC 和更新门禁。
- [单份备份](../../implemented/process/2026-11-17-single-deployment-backup.md)：复用保留规则，不变更保护现场语义。
- [迁移](../../implemented/process/2026-10-27-metar-server-migration.md)：历史事故，非本次策略 owner。

## Alternatives considered

- 只修五分钟更新入口：改动少，但维护继续依赖更新前置检查；独立维护能单独观察失败状态。
- 每次全清缓存：实现简单且现场有效，但频繁冷构建成本高，采用常规 GC 后达到阈值才全清。
- 调整 Docker daemon GC：覆盖更广，但需主机全局配置与版本适配；本次复用已验证可在线运行的 buildx 回收。

## Verification

`make deploy-test` 与发布回归通过；11项缓存测试覆盖共享缓存超限、低空闲、失败传播，维护入口测试覆盖锁忙与备份失败中止。生产安装与定时触发需独立验收，不由代码测试推断。

## Consequences

构建缓存是主机共享资源，升级可能需要冷编译；在用缓存或其他业务增长无法靠 GC 回收。20 GiB 是失败门禁而非磁盘配额。失败记录在 systemd journal，不声称已配置外部通知。
