# Proto 导入历史只读试点

## 边界与结构

本轮仅将导入记录列表抽取为共享查询用例，加入内部 Connect RPC。认证、商品查询、复核写入、利润计算和文件上传下载继续使用原有接口；不引入 GORM、不新增数据库表、不改变进程和端口。

- 契约：proto/sellerops/common/v1 与 proto/sellerops/screening/v1。
- Go 生成物：gen/go，独立模块 seller-ops/contracts；业务后端通过本地 replace 引用。
- TS 生成物：apps/web/src/gen，浏览器不直接调用 RPC，由 Next.js 服务端使用生成客户端。
- 内部入口：POST /sellerops.screening.v1.ScreeningImportService/ListImports，与业务服务同一监听端口，不经过公网网关。

所有生成文件提交 Git，禁止手改。生成工具固定为 Buf 1.75.0、protoc-gen-go 1.36.6、protoc-gen-connect-go/Connect Go 1.18.1、Protobuf ES 2.17.0、Connect ES/Node 2.3.0。本地需要 Go 1.25 与 Node 22；Web 依赖使用 bun install --frozen-lockfile。

## 开发命令

在根目录执行 npm run proto:gen、npm run proto:lint、npm run proto:check、npm run proto:breaking。Go 不在 PATH 时设置 GO_BINARY 为 go 可执行文件的绝对路径。

生成脚本将固定版本 Go 插件装入忽略的 .cache/proto/bin，Buf 和 ES 插件来自 Web 锁定依赖。proto:check 同时检查已跟踪文件漂移及未跟踪的新生成文件。proto:breaking 默认比较 origin/dev，可通过 PROTO_BASE_REF 指定已获取的目标分支；仅目标分支尚未包含 proto/buf.yaml 的首次引入跳过兼容检查，其余 lint 和生成检查仍执行。采用 FILE 检查级别。

## 兼容与安全

浏览器继续 GET /api/ecom/screening/imports，返回 items、page、page_size、total，现有字段、空值和时间文本保持不变。RPC 使用独立的枚举、可缺失日期和 Timestamp；legacy_fields 是临时兼容扩展，仅保存未建模列和原有时间文本，不供业务逻辑判断。counts、mapping 为动态来源数据。RPC 的 total 为 int64，转换到旧 JSON 时检查 JS 安全整数范围。

Next.js 每次验证真实会话，签名 POST 和生成服务的过程路径，再由 Go 验证身份、角色、签名和时效。请求消息不包含可供客户端选择的公司或用户。查询只返回当前公司且未标记 data_deleted_at 的导入记录，以 created_at DESC、id DESC 稳定排序。分页默认 1/50，最多 100；Connect 适配的页码上限为 int32，超出返回 422。

新旧 Handler 共用纯 Go 查询用例，既有 SQL/事务和 pgx 保持原状。公网不新增 RPC 路由。Connect 错误带业务原因和请求 ID，由兼容层映射回旧 HTTP 错误。基础设施故障不泄露 SQL 或数据库凭据，不静默回退。

## 切换与回滚

SCREENING_IMPORTS_TRANSPORT 是 Next.js 服务端环境变量，允许 legacy 和 connect，默认 legacy。修改后须重启 Web 服务。

1. 先部署同时支持旧 HTTP 与新 RPC 的后端和 Web，保持 legacy。
2. 在隔离验收环境设置 connect，跑现有导入/删除/浏览器验收并比较两种接口。
3. 在运行环境切到 connect，核验导入历史、请求状态、Server-Timing 和原文件下载，观察错误率。
4. 回滚时设为 legacy 并重启 Web；无需回滚数据库，旧接口不删除。

容器部署可通过 compose 的 Web 环境传递该变量。业务后端镜像改为仓库根目录构建上下文，以包含共享 Go 契约模块。

## 验证

Go 集成测试只使用名称以 _test 结尾的独立 PostgreSQL，验证默认/上限分页、稳定顺序、删除过滤、双公司隔离、空列表、七种导入状态、日期空值和旧 JSON 投影一致性。RPC 身份测试验证未签名、过期签名和伪造公司头均被拒绝。TS 单测验证旧 JSON、计数为零、动态扩展和 bigint 安全转换。

CI 必须通过 Proto 生成/兼容检查、Go 回归、Web 类型/lint/生产构建；现有浏览器验收在 connect 模式运行，旧 HTTP 由新旧投影对比回归覆盖。

本轮隔离验收结果：Go race 全套回归、go vet、Web 类型检查与生产构建、TS 兼容单测、Proto lint/生成漂移检查均通过。Connect 模式下完整浏览器流程通过（29.52 秒）。首次引入时 origin/dev 尚无契约，兼容性检查按 bootstrap 规则跳过。

2026-11-25 运行环境验证：已先以 legacy 启动，再切到 connect；两种内部接口对当前 10 条导入历史的数量与 ID 顺序核对一致，公网入口返回正常登录跳转。runtime.json 已持久化 connect。发布构建保留原生环境现有认证页面，未升级认证服务；数据库无变更。备份位于原生运行目录 backups/proto-pilot-1795579585214。
