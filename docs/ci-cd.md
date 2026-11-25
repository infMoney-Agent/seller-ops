# dev 分支的 CI/CD

通过 PR 合入 `dev`，必须通过 `Dev CI required`，分支必须与 `dev` 同步。当前规则已启用，无需额外审批票数；不绕过门禁。

CI 验证 Web 类型、生产构建和浏览器流程，登录服务 PostgreSQL 隔离测试，以及统一 Go 业务后端的竞态测试、PostgreSQL/Redis 集成和生命周期测试。业务离线测试不需要真实模型或 Amazon 凭据。

合并后 `dev-cd` 重跑检查，构建并发布 web、auth、backend 三个应用镜像，以提交 SHA 标记版本。登录、业务和前端各一个常驻应用进程；业务后台任务与接口共用同一个 Go 进程。迁移、数据库初始化和存储权限初始化是部署时退出的短期步骤。

## 服务器发布

服务器安装 `.github/scripts/deploy-dev.sh` 为发布 SSH 密钥的强制命令，使用仓库外 `~/.config/seller-ops-cd/dev.env` 配置。允许配置 `BUILD_IMAGE_SCRIPT` 进行服务器本地构建；参考 `.github/scripts/build-images.sh`。仓库、状态目录、镜像、环境文件和端口覆盖由既有部署配置确定，不将凭据写进仓库。

发布前校验目标 SHA 确实是当前 `dev`。数据库初始化成功后执行 `seller-backend migrate`，随后启动三个应用，清除旧独立 Worker 的孤立容器。健康检查包括登录会话、OIDC 和业务聚合健康接口。成功后切换 current 指针；失败则恢复上一版本应用镜像和配置，保留数据库和持久化文件，不降级删表。

GitHub `dev-server` Environment 的变量为 `DEPLOY_HOST`、`DEPLOY_USER`，机密为 `DEPLOY_SSH_KEY`、`DEPLOY_KNOWN_HOSTS`。GHCR 临时访问令牌由 Actions 提供，发布助手不将其记入日志。历史发布和当前数据卷继续保留以支持回滚。

## main 分支生产自动发布（单机 80 端口 HTTP）

将 `dev` 合并到 `main` 后，`.github/workflows/prod-cd.yaml` 自动复跑 `dev-ci.yaml`、构建并推送 `web`、`auth`、`backend` 三个 `linux/amd64` 镜像，并通过 SSH 强制命令触发生产机上的 `.github/scripts/deploy-prod.sh`。

- 线上 80 端口仅暴露系统 Web 页面及 Go 认证短登录/注册入口（`apps/auth/casdoor/compose.prod-http.yaml` + `apps/auth/casdoor/Caddyfile.prod-http`），屏蔽 `/sellerops.*` 内部 RPC；Casdoor 管理界面仅绑定 `127.0.0.1:18000`，仅允许通过 SSH 隧道维护。
- 生产服务器位于国内机房（如腾讯云广州）时，在 GitHub `prod-server` Environment 配置 `IMAGE_REGISTRY`（如 `ccr.ccs.tencentyun.com`）、`IMAGE_NAMESPACE` 及机密 `REGISTRY_USERNAME`、`REGISTRY_PASSWORD`，避免直连 GHCR 拉取超时；未配置时默认回退到 GHCR。
- `prod-server` Environment 还需配置变量 `DEPLOY_HOST`、`DEPLOY_USER`（可选 `DEPLOY_PORT`）与机密 `DEPLOY_SSH_KEY`、`DEPLOY_KNOWN_HOSTS`。
- 生产机使用仓库外 `~/.config/seller-ops-cd/prod.env` 与 `seller.prod.env`（模板见 `apps/auth/casdoor/env.prod.example`）。发布成功后保留当前与上一版本用于回滚，并自动清理更早的历史发布目录与旧应用镜像，防止小容量系统盘被占满。
