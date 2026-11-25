ALTER TABLE ps_products ADD COLUMN archived_at timestamptz;
CREATE INDEX ps_product_active_idx ON ps_products(tenant_id,id) WHERE archived_at IS NULL;
DROP INDEX ps_import_identity;
CREATE UNIQUE INDEX ps_import_identity ON ps_imports(tenant_id,file_sha256,marketplace,source_period,mapping_hash,COALESCE(observed_on,'0001-01-01'::date),source_type,period_type,COALESCE(period_start,'0001-01-01'::date),COALESCE(period_end,'0001-01-01'::date),default_sales_scope) WHERE NOT(counts ? 'data_deleted_at');
