# 卖家精灵导入字段与数据库设计

本轮增量迁移：007_import_columns.sql，Go 迁移器在同一事务内补齐已保存来源数据。业务库继续为 seller_screening，不访问用户管理数据库。

## 业务表

| 表 | 用途 |
|---|---|
| ps_products | 每个工作区、站点、子 ASIN 一行；名称、品牌、卖家、类目、尺寸、重量、包装等固定资料，父 ASIN 仅用于关联 |
| ps_product_monthly | 商品关联的月度或近30天经营数据，价格、销量、销售额、排名、评分、广告、优惠等；保留报表原始总销量与子体销量，不能相互替代 |
| ps_parent_monthly | 明确归属父体的销量与销售额，避免重复累加到每个子 ASIN |
| ps_profit_scenarios | 商品关联的利润副表；calculated 为用户输入成本后的测算快照，imported 为来源文件的 FBA、毛利率估算，按来源行唯一保存 |
| ps_imports / ps_import_rows | 文件、完整表头、来源月份、观察日期、原行数据与标准化快照；新增未知列仍保留在 raw_data 中，不能丢弃 |

现有任务、配置、人工历史表保持不变，不增加新的业务表或修改用户管理表。

## 新增结构化字段

| 来源列 / 含义 | 字段 | 保存位置 | 类型 |
|---|---|---|---|
| 商品详情页链接 | product_url | ps_products | text |
| 品牌链接 | brand_url | ps_products | text |
| 卖家首页 | seller_url | ps_products | text |
| 卖家信息 | seller_info | ps_products | text |
| SKU | sku | ps_products | text |
| 大类目 | main_category | ps_products | text |
| 类目路径 | category_path | ps_products | text |
| 上架时间 | listing_date | ps_products | text |
| 变体数 | variant_count | ps_products | bigint |
| 详细参数 | detail_parameters | ps_products | text |
| 商品尺寸 | product_dimensions_original | ps_products | text |
| 商品尺寸（单位换算） | product_dimensions | ps_products | text |
| 商品重量 | product_weight_original | ps_products | text |
| 商品重量（单位换算） | product_weight_converted | ps_products | text |
| 包装尺寸 | package_dimensions_original | ps_products | text |
| 包装尺寸（单位换算） | package_dimensions | ps_products | text |
| 包装重量 | package_weight_original | ps_products | text |
| 包装重量（单位换算） | package_weight_converted | ps_products | text |
| 包装尺寸分段 | package_size_tier | ps_products | text |
| 商品长(cm) | product_length_cm | ps_products | numeric(18,6) |
| 商品宽(cm) | product_width_cm | ps_products | numeric(18,6) |
| 商品高(cm) | product_height_cm | ps_products | numeric(18,6) |
| 小类BSR | small_bsr | ps_product_monthly | text |
| 大类BSR增长数 | bsr_change | ps_product_monthly | text |
| 大类BSR增长率 | bsr_growth_rate | ps_product_monthly | text |
| 销量同比增长率 | sales_yoy | ps_product_monthly | text |
| 销量环比增长率 | sales_mom | ps_product_monthly | text |
| prime价格($) | prime_price | ps_product_monthly | text |
| 买家运费($) | shipping_price | ps_product_monthly | text |
| 月新增评分数 | new_reviews | ps_product_monthly | text |
| 留评率 | review_rate | ps_product_monthly | text |
| 卖家数 | seller_count | ps_product_monthly | text |
| BuyBox类型 | buybox_type | ps_product_monthly | text |
| 配送方式 | fulfillment | ps_product_monthly | text |
| Coupon | coupon | ps_product_monthly | text |
| 秒杀 | deal | ps_product_monthly | text |
| SP广告 | sponsored_products | ps_product_monthly | text |
| 品牌广告 | sponsored_brands | ps_product_monthly | text |
| AC关键词 | ac_keywords | ps_product_monthly | text |
| Amazon's Choice | amazon_choice | ps_product_monthly | text |
| Best Seller标识 | best_seller | ps_product_monthly | text |
| New Release标识 | new_release | ps_product_monthly | text |
| A+页面 | aplus | ps_product_monthly | text |
| 品牌故事 | brand_story | ps_product_monthly | text |
| 视频介绍 | video | ps_product_monthly | text |
| LQS | lqs | ps_product_monthly | text |
| Q&A | qa_count | ps_product_monthly | text |
| 评级 | rating_level | ps_product_monthly | text |
| 上架天数 | listing_days | ps_product_monthly | text |
| FBA($) | imported_fba_fee_usd | ps_profit_scenarios | numeric(18,6) |
| 毛利率 | imported_gross_margin_rate | ps_profit_scenarios | numeric(18,6) |
| 报表原始月销量 | source_monthly_sales | ps_product_monthly | bigint |
| 报表原始月销售额 | source_monthly_revenue | ps_product_monthly | numeric(18,6) |

另将“大类BSR”对应到已有 bsr_rank。ASIN、标题、父 ASIN、品牌、卖家、价格、销量、评分等既有字段继续使用已有列。销量归属与商品层级是导入识别字段，保留在来源行中。

## 单位与利润

- 优先读取带单位换算的商品/包装尺寸和重量，缺失时依据明确单位转换原始英寸、磅、盎司。没有单位不猜测；原文仍保存在主表字段及来源行。
- 长宽高统一厘米、重量统一克，使用有理数计算，保留六位小数。商品库“尺寸 / 重量”一列按商品尺寸、净重、包装尺寸、毛重四行展示。
- 卖家独立成列，显示名称、所属地、有提供时的 ID；子体展开复用主列表列定义。
- FBA 与毛利率进入利润副表的 imported_fba_fee_usd、imported_gross_margin_rate；70% 保存为 0.700000。来源估算不包含采购与推广等假设，不能作为自行测算利润，列表利润查询只选择 calculated。
- 导入利润保留商品、经营期和来源行关联及快照，重复任务不重复写入。历史计算方案不覆盖。

## 历史补齐与兼容

- 先备份业务库，再运行 seller-backend migrate，成功后启动版本7服务。
- 读取既有原始来源补齐新字段。主表只补空值，保留人工参数、人工结论和原始观察；新增参数时增加数据版本，已保存利润方案保持原快照并提示过期。
- 每500条读取一批，迁移事务失败整体回滚；重复执行迁移不重复补齐。
- 映射页使用整份文件的表头，并显示字段保存位置。未提供的可选字段收进独立展开区，不再显示“当前页未匹配”。
- 回滚保留新增列及来源文件；降级旧版前需显式处理启动版本检查，不能直接声称版本6旧程序可读取版本7。数据库备份作为恢复依据，不自动删列。

## Modern Web Guidance

- forms / content-first-noise-reduction：可选缺失字段不当作错误；使用原生 details 按需展示，不扩大默认界面噪声。
- responsive-table：复用带名称、tabindex、role=region 的 TableScroll、语义表头与横向滚动。按用户要求在单元格分行，不把父子商品改成嵌套小表；为保留跨商品对照，不切换成窄屏卡片布局。

## 验证

- 有单位的尺寸/重量转换、未知单位不猜测、负毛利率与非法来源费用。
- 真实 PostgreSQL 的版本6→7升级、重复迁移、人工参数/结论保留、原始来源不可变、新月份字段写入、来源利润幂等与计算结果隔离。
- Web 类型检查、定向 lint、生产构建；隔离浏览器上传、映射重解析、确认导入与父体展开，核对新增尺寸重量和卖家列。
