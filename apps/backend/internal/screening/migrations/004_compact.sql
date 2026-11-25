ALTER TABLE ps_policies RENAME TO ps_configs;
ALTER TABLE ps_configs ADD COLUMN kind text NOT NULL DEFAULT 'screening' CHECK(kind IN ('screening','profit_template'));
ALTER TABLE ps_configs DROP CONSTRAINT ps_policies_tenant_id_version_key;
ALTER TABLE ps_configs ADD UNIQUE(tenant_id,kind,name,version);
DROP INDEX ps_one_active_policy;
CREATE UNIQUE INDEX ps_one_active_config ON ps_configs(tenant_id) WHERE active AND kind='screening';
INSERT INTO ps_configs(tenant_id,id,kind,version,name,config,config_hash,active,created_by,created_at)
 SELECT tenant_id,id,'profit_template',version,name,params,md5(params::text)||md5(params::text),false,created_by,created_at FROM ps_profit_templates;

ALTER TABLE ps_jobs DROP CONSTRAINT ps_jobs_kind_check;
ALTER TABLE ps_jobs DROP CONSTRAINT ps_jobs_state_check;
ALTER TABLE ps_jobs ADD CHECK(kind IN ('parse','import','screen','profit_batch','export','review','request'));
ALTER TABLE ps_jobs ADD CHECK(state IN ('queued','running','succeeded','partial','failed','preview','applied','undone'));
ALTER TABLE ps_jobs ADD COLUMN expires_at timestamptz;
ALTER TABLE ps_jobs ADD COLUMN undo_result jsonb;
ALTER TABLE ps_jobs ADD COLUMN request_hash char(64);
INSERT INTO ps_jobs(tenant_id,id,kind,state,idempotency_key,request_hash,payload,result,created_by,created_at,progress)
 SELECT tenant_id,md5('request:'||tenant_id::text||':'||request_key)::uuid,'request','succeeded',request_key,request_hash,'{}',result,0,created_at,100 FROM ps_requests;
INSERT INTO ps_jobs(tenant_id,id,kind,state,idempotency_key,payload,result,undo_result,expires_at,created_by,created_at,progress)
 SELECT tenant_id,id,'review',state,idempotency_key,payload,result,undo_result,expires_at,created_by,created_at,CASE WHEN state='preview' THEN 0 ELSE 100 END FROM ps_review_batches;
UPDATE ps_jobs j SET payload=j.payload||jsonb_build_object('legacy_run_id',r.id,'policy_id',r.policy_id),result=COALESCE(j.result,'{}'::jsonb)||jsonb_build_object('legacy_summary',r.summary,'legacy_screen_results',COALESCE((SELECT jsonb_agg(to_jsonb(s)) FROM ps_screen_results s WHERE s.tenant_id=r.tenant_id AND s.run_id=r.id),'[]'::jsonb)) FROM ps_screen_runs r WHERE j.tenant_id=r.tenant_id AND j.id=r.job_id;

