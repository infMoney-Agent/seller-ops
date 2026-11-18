# Seller Ops 开发约定

## 项目布局

- apps/web 是 Next.js 前端，提供简体中文仪表盘和同域业务 API 代理。
- apps/auth 是 Go 认证服务，负责 Casdoor OIDC、会话、工作区、成员和邀请。apps/auth/casdoor 保存部署配置。
- apps/ecom 是 Go 电商后端，负责网页采集、模型调用、Redis 监控和定时任务。
- apps/screening 是独立 Go 初筛服务，拥有独立 PostgreSQL 业务库、迁移和持久化任务。
- README.md 是使用入口，docs/deployment.md 是部署说明。根目录 compose.yaml 启动整套服务。

修改业务 API 时，同时检查 apps/web 的客户端调用、代理和服务端契约。浏览器通过已验证的会话访问业务 API，工作区和账号标识由服务端校验。生产凭据使用环境配置，测试使用独立的数据源。

## 开发与检查

- Web 依赖在 apps/web 安装，安装命令为 bun install --frozen-lockfile。根目录的 npm run dev、npm run build、npm run typecheck 和 npm run lint 代理到 Web。
- Go 测试在 apps/auth 执行 go test ./...。数据库集成测试使用独立的 TEST_DATABASE_URL。
- 业务测试分别在 apps/ecom、apps/screening 执行 go test -race ./... 和 go vet ./...。电商集成测试使用隔离的 ECOM_TEST_REDIS_URL（数据库15），初筛集成测试使用新建空的 SCREENING_TEST_DATABASE_URL（名称以 _test 结尾）。
- 新增业务后端使用 Go，保持用户管理与业务数据库隔离。不要引入 Python 运行时或代理回旧服务。
- Compose 本地测试覆盖文件位于 apps/auth/casdoor/compose.local.yaml，启动命令见 README.md。

## Web 约定

- 业务功能放在 apps/web/src/features，优先使用服务端组件，只有浏览器 API 或 React Hooks 需要客户端组件。
- 使用显式类型、功能已有的 API 层和 React Query。数据访问沿用 types、service、queries 分层。
- 用 cn() 合并样式，优先通过组合扩展共享 UI。图标统一从 src/components/icons.tsx 的 Icons 使用。
- 现有布局用 PageContainer 提供标题与操作。复用表单字段、useAppForm 和组件已有的加载状态。
- 面向用户的界面沿用简体中文。商品页面文案仍按用户选择的输出语言生成。
