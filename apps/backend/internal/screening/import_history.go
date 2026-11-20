package screening

import (
	"context"
	"time"
)

// History removal never deletes products, observations, original files or manual decisions.
func (s *service) removeImportHistory(ctx context.Context, a identity, id string) (any, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if previous, err := receipt(ctx, tx, a); err != nil || previous != nil {
		return previous, err
	}
	var state string
	if err = tx.QueryRow(ctx, "SELECT state FROM ps_imports WHERE tenant_id=$1 AND id=$2::uuid FOR UPDATE", a.Tenant, id).Scan(&state); err != nil {
		return nil, err
	}
	if state == "uploaded" || state == "parsing" || state == "importing" {
		return nil, fail(409, "文件正在处理中，完成后才能删除记录")
	}
	var running bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM ps_jobs WHERE tenant_id=$1 AND kind IN ('parse','import') AND payload->>'import_id'=$2 AND state IN ('queued','running'))", a.Tenant, id).Scan(&running); err != nil {
		return nil, err
	}
	if running {
		return nil, fail(409, "文件正在处理中，完成后才能删除记录")
	}
	_, err = tx.Exec(ctx, "UPDATE ps_imports SET counts=counts||$3::jsonb WHERE tenant_id=$1 AND id=$2::uuid", a.Tenant, id, encoded(Object{"history_deleted_at": time.Now().UTC().Format(time.RFC3339Nano), "history_deleted_by": a.User}))
	if err != nil {
		return nil, err
	}
	out := Object{"id": id, "deleted": true}
	if err = saveReceipt(ctx, tx, a, out); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}
