package screening

import (
	"bytes"
	"context"
	"fmt"
	"time"
)

func (s *service) importRemovalPreview(ctx context.Context, a identity, id string) (any, error) {
	if s.gormStore != nil {
		return s.gormImportRemovalPreview(ctx, a, id)
	}
	if _, err := s.one(ctx, "SELECT to_jsonb(i) FROM ps_imports i WHERE tenant_id=$1 AND id=$2::uuid AND NOT(counts ? 'data_deleted_at')", a.Tenant, id); err != nil {
		return nil, err
	}
	return s.one(ctx, `WITH affected AS(SELECT DISTINCT product_id FROM ps_import_rows WHERE tenant_id=$1 AND import_id=$2::uuid AND product_id IS NOT NULL) SELECT jsonb_build_object('products',count(*),'exclusive_products',count(*) FILTER(WHERE NOT EXISTS(SELECT 1 FROM ps_import_rows r JOIN ps_imports i ON i.tenant_id=r.tenant_id AND i.id=r.import_id WHERE r.tenant_id=$1 AND r.product_id=affected.product_id AND r.import_id<>$2::uuid AND r.state='committed' AND NOT(i.counts ? 'data_deleted_at'))),'source_rows',(SELECT count(*) FROM ps_import_rows WHERE tenant_id=$1 AND import_id=$2::uuid)) FROM affected`, a.Tenant, id)
}
func (s *service) removeImportHistory(ctx context.Context, a identity, id string) (any, error) {
	if s.gormStore != nil {
		return s.gormRemoveImportHistory(ctx, a, id)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if previous, e := receipt(ctx, tx, a); e != nil || previous != nil {
		return previous, e
	}
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('import-data',$1))", a.Tenant); err != nil {
		return nil, err
	}
	var state string
	var rawCounts []byte
	if err = tx.QueryRow(ctx, "SELECT state,counts FROM ps_imports WHERE tenant_id=$1 AND id=$2::uuid FOR UPDATE", a.Tenant, id).Scan(&state, &rawCounts); err != nil {
		return nil, err
	}
	if decodeObject(rawCounts)["data_deleted_at"] != nil {
		out := Object{"id": id, "deleted": true}
		if err = saveReceipt(ctx, tx, a, out); err != nil {
			return nil, err
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, err
		}
		return out, nil
	}
	if state == "uploaded" || state == "parsing" || state == "importing" {
		return nil, conflict("文件正在处理中，完成后才能删除")
	}
	var running bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM ps_jobs WHERE tenant_id=$1 AND kind IN ('parse','import') AND payload->>'import_id'=$2 AND state IN ('queued','running'))", a.Tenant, id).Scan(&running); err != nil {
		return nil, err
	}
	if running {
		return nil, conflict("文件正在处理中，完成后才能删除")
	}
	ids := []string{}
	rows, err := tx.Query(ctx, "SELECT DISTINCT product_id::text FROM ps_import_rows WHERE tenant_id=$1 AND import_id=$2::uuid AND product_id IS NOT NULL ORDER BY product_id::text", a.Tenant, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var pid string
		if err = rows.Scan(&pid); err != nil {
			break
		}
		ids = append(ids, pid)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, "SELECT id FROM ps_products WHERE tenant_id=$1 AND id=ANY($2::uuid[]) ORDER BY id FOR UPDATE", a.Tenant, ids); err != nil {
		return nil, err
	}
	parents := []string{}
	rows, err = tx.Query(ctx, "SELECT DISTINCT parent_asin FROM ps_parent_monthly WHERE tenant_id=$1 AND source_row_id IN(SELECT id FROM ps_import_rows WHERE tenant_id=$1 AND import_id=$2::uuid)", a.Tenant, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var parent string
		if err = rows.Scan(&parent); err != nil {
			break
		}
		parents = append(parents, parent)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, "UPDATE ps_imports SET counts=counts||$3::jsonb WHERE tenant_id=$1 AND id=$2::uuid", a.Tenant, id, encoded(Object{"history_deleted_at": time.Now().UTC().Format(time.RFC3339Nano), "data_deleted_at": time.Now().UTC().Format(time.RFC3339Nano), "history_deleted_by": a.User})); err != nil {
		return nil, err
	}
	sourceSQL := "SELECT id FROM ps_import_rows WHERE tenant_id=$1 AND import_id=$2::uuid"
	monthlySQL := "SELECT id FROM ps_product_monthly WHERE tenant_id=$1 AND source_row_id IN (" + sourceSQL + ")"
	if _, err = tx.Exec(ctx, "DELETE FROM ps_profit_scenarios WHERE tenant_id=$1 AND record_kind='imported' AND source_row_id IN ("+sourceSQL+")", a.Tenant, id); err != nil {
		return nil, err
	}
	// Calculation snapshots remain audit records; invalidate only their live foreign-key reference.
	if _, err = tx.Exec(ctx, "UPDATE ps_profit_scenarios SET monthly_id=NULL,monthly_revision=NULL WHERE tenant_id=$1 AND monthly_id IN ("+monthlySQL+")", a.Tenant, id); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, "UPDATE ps_products SET current_monthly_id=NULL,current_monthly_revision=NULL,machine_monthly_id=NULL,machine_monthly_revision=NULL,current_row_id=NULL,machine_source_row_id=NULL,machine_decision=NULL,machine_group_key=NULL,current_group_rep_id=NULL,machine_reasons='[]',machine_evidence='[]',screen_generation=screen_generation+1,data_revision=data_revision+1 WHERE tenant_id=$1 AND id=ANY($2::uuid[])", a.Tenant, ids); err != nil {
		return nil, err
	}
	deleted, err := tx.Exec(ctx, "DELETE FROM ps_product_monthly WHERE tenant_id=$1 AND source_row_id IN ("+sourceSQL+")", a.Tenant, id)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM ps_parent_monthly WHERE tenant_id=$1 AND source_row_id IN ("+sourceSQL+")", a.Tenant, id); err != nil {
		return nil, err
	}
	// Replay valid surviving observations in provenance order to rebuild overwritten periods.
	rows, err = tx.Query(ctx, `SELECT to_jsonb(r),to_jsonb(i) FROM ps_import_rows r JOIN ps_imports i ON i.tenant_id=r.tenant_id AND i.id=r.import_id WHERE r.tenant_id=$1 AND r.state='committed' AND NOT(i.counts ? 'data_deleted_at') AND (r.product_id=ANY($2::uuid[]) OR r.normalized_data->>'parent_asin'=ANY($3::text[])) ORDER BY i.period_end,i.observed_on,i.created_at,r.member_path,r.sheet_name,r.row_no,r.id`, a.Tenant, ids, parents)
	if err != nil {
		return nil, err
	}
	type observation struct{ row, batch Object }
	observations := []observation{}
	for rows.Next() {
		var row, batch []byte
		if err = rows.Scan(&row, &batch); err != nil {
			break
		}
		observations = append(observations, observation{decodeObject(row), decodeObject(batch)})
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	affected := map[string]bool{}
	for _, pid := range ids {
		affected[pid] = true
	}
	columns := append([]string{"title", "title_zh", "image_url", "category", "brand", "seller_id", "seller_name", "seller_region", "specs"}, parameterColumns...)
	for _, f := range importedColumns {
		if f.Table == "product" && !containsColumn(columns, f.Key) {
			columns = append(columns, f.Key)
		}
	}
	fields := map[string]Object{}
	for _, ob := range observations {
		raw, _ := ob.row["raw_data"].(map[string]any)
		mapping, _ := ob.batch["mapping"].(map[string]any)
		n, errors := normalizeRow(raw, mapping)
		if len(errors) > 0 {
			return nil, conflict("剩余来源存在无法解析的数据，删除已取消，请先核查来源")
		}
		meta, e := period(ob.batch, true)
		if e != nil {
			return nil, e
		}
		pid := str(ob.row["product_id"])
		if !affected[pid] {
			n["listing_role"] = "parent"
		} else {
			if fields[pid] == nil {
				fields[pid] = Object{}
			}
			for _, column := range columns {
				if n[column] != nil && str(n[column]) != "" {
					fields[pid][column] = n[column]
				}
			}
		}
		n["restore_monthly_only"] = true
		if _, err = importObservation(ctx, tx, a, ob.batch, meta, ob.row, n); err != nil {
			return nil, err
		}
	}
	archived := 0
	for _, pid := range ids {
		values := fields[pid]
		if values == nil {
			archived++
			if _, err = tx.Exec(ctx, "UPDATE ps_products SET archived_at=now(),updated_at=now() WHERE tenant_id=$1 AND id=$2::uuid", a.Tenant, pid); err != nil {
				return nil, err
			}
			continue
		}
		// Apply the latest explicit human change for each editable parameter, including cleared values.
		events, e := tx.Query(ctx, "SELECT before_value,after_value FROM ps_review_events WHERE tenant_id=$1 AND product_id=$2::uuid ORDER BY after_revision DESC", a.Tenant, pid)
		if e != nil {
			return nil, e
		}
		overrides := map[string]bool{}
		for events.Next() {
			var before, after []byte
			if err = events.Scan(&before, &after); err != nil {
				break
			}
			b, v := decodeObject(before), decodeObject(after)
			for _, column := range parameterColumns {
				if !overrides[column] && !bytes.Equal(encoded(b[column]), encoded(v[column])) {
					values[column] = v[column]
					overrides[column] = true
				}
			}
		}
		if err == nil {
			err = events.Err()
		}
		events.Close()
		if err != nil {
			return nil, err
		}
		args := []any{a.Tenant, pid}
		set := "archived_at=NULL,updated_at=now()"
		for _, column := range columns {
			v := values[column]
			if column == "specs" {
				if v == nil {
					v = Object{}
				}
				v = encoded(v)
			} else if str(v) == "" {
				v = nil
			}
			args = append(args, v)
			set += fmt.Sprintf(",%s=$%d", column, len(args))
		}
		if _, err = tx.Exec(ctx, "UPDATE ps_products SET "+set+" WHERE tenant_id=$1 AND id=$2::uuid", args...); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `UPDATE ps_products p SET current_monthly_id=m.id,current_monthly_revision=m.revision,current_row_id=m.source_row_id FROM (SELECT m.* FROM ps_product_monthly m JOIN ps_import_rows r ON r.tenant_id=m.tenant_id AND r.id=m.source_row_id JOIN ps_imports i ON i.tenant_id=r.tenant_id AND i.id=r.import_id WHERE m.tenant_id=$1 AND m.product_id=$2::uuid ORDER BY m.stat_month DESC,m.period_end DESC,m.observed_on DESC,i.created_at DESC,r.member_path DESC,r.sheet_name DESC,r.row_no DESC,r.id DESC LIMIT 1)m WHERE p.tenant_id=$1 AND p.id=$2::uuid`, a.Tenant, pid); err != nil {
			return nil, err
		}
	}
	if _, err = tx.Exec(ctx, "UPDATE ps_profit_scenarios q SET monthly_id=m.id,monthly_revision=m.revision FROM ps_product_monthly m WHERE q.tenant_id=$1 AND q.record_kind='imported' AND q.monthly_id IS NULL AND m.tenant_id=q.tenant_id AND m.product_id=q.product_id AND m.source_row_id=q.source_row_id", a.Tenant); err != nil {
		return nil, err
	}
	out := Object{"id": id, "deleted": true, "removed_products": archived, "retained_products": len(ids) - archived, "removed_monthly": deleted.RowsAffected()}
	if err = saveReceipt(ctx, tx, a, out); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}
