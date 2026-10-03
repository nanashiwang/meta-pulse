# 自定义信任根

目录仅放公开 CA 证书，不放私钥。默认只读挂载到 `/etc/meta-pulse-ca`。
需要私有 CA 时在生产配置指定 `SSL_CERT_FILE=/etc/meta-pulse-ca/ca.pem`；MySQL DSN 使用 `tls=true`，Redis 使用 `rediss://`，均验证服务器名称。
如系统还访问其他 HTTPS 服务，CA 文件应包含所需完整信任链。服务器可用 `META_PULSE_CA_DIR` 指向受管理目录。
