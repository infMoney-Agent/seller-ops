# 产品初筛数据库设计：商品主表 + 月度经营副表

本设计取代初版把价格、销量、评分放入商品主表的方案。初筛模块共13张表；其中商品业务核心是下面两张表，导入、机器结果、人工操作和利润方案作为独立辅助表保留。

## 1. 商品主表 ps_products

每个工作区、站点、ASIN只有一个商品档案。

| 字段组 | 字段 |
| --- | --- |
| 身份 | tenant_id、id、marketplace、asin |
| 基础档案 | title、title_zh、brand、category、image_url、specs |
| 当前卖家 | seller_id、seller_name、seller_region |
| 人工结论 | review_status、review_reason、review_note、reviewed_by、reviewed_at |
| 当前引用 | current_row_id、current_monthly_id、current_monthly_revision、current_result_id、current_group_rep_id |
| 并发与时间 | data_revision、review_revision、screen_generation、created_at、updated_at |

主键为 `(tenant_id,id)`，业务唯一键为 `(tenant_id,marketplace,asin)`。**主表不再包含价格、月销量、评分数或评分字段**，也不为每个月复制一份标题和卖家。

`current_row_id`指向最新商品档案的来源；`current_monthly_id`指向最新统计周期的经营数据。两个引用独立：补录旧月份可以增加历史数据，但不能把最新月份的价格和销量替换成旧月份。历史卖家信息仍可从不可变原始导入行追溯，主表展示当前卖家。

## 2. 月度副表 ps_product_monthly

同一个商品可以有多条经营记录，关联商品主表的 `(tenant_id,product_id)`。

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| tenant_id、id、product_id | bigint、uuid、uuid | 工作区、记录ID、商品ID |
| stat_month | date | 统计归属月份，以当月第一天表示 |
| period_type | text | calendar_month自然月、rolling_30d滚动30天、custom自定义 |
| period_start、period_end | date | 实际统计起止日期 |
| observed_on | date | 数据观察日期，区别于上传日期 |
| source_type | varchar(50) | sellersprite、manual、other或其他明确来源 |
| source_row_id | uuid | 原始导入行 |
| price_usd | numeric(18,6) | 该期观察价格，不表示月均价 |
| monthly_sales | bigint | 来源报告中的该期销量，不做跨来源累加 |
| monthly_revenue | numeric(18,6) | 该期销售额，缺失保持NULL，不由售价推算 |
| review_count、rating | bigint、numeric(3,2) | 观察时的评分数和评分 |
| bsr_rank | bigint | 观察时的排名 |
| currency | char(3) | 一期固定USD，对应美国站 |
| revision | bigint | 同一周期数据被更新时递增 |
| created_at、updated_at | timestamptz | 建立与更新时间 |

`stat_month`等于`period_end`所属月份的第一天。自然月必须完整覆盖该月；滚动30天必须恰好30天。不同滚动窗口即使属于同一个月份，也分别保存。

唯一约束：

```text
(tenant_id, product_id, source_type, period_type, period_start, period_end)
```

查询索引：

```text
(tenant_id, product_id, stat_month DESC, period_end DESC, observed_on DESC)
(tenant_id, stat_month, monthly_sales DESC)
(tenant_id, source_row_id)
(tenant_id, stat_month, period_type, product_id, period_end DESC, observed_on DESC, updated_at DESC, id DESC)
```

以ASIN查月份时先走主表的站点+ASIN唯一索引，再使用副表商品+月份索引，不需要在副表重复保存可变商品档案。

```sql
SELECT p.asin,p.title,p.seller_name,m.stat_month,m.price_usd,m.monthly_sales
FROM ps_products p
JOIN ps_product_monthly m ON m.tenant_id=p.tenant_id AND m.product_id=p.id
WHERE p.tenant_id=:workspace AND p.marketplace='US'
  AND p.asin=:asin AND m.stat_month=:month;
```

## 3. 写入与更新规则

1. 上传文件后，原始行进入`ps_import_rows`，保留ZIP成员、工作表、行号、raw_data与normalized_data。无效行也保留。
2. 确认导入时按工作区+站点+ASIN查找或建立商品档案。
3. 按商品、来源、口径、起止日期查找月度记录。不同月份或不同来源新增记录，不覆盖已有其他周期。
4. 同一周期重复导入，按观察日期、导入时间、成员路径、工作表、原行号和ID排序，较新的观察更新月度记录并增加revision。
5. 原始导入行保持不变，因此月度有效值更新后，旧值仍可追溯。
6. 默认经营数据按月份、周期结束日期、观察日期、更新时间、ID降序选择。补录旧月份不会抢占最新统计周期。
7. 商品基础资料更新按来源观察日期排序；人工结论、人工原因和备注不参与导入更新。

最新月份数据变化时增加商品data_revision；仅修改历史月度记录时使用月度revision检测变化。人工结论继续由review_revision保护。

## 4. 查询、复核与利润

列表及统计支持`stat_month=2026-10`、`period_type=calendar_month`、`source_type=sellersprite`，不传时采用各商品最新一期。查询某月份未指定来源时选择该周期观察日期较新的记录；商品历史页完整展示各来源记录，不把多来源销量相加。

- `GET /months`：工作区可选择的月份、口径和来源。
- `GET /products/{id}/monthly`：分页获取该商品所有周期及原始来源位置。
- 商品列表和详情继续返回price_usd、monthly_sales等兼容字段，但值来自副表联查。
- 人工复核仍为商品级；机器结果按当前最新资料计算，切换历史月份不伪造历史机器结论。
- 利润请求新增`monthly_id`和`expected_monthly_revision`。保存时记录月度ID、版本及monthly_snapshot。历史月份变化可以令该方案过期，同时输入与原结果保持不变。
- 批量利润按当前列表选择的月份/来源冻结月度记录及版本。
- Excel增加“月度数据”工作表，导出各期记录；商品页导出字段采用所选期数据。

金额在PostgreSQL中使用numeric，后端计算使用Decimal，API金额返回十进制字符串。

## 5. 兼容迁移

新增Alembic版本`screening_0003`，完整DDL位于`apps/backend/app/screening/monthly-schema.sql`。必须依次执行0001、0002、0003，不能仅拿初版schema.sql作为最终结构。

迁移先建立副表并从所有已确认导入行回填历史，再建立当前引用、利润与机器结果的月度引用，最后删除主表经营字段。如果新选定的最新统计周期与旧主表的来源或经营值不同，增加商品数据版本并清空过期的当前机器结果，人工结论不变。原始文件、导入行、人工事件和利润输入输出不会删除。对无法可靠定位的旧利润方案不猜测月份，保持引用NULL及原快照。

旧导入来源统一标记`legacy_import`，避免未经证据把历史来源认定为卖家精灵。有效YYYY-MM来源期回填自然月；明确Last-30-days/滚动30天回填对应窗口；其他旧来源按观察日保存为custom，保留原来源期文字。

初筛、人工复核、利润和任务辅助表继续保留。数据库仍独立于现有认证库和电商SQLite，跨工作区外键包含tenant_id。上线前备份、执行一次性迁移，回滚应用时保留新表及历史数据，不执行破坏性降级。
