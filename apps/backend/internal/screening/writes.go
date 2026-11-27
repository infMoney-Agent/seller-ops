package screening

import (
	"context"
	"regexp"
)

var asinPattern = regexp.MustCompile(`^[A-Z0-9]{10}$`)
var parameterColumns = []string{"parent_asin", "material", "product_length_cm", "product_width_cm", "product_height_cm", "weight_g", "package_weight_g", "package_length_cm", "package_width_cm", "package_height_cm", "units_per_pack", "carton_units", "carton_weight_g", "carton_length_cm", "carton_width_cm", "carton_height_cm"}
var decimalPattern = regexp.MustCompile(`^[0-9]{1,12}(\.[0-9]{1,6})?$`)

func (s *service) reviewProduct(ctx context.Context, a identity, id string, cmd ReviewProductCommand) (Object, error) {
	return s.mutateLockedProduct(ctx, a, id, cmd.ExpectedDataRevision, cmd.ExpectedReviewRevision, true, func(baseSet string, baseArgs []any) (string, []any, error) {
		if err := cmd.validate(); err != nil {
			return "", nil, err
		}
		args := append(baseArgs, string(cmd.Status), cmd.Reason, cmd.Note)
		return baseSet + ",review_status=$4,review_reason=$5,review_note=$6", args, nil
	})
}

func (s *service) updateProductCategory(ctx context.Context, a identity, id string, cmd UpdateCategoryCommand) (Object, error) {
	return s.mutateLockedProduct(ctx, a, id, cmd.ExpectedDataRevision, cmd.ExpectedReviewRevision, true, func(baseSet string, baseArgs []any) (string, []any, error) {
		if err := cmd.validate(); err != nil {
			return "", nil, err
		}
		args := append(baseArgs, cmd.Category)
		return baseSet + ",manual_category=$4", args, nil
	})
}

func (s *service) updateProductParameters(ctx context.Context, a identity, id string, cmd UpdateParametersCommand) (Object, error) {
	return s.mutateLockedProduct(ctx, a, id, cmd.ExpectedDataRevision, nil, false, cmd.buildUpdate)
}

func (s *service) mutateLockedProduct(ctx context.Context, a identity, id string, expectedDataRev, expectedReviewRev *int64, checkReviewRev bool, build func(baseSet string, baseArgs []any) (string, []any, error)) (Object, error) {
	tx, e := s.db.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if previous, e := receipt(ctx, tx, a); e != nil || previous != nil {
		return previous, e
	}
	var raw []byte
	if e = tx.QueryRow(ctx, "SELECT to_jsonb(p) FROM ps_products p WHERE tenant_id=$1 AND id=$2::uuid AND archived_at IS NULL FOR UPDATE", a.Tenant, id).Scan(&raw); e != nil {
		return nil, e
	}
	before := decodeObject(raw)
	if expectedDataRev == nil || *expectedDataRev != integer(before["data_revision"]) {
		return nil, conflict("商品数据已变化，请刷新")
	}
	if checkReviewRev {
		if expectedReviewRev == nil || *expectedReviewRev != integer(before["review_revision"]) {
			return nil, conflict("人工结果已变化，请刷新")
		}
	}
	baseArgs := []any{a.Tenant, id, a.User}
	baseSet := "review_revision=review_revision+1,reviewed_by=$3,reviewed_at=now(),updated_at=now()"
	set, args, e := build(baseSet, baseArgs)
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, "UPDATE ps_products SET "+set+" WHERE tenant_id=$1 AND id=$2::uuid", args...); e != nil {
		return nil, e
	}
	var afterRaw []byte
	if e = tx.QueryRow(ctx, "SELECT to_jsonb(p) FROM ps_products p WHERE tenant_id=$1 AND id=$2::uuid", a.Tenant, id).Scan(&afterRaw); e != nil {
		return nil, e
	}
	after := decodeObject(afterRaw)
	if _, e = tx.Exec(ctx, "INSERT INTO ps_review_events(tenant_id,id,product_id,actor_id,before_revision,after_revision,before_value,after_value) VALUES($1,$2,$3,$4,$5,$6,$7,$8)", a.Tenant, uuid(), id, a.User, integer(before["review_revision"]), integer(after["review_revision"]), raw, afterRaw); e != nil {
		return nil, e
	}
	if e = saveReceipt(ctx, tx, a, after); e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return after, nil
}

