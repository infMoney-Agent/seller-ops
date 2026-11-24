# 商品库商标分类筛选

商品库使用“商标分类”替代“经营数据来源”筛选；经营记录仍默认各商品最新一期。

- 分类表示商品用途对应的尼斯商品类别建议，不表示品牌已注册的类别。
- 沿用旧选品资料库：01–34 类按标签中的“第XX类”匹配；多类商品可命中多个类别。“待核实”包含分类缺失或标签带“待核实”的商品。
- 分类选项数量按符合其他筛选的子体数统计；商品列表仍按同父体折叠。列表、汇总、导出统一复用服务端筛选。
- 分类存于 ps_products.specs.trademark_class，可保留 trademark_basis、trademark_source；兼容读取 specs.legacy_site_snapshot.p.trademark_class。
- 2026-11-24 从此前下载的云端选品库备份补齐 4556 个商品缺失的分类，保留已有分类。新导入商品若没有分类信息归入待核实，不根据宽泛亚马逊目录强行推断。
- GET /api/v1/screening/trademark-options 返回当前工作区在其他筛选下的选项；GET /products、GET /summary 接受 trademark=01…34 或 pending。Next 代理允许对应 GET 接口。所有查询保持 tenant_id 隔离。

验证：Go 分类参数与多类计数测试、Next 生产构建和类型检查、真实数据逐类列表/汇总计数一致性、空租户隔离。原始记录及人工结论、利润历史未修改。
