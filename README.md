# Seller Ops · 电商运营工作台

Next.js 前端、Go 登录服务和 Go 业务后端组成的亚马逊运营工作台。业务后端已全部使用 Go，不需要 Python、Celery 或独立 Worker 进程。

## 运行结构

| 应用进程 | 目录 | 默认内部端口 | 职责 |
| --- | --- | --- | --- |
| 登录服务 | `apps/auth` | 8080 | Casdoor OIDC、账号会话、租户及成员权限 |
| 业务后端 | `apps/backend` | 8000 | 五个电商模块、产品初筛和所有后台任务 |
| 前端 | `apps/web` | 3000 | 中文页面、会话验证和同域业务 API 代理 |

三个应用可以使用不同内部端口。浏览器通过 Caddy 的统一 HTTPS 入口访问前端；Casdoor 使用独立认证域名。PostgreSQL、Redis、Casdoor 和 Caddy 是基础服务，不属于上述三个应用进程。抓取网页时 Chromium 按需启动。

统一业务后端只监听一个端口。电商监控和初筛任务由同一 Go 进程中的 goroutine 执行，正常退出时会停止任务并释放连接。没有单独的 ecom-worker、screening-worker 或定时调度服务。

## 功能

- 选品研究：读取 Amazon 美国站榜单当前页，通过已配置的模型评分。访问 Amazon 和模型均需要可用配置。
- 商品页面生成：生成或优化标题、五点描述、描述、搜索词及 A+ 草稿，支持六种输出语言。
- 评论分析、竞品监控和广告分析：按账号隔离数据，缺少外部授权或费用数据时明确提示。
- 产品初筛：XLSX/CSV/ZIP 导入、跨月商品数据、父子体、规则初筛、单条及批量复核、利润快照和 Excel 导出。

账号登录后使用绑定的租户，不提供工作区切换。初筛业务库 `seller_screening` 与用户管理库 `seller_ops`、Casdoor 数据库分开；合并进程不会合并数据库。监控列表和快照沿用 Redis。

## 目录

```text
apps/auth/                         Go 登录及账号管理
apps/backend/main.go               统一业务进程入口
apps/backend/internal/ecom/        采集、模型与原五个业务模块
apps/backend/internal/screening/   初筛逻辑、内嵌迁移与规则
apps/backend/scripts/init-db.sh    独立初筛数据库初始化
apps/web/                          Next.js 前端
compose.yaml                       当前部署配置
```

## Compose 启动

将 [环境配置示例](apps/auth/casdoor/env.example) 复制到仓库外的私有文件，填写密码、两个内部签名密钥和 OIDC 配置。已有数据卷不会自动重新执行 PostgreSQL 初始化脚本；独立初筛库由 `screening-db-init` 显式创建。

```sh
docker compose --env-file /path/to/seller.env --profile app config --quiet
docker compose --env-file /path/to/seller.env --profile app up -d --build --remove-orphans
docker compose --env-file /path/to/seller.env --profile app logs --tail=100 api backend web
```

`backend-migrate` 是部署时的一次性迁移步骤，成功后退出；`backend` 随后启动 API 和任务循环。保留原数据库、Redis 和文件卷，切换时移除旧的独立 Worker 容器。

首次配置 Casdoor、回调和 SSH 转发见 [部署说明](docs/deployment.md)。使用本地覆盖文件 `apps/auth/casdoor/compose.local.yaml` 时，应用入口为 `http://localhost:18443`，认证入口为 `http://localhost:18444`。

## 原生进程启动

业务后端的完整变量见 [配置示例](apps/backend/.env.example)。先配置独立 PostgreSQL、Redis、持久化文件目录和两个至少 32 字节的签名密钥。

```sh
cd apps/backend
go build -o seller-backend .
./seller-backend migrate
BACKEND_ADDR=127.0.0.1:8000 ./seller-backend serve
```

另开两个终端启动 `apps/auth` 的登录服务和 `apps/web` 的前端。Next.js 的配置为：

```text
SELLER_API_URL=http://127.0.0.1:8080
ECOM_API_URL=http://127.0.0.1:8000
SCREENING_API_URL=http://127.0.0.1:8000
SELLER_APP_ORIGIN=http://localhost:3000
```

前端的两个签名密钥分别与业务后端一致。登录服务的 `APP_ORIGIN` 和 Casdoor 的回调地址使用相同前端入口。网页采集需要 Chromium，可通过 `CHROME_BIN` 指定路径。

## 检查与发布

```sh
(cd apps/auth && go test ./...)
(cd apps/backend && go test -race ./... && go vet ./...)
(cd apps/web && bun install --frozen-lockfile)
npm run typecheck
NEXT_PUBLIC_SENTRY_DISABLED=true npm run build
```

数据库测试只使用独立的 `*_test` 初筛库和认证测试库；电商 Redis 测试只使用 DB15。统一进程的路由、健康检查和 Worker 退出也有回归测试。浏览器验收使用隔离的合成数据。

合并到 `dev` 必须经过 PR 和 `Dev CI required` 检查，随后 CI/CD 发布 web、auth、backend 三个应用镜像。发布与回滚见 [CI/CD 说明](docs/ci-cd.md)和[Go 后端说明](docs/go-backend-migration.md)。

## 设计与许可证

[产品初筛](docs/product-screening.md) · [九表数据库设计](docs/product-screening-database.md) · [SQL 快照](docs/product-screening-schema.sql) · [OpenAPI](docs/screening-openapi.json)

历史验证记录保存在 `docs/archive`。仓库许可证见 [LICENSE](LICENSE)，引入的 EcomAgent MIT 许可证见 [后端](apps/backend/internal/ecom/LICENSE)和[前端](apps/web/src/features/ecom/LICENSE)。[上游项目](https://github.com/yoligehude14753/ecom-agent)的原始导入记录保留在 Git 历史中。
