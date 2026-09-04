# 数据库权限验收

Meta Pulse 的运行账号与 new-api 主库账号必须分离：

- `PULSE_DB_DSN`：只连接 Pulse 独立数据库，允许 Pulse 迁移和业务写入；
- `NEWAPI_LOG_DSN`：只连接 new-api `LOG_DB`，只能读取 `logs` 表；
- Pulse 不配置、也不需要 new-api 主库写账号，不能写 `users.quota` 或其他余额表。

## DBA 授权模板

以下命令由 DBA 在 new-api 数据库所在实例执行。请替换账号、来源网段和密码；真实密码不得提交 Git：

```sql
CREATE USER 'pulse_log_ro'@'10.%' IDENTIFIED BY '<从密钥管理器读取>';
GRANT SELECT ON `new_api`.`logs` TO 'pulse_log_ro'@'10.%';
-- 不授予 INSERT、UPDATE、DELETE、CREATE、ALTER、DROP、TRIGGER 或 GRANT OPTION。
FLUSH PRIVILEGES;
```

如果 `LOG_DB` 使用独立 schema，请把 `new_api` 替换为实际 schema；不要将此账号授予 new-api 主库的写权限。Pulse 自身数据库应使用另一个账号，例如 `pulse_runtime`。

## 无写权限验收

部署完成后，在与 worker 相同的网络和密钥注入环境执行：

```bash
meta-pulse-tool access-check
```

该命令只执行 `SELECT CURRENT_USER(), DATABASE()`、`SELECT 1 FROM logs LIMIT 1` 和 `SHOW GRANTS`，不会写入持久化数据。它会拒绝：

- 任意写权限或 `ALL PRIVILEGES`；
- `WITH GRANT OPTION`；
- 无法从 `SHOW GRANTS` 安全判定的角色授权；
- 无法读取 `logs` 表的账号。

同时由 DBA 核对 Pulse 数据库账号的授权范围和 new-api 主库权限审计，确认两者不是同一账号；`access-check` 不能替代生产数据库审计。

预期结果至少包含：

```json
{
  "readable": true,
  "read_only": true
}
```

`access-check` 失败时不得开放 Usage Ingest 或奖励生产开关。
