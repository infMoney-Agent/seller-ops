CREATE TABLE ps_jobs (
 tenant_id bigint NOT NULL,id uuid NOT NULL,kind text NOT NULL CHECK(kind IN ('parse','import','screen','profit_batch','export')),
 state text NOT NULL DEFAULT 'queued' CHECK(state IN ('queued','running','succeeded','partial','failed')),
 idempotency_key text NOT NULL,payload jsonb NOT NULL DEFAULT '{}',result jsonb,error jsonb,
 progress integer NOT NULL DEFAULT 0 CHECK(progress BETWEEN 0 AND 100),attempts integer NOT NULL DEFAULT 0,
 lease_until timestamptz,lease_token uuid,created_by bigint NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,kind,idempotency_key));
CREATE TABLE ps_imports (
 tenant_id bigint NOT NULL,id uuid NOT NULL,file_name text NOT NULL,file_sha256 char(64) NOT NULL,storage_key text NOT NULL,
 file_size bigint NOT NULL CHECK(file_size>=0),marketplace text NOT NULL DEFAULT 'US' CHECK(marketplace='US'),source_period text NOT NULL DEFAULT '',observed_on date,
 mapping jsonb NOT NULL DEFAULT '{}',mapping_hash char(64) NOT NULL,parse_generation bigint NOT NULL DEFAULT 1,
 state text NOT NULL CHECK(state IN ('uploaded','parsing','preview','importing','completed','partial','failed')),
 counts jsonb NOT NULL DEFAULT '{}',created_by bigint NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),committed_at timestamptz,
 PRIMARY KEY(tenant_id,id));
CREATE UNIQUE INDEX ps_import_identity ON ps_imports(tenant_id,file_sha256,marketplace,source_period,mapping_hash,COALESCE(observed_on,'0001-01-01'::date));
CREATE TABLE ps_products (
 tenant_id bigint NOT NULL,id uuid NOT NULL,marketplace text NOT NULL DEFAULT 'US' CHECK(marketplace='US'),asin varchar(10) NOT NULL CHECK(asin ~ '^[A-Z0-9]{10}$'),title text NOT NULL,
 title_zh text,image_url text,category text,brand text,seller_id text,seller_name text,seller_region text,
 price_usd numeric(18,6) CHECK(price_usd>=0),monthly_sales bigint CHECK(monthly_sales>=0),review_count bigint CHECK(review_count>=0),rating numeric(3,2) CHECK(rating BETWEEN 0 AND 5),specs jsonb NOT NULL DEFAULT '{}',
 current_row_id uuid,current_result_id uuid,review_status text NOT NULL DEFAULT 'pending' CHECK(review_status IN ('pending','kept','excluded','unsure')),
 review_reason text NOT NULL DEFAULT '',review_note text NOT NULL DEFAULT '',reviewed_by bigint,reviewed_at timestamptz,
 data_revision bigint NOT NULL DEFAULT 0,review_revision bigint NOT NULL DEFAULT 0,screen_generation bigint NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,marketplace,asin),
 CHECK(review_status NOT IN ('excluded','unsure') OR length(trim(review_reason))>0));
CREATE TABLE ps_import_rows (
 tenant_id bigint NOT NULL,id uuid NOT NULL,import_id uuid NOT NULL,member_path text NOT NULL DEFAULT '',sheet_name text NOT NULL DEFAULT '',row_no integer NOT NULL CHECK(row_no>0),
 product_id uuid,state text NOT NULL CHECK(state IN ('valid','invalid','committed')),raw_data jsonb NOT NULL,normalized_data jsonb,errors jsonb NOT NULL DEFAULT '[]',
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,import_id,member_path,sheet_name,row_no),
 FOREIGN KEY(tenant_id,import_id) REFERENCES ps_imports(tenant_id,id),FOREIGN KEY(tenant_id,product_id) REFERENCES ps_products(tenant_id,id));
CREATE TABLE ps_policies (
 tenant_id bigint NOT NULL,id uuid NOT NULL,version integer NOT NULL CHECK(version>0),name text NOT NULL,config jsonb NOT NULL,config_hash char(64) NOT NULL,
 active boolean NOT NULL DEFAULT false,created_by bigint NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,version));
CREATE UNIQUE INDEX ps_one_active_policy ON ps_policies(tenant_id) WHERE active;
CREATE TABLE ps_screen_runs (
 tenant_id bigint NOT NULL,id uuid NOT NULL,job_id uuid NOT NULL,policy_id uuid NOT NULL,summary jsonb NOT NULL DEFAULT '{}',created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(tenant_id,id),
 FOREIGN KEY(tenant_id,job_id) REFERENCES ps_jobs(tenant_id,id),FOREIGN KEY(tenant_id,policy_id) REFERENCES ps_policies(tenant_id,id));
