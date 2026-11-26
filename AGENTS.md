# Seller Ops 开发约定

## 项目布局

- apps/web 是 Next.js 前端，提供简体中文仪表盘和同域业务 API 代理。
- apps/auth 是 Go 认证服务，负责 Casdoor OIDC、会话、工作区、成员和邀请。apps/auth/casdoor 保存部署配置。
- apps/backend 是统一 Go 业务进程；internal/ecom 负责五个电商模块，internal/screening 负责初筛。两个后台任务循环在同一进程运行，共用一个监听端口，初筛仍使用独立 PostgreSQL 业务库。
- README.md 是使用入口，docs/deployment.md 是部署说明。根目录 compose.yaml 启动整套服务。

修改业务 API 时，同时检查 apps/web 的客户端调用、代理和服务端契约。浏览器通过已验证的会话访问业务 API，工作区和账号标识由服务端校验。生产凭据使用环境配置，测试使用独立的数据源。

## 开发与检查

- Web 依赖在 apps/web 安装，安装命令为 bun install --frozen-lockfile。根目录的 npm run dev、npm run build、npm run typecheck 和 npm run lint 代理到 Web。
- Go 测试在 apps/auth 执行 go test ./...。数据库集成测试使用独立的 TEST_DATABASE_URL。
- 业务测试在 apps/backend 执行 go test -race ./... 和 go vet ./...。电商集成测试使用隔离的 ECOM_TEST_REDIS_URL（数据库15），初筛集成测试使用新建空的 SCREENING_TEST_DATABASE_URL（名称以 _test 结尾）。
- 新增业务后端使用 Go，保持用户管理与业务数据库隔离。不要引入 Python 运行时或代理回旧服务。
- Compose 本地测试覆盖文件位于 apps/auth/casdoor/compose.local.yaml，启动命令见 README.md。

## Web 约定

- 业务功能放在 apps/web/src/features，优先使用服务端组件，只有浏览器 API 或 React Hooks 需要客户端组件。
- 使用显式类型、功能已有的 API 层和 React Query。数据访问沿用 types、service、queries 分层。
- 用 cn() 合并样式，优先通过组合扩展共享 UI。图标统一从 src/components/icons.tsx 的 Icons 使用。
- 现有布局用 PageContainer 提供标题与操作。复用表单字段、useAppForm 和组件已有的加载状态。
- 面向用户的界面沿用简体中文。商品页面文案仍按用户选择的输出语言生成。

## Modern Web Guidance（团队前端开发必读）

- HTML、CSS、浏览器端 JavaScript、交互、性能和无障碍改动，必须先读 [.agents/skills/modern-web-guidance/SKILL.md](.agents/skills/modern-web-guidance/SKILL.md)，按具体需求搜索并获取相关指南，再实施和验证。仅后端、部署、文档或纯类型调整无需无关搜索。
- 项目统一入口：根目录执行 `npm run guidance -- search "responsive data table"`，再执行 `npm run guidance -- retrieve "实际搜索返回的ID"`。项目固定 CLI 版本；上游示例中的 @latest 在本项目使用此固定入口替代。离线可阅读随仓库保存的 guides，不伪造搜索或验收结果。
- 浏览器策略：面向当前稳定版 Chrome、Edge、Firefox、Safari；默认优先 Baseline Widely available。尚未达到该级别的 API 必须进行能力检测并提供可用回退。不启用未经明确需求授权的 Origin Trial 或单浏览器实验特性。
- 与现有 Next.js、React、TanStack 和共享 UI 组合使用。不能仅为追求新 API 删除已有必要的焦点管理、键盘行为、组件封装或回退。SSR 中保护浏览器对象访问。
- 用户明确要求、租户隔离、人工结论保留等业务约束和项目架构优先；指南不构成扩大功能范围、绕过权限或修改业务数据的授权。冲突时在 PR 中说明取舍。
- 前端 PR 必须在 Modern Web Guidance 栏列出实际采用的指南 ID、对应文件、兼容处理与验证结果；没有适用规则时说明具体原因。评审逐项核对，不用静态扫描代替真实交互验证。仅按影响范围运行验证，不因指南重复跑全部测试。
- `legacy-scan` 仅用于明确要求的整体旧模式排查；结果需人工判断，不将零告警作为合入条件。
- 新同事及工具接入见 [CONTRIBUTING.md](CONTRIBUTING.md) 和 [团队 Web 开发指南](docs/web-development-guidance.md)。上游更新须单独评审并同步版本锁，禁止在功能提交中悄悄替换全部指南。

## Proto 接口开发规范

- proto/ 定义业务和用户管理元数据契约；proto/gen/go 是独立共享模块，apps/web/src/gen 为 TS 生成物，均禁止手改。
- 修改契约后运行 npm run proto:gen、npm run proto:check、npm run proto:breaking，工具版本由脚本和 Web 锁文件固定。
- 浏览器仍使用既有 HTTP/JSON；Next.js 验证会话后调用内部 RPC，禁止信任客户端传入的公司或角色。
- 沿用 pgx 与原数据库结构，不因协议迁移引入 ORM、修改费用公式或恢复工作区切换/字段映射 UI。
- Web 侧已移除全部六个 *_TRANSPORT 过渡开关与旧元数据 HTTP 转发分支，所有业务与用户管理元数据接口唯一经由 Connect RPC 调用，失败不得静默回退；仅保留 4 个初筛文件流与登录/注册 Cookie HTTP 端点；演进与回滚见 docs/proto-connect-cutover-plan.md。

- 新增普通业务或用户管理元数据 API 必须先定义具名 Proto 方法，禁止增加无契约业务入口或通用任意路径/JSON 转发 RPC。HTTP/Connect 共用纯 Go 用例；文件流、登录/注册 Cookie 流程除外。
- 完整范围、独立开关和回滚见 docs/proto-migration-status.md。修改路由后运行 Web 的 test:imports-connect（含兼容与覆盖检查），并按涉及模块运行真实隔离集成测试。
