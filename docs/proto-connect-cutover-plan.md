# Connect RPC 切流与 Legacy 收口方案

## 1. 背景与目标

截至当前基线，业务与认证服务已完成 80 个具名 Connect RPC 方法与 Proto 契约迁移（详见 [Proto 接口迁移状态](proto-migration-status.md)），并已完成 **Phase 2（Web 侧全量 Connect 收口与 6 个过渡开关拆除）**。当前剩余的过渡态主要集中在 Go 后端（Phase 3）：

- Web 代理层已完成收口：6 个 `*_TRANSPORT` 开关与旧元数据 HTTP 转发分支已移除，仅保留 4 个初筛文件流端点及认证页面/Cookie HTTP 透传。
- Go 后端仍同时暴露旧元数据 HTTP 路由和 Connect RPC，且 `ScreeningService` / `EcomService` 的 Connect 适配层仍通过 `rpccompat.Legacy` 转回 `map[string]any` 后再拼接路径字符串分发到 `executeScreening` / `executeEcom`。

本方案的目标是**先平稳完成全量 Connect 切流，再分步拆除双轨过渡代码**，使系统收敛到单一、清晰的内部 RPC 架构，为后续 `screening` 核心域强类型化与错误解耦打下基础。

### 明确边界（不可移除的 HTTP 端点）

以下端点属于浏览器协议、Cookie 或文件流场景，**切流与收口后仍永久保留 HTTP**：

| 类别 | 保留端点 | 原因 |
| :--- | :--- | :--- |
| 初筛文件流 | `POST /api/v1/screening/imports`<br>`GET /api/v1/screening/imports/{id}/source`<br>`GET /api/v1/screening/imports/{id}/errors`<br>`GET /api/v1/screening/exports/{id}/download` | 大文件流式上传（30MB）与下载，不放入 Unary RPC |
| 认证页面与 Cookie | `/api/auth/login-page`、`/api/auth/login`、`/api/auth/callback`、`/api/auth/logout`、`/api/auth/register-page`、`/api/auth/registration/prepare` | OIDC 跳转、浏览器表单与临时注册 Cookie 流程 |
| 运维与文档 | `/health`、`/healthz`、`/openapi.json`、`/screening/openapi.json` | 健康探针与静态契约 |

此外，**全程不修改**：浏览器对外 HTTP/JSON 路径与返回结构、数据库表结构、利润费用公式、人工复核结论保护与并发版本校验、幂等键防重逻辑。

---

## 2. 6 个传输开关盘点与分批切流顺序

| 批次 | 环境变量 | 覆盖服务与范围 | 代码位置 | 风险等级 |
| :---: | :--- | :--- | :--- | :---: |
| **A** | `SCREENING_IMPORTS_TRANSPORT` | `ScreeningImportService`：导入列表（1） | `apps/web/src/app/api/ecom/screening/[...path]/route.ts` | 低（试点已验证） |
| **A** | `SCREENING_IMPORT_READS_TRANSPORT` | `ScreeningImportService`：导入详情、行预览、任务查询（3） | 同上 | 低（二阶段已验证） |
| **B** | `ECOM_TRANSPORT` | `EcomService`：选品研究、文案、评论、监控、广告（14） | `apps/web/src/app/api/ecom/[...path]/route.ts` | 中低（独立 Redis） |
| **B** | `SCREENING_IMPORT_WRITES_TRANSPORT` | `ScreeningImportService`：确认导入、映射设置、删除预览、删除、重试（5） | `apps/web/src/app/api/ecom/screening/[...path]/route.ts` | 中（含幂等与 202） |
| **C** | `SCREENING_BUSINESS_TRANSPORT` | `ScreeningService`：商品、汇总、父子体、分组复核、利润、规则、导出元数据（37） | 同上 | 中高（核心读写 + gzip） |
| **D** | `AUTH_TRANSPORT` | `AuthService`：会话、商品、成员、邀请、公司信息、注册状态与审批（20） | `apps/web/src/lib/auth-backend.ts` | 高（全局鉴权与 Cookie） |