CREATE TABLE ps_screen_results (
 tenant_id bigint NOT NULL,id uuid NOT NULL,run_id uuid NOT NULL,product_id uuid NOT NULL,source_row_id uuid NOT NULL,data_revision bigint NOT NULL,generation bigint NOT NULL,
 decision text NOT NULL CHECK(decision IN ('passed','excluded','insufficient')),recommendation text NOT NULL CHECK(recommendation IN ('regular','exploration')),
 direction text NOT NULL,group_key text NOT NULL,representative_id uuid NOT NULL,reasons jsonb NOT NULL,evidence jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,run_id,product_id),FOREIGN KEY(tenant_id,run_id) REFERENCES ps_screen_runs(tenant_id,id),
 FOREIGN KEY(tenant_id,product_id) REFERENCES ps_products(tenant_id,id),FOREIGN KEY(tenant_id,source_row_id) REFERENCES ps_import_rows(tenant_id,id),FOREIGN KEY(tenant_id,representative_id) REFERENCES ps_products(tenant_id,id));
CREATE TABLE ps_review_batches (
 tenant_id bigint NOT NULL,id uuid NOT NULL,idempotency_key text NOT NULL,state text NOT NULL CHECK(state IN ('preview','applied','undone')),
 payload jsonb NOT NULL,result jsonb,undo_result jsonb,expires_at timestamptz NOT NULL,created_by bigint NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,idempotency_key));
CREATE TABLE ps_review_events (
 tenant_id bigint NOT NULL,id uuid NOT NULL,product_id uuid NOT NULL,batch_id uuid,undo_of uuid,actor_id bigint NOT NULL,before_revision bigint NOT NULL,after_revision bigint NOT NULL,
 before_value jsonb NOT NULL,after_value jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,product_id,after_revision),UNIQUE(tenant_id,undo_of),
 FOREIGN KEY(tenant_id,product_id) REFERENCES ps_products(tenant_id,id),FOREIGN KEY(tenant_id,batch_id) REFERENCES ps_review_batches(tenant_id,id),FOREIGN KEY(tenant_id,undo_of) REFERENCES ps_review_events(tenant_id,id));
CREATE TABLE ps_profit_templates (
 tenant_id bigint NOT NULL,id uuid NOT NULL,name text NOT NULL,version integer NOT NULL CHECK(version>0),params jsonb NOT NULL,created_by bigint NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,name,version));
CREATE TABLE ps_profit_scenarios (
 tenant_id bigint NOT NULL,id uuid NOT NULL,product_id uuid NOT NULL,template_id uuid,name text NOT NULL,inputs jsonb NOT NULL,output jsonb,template_snapshot jsonb,formula_version text NOT NULL,
 product_data_revision bigint NOT NULL,revision bigint NOT NULL DEFAULT 1,created_by bigint NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(tenant_id,id),
 FOREIGN KEY(tenant_id,product_id) REFERENCES ps_products(tenant_id,id),FOREIGN KEY(tenant_id,template_id) REFERENCES ps_profit_templates(tenant_id,id));
CREATE TABLE ps_requests (
 tenant_id bigint NOT NULL,request_key text NOT NULL,request_hash char(64) NOT NULL,result jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(tenant_id,request_key));
ALTER TABLE ps_products ADD FOREIGN KEY(tenant_id,current_row_id) REFERENCES ps_import_rows(tenant_id,id),ADD FOREIGN KEY(tenant_id,current_result_id) REFERENCES ps_screen_results(tenant_id,id);
CREATE INDEX ps_product_review_idx ON ps_products(tenant_id,review_status,updated_at DESC,id);
CREATE INDEX ps_product_seller_idx ON ps_products(tenant_id,seller_region,seller_id);
CREATE INDEX ps_import_row_product_idx ON ps_import_rows(tenant_id,product_id,import_id);
CREATE INDEX ps_screen_group_idx ON ps_screen_results(tenant_id,group_key);
CREATE INDEX ps_screen_decision_idx ON ps_screen_results(tenant_id,decision,recommendation);
CREATE INDEX ps_review_history_idx ON ps_review_events(tenant_id,product_id,created_at DESC);
CREATE INDEX ps_profit_product_idx ON ps_profit_scenarios(tenant_id,product_id,created_at DESC);
CREATE INDEX ps_job_queue_idx ON ps_jobs(state,lease_until,created_at);
