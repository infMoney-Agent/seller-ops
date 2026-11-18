package main

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"log"
	"time"
)

// Workers only claim their own engine version, allowing staged migration without
// accidentally consuming jobs still owned by the previous implementation.
const goEngine = "go-v1"

func (s *service) enqueueScreen(ctx context.Context, a identity, b Object, policyID string) (any, error) {
	tx, e := s.db.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if previous, e := receipt(ctx, tx, a); e != nil || previous != nil {
		return previous, e
	}
	var policy []byte
	if policyID == "" {
		e = tx.QueryRow(ctx, "SELECT to_jsonb(c) FROM ps_configs c WHERE tenant_id=$1 AND kind='screening' AND active", a.Tenant).Scan(&policy)
	} else {
		e = tx.QueryRow(ctx, "SELECT to_jsonb(c) FROM ps_configs c WHERE tenant_id=$1 AND id=$2::uuid AND kind='screening'", a.Tenant, policyID).Scan(&policy)
	}
	if e != nil {
		return nil, e
	}
	cfg := decodeObject(policy)
	config, ok := cfg["config"].(map[string]any)
	if !ok {
		return nil, fail(422, "规则配置无效")
	}
	if e = validatePolicy(config); e != nil {
		return nil, fail(422, e.Error())
	}
	ids := []string{}
	if raw, ok := b["product_ids"].([]any); ok {
		if len(raw) > 100000 {
			return nil, fail(422, "商品数量超限")
		}
		for _, v := range raw {
			ids = append(ids, str(v))
		}
	} else if len(b) > 0 {
		if filters, ok := b["filters"].(map[string]any); ok && len(filters) > 0 {
			return nil, fail(422, "Go 重筛暂只接受明确商品集合或全工作区")
		}
	}
	var rows pgx.Rows
	if len(ids) > 0 {
		rows, e = tx.Query(ctx, "UPDATE ps_products SET screen_generation=screen_generation+1 WHERE tenant_id=$1 AND id=ANY($2::uuid[]) RETURNING id::text,data_revision,screen_generation", a.Tenant, ids)
	} else {
		rows, e = tx.Query(ctx, "UPDATE ps_products SET screen_generation=screen_generation+1 WHERE tenant_id=$1 RETURNING id::text,data_revision,screen_generation", a.Tenant)
	}
	if e != nil {
		return nil, e
	}
	targets := []Object{}
	for rows.Next() {
		var id string
		var revision, generation int64
		if e = rows.Scan(&id, &revision, &generation); e != nil {
			rows.Close()
			return nil, e
		}
		targets = append(targets, Object{"id": id, "revision": revision, "generation": generation})
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	id := uuid()
	payload := Object{"engine": goEngine, "policy": cfg, "targets": targets}
	_, e = tx.Exec(ctx, "INSERT INTO ps_jobs(tenant_id,id,kind,idempotency_key,payload,created_by) VALUES($1,$2::uuid,'screen',$2::text,$3,$4)", a.Tenant, id, encoded(payload), a.User)
	if e != nil {
		return nil, e
	}
	result := Object{"job_id": id, "run_id": id, "state": "queued"}
	if e = saveReceipt(ctx, tx, a, result); e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return result, nil
}

func (s *service) worker(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		if _, e := s.db.Exec(ctx, "UPDATE ps_jobs SET state='failed',error=jsonb_build_object('message','任务超过最大重试次数'),lease_until=NULL,lease_token=NULL,updated_at=now() WHERE payload->>'engine'=$1 AND kind='screen' AND attempts>=5 AND state='running' AND lease_until<now()", goEngine); e != nil {
			log.Printf("screening lease recovery failed: %T", e)
			continue
		}
		var raw []byte
		token := uuid()
		e := s.db.QueryRow(ctx, `WITH candidate AS (
   SELECT tenant_id,id FROM ps_jobs WHERE payload->>'engine'=$1 AND kind='screen' AND attempts<5
   AND (state='queued' OR (state='running' AND lease_until<now())) ORDER BY created_at,id FOR UPDATE SKIP LOCKED LIMIT 1)
   UPDATE ps_jobs j SET state='running',attempts=attempts+1,lease_until=now()+interval '60 seconds',lease_token=$2,updated_at=now()
   FROM candidate c WHERE j.tenant_id=c.tenant_id AND j.id=c.id RETURNING to_jsonb(j)`, goEngine, token).Scan(&raw)
		if e == pgx.ErrNoRows {
			continue
		}
		if e != nil {
			log.Printf("screening queue claim failed: %T", e)
			continue
		}
		job := decodeObject(raw)
		tenant := integer(job["tenant_id"])
		id := str(job["id"])
		jobCtx, cancel := context.WithCancel(ctx)
		heartbeatDone := make(chan struct{})
		go func() {
			defer close(heartbeatDone)
			heart := time.NewTicker(15 * time.Second)
			defer heart.Stop()
			for {
				select {
				case <-jobCtx.Done():
					return
				case <-heart.C:
					result, e := s.db.Exec(jobCtx, "UPDATE ps_jobs SET lease_until=now()+interval '60 seconds',updated_at=now() WHERE tenant_id=$1 AND id=$2::uuid AND lease_token=$3::uuid AND state='running'", tenant, id, token)
					if e != nil || result.RowsAffected() != 1 {
						cancel()
						return
					}
				}
			}
		}()
		e = s.runScreen(jobCtx, job, token)
		cancel()
		<-heartbeatDone
		if e != nil && ctx.Err() == nil {
			log.Printf("screening job %s failed: %T", id, e)
			s.db.Exec(ctx, "UPDATE ps_jobs SET state=CASE WHEN attempts<5 THEN 'queued' ELSE 'failed' END,error=$4,lease_until=NULL,lease_token=NULL,updated_at=now() WHERE tenant_id=$1 AND id=$2::uuid AND lease_token=$3::uuid", tenant, id, token, encoded(Object{"message": "初筛任务失败，可查看任务日志后重试"}))
		}
	}
}
func (s *service) runScreen(ctx context.Context, job Object, token string) error {
	payload, ok := job["payload"].(map[string]any)
	if !ok {
		return fmt.Errorf("invalid job payload")
	}
	policy, ok := payload["policy"].(map[string]any)
	if !ok {
		return fmt.Errorf("missing policy snapshot")
	}
	config, ok := policy["config"].(map[string]any)
	if !ok {
		return fmt.Errorf("invalid policy snapshot")
	}
	targets, ok := payload["targets"].([]any)
	if !ok {
		return fmt.Errorf("missing targets")
	}
	tenant := integer(job["tenant_id"])
	id := str(job["id"])
	var completed int64
	if result, ok := job["result"].(map[string]any); ok {
		completed = integer(result["completed"])
	}
	for index := completed; index < int64(len(targets)); index++ {
		target := targets[index].(map[string]any)
		tx, e := s.db.Begin(ctx)
		if e != nil {
			return e
		}
		var lease bool
		e = tx.QueryRow(ctx, "SELECT true FROM ps_jobs WHERE tenant_id=$1 AND id=$2::uuid AND lease_token=$3::uuid AND state='running' FOR UPDATE", tenant, id, token).Scan(&lease)
		if e != nil {
			tx.Rollback(ctx)
			return e
		}
		var raw []byte
		e = tx.QueryRow(ctx, "SELECT to_jsonb(p) FROM ps_products p WHERE tenant_id=$1 AND id=$2::uuid AND data_revision=$3 AND screen_generation=$4 FOR UPDATE", tenant, target["id"], integer(target["revision"]), integer(target["generation"])).Scan(&raw)
		if e != nil && e != pgx.ErrNoRows {
			tx.Rollback(ctx)
			return e
		}
		if e == nil {
			p := decodeObject(raw)
			result := classify(p, config)
			group := str(p["asin"])
			if str(p["seller_id"]) != "" {
				group = shaDigest([]byte(str(p["seller_id"]) + "|" + str(p["asin"])))
			} else {
				group = shaDigest([]byte(group))
			}
			_, e = tx.Exec(ctx, `UPDATE ps_products SET machine_decision=$5,machine_recommendation=$6,machine_direction=$7,machine_group_key=$8,machine_reasons=$9,machine_evidence=$10,machine_policy_id=$11::uuid,machine_source_row_id=current_row_id,machine_monthly_id=current_monthly_id,machine_monthly_revision=current_monthly_revision,current_group_rep_id=id,screened_at=now() WHERE tenant_id=$1 AND id=$2::uuid AND data_revision=$3 AND screen_generation=$4`, tenant, target["id"], integer(target["revision"]), integer(target["generation"]), result["decision"], result["recommendation"], result["direction"], group, encoded(result["reasons"]), encoded(result["evidence"]), policy["id"])
			if e != nil {
				tx.Rollback(ctx)
				return e
			}
		}
		_, e = tx.Exec(ctx, "UPDATE ps_jobs SET result=$4,progress=$5,updated_at=now() WHERE tenant_id=$1 AND id=$2::uuid AND lease_token=$3::uuid", tenant, id, token, encoded(Object{"completed": index + 1, "total": len(targets)}), int((index+1)*100/int64(len(targets))))
		if e != nil {
			tx.Rollback(ctx)
			return e
		}
		if e = tx.Commit(ctx); e != nil {
			return e
		}
	}
	_, e := s.db.Exec(ctx, "UPDATE ps_jobs SET state='succeeded',progress=100,lease_until=NULL,lease_token=NULL,updated_at=now() WHERE tenant_id=$1 AND id=$2::uuid AND lease_token=$3::uuid", tenant, id, token)
	return e
}
