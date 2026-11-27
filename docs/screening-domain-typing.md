# 初筛领域强类型化与内部 RPC 性能收口说明（v1 & v2）

本文记录在完成 Connect RPC Phase 1–3 全量切换与旧元数据 HTTP 路由清理后，针对后端领域模型（v1 / v2）与 Web BFF 内部通信链路的持续优化演进。

## 1. 已完成范围

### 1.1 初筛领域错误与传输状态码彻底解耦（v1）
- [`errors.go`](../apps/backend/internal/screening/errors.go) 定义语义化 `errorKind` 与 `domainError`，彻底移除原业务层直接绑定 HTTP 数字状态码的 `apiError` 与 `fail(<status>, msg)`。
- 仅在 Connect RPC（[`import_connect.go`](../apps/backend/internal/screening/import_connect.go) `rpcError`）、4 个文件流 HTTP（[`http.go`](../apps/backend/internal/screening/http.go) `respond`）与异步任务（[`jobs.go`](../apps/backend/internal/screening/jobs.go) `workError`）边界完成协议映射。

### 1.2 核心写路径、复核批次、利润模型与后台任务全量强类型化（v1 & v2）
- **商品与复核批次命令/快照**（[`domain_commands.go`](../apps/backend/internal/screening/domain_commands.go)、[`writes.go`](../apps/backend/internal/screening/writes.go)、[`review_batches.go`](../apps/backend/internal/screening/review_batches.go)、[`policies.go`](../apps/backend/internal/screening/policies.go)）：
  - 具名方法 `reviewProduct`、`updateProductCategory`、`updateProductParameters`、`calculateProfit`、`saveProfit`、`createConfig`、`testPolicy`、`activatePolicy`、`previewBatch`、`applyBatch` 全部采用显式 Command 与结果结构体。
- **利润测算领域模型结构化**（[`profit_model.go`](../apps/backend/internal/screening/profit_model.go)、[`profit.go`](../apps/backend/internal/screening/profit.go)、[`profit_freight.go`](../apps/backend/internal/screening/profit_freight.go)、[`profit_referral.go`](../apps/backend/internal/screening/profit_referral.go)、[`profit_storage.go`](../apps/backend/internal/screening/profit_storage.go)）：
  - 新增 `ProfitInputs`、`ProductProfitFacts`、`StorageResolution`、`ReferralResolution` 与 `ProfitOutput`，在完整保持 `big.Rat` 精确有理数计算公式与错误文案不变的前提下，消除成本、仓租、佣金、头程计算内部的无类型 map 读写。
- **导入周期与 5 类后台任务 Payload 强类型化**（[`job_models.go`](../apps/backend/internal/screening/job_models.go)、[`jobs.go`](../apps/backend/internal/screening/jobs.go)、[`exports.go`](../apps/backend/internal/screening/exports.go)、[`import_tasks.go`](../apps/backend/internal/screening/import_tasks.go)、[`imports.go`](../apps/backend/internal/screening/imports.go)）：
  - 新增 `ProductSelectionCommand`、`EnqueueScreenCommand`、`QueueProfitBatchCommand`、`ImportPeriodSpec`、`JobSubmission` 以及 `ParseJobPayload`、`ImportJobPayload`、`ExportJobPayload`、`ProfitBatchJobPayload`、`ScreenJobPayload`，消除任务调度与 Worker 执行中的 `.payload.(map[string]any)` 强转。

### 1.3 Web BFF 单例连接池、Protobuf 二进制传输与热路径直出（P1 & P3）
- **单例 Transport 与二进制协议**：新增 [`connect-transport.ts`](../apps/web/src/lib/connect-transport.ts)，`AuthService`、`EcomService`、`ScreeningService`、`ScreeningImportService` 统一复用单例 `Transport` / `Client` 并启用 `useBinaryFormat: true`（`application/proto`）。
- **高频 RPC 绕过 `rpccompat` 双重 JSON 反射**：[`apps/auth/workspace.go`](../apps/auth/workspace.go) 的 `AuthService/GetSession` 与 [`apps/backend/internal/ecom/connect.go`](../apps/backend/internal/ecom/connect.go) 的高频接口直接构造生成的 Proto 响应结构。
- **BFF 会话守卫收敛与目录归位**：新增 [`workspace-session.ts`](../apps/web/src/lib/workspace-session.ts) 统一处理 BFF 会话、租户绑定与工作区切换校验；将电商 RPC 客户端归位至 `apps/web/src/features/ecom/`；并在 [`next.config.ts`](../apps/web/next.config.ts) 消除服务端构建时 `@opentelemetry/instrumentation` 的无关动态依赖告警。

## 2. 长期演进项（P4 / v3）

- **Proto `legacy_fields` 渐进瘦身**：按前端实际消费字段盘点列表，遵循 `buf breaking`（`FILE` 级别，只增新字段号、不删改旧编号）分批提升至具名 Proto 字段，逐步收窄 `packages/rpccompat` 的使用范围。
