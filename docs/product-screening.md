# 产品初筛

新增入口：`/dashboard/ecom/screening`。功能为商品导入、规则筛选、同类归并、人工复核、FBA 假设利润测算与 XLSX 导出。原选品研究和 SQLite 功能保持独立。

## 服务和数据

初筛使用独立 PostgreSQL 数据库 `seller_screening`。Alembic 配置为 `apps/backend/alembic-screening.ini`，版本化建表 SQL 为 `apps/backend/app/screening/schema.sql`。现有认证数据库的用户、成员、工作区不迁移，也没有跨库外键。

业务表：`ps_jobs`、`ps_imports`、`ps_import_rows`、`ps_products`、`ps_policies`、`ps_screen_runs`、`ps_screen_results`、`ps_review_batches`、`ps_review_events`、`ps_profit_templates`、`ps_profit_scenarios`。额外的 `ps_requests` 保存写请求的请求哈希和响应，保证幂等键相同但内容不同返回 409。第二版迁移新增 `ps_products.current_group_rep_id`，将当前派生归并关系与历史运行快照分离。

所有业务引用包含 `tenant_id`。Next.js 每次向现有 Go 服务校验会话及当前工作区，再对时间戳、方法、路径、用户、工作区及角色签名。FastAPI 不信任浏览器身份头。服务间必须保持网络隔离，HMAC 密钥至少32字符，不向浏览器暴露。有效签名窗口60秒。

同工作区成员可导入、复核、测算、导出；公共规则和模板写入仅限所有者。工作区切换时清空当前页面选择，旧页面请求返回409。

原始文件与导出文件保存在 API、Worker 共享的 `screening-data` 卷。文件只通过有权限的下载接口获取。

## 部署前配置

在现有私有 Compose 环境文件中追加两个独立随机值，不要提交到 Git：

```text
SCREENING_DB_PASSWORD=<独立数据库密码，建议使用随机十六进制串>
SCREENING_INTERNAL_SECRET=<至少32字符的随机密钥>
```

应用会使用以下内部变量：

```text
SCREENING_DATABASE_URL=postgresql+asyncpg://seller_screening:密码@db:5432/seller_screening
SCREENING_STORAGE_DIR=/screening-data
SCREENING_INTERNAL_SECRET=与Web一致的密钥
```

`screening-db-init` 是独立一次性服务，既支持新 PostgreSQL 卷，也支持已有卷。它显式建库建账号，不依赖首次初始化脚本重跑。已有同名账号不会被静默重设密码；修改密码必须由运维显式轮换。

`screening-migrate` 在初始化完成后运行 `python -m alembic -c alembic-screening.ini upgrade head`。API、Worker、Beat 均等待迁移成功。迁移失败不会启动新版消费者。

上线前依次完成：配置检查 → 初始化与迁移 → 启动服务 → 健康检查 → 导入独立验收文件 → 验证复核、测算、导出。自动部署脚本已增加迁移服务镜像。合入 dev 会自动部署，因此必须先准备私有环境变量。

不应在本分支开发阶段直接改写现有线上库或环境文件。回滚使用上版镜像并保留数据库和文件卷，不执行删表降级；Alembic 降级明确拒绝破坏性操作。

## 导入与复核

支持 XLSX、UTF-8/GB18030 CSV、含上述文件的 ZIP。单文件30MB、展开250MB、100个成员、10万数据行，不支持嵌套ZIP或加密ZIP。默认前20行查找 ASIN 表头。映射为标准字段到原始列名，如 `{"title":"商品标题"}`。

先解析预览，再确认入库。观察日期在确认前必填，来源期用于跨期统计。重复识别除文件哈希、站点、来源期、映射外，还包括观察日期，防止相同字节文件在不同观察日期被误当成同一观察。原始文件、ZIP成员、工作表与行号可追溯。无效行保留并可下载报告。ZIP成员解析失败会留下错误记录，不会静默丢弃。

当前来源按观察日期、导入时间、成员路径、工作表、行号和ID稳定排序。重复ASIN仅更新最新商品字段，不更新人工结论。机器与人工结果独立；待确认不会归入保留。

通用筛选策略、方向和用途/材质/结构归并规则迁自现有资料库。去掉历史 ASIN 覆盖及历史探索推荐。缺少卖家身份时不跨 ASIN 归并。人工样本仅供参考，不自动批量采纳。

批量预览最多100个，15分钟有效。提交时校验目标数据及人工版本；样本变化整批要求重新预览，目标变化逐项跳过。已复核商品默认不覆盖。撤销仅恢复之后未修改的商品，每次操作均写入不可删除事件。

任务状态保存在PostgreSQL。Beat每5秒扫描待投递及租约超时任务，Celery只传输工作区与任务ID。任务按500行导入、100个商品筛选更新进度，幂等断点恢复。失败任务在页面显示详情并支持重试。

## 利润公式

金额使用Decimal，最后一步按HALF_UP舍入。汇率表示1美元对应人民币。费用不使用隐含零值，未知字段返回待补充；明确无费用请输入0。模板只保存用户录入的假设，不内置实时Amazon费率。

```text
P = 售价USD - 优惠USD
C = (采购CNY + 包装CNY + 头程CNY + 其他落地CNY) / 汇率
A = 退货率 × 每次退货净损失USD
F = C + FBA费 + 仓储费 + 其他USD + A
单件利润 = P × (1 - 佣金率 - 广告费率) - F
利润率 = 单件利润 / P
采购资金回报率 = 单件利润 / C
月利润 = 单件利润 × 预计月销量
保本净售价 = F / (1 - 佣金率 - 广告费率)
保本广告费率 = 1 - 佣金率 - F / P
```

每次退货净损失指相对于正常订单的净减少额，综合退款、手续费、逆向物流及可回收价值，避免重复扣采购成本。采购成本为零时不计算回报率；费率分母非正时不返回保本售价。结果是输入假设下的估计，与AI选品评分无关。

输入输出、数据版本、公式版本、模板快照均保存。数据或模板更新后历史方案提示过期，重新测算创建新快照。导出包含机器/人工结论、利润结果及来源记录，文本防Excel公式注入。

## API 与验证

浏览器前缀 `/api/ecom/screening`，内部前缀 `/api/v1/screening`。完整逐项契约由FastAPI OpenAPI生成。新增任务重试接口 `POST /jobs/{id}/retry`。

写请求需要 `Idempotency-Key`；JSON上限256KB，上传文件上限30MB。列表默认50、最大100。异步接口返回202和任务ID，前端轮询任务状态。错误返回code、message和request_id。

```bash
cd apps/backend
export SCREENING_TEST_DATABASE_URL=postgresql+asyncpg://测试账号:密码@127.0.0.1:55432/screening_test
SCREENING_DATABASE_URL="$SCREENING_TEST_DATABASE_URL" python -m alembic -c alembic-screening.ini upgrade head
python -m pytest tests/test_screening.py -q
```

测试数据库必须与业务库独立。CI独立启动PostgreSQL和Redis，在测试前应用迁移。Web运行类型检查、局部lint及生产构建。上线需重点查看队列积压、任务错误、请求耗时、数据库磁盘和共享文件卷容量。
