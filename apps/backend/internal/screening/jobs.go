package screening

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"log"
	"time"
)

// Workers only claim their own engine version, allowing staged migration without
// accidentally consuming jobs still owned by the previous implementation.
const goEngine = "go-v1"

func (s *service) enqueueScreen(ctx context.Context, a identity, cmd EnqueueScreenCommand, policyID string) (*JobSubmission, error) {
	selection := cmd.Selection
	if selection.HasProductIDs || selection.HasFilterKeys || len(selection.Filters) > 0 {
		ids, e := s.selection(ctx, a, selection)
		if e != nil {
			return nil, e
		}
		selection = ProductSelectionCommand{ProductIDs: ids, HasProductIDs: true}
	}
	tx, e := s.db.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if previous, e := typedReceipt[JobSubmission](ctx, tx, a); e != nil || previous != nil {
		return previous, e
	}
	result, e := s.enqueueScreenTx(ctx, tx, a, EnqueueScreenCommand{Selection: selection}, policyID)
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

func (s *service) enqueueScreenTx(ctx context.Context, tx pgx.Tx, a identity, cmd EnqueueScreenCommand, policyID string) (*JobSubmission, error) {
	var e error
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
		return nil, invalid("规则配置无效")
	}
	if e = validatePolicy(config); e != nil {
		return nil, invalid(e.Error())
	}
	ids := cmd.Selection.ProductIDs
	if cmd.Selection.HasProductIDs {
		if len(ids) > 100000 {
			return nil, invalid("商品数量超限")
		}
	} else if cmd.Selection.HasFilterKeys {
		return nil, invalid("Go 重筛暂只接受明确商品集合或全工作区")
	}
	var rows pgx.Rows
	if cmd.Selection.HasProductIDs {
		rows, e = tx.Query(ctx, "UPDATE ps_products SET screen_generation=screen_generation+1 WHERE tenant_id=$1 AND archived_at IS NULL AND id=ANY($2::uuid[]) RETURNING id::text,data_revision,screen_generation", a.Tenant, ids)
	} else {
		rows, e = tx.Query(ctx, "UPDATE ps_products SET screen_generation=screen_generation+1 WHERE tenant_id=$1 AND archived_at IS NULL RETURNING id::text,data_revision,screen_generation", a.Tenant)
	}
	if e != nil {
		return nil, e
	}
	targets := []ScreenTargetSnapshot{}
	for rows.Next() {
		var target ScreenTargetSnapshot
		if e = rows.Scan(&target.ID, &target.Revision, &target.Generation); e != nil {
			rows.Close()
			return nil, e
		}
		targets = append(targets, target)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	id := uuid()
	payload := ScreenJobPayload{Engine: goEngine, Policy: cfg, Targets: targets}
	_, e = tx.Exec(ctx, "INSERT INTO ps_jobs(tenant_id,id,kind,idempotency_key,payload,created_by) VALUES($1,$2::uuid,'screen',$2::text,$3,$4)", a.Tenant, id, encoded(payload), a.User)
	if e != nil {
		return nil, e
	}
	return &JobSubmission{JobID: id, RunID: id, State: "queued"}, nil
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
		if _, e := s.db.Exec(ctx, "WITH failed AS (UPDATE ps_jobs SET state='failed',error=jsonb_build_object('message','任务超过最大重试次数，请重试'),lease_until=NULL,lease_token=NULL,updated_at=now() WHERE payload->>'engine'=$1 AND kind IN ('screen','parse','import','export','profit_batch') AND attempts>=5 AND state='running' AND lease_until<now() RETURNING tenant_id,kind,payload) UPDATE ps_imports i SET state='failed' FROM failed j WHERE j.kind IN ('parse','import') AND i.tenant_id=j.tenant_id AND i.id::text=j.payload->>'import_id'", goEngine); e != nil {
			log.Printf("screening lease recovery failed: %T", e)
			continue
		}
		var raw []byte
		token := uuid()
		e := s.db.QueryRow(ctx, `WITH candidate AS (
   SELECT tenant_id,id FROM ps_jobs WHERE payload->>'engine'=$1 AND kind IN ('screen','parse','import','export','profit_batch') AND attempts<5
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
		e = s.runWork(jobCtx, job, token)
		cancel()
		<-heartbeatDone
		if e != nil && ctx.Err() == nil {
			detail := workError(e)
			log.Printf("screening job %s tenant=%d kind=%s failed: code=%s constraint=%s", id, tenant, str(job["kind"]), str(detail["code"]), str(detail["constraint"]))
			if err := s.recordWorkFailure(ctx, tenant, id, token, detail); err != nil {
				log.Printf("screening failure persistence failed: %T", err)
			}
		}
	}
}

func (s *service) runScreen(ctx context.Context, job Object, token string) error {
	payload, err := decodeJobPayload[ScreenJobPayload](job)
	if err != nil || payload.Policy == nil {
		return fmt.Errorf("invalid job payload")
	}
	config, ok := payload.Policy["config"].(map[string]any)
	if !ok {
		return fmt.Errorf("invalid policy snapshot")
	}
	targets := payload.Targets
	if targets == nil {
		return fmt.Errorf("missing targets")
	}
	tenant := integer(job["tenant_id"])
	id := str(job["id"])
	var completed int64
	if result, ok := job["result"].(map[string]any); ok {
		completed = integer(result["completed"])
	}
	for index := completed; index < int64(len(targets)); index++ {
		target := targets[index]
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
		e = tx.QueryRow(ctx, "SELECT to_jsonb(p)||jsonb_build_object('price_usd',m.price_usd::text,'sales_scope',m.sales_scope,'monthly_sales',CASE WHEN m.sales_scope='child' THEN m.monthly_sales END,'sales_quantity_kind',m.sales_quantity_kind,'occurrences',(SELECT count(DISTINCT stat_month) FROM ps_product_monthly h WHERE h.tenant_id=p.tenant_id AND h.product_id=p.id)) FROM ps_products p LEFT JOIN ps_product_monthly m ON m.tenant_id=p.tenant_id AND m.id=p.current_monthly_id WHERE p.tenant_id=$1 AND p.id=$2::uuid AND p.data_revision=$3 AND p.screen_generation=$4 FOR UPDATE OF p", tenant, target.ID, target.Revision, target.Generation).Scan(&raw)
		if e != nil && e != pgx.ErrNoRows {
			tx.Rollback(ctx)
			return e
		}
		if e == nil {
			p := decodeObject(raw)
			result := classify(p, config)
			group := groupKey(p, result)
			_, e = tx.Exec(ctx, `UPDATE ps_products SET machine_decision=$5,machine_recommendation=$6,machine_direction=$7,machine_group_key=$8,machine_reasons=$9,machine_evidence=$10,machine_policy_id=$11::uuid,machine_source_row_id=current_row_id,machine_monthly_id=current_monthly_id,machine_monthly_revision=current_monthly_revision,current_group_rep_id=id,screened_at=now() WHERE tenant_id=$1 AND id=$2::uuid AND data_revision=$3 AND screen_generation=$4`, tenant, target.ID, target.Revision, target.Generation, result["decision"], result["recommendation"], result["direction"], group, encoded(result["reasons"]), encoded(result["evidence"]), payload.Policy["id"])
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

func (s *service) runWork(ctx context.Context, job Object, token string) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if e, ok := recovered.(error); ok {
				err = e
			} else {
				err = fmt.Errorf("task panic: %T", recovered)
			}
		}
	}()
	switch str(job["kind"]) {
	case "parse":
		return s.runParse(ctx, job, token)
	case "import":
		return s.runImport(ctx, job, token)
	case "export":
		return s.runExport(ctx, job, token)
	case "profit_batch":
		return s.runProfitBatch(ctx, job, token)
	case "screen":
		return s.runScreen(ctx, job, token)
	default:
		return fmt.Errorf("unsupported job kind")
	}
}

// Persist job and import failure together; never expose SQL detail or raw rows.
func workError(err error) Object {
	out := Object{"code": "job_failed", "message": "任务执行失败，请重试；若持续失败，请联系管理员并提供任务编号"}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		out["code"] = pg.Code
		out["constraint"] = pg.ConstraintName
		switch pg.Code {
		case "23514":
			out["message"] = "数据未通过数据库约束校验，请检查字段或更新数据库迁移后重试"
		case "23505":
			out["message"] = "存在重复数据，请检查来源期、字段映射及重复导入记录"
		case "23503":
			out["message"] = "关联数据不存在，请刷新后重试"
		case "53100":
			out["message"] = "服务器磁盘空间不足，请清理空间后重试"
		}
	}
	var d domainError
	if errors.As(err, &d) {
		out["code"] = "validation_failed"
		out["message"] = d.message
	}
	return out
}
func (s *service) recordWorkFailure(ctx context.Context, tenant int64, id, token string, detail Object) error {
	_, err := s.db.Exec(ctx, `WITH failed AS (
 UPDATE ps_jobs SET state=CASE WHEN attempts<5 THEN 'queued' ELSE 'failed' END,error=$4,lease_until=NULL,lease_token=NULL,updated_at=now()
 WHERE tenant_id=$1 AND id=$2::uuid AND lease_token=$3::uuid RETURNING tenant_id,kind,state,payload)
 UPDATE ps_imports i SET state='failed' FROM failed j WHERE j.state='failed' AND j.kind IN ('parse','import') AND i.tenant_id=j.tenant_id AND i.id::text=j.payload->>'import_id'`, tenant, id, token, encoded(detail))
	return err
}
