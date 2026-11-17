# dev 分支的 CI/CD

向 dev 提交 PR 后，dev-ci 并行检查 Web 的类型和生产构建、Go 的 PostgreSQL 集成测试，以及 FastAPI 的 Redis 离线测试。Dev CI required 汇总结果。当前 GitHub 套餐不支持私有仓库的分支保护。在升级到支持该功能的套餐后，为 dev 要求通过 PR 合入，设 Dev CI required 为必过检查，要求分支与 dev 保持同步，并将规则应用于管理员。现存的前端 lint 问题暂未纳入门禁。

合入 dev 后，dev-cd 重新运行同一套检查，构建 Web、Go 和 Python 镜像，用 Git 提交 SHA 在 GHCR 打标签。Celery Worker 和 Beat 使用同一个 Python 镜像。构建通过后，工作流通过 SSH 更新服务器上的固定 Compose 项目。更新沿用该项目的持久化卷。服务健康检查通过后，服务器记录当前版本。如果新版本启动失败，则使用上一版本的配置和镜像恢复，工作流会报告失败。

## 部署环境

GitHub 的 dev-server Environment 只允许 dev 使用。变量为 DEPLOY_HOST、DEPLOY_USER，机密为 DEPLOY_SSH_KEY、DEPLOY_KNOWN_HOSTS。GHCR 的短期读取令牌由本次 Actions 任务提供。

服务器安装 .github/scripts/deploy-dev.sh 作为专用 SSH 密钥的强制命令，并配置 ~/.config/seller-ops-cd/dev.env。配置包括仓库位置、部署状态目录、Compose 项目名、私有环境文件、服务器覆盖配置、Docker 套接字文件和健康检查地址。首次启用前，将现有健康部署的 Compose 配置及本地镜像标签登记为 current。部署保留既有模型配置、Casdoor 和 Redis 数据。
