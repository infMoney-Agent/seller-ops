ALTER TABLE ps_imports ADD COLUMN source_type varchar(50) NOT NULL DEFAULT 'legacy_import';
ALTER TABLE ps_imports ADD COLUMN period_type text NOT NULL DEFAULT 'custom' CHECK(period_type IN ('calendar_month','rolling_30d','custom'));
ALTER TABLE ps_imports ADD COLUMN period_start date;
ALTER TABLE ps_imports ADD COLUMN period_end date;
UPDATE ps_imports SET
 period_type=CASE WHEN source_period ~ '^[12][0-9]{3}-(0[1-9]|1[0-2])$' THEN 'calendar_month' WHEN source_period ~* 'last.?30|滚动30' THEN 'rolling_30d' ELSE 'custom' END,
 period_start=CASE WHEN source_period ~ '^[12][0-9]{3}-(0[1-9]|1[0-2])$' THEN to_date(source_period||'-01','YYYY-MM-DD') WHEN source_period ~* 'last.?30|滚动30' THEN observed_on-29 ELSE observed_on END,
 period_end=CASE WHEN source_period ~ '^[12][0-9]{3}-(0[1-9]|1[0-2])$' THEN (to_date(source_period||'-01','YYYY-MM-DD')+interval '1 month - 1 day')::date ELSE observed_on END;
ALTER TABLE ps_imports ADD CHECK((period_start IS NULL AND period_end IS NULL) OR (period_start IS NOT NULL AND period_end IS NOT NULL AND period_start<=period_end));
DROP INDEX ps_import_identity;
CREATE UNIQUE INDEX ps_import_identity ON ps_imports(tenant_id,file_sha256,marketplace,source_period,mapping_hash,COALESCE(observed_on,'0001-01-01'::date),source_type,period_type,COALESCE(period_start,'0001-01-01'::date),COALESCE(period_end,'0001-01-01'::date));

CREATE TABLE ps_product_monthly (
 tenant_id bigint NOT NULL,id uuid NOT NULL,product_id uuid NOT NULL,
 stat_month date NOT NULL CHECK(extract(day FROM stat_month)=1),
 period_type text NOT NULL CHECK(period_type IN ('calendar_month','rolling_30d','custom')),
 period_start date NOT NULL,period_end date NOT NULL,observed_on date NOT NULL,
 source_type varchar(50) NOT NULL,source_row_id uuid NOT NULL,
 currency char(3) NOT NULL DEFAULT 'USD' CHECK(currency='USD'),
 price_usd numeric(18,6) CHECK(price_usd>=0),monthly_sales bigint CHECK(monthly_sales>=0),
 monthly_revenue numeric(18,6) CHECK(monthly_revenue>=0),review_count bigint CHECK(review_count>=0),
 rating numeric(3,2) CHECK(rating BETWEEN 0 AND 5),bsr_rank bigint CHECK(bsr_rank>=0),
 revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,product_id,source_type,period_type,period_start,period_end),
 FOREIGN KEY(tenant_id,product_id) REFERENCES ps_products(tenant_id,id),
 FOREIGN KEY(tenant_id,source_row_id) REFERENCES ps_import_rows(tenant_id,id),
 CHECK(period_start<=period_end AND stat_month=date_trunc('month',period_end)::date),
 CHECK(period_type<>'rolling_30d' OR period_end-period_start=29),
 CHECK(period_type<>'calendar_month' OR (period_start=stat_month AND period_end=(stat_month+interval '1 month - 1 day')::date))
);
CREATE INDEX ps_monthly_product_month_idx ON ps_product_monthly(tenant_id,product_id,stat_month DESC,period_end DESC,observed_on DESC);
CREATE INDEX ps_monthly_month_sales_idx ON ps_product_monthly(tenant_id,stat_month,monthly_sales DESC);
CREATE INDEX ps_monthly_source_row_idx ON ps_product_monthly(tenant_id,source_row_id);
CREATE INDEX ps_monthly_period_lookup_idx ON ps_product_monthly(tenant_id,stat_month,period_type,product_id,period_end DESC,observed_on DESC,updated_at DESC,id DESC) INCLUDE(price_usd,monthly_sales);

