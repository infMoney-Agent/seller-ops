# Proto 接口迁移状态

## 覆盖范围

现有业务与用户管理元数据已具备生成的 Connect RPC，共 80 个具名方法。浏览器继续使用原 HTTP/JSON 地址；Next.js 服务端通过生成的契约和客户端调用内部服务。不存在接收任意方法、路径和 JSON 的通用转发 RPC，也不反向调用旧 HTTP Handler。

| 服务 | 方法数 | 范围 |
|---|---:|---|
| ScreeningImportService | 9 | 导入历史、详情、预览、任务状态、确认、设置、删除及重试 |
| ScreeningService | 37 | 商品、统计、父子体、来源、分组复核、利润、规则、导出元数据 |
| EcomService | 14 | 选品研究、文案、评论、监控、广告；含原内部评论 GET 查询 |
| AuthService | 20 | 会话、商品管理、公司成员、邀请、公司信息、加入申请及审批 |

CI 将初筛页面暴露的 50 个方法/路径逐一分类：46 个 RPC 元数据接口、4 个 HTTP 文件端点；每个路由映射必须有实际生成的方法，生成方法也必须有映射。

## 有意保留 HTTP 的端点

- 文件：POST imports、GET imports/{id}/source、GET imports/{id}/errors、GET exports/{id}/download。
- 登录、注册表单、OIDC 回调和注销：/api/auth/login-page、/api/auth/login、/api/auth/callback、/api/auth/logout、/api/auth/register-page。
- /api/auth/registration/prepare 属于注册流程，校验注册 CSRF 并设置临时注册 Cookie，保留 HTTP；registration/status 已走 RPC。
- 健康检查、OpenAPI 旧契约和静态文件继续使用 HTTP。

这些是浏览器协议、Cookie 或文件流端点，不属于遗漏的普通业务 Unary RPC。

## 架构与兼容

- HTTP 与 RPC 共用纯 Go 查询和命令用例，保持 pgx、原事务、租户过滤、并发版本、幂等键和审计记录。
- protobuf 类型仅存在于传输适配层；业务层不接收 pb 消息，也不拼装模拟 HTTP 请求调用旧路由。
- packages/rpccompat 与 Web 的 rpc-compat 维护原 JSON 投影：明确的字段、可选值、数组、枚举、64 位整数、精确金额文本和时间。legacy_fields 仅保留未建模来源扩展、显式 null 及原始时间文本；omitted_fields 区分缺失和空数组。
- 所有身份从真实会话派生；业务 RPC 校验签名与时效。Auth RPC 自行验证会话 Cookie，成员/管理员权限继续使用原业务校验；不信任浏览器身份头。
- 公网网关不公开 /sellerops.* RPC 前缀。文件与登录流程继续沿用原传输。商品与分组查询继续压缩响应，防止传输性能回退。
- 未修改数据库表、费用公式或人工复核规则。未迁移、删除业务数据。

## 传输状态及发布顺序

Web 服务端已移除旧元数据 HTTP 转发分支及六个过渡开关（原 `SCREENING_IMPORTS_TRANSPORT`、`SCREENING_IMPORT_READS_TRANSPORT`、`SCREENING_IMPORT_WRITES_TRANSPORT`、`SCREENING_BUSINESS_TRANSPORT`、`ECOM_TRANSPORT`、`AUTH_TRANSPORT`），全部 80 个元数据接口唯一通过 Connect RPC 调用，RPC 失败不会静默降级；Web 仅对 4 个初筛文件流端点和登录/OIDC/注册表单与 `registration/prepare` 保留 HTTP 转发。

部署须确保两个 Go 服务与公网网关运行在支持全部 80 个 RPC 的基线，并配置 Web `SELLER_APP_ORIGIN`。公网网关屏蔽 `/sellerops.*` 并将 `registration/status` 交给 Next 代理，其余注册 Cookie 流程仍交给认证服务。如需紧急回滚，恢复已备份的上一版本应用镜像或前端/Go 构建产物，不执行数据库降级。

Go 后端（`apps/backend` 与 `apps/auth`）已完成 Phase 3 Step 3.1 / 3.2 收口：已下线全部旧内部元数据 HTTP 路由，全部 80 个 Connect RPC 均直接调用具名 Go 业务用例；仅保留 4 个初筛文件流端点、认证 OIDC/登录/注册表单与 `registration/prepare`、健康检查和 OpenAPI HTTP 端点。

## 本轮验证

2026-11-25：两个 Go 服务均通过真实隔离 PostgreSQL / Redis 的 race 回归；认证服务的最后管理员、跨公司访问、邀请、注册及审批回归通过。18 个初筛读取和 8 个电商模块接口通过 HTTP/RPC 返回对比。兼容和路由覆盖测试、类型检查、lint 与生产构建通过；六个开关全为 connect 时完整浏览器验收通过（32.26 秒）。测试夹具仅存在于 *_test.go，不编入生产二进制。
