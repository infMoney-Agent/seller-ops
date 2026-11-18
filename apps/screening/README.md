# Go 产品初筛服务

独立 Go 1.25 服务。与 `apps/auth` 用户管理、`apps/ecom` 五个电商模块分开部署；初筛拥有独立 PostgreSQL 业务库和文件卷。

```text
SCREENING_DATABASE_URL=postgresql://seller_screening:<password>@db:5432/seller_screening
SCREENING_INTERNAL_SECRET=<至少32字节，与Next代理一致>
SCREENING_STORAGE_DIR=/screening-data
SCREENING_ADDR=:8090
```

```sh
go build -o screening .
./screening migrate
./screening serve
# 单独进程
./screening worker
```

迁移只在部署步骤执行一次。API/Worker 不自动建表。`migrations/001` 至 `005` 内嵌在 Go 二进制中，兼容已有 Alembic 版本，运行不依赖 Python。

支持商品和分月数据、父体汇总、XLSX/CSV/ZIP 导入、规则版本和重筛、单条/批量复核与条件撤销、参数修改、精确利润快照和 XLSX 导出。九表设计见 [数据库设计](../../docs/product-screening-database.md)，完整接口契约见 [OpenAPI](../../docs/screening-openapi.json)。

后台任务直接由 PostgreSQL 持久化。Worker 使用租约、心跳和版本保护，接管失败或超时任务。批量复核预览也保存在任务表中，人工修改保留独立事件历史。

规则使用标准 Go 正则处理普通表达式，含前后断言等表达式由 Go `regexp2` 执行并设置50毫秒超时。归并采用用途/材质与保留结构词的标题精确归一，并要求相同有效卖家身份；未知卖家保持逐 ASIN 独立。与旧版某些更积极的类目结构归并相比，此实现更保守，可能展示更多待复核行。

```sh
go test -race ./...
go vet ./...
# 使用新建、空的 *_test 数据库；测试不会先清库
SCREENING_TEST_DATABASE_URL=postgresql://tester@127.0.0.1:5432/screening_ci_test go test -race -count=1 -v ./...
```

性能验收可显式使用已生成的十万商品/五十万观察合成数据，通过 `SCREENING_BENCH_DATABASE_URL` 运行 `TestLargeCatalogPagination`。浏览器夹具只编译进测试二进制，不存在于生产可执行文件中。

部署切换、验证结果和限制见 [Go 后端迁移说明](../../docs/go-backend-migration.md)。本轮没有执行生产部署。
