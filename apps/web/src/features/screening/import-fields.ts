// Standard fields and supported aliases shared with the Go importer.
export const importFields = [
  {
    key: 'asin',
    label: 'ASIN',
    aliases: ['ASIN', 'asin']
  },
  {
    key: 'title',
    label: '商品标题',
    aliases: ['商品标题', 'Title', 'title']
  },
  {
    key: 'title_zh',
    label: '商品标题中文（机器翻译）',
    aliases: ['商品标题中文（机器翻译）', '中文标题', '商品标题中文']
  },
  {
    key: 'image_url',
    label: '商品主图',
    aliases: ['商品主图', '主图', 'Image URL']
  },
  {
    key: 'category',
    label: '小类目',
    aliases: ['小类目', 'Category']
  },
  {
    key: 'brand',
    label: '品牌',
    aliases: ['品牌', 'Brand']
  },
  {
    key: 'seller_id',
    label: '卖家ID',
    aliases: ['卖家ID', 'Seller ID', 'SellerId']
  },
  {
    key: 'seller_name',
    label: '卖家名称',
    aliases: ['卖家名称', 'BuyBox卖家', 'Seller Name']
  },
  {
    key: 'seller_region',
    label: '卖家所属地',
    aliases: ['卖家所属地', '卖家地区', 'Seller Region']
  },
  {
    key: 'price_usd',
    label: '价格($)',
    aliases: ['价格($)', 'Price', 'price']
  },
  {
    key: 'monthly_sales',
    label: '月销量',
    aliases: ['月销量', 'Monthly Sales']
  },
  {
    key: 'monthly_revenue',
    label: '月销售额($)',
    aliases: ['月销售额($)', '月销售额', 'Monthly Revenue']
  },
  {
    key: 'bsr_rank',
    label: 'BSR排名',
    aliases: ['BSR排名', 'BSR Rank', 'BSR']
  },
  {
    key: 'review_count',
    label: '评分数',
    aliases: ['评分数', 'Review Count']
  },
  {
    key: 'rating',
    label: '评分',
    aliases: ['评分', 'Rating']
  },
  {
    key: 'material',
    label: '材质',
    aliases: ['材质', 'Material']
  },
  {
    key: 'weight_g',
    label: '产品净重(g)',
    aliases: ['产品净重(g)', 'Weight (g)']
  },
  {
    key: 'package_weight_g',
    label: '包装重量(g)',
    aliases: ['包装重量(g)', 'Package Weight (g)']
  },
  {
    key: 'package_length_cm',
    label: '包装长(cm)',
    aliases: ['包装长(cm)', 'Package Length (cm)']
  },
  {
    key: 'package_width_cm',
    label: '包装宽(cm)',
    aliases: ['包装宽(cm)', 'Package Width (cm)']
  },
  {
    key: 'package_height_cm',
    label: '包装高(cm)',
    aliases: ['包装高(cm)', 'Package Height (cm)']
  },
  {
    key: 'units_per_pack',
    label: '每套数量',
    aliases: ['每套数量', 'Units per Pack']
  },
  {
    key: 'carton_units',
    label: '装箱数量',
    aliases: ['装箱数量', 'Carton Units']
  },
  {
    key: 'carton_weight_g',
    label: '外箱重量(g)',
    aliases: ['外箱重量(g)', 'Carton Weight (g)']
  },
  {
    key: 'carton_length_cm',
    label: '外箱长(cm)',
    aliases: ['外箱长(cm)', 'Carton Length (cm)']
  },
  {
    key: 'carton_width_cm',
    label: '外箱宽(cm)',
    aliases: ['外箱宽(cm)', 'Carton Width (cm)']
  },
  {
    key: 'carton_height_cm',
    label: '外箱高(cm)',
    aliases: ['外箱高(cm)', 'Carton Height (cm)']
  },
  {
    key: 'parent_asin',
    label: '父ASIN',
    aliases: ['父ASIN', '父体ASIN', 'Parent ASIN', 'parent_asin']
  },
  {
    key: 'listing_role',
    label: '商品层级',
    aliases: ['商品层级', 'ASIN类型', 'Listing Role']
  },
  {
    key: 'sales_scope',
    label: '销量口径',
    aliases: ['销量口径', 'Sales Scope']
  },
  {
    key: 'child_monthly_sales',
    label: '子体月销量',
    aliases: ['子体月销量', '子ASIN月销量', 'Child Monthly Sales']
  },
  {
    key: 'parent_monthly_sales',
    label: '父体月销量',
    aliases: ['父体月销量', '父ASIN月销量', 'Parent Monthly Sales']
  },
  {
    key: 'child_monthly_revenue',
    label: '子体月销售额($)',
    aliases: ['子体月销售额($)', 'Child Monthly Revenue']
  },
  {
    key: 'parent_monthly_revenue',
    label: '父体月销售额($)',
    aliases: ['父体月销售额($)', 'Parent Monthly Revenue']
  }
];
