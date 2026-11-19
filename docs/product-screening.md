# 产品初筛

产品初筛后端使用 Go，前端沿用 Next.js、React Query 和 TanStack Table。页面位于 `/dashboard/ecom/screening`，流程为：导入商品 → 规则初筛与归并 → 人工复核 → FBA 利润测算 → 导出。

整个业务后端已迁为 Go。原五个模块位于 `apps/backend/internal/ecom`，初筛位于 `apps/backend/internal/screening`；两者在 `apps/backend` 同一进程运行，用户管理仍位于 `apps/auth`。不需要 Python/FastAPI/Celery/Alembic 运行环境。

## 数据模型

使用独立 PostgreSQL 业务库 `seller_screening`，与用户管理库分开。保留九张业务表：

| 分类 | 表 | 作用 |
| --- | --- | --- |
| 核心 | `ps_products` | 子 ASIN 主表：基础信息、卖家、父 ASIN、唯一参数、当前机器和人工结果 |
| 核心 | `ps_product_monthly` | 商品按来源及周期保存价格、销量、销售额、评论等指标 |
| 核心 | `ps_profit_scenarios` | 绑定商品和月份的多方案测算快照 |
| 核心 | `ps_parent_monthly` | 来源明确报告的父体汇总，不复制到各子体 |
| 辅助 | `ps_imports`、`ps_import_rows` | 原文件、批次、字段映射、原始行、标准化结果和错误 |
| 辅助 | `ps_configs` | 通过 kind 区分规则和公共利润模板，保留版本 |
| 辅助 | `ps_jobs` | 异步任务、批量预览/执行/撤销结果、幂等回执 |
| 辅助 | `ps_review_events` | 人工操作前后快照、版本、操作人和审计 |

材质、尺寸、重量及包装数量直接放商品主表。同父体用于关联复核和归类；销量、价格和利润仍区分各子 ASIN。详细字段、索引和历史迁移规则见 [数据库设计](product-screening-database.md) 与 [DDL快照](product-screening-schema.sql)。

## 页面与行为

商品库、导入记录、批量复核、利润测算、规则与历史、父体数据六个页签。支持来源、月份/周期、类目、卖家地区、机器/人工结果、方向、参考样本和利润区间筛选。分页默认50，最多100；查询先在服务端筛选、排序和归并，再分页。

导入支持 XLSX、CSV、ZIP。上传最多30MB，展开最多250MB，最多100个文件、10万行，禁止嵌套ZIP。确认前提供观察日期和统计周期；保留原始文件、子文件、工作表和行号。多次导入保留来源历史，补录旧月份不覆盖当前新月份，人工结论与利润快照保留。

销量口径区分 child、parent、unknown。明确子体指标可用于子体销量；父体指标独立保存；未知口径保留来源值，不静默加入合计。自然月、滚动30天和自定义周期分别保存，避免混用。

规则配置不可变版本保留，所有者可新建、试运行和启用。启用与重筛任务创建在同一事务完成。普通表达式使用 Go 标准正则，复杂断言使用有执行超时的 Go 正则引擎。归并采用保守的用途、材质和结构标题归一，缺少有效卖家身份时不跨 ASIN 归并。

单条复核支持保留、排除和待确认；后两者必须填写原因。批量最多100个，默认只处理待复核商品；人工归类不改变复核结论。明确选择同父体范围或人工参考样本，生成15分钟预览后执行。提交校验来源版本、人工版本、父体锚点和样本版本；冲突目标跳过，样本变化要求重做预览。撤销只恢复之后未被再次人工修改的记录。

## 利润

采购、包装、头程及其他落地成本输入人民币；销售和平台费用输入美元。汇率、费率、FBA费用均由用户提供，必要费用缺失返回待补充，不当作零。参数供核对费用和保存测算依据使用，不按包装件数重复乘成本。

```text
P = 售价 - 单件优惠
C = (采购 + 包装 + 头程 + 其他人民币成本) / 汇率
F = C + FBA费用 + 仓储费 + 其他美元费用 + 退货率 × 每次退货净损失
单件利润 = P × (1 - 佣金率 - 广告费率) - F
销售利润率 = 单件利润 / P
采购资金回报率 = 单件利润 / C
月利润 = 单件利润 × 预计月销量
保本净售价 = F / (1 - 佣金率 - 广告费率)
保本广告费率 = 1 - 佣金率 - F / P
```

Go 使用精确有理数运算，最终结果才舍入。售价、汇率必须大于零，比例在0到1之间，费用非负；采购成本为零时不计算 ROI，保本分母非正时不返回保本售价。保存商品参数、来源月份、模板、输入、输出和公式版本快照，来源变化后提示过期。

## 接口

浏览器使用 `/api/ecom/screening/*`；Next.js 验证真实会话、工作区与 Origin，签名调用 Go `/api/v1/screening/*`。公共规则/模板写入限所有者，其余业务操作允许成员。

| 接口组 | 路径 |
| --- | --- |
| 查询 | summary、months、products、products/{id}、groups/{key}/products |
| 商品详情 | products/{id}/monthly、observations、history、references、family |
| 人工操作 | PATCH products/{id}/review、category、parameters |
| 导入 | POST/GET imports；详情、mapping、preview、commit、errors、source |
| 规则 | GET/POST policies；policies/{id}/test、activate；POST screen-runs |
| 批量复核 | review-batches/preview、review-batches、{id}/apply、{id}/undo |
| 利润 | profit/calculate、profit/templates、products/{id}/profit-scenarios、profit/batches |
| 任务与导出 | exports、jobs/{id}、jobs/{id}/retry、exports/{id}/download |
| 父体 | parents/monthly |

完整路径和请求结构见 [OpenAPI](screening-openapi.json)。修改请求带 Idempotency-Key；相同键不同内容返回409。普通JSON最多256KB；金额使用十进制定点字符串；列表返回 items/page/page_size/total。

## 后台任务和导出

PostgreSQL 保存队列状态，Go Worker 使用租约、心跳、重试、幂等回执和数据版本保护，无需 Celery/Redis 作为初筛队列。导入按500行事务提交。机器重筛不修改人工结论。

导出在任务开始执行时取数据库 Repeatable Read 一致性快照，使用流式 XLSX 写入商品、月份、来源、父体汇总及利润方案，文本处理 Excel 公式注入。文件放 API/Worker 共用的持久化卷。

## 部署与验收

Go 服务配置、旧任务接管、完整测试记录和回滚边界见 [Go 后端迁移说明](go-backend-migration.md)。生产库建库由独立初始化步骤完成，Go 迁移在 API/Worker 启动前单独运行。

本轮在功能分支交付，未合入触发自动部署的 dev。模型与 Amazon 实际凭据调用、镜像构建和完整容器部署尚未验收。

商品库默认显示各商品最新一期。价格 / 月销量列的“查看详细数据”按钮打开小卡片，按需调用 monthly?recent_months=12，以该商品最新数据月份为截止月份回溯 12 个月，包含截止月份；不填补缺失月份，不合并不同来源/周期。关闭卡片时卸载历史组件，列表初次加载不请求历史数据。
