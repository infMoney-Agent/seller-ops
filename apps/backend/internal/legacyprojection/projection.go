package legacyprojection

const ReferenceSQL = "EXISTS(SELECT 1 FROM ps_products ref WHERE ref.tenant_id=p.tenant_id AND ref.archived_at IS NULL AND ref.id<>p.id AND ref.machine_direction=p.machine_direction AND ref.review_status<>'pending')"

// Display-only fallback: retain source values and machine screening semantics.
// Use the observation's variant count, never today's product parameters.
const DisplayChildSalesSQL = `CASE WHEN m.sales_scope='child' AND m.monthly_sales IS NOT NULL AND m.sales_quantity_kind IS DISTINCT FROM 'single_variant_estimate' THEN m.monthly_sales ELSE
(SELECT COALESCE((src.normalized_data->>'source_monthly_sales')::bigint,(src.normalized_data->>'parent_monthly_sales')::bigint)
 FROM ps_import_rows src
 WHERE src.tenant_id=m.tenant_id AND src.id=m.source_row_id
 AND src.normalized_data->>'variant_count'='1'
 AND COALESCE(src.normalized_data->>'listing_role','child')<>'parent'
 AND NULLIF(m.parent_asin_snapshot,'') IS NOT NULL
 AND NOT EXISTS (
 SELECT 1 FROM ps_products sibling
 JOIN ps_product_monthly sm ON sm.tenant_id=sibling.tenant_id AND sm.product_id=sibling.id
 JOIN ps_import_rows sr ON sr.tenant_id=sm.tenant_id AND sr.id=sm.source_row_id
 WHERE sibling.tenant_id=p.tenant_id AND sibling.marketplace=p.marketplace AND sibling.archived_at IS NULL
 AND sm.parent_asin_snapshot=m.parent_asin_snapshot
 AND sm.period_type=m.period_type AND sm.period_start=m.period_start AND sm.period_end=m.period_end
 AND (sibling.id<>p.id OR (sr.normalized_data->>'variant_count')::numeric>1)
 )) END`

const ProductProjection = "to_jsonb(p)||jsonb_build_object('display_child_sales'," + DisplayChildSalesSQL + ",'monthly_id',m.id,'monthly_revision',m.revision,'stat_month',m.stat_month,'period_type',m.period_type,'period_start',m.period_start,'period_end',m.period_end,'source_type',m.source_type,'sales_scope',m.sales_scope,'sales_quantity_kind',m.sales_quantity_kind,'child_sales_raw',m.child_sales_raw,'price_usd',m.price_usd::text,'reported_monthly_sales',m.monthly_sales,'monthly_sales',CASE WHEN m.sales_scope='child' THEN m.monthly_sales END,'monthly_revenue',CASE WHEN m.sales_scope='child' THEN m.monthly_revenue::text END,'review_count',m.review_count,'rating',m.rating::text,'bsr_rank',m.bsr_rank,'decision',p.machine_decision,'recommendation',p.machine_recommendation,'direction',p.machine_direction,'group_key',p.machine_group_key,'reasons',p.machine_reasons,'profit_output',q.output,'profit_data_revision',q.product_data_revision,'profit_monthly_revision',q.monthly_revision,'has_reference'," + ReferenceSQL + ")"
const ParentFamilySQL = "COALESCE('parent:'||p.marketplace||':'||NULLIF(p.parent_asin,''),'product:'||p.id::text)"
const FamilyRankSQL = "m.stat_month DESC NULLS LAST,m.period_end DESC NULLS LAST,m.period_type,m.source_type," + DisplayChildSalesSQL + " DESC NULLS LAST,p.updated_at DESC,p.id"
const ProfitJoin = " LEFT JOIN LATERAL(SELECT output,product_data_revision,monthly_revision FROM ps_profit_scenarios q WHERE q.tenant_id=p.tenant_id AND q.product_id=p.id AND q.record_kind='calculated' AND q.monthly_id IS NOT DISTINCT FROM m.id ORDER BY created_at DESC,id DESC LIMIT 1) q ON true "
