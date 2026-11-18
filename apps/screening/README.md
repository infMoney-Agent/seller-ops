# 产品初筛 Go 服务

产品初筛的目标后端为独立 Go 服务，不放入 `apps/auth`，也不再以 FastAPI/Celery 作为本模块的最终运行依赖。已有 Python 电商研究、监控等模块保留原状。

## 当前实现范围

本目录目前是迁移第一阶段，**尚未接管前端生产请求**。

已实现并验证：

- 独立 Go module、HTTP 进程、Worker 进程、迁移命令和容器构建文件。
- PostgreSQL 专用连接校验；禁止连接 `seller_ops`、`casdoor`、显式认证库或同地址同库的已有服务库。
- 与 Next.js 代理相同的身份 HMAC 格式，覆盖时间、方法、含查询参数的路径、用户、工作区和角色。
- 基础商品、月份、父体记录、复核历史、配置、任务查询。
- 单条人工复核、人工分类、主表参数修改，数据库事务、版本冲突与审计事件。
- 精确利润试算、保存来源月份和商品参数快照、公共模板权限控制。
- 修改操作的幂等回执与业务修改在同一事务提交。
- Go Worker 领取标记为 `go-v1` 的重筛任务；租约、心跳、过期恢复、最多五次尝试、断点进度和数据版本保护。
- 全新数据库建表，以及接管已有 `screening_0001` 至 `screening_0004` Alembic 版本。运行迁移不需要 Python。

迁移尚未完成的部分：

- XLSX/CSV/ZIP 导入、字段映射和分批入库。
- 批量复核的预览、父体范围锁定、引用样本校验和撤销。
- 导出、批量利润任务、规则启用/试运行、完整归并算法和筛选查询的行为对齐。
- Next.js 代理、Compose、发布流水线切换，以及完整浏览器流程验收。

这些接口不能指向旧 Python 服务作为隐式兜底。现有前端代理暂不切换，避免将不完整服务当成已完成迁移。

## 运行

需要 Go 1.25 和独立 PostgreSQL 业务库。环境变量：

```text
SCREENING_DATABASE_URL=postgresql://seller_screening:<password>@db:5432/seller_screening
SCREENING_INTERNAL_SECRET=<至少32字节、与Next代理一致>
SCREENING_STORAGE_DIR=/screening-data
SCREENING_ADDR=:8090
```

```sh
go build -o screening .
./screening migrate
./screening serve
# 单独进程运行
./screening worker
```

迁移只在部署步骤执行一次；API 和 Worker 不自动建表。迁移锁避免并发执行。迁移跟踪表 `screening_schema_versions` 属于基础设施元数据，九张 `ps_` 表属于业务模型。回滚应用时保留业务库和原始文件，不执行删表降级。

## 测试

```sh
go test -race ./...
go vet ./...
# 完整集成验证需要一个新建、空的 *_test 数据库
SCREENING_TEST_DATABASE_URL=postgresql://tester@127.0.0.1:5432/screening_ci_test go test -race -count=1 -v ./...
```

集成测试拒绝非测试命名和已有业务表的数据库，不清空数据；每次验证新建专用测试库。

## 规则兼容性

Go 正则使用标准库 RE2，避免回溯型表达式造成无限执行。旧规则中的后行断言、反向引用等不能直接迁入。当前移植覆盖电动/手动泵、未知泵类型、空容器和无源替换配件等边界；完整默认规则、方向归并及价格/期数筛选的兼容性仍需完成。不能声称当前已与旧规则完全等价。

## 性能约定

Go 适合统一技术栈和并发任务，但不预先承诺快于原实现。保留十万商品、五十万观察记录的分页 P95 验收要求；只有完成 SQL、索引及真实流程压测，才能报告性能结论。
