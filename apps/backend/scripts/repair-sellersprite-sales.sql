-- Repair an explicitly selected SellerSprite import after correcting header aliases.
-- Default is a dry run: psql ... -v import_id=UUID -f repair-sellersprite-sales.sql
-- Apply after a database backup: add -v apply=true. Source rows and reviews stay immutable.
\set ON_ERROR_STOP on
\if :{?apply}
\else
\set apply false
\endif
BEGIN;
SET LOCAL lock_timeout = '5s';
CREATE TEMP TABLE repair_guards ON COMMIT DROP AS
SELECT 'reviews' kind, md5(coalesce(string_agg(jsonb_build_array(tenant_id,id,review_status,review_reason,review_note,review_revision,reviewed_by,reviewed_at)::text, '' ORDER BY tenant_id,id),'')) digest FROM ps_products
UNION ALL SELECT 'events',md5(coalesce(string_agg(to_jsonb(e)::text,'' ORDER BY tenant_id,id),'')) FROM ps_review_events e
UNION ALL SELECT 'profits',md5(coalesce(string_agg(to_jsonb(p)::text,'' ORDER BY tenant_id,id),'')) FROM ps_profit_scenarios p
UNION ALL SELECT 'sources',md5(coalesce(string_agg(to_jsonb(r)::text,'' ORDER BY tenant_id,id),'')) FROM ps_import_rows r;
CREATE TEMP TABLE repair_sales ON COMMIT DROP AS
WITH raw AS (
 SELECT m.*,i.created_by,i.id import_id,r.raw_data,
  trim(replace(coalesce(r.raw_data->>'子体销量',''),',','')) child_sales,
  trim(replace(replace(coalesce(r.raw_data->>'子体销售额($)',''),',',''),'$','')) child_revenue,
  trim(replace(coalesce(r.raw_data->>'月销量',''),',','')) reported_sales,
  trim(replace(replace(coalesce(r.raw_data->>'月销售额($)',''),',',''),'$','')) reported_revenue,
  upper(trim(r.raw_data->>'ASIN')) = upper(trim(r.raw_data->>'父ASIN')) AND trim(r.raw_data->>'变体数')='1' single_variant
 FROM ps_product_monthly m
 JOIN ps_import_rows r ON r.tenant_id=m.tenant_id AND r.id=m.source_row_id
 JOIN ps_imports i ON i.tenant_id=r.tenant_id AND i.id=r.import_id
 WHERE i.id=:'import_id'::uuid AND i.source_type='sellersprite' AND m.sales_scope='unknown'
 AND NOT(i.mapping ?| ARRAY['child_monthly_sales','child_monthly_revenue','variant_count','sales_scope'])
), parsed AS (
 SELECT *,CASE WHEN child_sales ~ '^[0-9]+$' THEN child_sales::bigint END child_value,
 CASE WHEN reported_sales ~ '^[0-9]+$' THEN reported_sales::bigint END reported_value,
 CASE WHEN child_revenue ~ '^[0-9]+([.][0-9]+)?$' THEN child_revenue::numeric END child_amount,
 CASE WHEN reported_revenue ~ '^[0-9]+([.][0-9]+)?$' THEN reported_revenue::numeric END reported_amount FROM raw
)
SELECT *,coalesce(child_value,CASE WHEN single_variant THEN reported_value END) new_sales,
 CASE WHEN child_value IS NOT NULL THEN child_amount ELSE reported_amount END new_revenue
FROM parsed WHERE child_value IS NOT NULL OR (single_variant AND reported_value IS NOT NULL);
-- Fail before updating if a workspace cannot enqueue a refresh of machine results.
DO $$ BEGIN IF EXISTS (SELECT 1 FROM repair_sales r WHERE NOT EXISTS(SELECT 1 FROM ps_configs c WHERE c.tenant_id=r.tenant_id AND c.kind='screening' AND c.active)) THEN RAISE EXCEPTION 'Active screening policy required'; END IF; END $$;
UPDATE ps_product_monthly m SET monthly_sales=r.new_sales,monthly_revenue=r.new_revenue,
 sales_scope='child',revision=m.revision+1,updated_at=now()
