# dev 分支的 CI/CD

通过 PR 合入 `dev`，必须通过 `Dev CI required`，分支必须与 `dev` 同步。当前规则已启用，无需额外审批票数；不绕过门禁。

CI 验证 Web 类型、生产构建和浏览器流程，登录服务 PostgreSQL 隔离测试，以及统一 Go 业务后端的竞态测试、PostgreSQL/Redis 集成和生命周期测试。业务离线测试不需要真实模型或 Amazon 凭据。

合并后 `dev-cd` 重跑检查，构建并发布 web、auth、backend 三个应用镜像，以提交 SHA 标记版本。登录、业务和前端各一个常驻应用进程；业务后台任务与接口共用同一个 Go 进程。迁移、数据库初始化和存储权限初始化是部署时退出的短期步骤。

## 服务器发布

服务器安装 `.github/scripts/deploy-dev.sh` 为发布 SSH 密钥的强制命令，使用仓库外 `~/.config/seller-ops-cd/dev.env` 配置。允许配置 `BUILD_IMAGE_SCRIPT` 进行服务器本地构建；参考 `.github/scripts/build-images.sh`。仓库、状态目录、镜像、环境文件和端口覆盖由既有部署配置确定，不将凭据写进仓库。

发布前校验目标 SHA 确实是当前 `dev`。数据库初始化成功后执行 `seller-backend migrate`，随后启动三个应用，清除旧独立 Worker 的孤立容器。健康检查包括登录会话、OIDC 和业务聚合健康接口。成功后切换 current 指针；失败则恢复上一版本应用镜像和配置，保留数据库和持久化文件，不降级删表。

GitHub `dev-server` Environment 的变量为 `DEPLOY_HOST`、`DEPLOY_USER`，机密为 `DEPLOY_SSH_KEY`、`DEPLOY_KNOWN_HOSTS`。GHCR 临时访问令牌由 Actions 提供，发布助手不将其记入日志。历史发布和当前数据卷继续保留以支持回滚。
