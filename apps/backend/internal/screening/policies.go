package screening

import (
	"context"
	"encoding/json"
)

func unmarshal(data []byte, target any) error { return json.Unmarshal(data, target) }
func (s *service) ensurePolicy(ctx context.Context, a identity) error {
	tx, e := s.db.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", a.Tenant); e != nil {
		return e
	}
	var exists bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM ps_configs WHERE tenant_id=$1 AND kind='screening' AND active)", a.Tenant).Scan(&exists); e != nil {
		return e
	}
	if exists {
		return nil
	}
	config := decodeObject(defaultPolicyData)
	if e = validatePolicy(config); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, "INSERT INTO ps_configs(tenant_id,id,kind,version,name,config,config_hash,active,created_by) SELECT $1,$2,'screening',COALESCE(max(version),0)+1,'通用初筛规则',$3,$4,true,$5 FROM ps_configs WHERE tenant_id=$1 AND kind='screening'", a.Tenant, uuid(), defaultPolicyData, shaDigest(defaultPolicyData), a.User)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func (s *service) loadValidatedPolicy(ctx context.Context, a identity, id string) (Object, error) {
	policy, e := s.one(ctx, "SELECT to_jsonb(c) FROM ps_configs c WHERE tenant_id=$1 AND id=$2::uuid AND kind='screening'", a.Tenant, id)
	if e != nil {
		return nil, e
	}
	config := policy["config"].(map[string]any)
	if e = validatePolicy(config); e != nil {
		return nil, invalid(e.Error())
	}
	return config, nil
}

func (s *service) testPolicy(ctx context.Context, a identity, id string, cmd TestPolicyCommand) (*PolicyTestResult, error) {
	config, e := s.loadValidatedPolicy(ctx, a, id)
	if e != nil {
		return nil, e
	}
	if !cmd.ValidList || len(cmd.ProductIDs) < 1 || len(cmd.ProductIDs) > 100 {
		return nil, invalid("试运行需要1到100个明确商品")
	}
	items := make([]Object, 0, len(cmd.ProductIDs))
	for _, pid := range cmd.ProductIDs {
		p, e := s.productView(ctx, a, pid)
		if e != nil {
			return nil, e
		}
		out := classify(p, config)
		out["id"] = pid
		items = append(items, out)
	}
	return &PolicyTestResult{Items: items}, nil
}

func (s *service) activatePolicy(ctx context.Context, a identity, id string) (any, error) {
	if _, e := s.loadValidatedPolicy(ctx, a, id); e != nil {
		return nil, e
	}
	if a.Role != "owner" {
		return nil, forbidden("只有所有者可启用规则")
	}
	tx, e := s.db.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if previous, e := typedReceipt[JobSubmission](ctx, tx, a); e != nil || previous != nil {
		return previous, e
	}
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", a.Tenant); e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, "UPDATE ps_configs SET active=false WHERE tenant_id=$1 AND kind='screening' AND active", a.Tenant); e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, "UPDATE ps_configs SET active=true WHERE tenant_id=$1 AND id=$2::uuid", a.Tenant, id); e != nil {
		return nil, e
	}
	result, e := s.enqueueScreenTx(ctx, tx, a, EnqueueScreenCommand{}, id)
	if e != nil {
		return nil, e
	}
	if e = saveReceipt(ctx, tx, a, result); e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return result, nil
}