FROM repair_sales r WHERE m.tenant_id=r.tenant_id AND m.id=r.id;
INSERT INTO ps_parent_monthly(tenant_id,id,parent_asin,stat_month,period_type,period_start,period_end,observed_on,source_type,source_row_id,monthly_sales)
SELECT tenant_id,gen_random_uuid(),parent_asin_snapshot,stat_month,period_type,period_start,period_end,observed_on,source_type,source_row_id,new_sales FROM repair_sales WHERE single_variant
ON CONFLICT(tenant_id,marketplace,parent_asin,source_type,period_type,period_start,period_end)
DO UPDATE SET monthly_sales=EXCLUDED.monthly_sales,revision=ps_parent_monthly.revision+1,updated_at=now()
WHERE ps_parent_monthly.source_row_id=EXCLUDED.source_row_id;
CREATE TEMP TABLE repair_products ON COMMIT DROP AS
WITH updated AS (
 UPDATE ps_products p SET current_monthly_revision=m.revision,data_revision=p.data_revision+1,
 screen_generation=p.screen_generation+1,machine_decision=NULL,machine_group_key=NULL,current_group_rep_id=NULL,updated_at=now()
 FROM ps_product_monthly m JOIN repair_sales r ON r.tenant_id=m.tenant_id AND r.id=m.id
 WHERE p.tenant_id=m.tenant_id AND p.current_monthly_id=m.id
 RETURNING p.tenant_id,p.id,p.data_revision,p.screen_generation
) SELECT * FROM updated;
INSERT INTO ps_jobs(tenant_id,id,kind,idempotency_key,payload,created_by)
SELECT p.tenant_id,gen_random_uuid(),'screen','repair-sellersprite-sales-v1:' || :'import_id',
 jsonb_build_object('engine','go-v1','policy',to_jsonb(c),'repair','sellersprite-sales-v1','import_id',:'import_id',
 'targets',jsonb_agg(jsonb_build_object('id',p.id,'revision',p.data_revision,'generation',p.screen_generation))),max(r.created_by)
FROM repair_products p JOIN ps_configs c ON c.tenant_id=p.tenant_id AND c.kind='screening' AND c.active
JOIN ps_imports r ON r.tenant_id=p.tenant_id AND r.id=:'import_id'::uuid GROUP BY p.tenant_id,c.*;
DO $$ DECLARE bad boolean; BEGIN
WITH current_guards AS (
SELECT 'reviews' kind,md5(coalesce(string_agg(jsonb_build_array(tenant_id,id,review_status,review_reason,review_note,review_revision,reviewed_by,reviewed_at)::text,'' ORDER BY tenant_id,id),'')) digest FROM ps_products
UNION ALL SELECT 'events',md5(coalesce(string_agg(to_jsonb(e)::text,'' ORDER BY tenant_id,id),'')) FROM ps_review_events e
UNION ALL SELECT 'profits',md5(coalesce(string_agg(to_jsonb(p)::text,'' ORDER BY tenant_id,id),'')) FROM ps_profit_scenarios p
UNION ALL SELECT 'sources',md5(coalesce(string_agg(to_jsonb(r)::text,'' ORDER BY tenant_id,id),'')) FROM ps_import_rows r)
SELECT EXISTS(SELECT 1 FROM current_guards c JOIN repair_guards b USING(kind) WHERE c.digest<>b.digest) INTO bad;
IF bad THEN RAISE EXCEPTION 'Protected review/profit/source history changed';END IF;
END $$;
SELECT count(*) repaired_child_records,count(*) FILTER(WHERE single_variant) mirrored_parent_records,
 count(*) FILTER(WHERE child_value IS NULL) inferred_single_variant_records FROM repair_sales;
\if :apply
COMMIT;
\else
ROLLBACK;
\endif
