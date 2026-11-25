# 分组复核中的父体折叠

候选商品沿用商品库的站点和明确父 ASIN 归并规则；缺少父 ASIN 的商品独立展示。先对整个筛选结果按父体归并，再按每页 50 个父体或独立商品分页。代表选择沿用商品库的最新一期、同来源口径子体销量排序，不把父体销量累加成子体销量。

GET /api/ecom/screening/review-workspace 返回 group.products（子体总数）、group.display_products（折叠后的商品数），items 中包含 family_count。展开时携带 family_product_id（代表商品 UUID）和 member_page；服务端验证租户，并保留类目、筛选和显式已选商品范围，每页最多 50 个子体。

折叠不合并人工结论。全选处理当前展示的代表和已展开页中的待复核子体；已复核商品不覆盖。批量弹窗提交明确勾选的商品 ID，未展示且未勾选的兄弟子体不会自动被复核。
