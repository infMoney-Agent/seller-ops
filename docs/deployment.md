# 自托管 seller-ops 测试环境

需要 Docker Compose、两个测试域名和可用的 80/443 端口。已有反向代理占用端口时，在该代理中配置两个测试域名，按部署环境调整 Compose 的代理入口。不要在现有生产栈里首次运行。

1. 将 `apps/auth/casdoor/env.example` 复制到仓库外的私有文件，填写域名和三个独立的长随机十六进制密码。以下命令用 `/path/to/seller.env` 指代该文件。
2. 先只启动数据库和 Casdoor，用回环端口完成初始化。

   ```sh
   docker compose --env-file /path/to/seller.env -p seller-test -f compose.yaml -f apps/auth/casdoor/bootstrap.yaml up -d db casdoor
   ```

   通过 SSH 的 `-L 18000:127.0.0.1:18000` 在本机浏览器访问 `http://localhost:18000`。立即修改初始化管理员密码。新建一个身份组织和 OIDC 应用，关闭公开注册，配置精确回调 `https://<APP_DOMAIN>/api/auth/callback`，使用签名 ID Token，并确认登录返回标准的 `iss`、`sub`、`nonce`、`email` 和 `email_verified` 声明。录入测试账号并完成邮箱验证。将客户端 ID 和密钥填入环境文件。
3. 将两个测试域名指向测试服务器，启动测试栈。这里省略 bootstrap 文件以移除 Casdoor 的回环端口映射。

   ```sh
   docker compose --env-file /path/to/seller.env -p seller-test -f compose.yaml --profile app up -d --build
   ```

4. 测试账号使用已验证邮箱登录，系统自动绑定租户，不提供工作区切换。验证不同账号/租户的商品、成员和业务数据隔离；同时检查 `backend` 的聚合健康和后台任务。
5. 验收后再安排切换正式入口。切换前备份旧部署配置与数据库，并单独备份新 PostgreSQL 的 `casdoor`、`seller_ops` 和独立初筛 `seller_screening` 数据库。回滚时恢复旧入口与配置，保留新数据库卷。

Go 数据库集成测试需要一个空的临时 PostgreSQL 数据库。设置 `TEST_DATABASE_URL` 后，在 `apps/auth` 运行 `go test ./...`。测试会写入测试账号、工作区和产品。

## Proto 元数据传输

详见 [Proto 迁移状态](proto-migration-status.md)。业务与认证镜像均使用仓库根目录作为构建上下文，包含 proto/gen/go 和 packages/rpccompat。Web 的六个 *_TRANSPORT 开关默认 legacy，更新两个 Go 服务后可逐组启用 connect；AUTH_TRANSPORT 要同时配置 SELLER_APP_ORIGIN，身份仍由 Go 认证服务验证。内部 RPC 不应映射到公网。回滚仅修改开关和恢复应用构建，不执行数据库降级。

### Native build and Casdoor restart checks

On small hosts use NEXT_BUILD_CPUS=1 and NODE_OPTIONS=--max-old-space-size=2048 for production builds. Build outside the live Web directory.

Casdoor SQLite schema synchronization cannot read expression indexes such as user(lower(trim(name))). Global normalized username uniqueness is already enforced by seller_ops_user_name_insert and seller_ops_user_name_update triggers. Retain both triggers; do not add the redundant expression index. Back up the database before repairing an existing incompatible index. Verify Casdoor startup and authentication after a restart.
