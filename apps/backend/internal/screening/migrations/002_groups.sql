ALTER TABLE ps_products ADD COLUMN current_group_rep_id uuid;
ALTER TABLE ps_products ADD FOREIGN KEY(tenant_id,current_group_rep_id) REFERENCES ps_products(tenant_id,id);
