# Seller Ops 开发约定

## 项目布局

- apps/web 是 Next.js 前端，提供简体中文仪表盘和同域业务 API 代理。
- apps/auth 是 Go 认证服务，负责 Casdoor OIDC、会话、工作区、成员和邀请。apps/auth/casdoor 保存部署配置。
- apps/backend 是 FastAPI 电商后端，包含采集、模型调用、Redis 状态和 Celery 任务。
- README.md 是使用入口，docs/deployment.md 是部署说明。根目录 compose.yaml 启动整套服务。

修改业务 API 时，同时检查 apps/web 的客户端调用、代理和服务端契约。浏览器通过已验证的会话访问业务 API，工作区和账号标识由服务端校验。生产凭据使用环境配置，测试使用独立的数据源。

## 开发与检查

- Web 依赖在 apps/web 安装，安装命令为 bun install --frozen-lockfile。根目录的 npm run dev、npm run build、npm run typecheck 和 npm run lint 代理到 Web。
- Go 测试在 apps/auth 执行 go test ./...。数据库集成测试使用独立的 TEST_DATABASE_URL。
- Python 测试在 apps/backend 执行 python -m pytest，监控测试需要可用的测试 Redis 实例。
- Compose 本地测试覆盖文件位于 apps/auth/casdoor/compose.local.yaml，启动命令见 README.md。

## Web 约定

- 业务功能放在 apps/web/src/features，优先使用服务端组件，只有浏览器 API 或 React Hooks 需要客户端组件。
- 使用显式类型、功能已有的 API 层和 React Query。数据访问沿用 types、service、queries 分层。
- 用 cn() 合并样式，优先通过组合扩展共享 UI。图标统一从 src/components/icons.tsx 的 Icons 使用。
- 现有布局用 PageContainer 提供标题与操作。复用表单字段、useAppForm 和组件已有的加载状态。
- 面向用户的界面沿用简体中文。商品页面文案仍按用户选择的输出语言生成。