INSERT INTO ps_product_monthly(tenant_id,id,product_id,stat_month,period_type,period_start,period_end,observed_on,source_type,source_row_id,price_usd,monthly_sales,monthly_revenue,review_count,rating,bsr_rank)
SELECT tenant_id,id,product_id,date_trunc('month',period_end)::date,period_type,period_start,period_end,observed_on,source_type,id,
 NULLIF(normalized_data->>'price_usd','')::numeric,NULLIF(normalized_data->>'monthly_sales','')::bigint,NULLIF(normalized_data->>'monthly_revenue','')::numeric,NULLIF(normalized_data->>'review_count','')::bigint,NULLIF(normalized_data->>'rating','')::numeric,NULLIF(normalized_data->>'bsr_rank','')::bigint
FROM (SELECT r.tenant_id,r.id,r.product_id,r.normalized_data,i.period_type,i.period_start,i.period_end,i.observed_on,i.source_type,
 row_number() OVER(PARTITION BY r.tenant_id,r.product_id,i.source_type,i.period_type,i.period_start,i.period_end ORDER BY i.observed_on DESC,i.created_at DESC,r.member_path DESC,r.sheet_name DESC,r.row_no DESC,r.id DESC) AS rank
 FROM ps_import_rows r JOIN ps_imports i ON i.tenant_id=r.tenant_id AND i.id=r.import_id WHERE r.state='committed' AND r.product_id IS NOT NULL AND i.observed_on IS NOT NULL) candidates WHERE rank=1;

ALTER TABLE ps_products ADD COLUMN current_monthly_id uuid;
ALTER TABLE ps_products ADD COLUMN current_monthly_revision bigint;
ALTER TABLE ps_products ADD FOREIGN KEY(tenant_id,current_monthly_id) REFERENCES ps_product_monthly(tenant_id,id);
WITH latest AS (SELECT tenant_id,product_id,id,revision,row_number() OVER(PARTITION BY tenant_id,product_id ORDER BY stat_month DESC,period_end DESC,observed_on DESC,updated_at DESC,id DESC) AS rank FROM ps_product_monthly)
UPDATE ps_products p SET current_monthly_id=m.id,current_monthly_revision=m.revision FROM latest m WHERE m.rank=1 AND p.tenant_id=m.tenant_id AND p.id=m.product_id;
UPDATE ps_products p SET data_revision=p.data_revision+1,screen_generation=p.screen_generation+1,current_result_id=NULL,current_group_rep_id=NULL FROM ps_product_monthly m WHERE m.tenant_id=p.tenant_id AND m.id=p.current_monthly_id AND (p.current_row_id IS DISTINCT FROM m.source_row_id OR (p.price_usd,p.monthly_sales,p.review_count,p.rating) IS DISTINCT FROM (m.price_usd,m.monthly_sales,m.review_count,m.rating));

ALTER TABLE ps_profit_scenarios ADD COLUMN monthly_id uuid;
ALTER TABLE ps_profit_scenarios ADD COLUMN monthly_revision bigint;
ALTER TABLE ps_profit_scenarios ADD COLUMN monthly_snapshot jsonb;
ALTER TABLE ps_profit_scenarios ADD FOREIGN KEY(tenant_id,monthly_id) REFERENCES ps_product_monthly(tenant_id,id);
CREATE INDEX ps_profit_monthly_idx ON ps_profit_scenarios(tenant_id,product_id,monthly_id,created_at DESC,id DESC);
UPDATE ps_profit_scenarios q SET monthly_id=m.id,monthly_revision=m.revision,monthly_snapshot=to_jsonb(m) FROM ps_products p JOIN ps_product_monthly m ON m.tenant_id=p.tenant_id AND m.id=p.current_monthly_id WHERE q.tenant_id=p.tenant_id AND q.product_id=p.id AND q.product_data_revision=p.data_revision AND p.current_row_id=m.source_row_id;
ALTER TABLE ps_screen_results ADD COLUMN monthly_id uuid;
ALTER TABLE ps_screen_results ADD COLUMN monthly_revision bigint;
ALTER TABLE ps_screen_results ADD FOREIGN KEY(tenant_id,monthly_id) REFERENCES ps_product_monthly(tenant_id,id);
UPDATE ps_screen_results s SET monthly_id=m.id,monthly_revision=m.revision FROM ps_product_monthly m WHERE m.tenant_id=s.tenant_id AND m.product_id=s.product_id AND m.source_row_id=s.source_row_id;
ALTER TABLE ps_products DROP COLUMN price_usd,DROP COLUMN monthly_sales,DROP COLUMN review_count,DROP COLUMN rating;