ALTER TABLE ps_products ADD COLUMN parent_asin varchar(10) CHECK(parent_asin IS NULL OR (parent_asin ~ '^[A-Z0-9]{10}$' AND parent_asin<>asin));
ALTER TABLE ps_products ADD COLUMN manual_category text NOT NULL DEFAULT '';
ALTER TABLE ps_products ADD COLUMN material text;
ALTER TABLE ps_products ADD COLUMN weight_g numeric(18,6) CHECK(weight_g>=0);
ALTER TABLE ps_products ADD COLUMN package_weight_g numeric(18,6) CHECK(package_weight_g>=0);
ALTER TABLE ps_products ADD COLUMN package_length_cm numeric(18,6) CHECK(package_length_cm>=0);
ALTER TABLE ps_products ADD COLUMN package_width_cm numeric(18,6) CHECK(package_width_cm>=0);
ALTER TABLE ps_products ADD COLUMN package_height_cm numeric(18,6) CHECK(package_height_cm>=0);
ALTER TABLE ps_products ADD COLUMN units_per_pack integer CHECK(units_per_pack>0);
ALTER TABLE ps_products ADD COLUMN carton_units integer CHECK(carton_units>0);
ALTER TABLE ps_products ADD COLUMN carton_weight_g numeric(18,6) CHECK(carton_weight_g>=0);
ALTER TABLE ps_products ADD COLUMN carton_length_cm numeric(18,6) CHECK(carton_length_cm>=0);
ALTER TABLE ps_products ADD COLUMN carton_width_cm numeric(18,6) CHECK(carton_width_cm>=0);
ALTER TABLE ps_products ADD COLUMN carton_height_cm numeric(18,6) CHECK(carton_height_cm>=0);
ALTER TABLE ps_products ADD COLUMN machine_decision text CHECK(machine_decision IN ('passed','excluded','insufficient'));
ALTER TABLE ps_products ADD COLUMN machine_recommendation text CHECK(machine_recommendation IN ('regular','exploration'));
ALTER TABLE ps_products ADD COLUMN machine_direction text;
ALTER TABLE ps_products ADD COLUMN machine_group_key text;
ALTER TABLE ps_products ADD COLUMN machine_reasons jsonb NOT NULL DEFAULT '[]';
ALTER TABLE ps_products ADD COLUMN machine_evidence jsonb NOT NULL DEFAULT '[]';
ALTER TABLE ps_products ADD COLUMN machine_policy_id uuid;
ALTER TABLE ps_products ADD COLUMN machine_source_row_id uuid;
ALTER TABLE ps_products ADD COLUMN machine_monthly_id uuid;
ALTER TABLE ps_products ADD COLUMN machine_monthly_revision bigint;
ALTER TABLE ps_products ADD COLUMN screened_at timestamptz;
ALTER TABLE ps_products ADD FOREIGN KEY(tenant_id,machine_policy_id) REFERENCES ps_configs(tenant_id,id);
ALTER TABLE ps_products ADD FOREIGN KEY(tenant_id,machine_source_row_id) REFERENCES ps_import_rows(tenant_id,id);
ALTER TABLE ps_products ADD FOREIGN KEY(tenant_id,machine_monthly_id) REFERENCES ps_product_monthly(tenant_id,id);
UPDATE ps_products p SET machine_decision=s.decision,machine_recommendation=s.recommendation,machine_direction=s.direction,machine_group_key=s.group_key,machine_reasons=s.reasons,machine_evidence=s.evidence,machine_policy_id=r.policy_id,machine_source_row_id=s.source_row_id,machine_monthly_id=s.monthly_id,machine_monthly_revision=s.monthly_revision,screened_at=s.created_at,current_group_rep_id=COALESCE(p.current_group_rep_id,s.representative_id) FROM ps_screen_results s JOIN ps_screen_runs r ON r.tenant_id=s.tenant_id AND r.id=s.run_id WHERE p.tenant_id=s.tenant_id AND p.current_result_id=s.id;
CREATE INDEX ps_product_family_idx ON ps_products(tenant_id,marketplace,parent_asin);
CREATE INDEX ps_product_machine_idx ON ps_products(tenant_id,machine_decision,machine_recommendation);
CREATE INDEX ps_product_machine_group_idx ON ps_products(tenant_id,machine_group_key);
ALTER TABLE ps_review_events DROP CONSTRAINT ps_review_events_tenant_id_batch_id_fkey;
ALTER TABLE ps_review_events ADD FOREIGN KEY(tenant_id,batch_id) REFERENCES ps_jobs(tenant_id,id);
ALTER TABLE ps_profit_scenarios DROP CONSTRAINT ps_profit_scenarios_tenant_id_template_id_fkey;
ALTER TABLE ps_profit_scenarios ADD FOREIGN KEY(tenant_id,template_id) REFERENCES ps_configs(tenant_id,id);
ALTER TABLE ps_profit_scenarios ADD COLUMN product_snapshot jsonb;
ALTER TABLE ps_products DROP COLUMN current_result_id;
DROP TABLE ps_screen_results;
DROP TABLE ps_screen_runs;
DROP TABLE ps_review_batches;
DROP TABLE ps_profit_templates;
DROP TABLE ps_requests;
CREATE INDEX ps_job_review_history_idx ON ps_jobs(tenant_id,created_at DESC) WHERE kind='review';

ALTER TABLE ps_imports ADD COLUMN default_sales_scope text NOT NULL DEFAULT 'unknown' CHECK(default_sales_scope IN ('child','parent','unknown'));
DROP INDEX ps_import_identity;
CREATE UNIQUE INDEX ps_import_identity ON ps_imports(tenant_id,file_sha256,marketplace,source_period,mapping_hash,COALESCE(observed_on,'0001-01-01'::date),source_type,period_type,COALESCE(period_start,'0001-01-01'::date),COALESCE(period_end,'0001-01-01'::date),default_sales_scope);
ALTER TABLE ps_product_monthly ADD COLUMN sales_scope text NOT NULL DEFAULT 'unknown' CHECK(sales_scope IN ('child','parent','unknown'));
ALTER TABLE ps_product_monthly ADD COLUMN parent_asin_snapshot varchar(10) CHECK(parent_asin_snapshot IS NULL OR parent_asin_snapshot ~ '^[A-Z0-9]{10}$');
CREATE TABLE ps_parent_monthly (
 tenant_id bigint NOT NULL,id uuid NOT NULL,marketplace text NOT NULL DEFAULT 'US' CHECK(marketplace='US'),parent_asin varchar(10) NOT NULL CHECK(parent_asin ~ '^[A-Z0-9]{10}$'),
 stat_month date NOT NULL CHECK(extract(day FROM stat_month)=1),period_type text NOT NULL CHECK(period_type IN ('calendar_month','rolling_30d','custom')),
 period_start date NOT NULL,period_end date NOT NULL,observed_on date NOT NULL,source_type varchar(50) NOT NULL,source_row_id uuid NOT NULL,
 monthly_sales bigint CHECK(monthly_sales>=0),monthly_revenue numeric(18,6) CHECK(monthly_revenue>=0),currency char(3) NOT NULL DEFAULT 'USD' CHECK(currency='USD'),
 revision bigint NOT NULL DEFAULT 1,created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,marketplace,parent_asin,source_type,period_type,period_start,period_end),
 FOREIGN KEY(tenant_id,source_row_id) REFERENCES ps_import_rows(tenant_id,id),
 CHECK(period_start<=period_end AND stat_month=date_trunc('month',period_end)::date),
 CHECK(period_type<>'rolling_30d' OR period_end-period_start=29),
 CHECK(period_type<>'calendar_month' OR (period_start=stat_month AND period_end=(stat_month+interval '1 month - 1 day')::date))
);
CREATE INDEX ps_parent_month_idx ON ps_parent_monthly(tenant_id,marketplace,parent_asin,stat_month DESC,period_end DESC);