func (s *service) calculateProfit(ctx context.Context, a identity, cmd CalculateProfitCommand) (*ProfitCalculationResult, error) {
	p, e := s.one(ctx, "SELECT to_jsonb(p)||"+profitSourceProjection+" FROM ps_products p WHERE tenant_id=$1 AND id=$2::uuid AND archived_at IS NULL", a.Tenant, cmd.ProductID)
	if e != nil {
		return nil, e
	}
	if cmd.ExpectedDataRevision == nil || *cmd.ExpectedDataRevision != integer(p["data_revision"]) {
		return nil, conflict("商品数据已变化")
	}
	if _, e = s.monthlyReference(ctx, a, p, cmd.Monthly); e != nil {
		return nil, e
	}
	if !cmd.HasScenarioList || len(cmd.Scenarios) == 0 || len(cmd.Scenarios) > 20 {
		if !cmd.HasScenarioList || !cmd.InvalidScenario {
			return nil, invalid("每次试算1到20个方案")
		}
	}
	if cmd.InvalidScenario {
		return nil, invalid("方案无效")
	}
	items := make([]CalculatedScenarioItem, 0, len(cmd.Scenarios))
	for _, in := range cmd.Scenarios {
		out, e := calculateProductProfitStructured(in, p)
		if e != nil {
			return nil, invalid(e.Error())
		}
		items = append(items, CalculatedScenarioItem{Name: optionalImportString(in["name"]), Inputs: in, Output: out})
	}
	return &ProfitCalculationResult{Items: items, FormulaVersion: formulaVersion}, nil
}

func (s *service) monthlyReference(ctx context.Context, a identity, p Object, ref MonthlyReferenceSpec) (Object, error) {
	id := ref.MonthlyID
	if id == "" {
		if ref.ExpectedMonthlyRevision != nil {
			return nil, invalid("月度ID和版本必须同时提供")
		}
		id = str(p["current_monthly_id"])
		if id == "" {
			return nil, nil
		}
	}
	m, e := s.one(ctx, "SELECT to_jsonb(m) FROM ps_product_monthly m WHERE tenant_id=$1 AND id=$2::uuid AND product_id=$3::uuid", a.Tenant, id, p["id"])
	if e != nil {
		return nil, e
	}
	if ref.MonthlyID != "" {
		var expected int64
		if ref.ExpectedMonthlyRevision != nil {
			expected = *ref.ExpectedMonthlyRevision
		}
		if expected != integer(m["revision"]) {
			return nil, conflict("月度数据已变化")
		}
	}
	return m, nil
}

