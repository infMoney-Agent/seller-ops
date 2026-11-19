// Standard fields and supported aliases shared with the Go importer.
export const importFields = [
  {
    key: 'asin',
    label: 'ASIN',
    aliases: ['ASIN', 'asin'],
    storage: '商品主表'
  },
  {
    key: 'title',
    label: '商品标题',
    aliases: ['商品标题', 'Title', 'title'],
    storage: '商品主表'
  },
  {
    key: 'title_zh',
    label: '商品标题中文（机器翻译）',
    aliases: ['商品标题中文（机器翻译）', '中文标题', '商品标题中文'],
    storage: '商品主表'
  },
  {
    key: 'image_url',
    label: '商品主图',
    aliases: ['商品主图', '主图', 'Image URL'],
    storage: '商品主表'
  },
  {
    key: 'category',
    label: '小类目',
    aliases: ['小类目', 'Category'],
    storage: '商品主表'
  },
  {
    key: 'brand',
    label: '品牌',
    aliases: ['品牌', 'Brand'],
    storage: '商品主表'
  },
  {
    key: 'seller_id',
    label: '卖家ID',
    aliases: ['卖家ID', 'Seller ID', 'SellerId'],
    storage: '商品主表'
  },
  {
    key: 'seller_name',
    label: '卖家名称',
    aliases: ['卖家名称', 'BuyBox卖家', 'Seller Name'],
    storage: '商品主表'
  },
  {
    key: 'seller_region',
    label: '卖家所属地',
    aliases: ['卖家所属地', '卖家地区', 'Seller Region'],
    storage: '商品主表'
  },
  {
    key: 'price_usd',
    label: '价格($)',
    aliases: ['价格($)', 'Price', 'price'],
    storage: '经营数据副表'
  },
  {
    key: 'monthly_sales',
    label: '月销量',
    aliases: ['月销量', 'Monthly Sales'],
    storage: '经营数据副表'
  },
  {
    key: 'monthly_revenue',
    label: '月销售额($)',
    aliases: ['月销售额($)', '月销售额', 'Monthly Revenue'],
    storage: '经营数据副表'
  },
  {
    key: 'bsr_rank',
    label: 'BSR排名',
    aliases: ['BSR排名', 'BSR Rank', 'BSR', '大类BSR'],
    storage: '经营数据副表'
  },
  {
    key: 'review_count',
    label: '评分数',
    aliases: ['评分数', 'Review Count'],
    storage: '经营数据副表'
  },
  {
    key: 'rating',
    label: '评分',
    aliases: ['评分', 'Rating'],
    storage: '经营数据副表'
  },
  {
    key: 'material',
    label: '材质',
    aliases: ['材质', 'Material'],
    storage: '商品主表'
  },
  {
    key: 'weight_g',
    label: '产品净重(g)',
    aliases: ['产品净重(g)', 'Weight (g)'],
    storage: '商品主表'
  },
  {
    key: 'package_weight_g',
    label: '包装重量(g)',
    aliases: ['包装重量(g)', 'Package Weight (g)'],
    storage: '商品主表'
  },
  {
    key: 'package_length_cm',
    label: '包装长(cm)',
    aliases: ['包装长(cm)', 'Package Length (cm)'],
    storage: '商品主表'
  },
  {
    key: 'package_width_cm',
    label: '包装宽(cm)',
    aliases: ['包装宽(cm)', 'Package Width (cm)'],
    storage: '商品主表'
  },
  {
    key: 'package_height_cm',
    label: '包装高(cm)',
    aliases: ['包装高(cm)', 'Package Height (cm)'],
    storage: '商品主表'
  },
  {
    key: 'units_per_pack',
    label: '每套数量',
    aliases: ['每套数量', 'Units per Pack'],
    storage: '商品主表'
  },
  {
    key: 'carton_units',
    label: '装箱数量',
    aliases: ['装箱数量', 'Carton Units'],
    storage: '商品主表'
  },
  {
    key: 'carton_weight_g',
    label: '外箱重量(g)',
    aliases: ['外箱重量(g)', 'Carton Weight (g)'],
    storage: '商品主表'
  },
  {
    key: 'carton_length_cm',
    label: '外箱长(cm)',
    aliases: ['外箱长(cm)', 'Carton Length (cm)'],
    storage: '商品主表'
  },
  {
    key: 'carton_width_cm',
    label: '外箱宽(cm)',
    aliases: ['外箱宽(cm)', 'Carton Width (cm)'],
    storage: '商品主表'
  },
  {
    key: 'carton_height_cm',
    label: '外箱高(cm)',
    aliases: ['外箱高(cm)', 'Carton Height (cm)'],
    storage: '商品主表'
  },
  {
    key: 'parent_asin',
    label: '父ASIN',
    aliases: ['父ASIN', '父体ASIN', 'Parent ASIN', 'parent_asin'],
    storage: '商品主表'
  },
  {
    key: 'listing_role',
    label: '商品层级',
    aliases: ['商品层级', 'ASIN类型', 'Listing Role'],
    storage: '来源记录 / 导入识别'
  },
  {
    key: 'sales_scope',
    label: '销量口径',
    aliases: ['销量口径', 'Sales Scope'],
    storage: '来源记录 / 导入识别'
  },
  {
    key: 'child_monthly_sales',
    label: '子体月销量',
    aliases: ['子体月销量', '子ASIN月销量', 'Child Monthly Sales', '子体销量'],
    storage: '经营数据副表'
  },
  {
    key: 'parent_monthly_sales',
    label: '父体月销量',
    aliases: ['父体月销量', '父ASIN月销量', 'Parent Monthly Sales'],
    storage: '父体经营副表'
  },
  {
    key: 'child_monthly_revenue',
    label: '子体月销售额($)',
    aliases: ['子体月销售额($)', 'Child Monthly Revenue', '子体销售额($)', '子体销售额'],
    storage: '经营数据副表'
  },
  {
    key: 'parent_monthly_revenue',
    label: '父体月销售额($)',
    aliases: ['父体月销售额($)', 'Parent Monthly Revenue'],
    storage: '父体经营副表'
  },
  {
    key: 'variant_count',
    label: '变体数',
    aliases: ['变体数', '变体数量', 'Variation Count', 'Variants'],
    storage: '商品主表'
  },
  {
    key: 'product_url',
    label: '商品详情页链接',
    aliases: ['商品详情页链接'],
    storage: '商品主表'
  },
  {
    key: 'brand_url',
    label: '品牌链接',
    aliases: ['品牌链接'],
    storage: '商品主表'
  },
  {
    key: 'seller_url',
    label: '卖家首页',
    aliases: ['卖家首页'],
    storage: '商品主表'
  },
  {
    key: 'seller_info',
    label: '卖家信息',
    aliases: ['卖家信息'],
    storage: '商品主表'
  },
  {
    key: 'sku',
    label: 'SKU',
    aliases: ['SKU'],
    storage: '商品主表'
  },
  {
    key: 'main_category',
    label: '大类目',
    aliases: ['大类目'],
    storage: '商品主表'
  },
  {
    key: 'category_path',
    label: '类目路径',
    aliases: ['类目路径'],
    storage: '商品主表'
  },
  {
    key: 'listing_date',
    label: '上架时间',
    aliases: ['上架时间'],
    storage: '商品主表'
  },
  {
    key: 'detail_parameters',
    label: '详细参数',
    aliases: ['详细参数'],
    storage: '商品主表'
  },
  {
    key: 'product_dimensions_original',
    label: '商品尺寸',
    aliases: ['商品尺寸'],
    storage: '商品主表'
  },
  {
    key: 'product_dimensions',
    label: '商品尺寸（单位换算）',
    aliases: ['商品尺寸（单位换算）'],
    storage: '商品主表'
  },
  {
    key: 'product_weight_original',
    label: '商品重量',
    aliases: ['商品重量'],
    storage: '商品主表'
  },
  {
    key: 'product_weight_converted',
    label: '商品重量（单位换算）',
    aliases: ['商品重量（单位换算）'],
    storage: '商品主表'
  },
  {
    key: 'package_dimensions_original',
    label: '包装尺寸',
    aliases: ['包装尺寸'],
    storage: '商品主表'
  },
  {
    key: 'package_dimensions',
    label: '包装尺寸（单位换算）',
    aliases: ['包装尺寸（单位换算）'],
    storage: '商品主表'
  },
  {
    key: 'package_weight_original',
    label: '包装重量',
    aliases: ['包装重量'],
    storage: '商品主表'
  },
  {
    key: 'package_weight_converted',
    label: '包装重量（单位换算）',
    aliases: ['包装重量（单位换算）'],
    storage: '商品主表'
  },
  {
    key: 'package_size_tier',
    label: '包装尺寸分段',
    aliases: ['包装尺寸分段'],
    storage: '商品主表'
  },
  {
    key: 'product_length_cm',
    label: '商品长(cm)',
    aliases: ['商品长(cm)'],
    storage: '商品主表'
  },
  {
    key: 'product_width_cm',
    label: '商品宽(cm)',
    aliases: ['商品宽(cm)'],
    storage: '商品主表'
  },
  {
    key: 'product_height_cm',
    label: '商品高(cm)',
    aliases: ['商品高(cm)'],
    storage: '商品主表'
  },
  {
    key: 'small_bsr',
    label: '小类BSR',
    aliases: ['小类BSR'],
    storage: '经营数据副表'
  },
  {
    key: 'bsr_change',
    label: '大类BSR增长数',
    aliases: ['大类BSR增长数'],
    storage: '经营数据副表'
  },
  {
    key: 'bsr_growth_rate',
    label: '大类BSR增长率',
    aliases: ['大类BSR增长率'],
    storage: '经营数据副表'
  },
  {
    key: 'sales_yoy',
    label: '销量同比增长率',
    aliases: ['销量同比增长率'],
    storage: '经营数据副表'
  },
  {
    key: 'sales_mom',
    label: '销量环比增长率',
    aliases: ['销量环比增长率'],
    storage: '经营数据副表'
  },
  {
    key: 'prime_price',
    label: 'prime价格($)',
    aliases: ['prime价格($)'],
    storage: '经营数据副表'
  },
  {
    key: 'shipping_price',
    label: '买家运费($)',
    aliases: ['买家运费($)'],
    storage: '经营数据副表'
  },
  {
    key: 'new_reviews',
    label: '月新增评分数',
    aliases: ['月新增评分数'],
    storage: '经营数据副表'
  },
  {
    key: 'review_rate',
    label: '留评率',
    aliases: ['留评率'],
    storage: '经营数据副表'
  },
  {
    key: 'seller_count',
    label: '卖家数',
    aliases: ['卖家数'],
    storage: '经营数据副表'
  },
  {
    key: 'buybox_type',
    label: 'BuyBox类型',
    aliases: ['BuyBox类型'],
    storage: '经营数据副表'
  },
  {
    key: 'fulfillment',
    label: '配送方式',
    aliases: ['配送方式'],
    storage: '经营数据副表'
  },
  {
    key: 'coupon',
    label: 'Coupon',
    aliases: ['Coupon'],
    storage: '经营数据副表'
  },
  {
    key: 'deal',
    label: '秒杀',
    aliases: ['秒杀'],
    storage: '经营数据副表'
  },
  {
    key: 'sponsored_products',
    label: 'SP广告',
    aliases: ['SP广告'],
    storage: '经营数据副表'
  },
  {
    key: 'sponsored_brands',
    label: '品牌广告',
    aliases: ['品牌广告'],
    storage: '经营数据副表'
  },
  {
    key: 'ac_keywords',
    label: 'AC关键词',
    aliases: ['AC关键词'],
    storage: '经营数据副表'
  },
  {
    key: 'amazon_choice',
    label: "Amazon's Choice",
    aliases: ["Amazon's Choice"],
    storage: '经营数据副表'
  },
  {
    key: 'best_seller',
    label: 'Best Seller标识',
    aliases: ['Best Seller标识'],
    storage: '经营数据副表'
  },
  {
    key: 'new_release',
    label: 'New Release标识',
    aliases: ['New Release标识'],
    storage: '经营数据副表'
  },
  {
    key: 'aplus',
    label: 'A+页面',
    aliases: ['A+页面'],
    storage: '经营数据副表'
  },
  {
    key: 'brand_story',
    label: '品牌故事',
    aliases: ['品牌故事'],
    storage: '经营数据副表'
  },
  {
    key: 'video',
    label: '视频介绍',
    aliases: ['视频介绍'],
    storage: '经营数据副表'
  },
  {
    key: 'lqs',
    label: 'LQS',
    aliases: ['LQS'],
    storage: '经营数据副表'
  },
  {
    key: 'qa_count',
    label: 'Q&A',
    aliases: ['Q&A'],
    storage: '经营数据副表'
  },
  {
    key: 'rating_level',
    label: '评级',
    aliases: ['评级'],
    storage: '经营数据副表'
  },
  {
    key: 'listing_days',
    label: '上架天数',
    aliases: ['上架天数'],
    storage: '经营数据副表'
  },
  {
    key: 'imported_fba_fee_usd',
    label: 'FBA($)',
    aliases: ['FBA($)'],
    storage: '利润副表'
  },
  {
    key: 'imported_gross_margin_rate',
    label: '毛利率',
    aliases: ['毛利率'],
    storage: '利润副表'
  }
];
