# 初筛（Screening）领域错误解耦与核心写路径强类型化说明（v1）

本文记录在完成 Connect RPC Phase 1–3 全量切换与旧元数据 HTTP 路由清理后，针对 `apps/backend/internal/screening` 落地的第一版领域模型演进（方向 A v1），以及后续 v2/v3 的推进边界。

## 1. v1 已完成范围

### 1.1 领域错误语义与传输状态码彻底解耦
- 新增 [`errors.go`](../apps/backend/internal/screening/errors.go)，以语义化 `errorKind`（`UNAUTHENTICATED`、`PERMISSION_DENIED`、`NOT_FOUND`、`CONFLICT`、`PAYLOAD_TOO_LARGE`、`INVALID_ARGUMENT`、`INTERNAL`）和 `domainError` 替代原业务层直接携带 HTTP 数字状态码的 `apiError` 与 `fail(<httpStatus>, msg)`。
- 仅在传输与任务边界完成协议映射：
  - Connect RPC 边界（[`import_connect.go`](../apps/backend/internal/screening/import_connect.go) `rpcError`）：将 `errorKind` 映射为 `connect.Code*` 与 `common.v1.ErrorDetail.reason`。
  - 保留的 4 个文件流 HTTP 边界（[`http.go`](../apps/backend/internal/screening/http.go) `respond`）：通过 `httpStatusForKind` 映射回既有 HTTP 状态码，外部错误结构保持不变。
  - 异步任务 worker 边界（[`jobs.go`](../apps/backend/internal/screening/jobs.go) `workError` 与 [`rules.go`](../apps/backend/internal/screening/rules.go) 正则超时保护）：识别 `domainError` 并记录安全错误摘要。

### 1.2 核心写路径与复核批次去 `map[string]any`（强类型命令与快照）
- 新增 [`domain_commands.go`](../apps/backend/internal/screening/domain_commands.go)，显式建模核心写操作的值对象、输入命令与返回结构：
  - 值对象枚举：`ReviewDecision`（`kept`/`excluded`/`unsure`）、`BatchScope`（`selected`/`family`/`selected_families`）、`BatchAction`（`review`/`categorize`）、`BatchMode`（`direct`/`reference`）、`ConfigKind`（`screening`/`profit_template`）。
  - 商品写命令：将原复合 `mutateProduct(..., action, b Object)` 拆分为具名强类型方法 `reviewProduct`、`updateProductCategory`、`updateProductParameters`（[`writes.go`](../apps/backend/internal/screening/writes.go)），通过 `ParameterPatch` 精确表达“未传入 / 显式置空 `NULL` / 文本或数值更新”，并完整保留 `404 -> 409（乐观锁版本） -> 422（参数校验）` 的错误优先级。
  - 利润试算、落库与公共配置：`calculateProfit`、`saveProfit`、`createConfig`、`testPolicy`、`activatePolicy`（[`writes.go`](../apps/backend/internal/screening/writes.go)、[`policies.go`](../apps/backend/internal/screening/policies.go)）全部改为接收具名 Command 结构并返回强类型结果结构（`*ProfitCalculationResult`、`*SavedProfitScenario`、`*ConfigRecord`、`*PolicyTestResult`）。
  - 复核批次预览、执行与条件撤销：[`review_batches.go`](../apps/backend/internal/screening/review_batches.go) 与 [`review_families.go`](../apps/backend/internal/screening/review_families.go) 引入 `PreviewBatchCommand`、`BatchAnchorSnapshot`、`BatchReferenceSnapshot`、`BatchTargetSnapshot`、`ReviewBatchPayload` 与 `BatchApplyResult`，消除多层 `.(map[string]any)` / `.([]any)` 断言，同时保持 `ps_jobs` JSONB 快照结构与审计事件完全兼容。
- [`idempotency.go`](../apps/backend/internal/screening/idempotency.go) 新增泛型 `typedReceipt[T]`，并保留 Connect 入口的 `buildIdempotencyHash` 请求指纹计算规则，历史幂等回执零破坏。

### 1.3 残留死 HTTP 包装清理
- 清理 `screening` 查询模块中不再被路由使用的 9 个 `*http.Request` 包装函数（`products`、`summary`、`monthlyList`、`profitScenarios`、`family`、`trademarkOptions`、`profitProducts`、`reviewWorkspace`、`reviewCategories`）及 `reviewCandidateQuery`，相关测试统一直接调用纯 Go `*Usecase` 方法。
- 清理 `apps/auth/internal/auth/service.go` 中无调用方的 `Authenticate` HTTP 包装与 `browser_fixture_test.go` 中冗余的旧 `GET /api/auth/session` 挂载。

## 2. 不变约束

- 浏览器对外 HTTP/JSON 路径、状态码、字段名与空值语义不变。
- Proto 契约满足 `buf breaking`（`FILE` 级别），不手改生成物。
- PostgreSQL 表结构、`big.Rat` 利润公式（[`profit.go`](../apps/backend/internal/screening/profit.go)）、人工结论防覆盖、乐观锁版本校验及多租户隔离保持不变。

## 3. 后续路线图（v2 / v3）

1. **v2：导入/导出与筛选任务写路径强类型化 + `ProfitInputs` 结构化**
   - 将 `imports.go`、`import_tasks.go`、`exports.go`、`jobs.go` 中剩余的 `editImport`、`queueExport`、`queueProfit`、`enqueueScreen` 请求与任务 `payload` 收敛为显式 Go 结构体。
   - 在保持 `profit.go` `big.Rat` 精确十进制计算与错误文案完全不变的前提下，将 `ProfitInputs` / `ProfitOutput` 从 `Object` 提炼为显式结构体。
2. **v3：Proto `legacy_fields` 渐进瘦身**
   - 盘点前端实际消费的 `legacy_fields` 字段，按 Proto 演进规范（只增新字段号、不删改旧编号）逐步提升为具名 Proto 字段，减少 `rpccompat` 动态 `Struct` 转换开销。