func (s *service) saveProfit(ctx context.Context, a identity, id string, cmd SaveProfitScenarioCommand) (*SavedProfitScenario, error) {
	// Lock the product while taking source and parameter snapshots.
	tx, e := s.db.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if previous, e := typedReceipt[SavedProfitScenario](ctx, tx, a); e != nil || previous != nil {
		return previous, e
	}
	var raw []byte
	if e = tx.QueryRow(ctx, "SELECT to_jsonb(p)||"+profitSourceProjection+" FROM ps_products p WHERE tenant_id=$1 AND id=$2::uuid FOR SHARE", a.Tenant, id).Scan(&raw); e != nil {
		return nil, e
	}
	p := decodeObject(raw)
	if cmd.ExpectedDataRevision == nil || *cmd.ExpectedDataRevision != integer(p["data_revision"]) {
		return nil, conflict("商品数据已变化")
	}
	if !cmd.HasInputs {
		return nil, invalid("测算输入无效")
	}
	in := cmd.Inputs
	out, e := calculateProductProfitStructured(in, p)
	if e != nil {
		return nil, invalid(e.Error())
	}
	m, e := s.monthlyReference(ctx, a, p, cmd.Monthly)
	if e != nil {
		return nil, e
	}
	var mid, mrev, templateID, template any
	if m != nil {
		mid = m["id"]
		mrev = integer(m["revision"])
		var currentRevision int64
		if e = tx.QueryRow(ctx, "SELECT revision FROM ps_product_monthly WHERE tenant_id=$1 AND id=$2::uuid FOR SHARE", a.Tenant, mid).Scan(&currentRevision); e != nil {
			return nil, e
		}
		if currentRevision != integer(m["revision"]) {
			return nil, conflict("月度数据已变化")
		}
	}
	if cmd.TemplateID != "" {
		var templateRaw []byte
		if e = tx.QueryRow(ctx, "SELECT to_jsonb(c) FROM ps_configs c WHERE tenant_id=$1 AND id=$2::uuid AND kind='profit_template'", a.Tenant, cmd.TemplateID).Scan(&templateRaw); e != nil {
			return nil, e
		}
		template = templateRaw
		templateID = cmd.TemplateID
	}
	sid := uuid()
	_, e = tx.Exec(ctx, "INSERT INTO ps_profit_scenarios(tenant_id,id,product_id,template_id,name,inputs,output,template_snapshot,formula_version,product_data_revision,created_by,monthly_id,monthly_revision,monthly_snapshot,product_snapshot) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)", a.Tenant, sid, id, templateID, str(in["name"]), encoded(in), encoded(out), template, formulaVersion, integer(p["data_revision"]), a.User, mid, mrev, nullableJSON(m), raw)
	if e != nil {
		return nil, e
	}
	result := &SavedProfitScenario{ID: sid, Name: optionalImportString(in["name"]), Inputs: in, Output: out, Stale: false}
	if e = saveReceipt(ctx, tx, a, result); e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return result, nil
}

func nullableJSON(v any) any {
	if v == nil {
		return nil
	}
	return encoded(v)
}

func (s *service) createConfig(ctx context.Context, a identity, cmd CreateConfigCommand) (*ConfigRecord, error) {
	if a.Role != "owner" {
		return nil, forbidden("只有所有者可管理公共配置")
	}
	if cmd.Name == "" || len([]rune(cmd.Name)) > 100 {
		return nil, invalid("名称无效")
	}
	if cmd.Kind == ConfigProfitTemplate && cmd.HasConfig {
		if _, e := calculate(cmd.Config); e != nil {
			return nil, invalid(e.Error())
		}
	}
	if !cmd.HasConfig {
		return nil, invalid("配置无效")
	}
	if cmd.Kind == ConfigScreening {
		if e := validatePolicy(cmd.Config); e != nil {
			return nil, invalid(e.Error())
		}
	}
	tx, e := s.db.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if previous, e := typedReceipt[ConfigRecord](ctx, tx, a); e != nil || previous != nil {
		return previous, e
	}
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", a.Tenant); e != nil {
		return nil, e
	}
	var version int
	if e = tx.QueryRow(ctx, "SELECT COALESCE(max(version),0)+1 FROM ps_configs WHERE tenant_id=$1 AND kind=$2", a.Tenant, string(cmd.Kind)).Scan(&version); e != nil {
		return nil, e
	}
	id := uuid()
	hash := shaDigest(encoded(cmd.Config))
	if _, e = tx.Exec(ctx, "INSERT INTO ps_configs(tenant_id,id,kind,version,name,config,config_hash,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8)", a.Tenant, id, string(cmd.Kind), version, cmd.Name, encoded(cmd.Config), hash, a.User); e != nil {
		return nil, e
	}
	result := &ConfigRecord{ID: id, Name: cmd.Name, Version: version, Config: cmd.Config, Params: cmd.Config, Active: false}
	if e = saveReceipt(ctx, tx, a, result); e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return result, nil
}