---

## 3. Phase 1：生产灰度切流与稳定观察

### 3.1 切流前检查清单（Pre-flight）

在开启任何尚未切流的开关前，逐项确认：

1. **版本顺序正确**：`apps/backend`、`apps/auth`、`apps/auth/cmd/public-gateway` 均已部署为支持全部 80 个 Connect RPC 的版本，随后再部署同基线 `apps/web`。
2. **`AUTH_TRANSPORT` 必备配置**：确认 Web 运行环境已配置 `SELLER_APP_ORIGIN`（与公网访问 Origin 一致）。未配置时 `AUTH_TRANSPORT=connect` 会直接返回 `503 认证服务尚未配置应用来源`（见 `apps/web/src/lib/auth-backend.ts`）。
3. **公网网关路由正确**：
   - `/sellerops.*` 前缀在公网网关直接返回 `404`（不暴露内部 RPC）；
   - `/api/auth/registration/status` 由公网网关转发至 Web 代理（其余 `/api/auth/registration/*` 继续直连 `apps/auth`）。
4. **可回滚备份就绪**：保留当前 `runtime.json`/环境变量配置、当前运行中的二进制与 Web 构建产物，并备份 `casdoor`、`seller_ops`、`seller_screening` 数据库。

### 3.2 分批切流执行与单批验收

遵循**“先读后写、先外围后核心、最后切认证”**原则，每批修改环境变量并重启 Web 服务：

#### 批次 A：`SCREENING_IMPORTS_TRANSPORT=connect` + `SCREENING_IMPORT_READS_TRANSPORT=connect`
- **核验项**：
  - 导入历史列表分页、状态、日期空值、`counts`/`mapping` 正常展示；
  - 导入详情 `latest_job_id`、行预览排序、任务轮询状态正常；
  - 响应头 `Server-Timing` 与原文件/错误清单下载正常。
- **观察窗口**：稳定运行无异常后进入批次 B（若运行环境已持久化为 `connect`，复核确认即可）。

#### 批次 B：`ECOM_TRANSPORT=connect` + `SCREENING_IMPORT_WRITES_TRANSPORT=connect`
- **核验项**：
  - 电商模块：类目/语言列表、竞品监控增删查与快照、选品/评论/文案/广告接口返回正常；
  - 导入写链路：修改导入设置、确认导入（返回 `202`）、失败任务重试、删除预览与删除导入；
  - 幂等性验证：携带相同 `Idempotency-Key` 重放写请求返回一致回执，修改请求体触发 `409`。
- **观察窗口**：观察 1~2 天业务写入与后台任务无报错。

#### 批次 C：`SCREENING_BUSINESS_TRANSPORT=connect`
- **核验项**：
  - 商品列表、分组复核工作台、父子体详情、筛选/商标选项、统计汇总；
  - 人工复核结论提交、人工分类修改、商品参数更新（含 `expected_data_revision` / `expected_review_revision` 冲突返回 `409`）；
  - 利润测算、保存测算方案、规则创建/测试/激活、批量复核预览/应用/撤销、导出任务创建与下载；
  - 性能核验：商品列表与分组查询响应继续保持 gzip 压缩，`Server-Timing` 延迟无明显回退。
- **观察窗口**：覆盖至少 1 个完整选品复核与利润测算工作周期（建议 2~3 天）。

#### 批次 D：`AUTH_TRANSPORT=connect`
- **核验项**：
  - 会话检查 `/api/auth/session`（被所有业务代理调用）响应稳定；
  - 成员列表、角色变更、移除成员（最后管理员保护返回 `409`）；
  - 邀请创建、查看（匿名可访问 `InspectInvitation`）、接受、撤销；
  - 公司信息查询、注册状态查询 `/api/auth/registration/status`、加入申请审批；
  - 异常会话清理：`Set-Cookie` 透传正常，公司身份变化时能正确清除会话 Cookie 并返回 `401`。
- **观察窗口**：全量 6 开关均处于 `connect` 状态，连续稳定观察 **7 天** 且无回滚。

### 3.3 应急回滚预案

> 原则：Connect 调用失败不会静默降级，发现异常应显式回滚。

- **单组回滚**：将异常组对应的 `*_TRANSPORT` 改回 `legacy` 并重启 Web 服务，无需改动 Go 后端与数据库。
- **全量回滚**：6 个开关全部改回 `legacy` 并重启 Web；如涉及构建问题，恢复切换前备份的 Web/Go 产物，**不执行数据库降级**。

---

## 4. Phase 2：默认值切换与 Web 侧双轨清理

> **准入条件**：Phase 1 全量 6 开关在生产/运行环境连续稳定观察达标，无未解故障。

### Step 2.1：默认模式切换为 `connect`（已完成，保留 `=legacy` 逃生舱）

> 状态：**已完成**（批次 A/B/C/D 全部 6 个开关默认值已切为 `connect`，零数据库变更；仍保留显式 `=legacy` 紧急回滚能力）。

- [compose.yaml](file:///data/workspace/yijiangli/seller-ops/compose.yaml#L25-L30)：6 个 `*_TRANSPORT` 默认值已从 `:-legacy` 改为 `:-connect`。
- 以下 3 个 Web 文件中的兜底默认值 `'legacy'` 已改为 `'connect'`：
  - [apps/web/src/app/api/ecom/screening/[...path]/route.ts](file:///data/workspace/yijiangli/seller-ops/apps/web/src/app/api/ecom/screening/%5B...path%5D/route.ts#L86-L162)
  - [apps/web/src/app/api/ecom/[...path]/route.ts](file:///data/workspace/yijiangli/seller-ops/apps/web/src/app/api/ecom/%5B...path%5D/route.ts#L60)
  - [apps/web/src/lib/auth-backend.ts](file:///data/workspace/yijiangli/seller-ops/apps/web/src/lib/auth-backend.ts#L30)

### Step 2.2：移除 Web 侧 Legacy 代理分支与 6 个过渡开关（已完成）

> 状态：**已完成**（Web 侧 6 个过渡开关与旧元数据 HTTP 转发分支已移除，4 个初筛文件流与认证页面/Cookie HTTP 端点保留；配置、CI 与文档已同步更新）。

1. **清理 `apps/web/src/app/api/ecom/[...path]/route.ts`**
   - 已删除 `ECOM_TRANSPORT` 判断与整个旧 HTTP 转发逻辑（`createHmac`、`ECOM_API_URL` fetch、`64KB` 旧 body buffer 读取）。
   - 该文件只负责：路由白名单校验 -> Origin 校验 -> 会话鉴权 -> `connectEcom(...)`。

2. **清理 `apps/web/src/app/api/ecom/screening/[...path]/route.ts`**
   - 已删除 `SCREENING_BUSINESS_TRANSPORT`、`SCREENING_IMPORTS_TRANSPORT`、`SCREENING_IMPORT_READS_TRANSPORT`、`SCREENING_IMPORT_WRITES_TRANSPORT` 四个开关分支。
   - 46 个元数据路由直接按分类进入 `connectScreening` / `connectImportRead` / `connectImportCommand`。
   - **已显式收窄旧 HTTP 转发代码**：仅允许 4 个文件流端点（`POST imports`、`GET imports/{id}/source`、`GET imports/{id}/errors`、`GET exports/{id}/download`）走流式 `fetch` + HMAC 签名，其余未匹配路径直接返回 `404`（同时移除旧 JSON 路径重复的 gzip 代码，保留 `screeningRPCResponse` 压缩逻辑）。

3. **清理 `apps/web/src/lib/auth-backend.ts`**
   - 已删除 `AUTH_TRANSPORT` 判断。
   - 命中 `authRPCRoutes`（20 个方法）一律走 `AuthService` Connect RPC；未命中的保留端点（登录/回调/注销/注册表单与 `registration/prepare`）继续透传 HTTP 到 `SELLER_API_URL`。

4. **清理配置与 CI 环境变量**
   - `compose.yaml`：已移除 6 个已失效的 `*_TRANSPORT` 环境变量传递。
   - `.github/workflows/dev-ci.yaml`：已移除浏览器验收 job 中显式传入的 6 个 `*_TRANSPORT: connect`（已成唯一路径）。
   - 已同步更新 `docs/deployment.md`、`docs/proto-migration-status.md`、`AGENTS.md`。

---

## 5. Phase 3：Go 后端内部收口（消除过渡期胶水层）

> **准入条件**：Phase 2 已稳定上线，且确认不再需要通过环境变量切回旧 HTTP 元数据接口。

### Step 3.1：下线 Go 后端已废弃的内部元数据 HTTP 路由

1. **电商模块（`apps/backend/internal/ecom/` & `apps/backend/main.go`）**
   - `apps/backend/main.go`：移除 `/api/v1/` 向 `ecommerce.Handler()` 的旧元数据路由挂载（保留 `ecom.RPCPrefix` 与按需保留 `GET /openapi.json`）。
   - `apps/backend/internal/ecom/http.go` & `modules.go`：移除旧 HTTP `route()` 与 `/api/v1/` Handler 分发逻辑。

2. **初筛模块（`apps/backend/internal/screening/`）**
   - `apps/backend/internal/screening/api.go`：`/api/v1/screening/` 的 HTTP `route()` 仅保留 4 个文件上传/下载端点（`upload`、`importDownload`、`exportDownload`），其余路径统一返回 `404`。
   - 保留 `verifyIdentity`（4 个文件流端点仍需 HMAC 签名验权）。

3. **认证服务（`apps/auth/`）**
   - `apps/auth/main.go`、`business.go`、`workspace.go`：移除已完全由 `AuthService` 接管的 20 个旧 JSON HTTP 路由注册（`/api/auth/session`、`/api/products*`、`/api/users*`、`/api/invitations*`、`/api/auth/registration/status` 等）。
   - 保留 `s.auth.Register(mux)` 中必要的浏览器 Cookie / OIDC / 注册表单与 `registration/prepare` 端点。

### Step 3.2：拆除 Connect 层的“伪 REST 字符串路径路由”

当前最核心的过渡态债务是：Connect Handler 拿到强类型 Proto 请求后，又退化成字符串路径 + `map[string]any` 分发（例如 `screening_connect.go` 中的 `s.screeningRPC(ctx, "GET", "products/"+req.Msg.GetId()+"/family", ...)`）。

收口改造（可按模块分 PR 执行）：

1. **`apps/backend/internal/screening/screening_connect.go`**
   - 将 37 个 RPC 方法逐批改为**直接调用对应具名 Go 用例函数**（如 `productsUsecase`、`summaryUsecase`、`mutateProduct`、`saveProfit`、`reviewWorkspaceUsecase` 等），参考 `import_connect.go` 中 `ListImports` 与 `import_mutations_connect.go` 中 `GetImportRemovalPreview` 的直调方式。
   - 删除 `screeningCommand`、`executeScreening` 整套伪 HTTP 路由分发器。
   - **幂等指纹兼容**：抽取独立的 `buildIdempotencyHash(method, canonicalPath, rawQuery, body)` 辅助函数，保持与既有 `ps_idempotency` 表内指纹算法一致，避免切流后重放失效。

2. **`apps/backend/internal/ecom/connect.go` & `modules.go`**
   - 将 14 个 RPC 方法改为直接调用具名函数（如 `listCategories`、`researchProducts`、`scoreProduct`、`generateListing`、`analyzeReviews`、`optimizeAds` 以及 `monitor` 拆出的 5 个具体方法），删除 `executeEcom` 和 `monitorUsecase` 里的字符串路径解析。

3. **`apps/auth/connect.go` & `business.go`**
   - 将 `productsUsecase`、`invitationsUsecase` 等按 HTTP Method 分支的复合函数拆分为清晰的具名方法（`ListProducts`、`CreateProduct`、`UpdateProduct`、`DeleteProduct` 等），由 `AuthService` RPC 方法直接调用。

### Step 3.3：测试与兼容投影收口

- **保留并强化**：
  - `apps/web/tests/rpc-coverage.test.mjs`（确保 50 个暴露路由 = 46 个 RPC + 4 个文件流，不遗漏也不退化）；
  - `apps/web/tests/rpc-compat.test.mjs` 与 `import-connect.test.mjs`；
  - Go 端 RPC 身份签名、过期、跨租户隔离、幂等冲突、最后管理员保护等安全回归测试。
- **清理冗余**：
  - 移除仅用于验证“旧 HTTP vs 新 Connect 投影一致”且对应旧 HTTP 已下线的对比测试代码。
- **规划 `legacy_fields` / `omitted_fields` 逐步退出**：
  - 盘点仍写入 `legacy_fields` 的字段，前端真正消费的字段按 Proto 演进规范（遵守 `buf breaking` FILE 级别，**只增新字段号、不复用/删改旧编号**）提升为具名字段，为后续移除 `packages/rpccompat` 创造条件。

---

## 6. 验证矩阵与每阶段退出标准

每个阶段合入/发布前，按影响范围执行以下检查并全部通过：

| 检查项 | 命令 / 方式 | 适用阶段 |
| :--- | :--- | :--- |
| Proto 契约与兼容检查 | `npm run proto:check && npm run proto:breaking` | Phase 2、Phase 3（涉及 proto 时） |
| Web RPC 覆盖与兼容单测 | `cd apps/web && npm run test:imports-connect` | Phase 2、Phase 3 |
| Web 类型、Lint 与生产构建 | `npm run typecheck && npm run lint && npm run build` | Phase 2 |
| Backend 并发与静态检查 | `cd apps/backend && go vet ./... && go test -race ./...`（使用独立 `SCREENING_TEST_DATABASE_URL` 与 `ECOM_TEST_REDIS_URL` db15） | Phase 3 |
| Auth 认证与集成测试 | `cd apps/auth && go test ./...`（使用独立 `TEST_DATABASE_URL`） | Phase 3 |
| 浏览器端到端验收 | `cd apps/backend && go test ./internal/ecom -count=1 -run '^TestBrowserScreening$' -v` | Phase 1、Phase 2、Phase 3 |
| 公网安全核验 | 验证公网请求 `/sellerops.screening.v1.ScreeningService/ListProducts` 返回 `404`；伪造/过期签名调用内部 RPC 返回 `401` | Phase 1、Phase 2 |

---

## 7. 关键风险与防踩坑提示

1. **`AUTH_TRANSPORT=connect` 忘记配 `SELLER_APP_ORIGIN`**：会导致所有鉴权 RPC 直接 `503`，切批次 D 前务必先检查 Web 环境变量。
2. **误删初筛文件流的签名与 30MB 限流逻辑**：清理 `apps/web/src/app/api/ecom/screening/[...path]/route.ts` 时，必须保留 `POST imports`（30MB + 64KB 限制、180s 超时）和 3 个下载端点的流式透传与 HMAC 签名。
3. **性能回退风险**：`screening` 商品列表与分组查询在 Connect 模式下依赖 `screeningRPCResponse` 的 gzip 压缩逻辑，清理 `route.ts` 时不可误删。
4. **跨协议幂等冲突**：Phase 3 拆掉 `executeScreening` 后，写接口的幂等 `RequestHash` 生成规则须保持一致（或在所有历史幂等记录过期后再简化）。
5. **先切流稳态，再做领域模型改造**：完成本方案 Phase 1–3 后，后端将不存在双轨入口与字符串路由转发，届时再推进 **`screening` 核心写路径去 `map[string]any`（强类型领域结构/值对象）+ 业务错误与 HTTP 状态码解耦**，改动面最小、收益最高。
