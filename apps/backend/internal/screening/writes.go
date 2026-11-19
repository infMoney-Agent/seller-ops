package screening

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

var asinPattern = regexp.MustCompile(`^[A-Z0-9]{10}$`)
var parameterColumns = []string{"parent_asin", "material", "weight_g", "package_weight_g", "package_length_cm", "package_width_cm", "package_height_cm", "units_per_pack", "carton_units", "carton_weight_g", "carton_length_cm", "carton_width_cm", "carton_height_cm"}

func (s *service) mutateProduct(ctx context.Context, a identity, id, action string, b Object) (any, error) {
	tx, e := s.db.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if previous, e := receipt(ctx, tx, a); e != nil || previous != nil {
		return previous, e
	}
	var raw []byte
	if e = tx.QueryRow(ctx, "SELECT to_jsonb(p) FROM ps_products p WHERE tenant_id=$1 AND id=$2::uuid FOR UPDATE", a.Tenant, id).Scan(&raw); e != nil {
		return nil, e
	}
	before := decodeObject(raw)
	if _, ok := b["expected_data_revision"]; !ok || integer(b["expected_data_revision"]) != integer(before["data_revision"]) {
		return nil, fail(409, "商品数据已变化，请刷新")
	}
	if action != "parameters" {
		if _, ok := b["expected_review_revision"]; !ok || integer(b["expected_review_revision"]) != integer(before["review_revision"]) {
			return nil, fail(409, "人工结果已变化，请刷新")
		}
	}
	args := []any{a.Tenant, id, a.User}
	set := "review_revision=review_revision+1,reviewed_by=$3,reviewed_at=now(),updated_at=now()"
	switch action {
	case "review":
		status := str(b["status"])
		if status != "kept" && status != "excluded" && status != "unsure" {
			return nil, fail(422, "复核结论无效")
		}
		if status != "kept" && strings.TrimSpace(str(b["reason"])) == "" {
			return nil, fail(422, "排除和待确认必须填写原因")
		}
		if len(str(b["reason"])) > 8000 || len(str(b["note"])) > 40000 {
			return nil, fail(422, "原因或备注过长")
		}
		args = append(args, status, str(b["reason"]), str(b["note"]))
		set += ",review_status=$4,review_reason=$5,review_note=$6"
	case "category":
		category := strings.TrimSpace(str(b["category"]))
		if category == "" || len([]rune(category)) > 200 {
			return nil, fail(422, "人工分类不能为空且不超过200字")
		}
		args = append(args, category)
		set += ",manual_category=$4"
	case "parameters":
		for key := range b {
			if key == "expected_data_revision" {
				continue
			}
			known := false
			for _, column := range parameterColumns {
				if key == column {
					known = true
				}
			}
			if !known {
				return nil, fail(422, "未知商品参数")
			}
		}
		for _, column := range parameterColumns {
			v, ok := b[column]
			if !ok {
				continue
			}
			if v != nil {
				text := str(v)
				switch column {
				case "parent_asin":
					// 独立商品允许父ASIN等于自身。
					if !asinPattern.MatchString(text) {
						return nil, fail(422, "父ASIN无效")
					}
				case "material":
					if len([]rune(text)) > 200 {
						return nil, fail(422, "材质过长")
					}
				default:
					if !decimalPattern.MatchString(text) {
						return nil, fail(422, "参数必须是非负数且最多6位小数")
					}
					if column == "units_per_pack" || column == "carton_units" {
						if integer(v) < 1 || integer(v) > 2147483647 || strings.Contains(text, ".") {
							return nil, fail(422, "包装数量必须是正整数")
						}
					}
				}
			}
			args = append(args, v)
			set += fmt.Sprintf(",%s=$%d", column, len(args))
		}
		set += ",data_revision=data_revision+1,screen_generation=screen_generation+1,machine_decision=NULL,machine_group_key=NULL,current_group_rep_id=NULL"
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

var decimalPattern = regexp.MustCompile(`^[0-9]{1,12}(\.[0-9]{1,6})?$`)

func (s *service) calculateRequest(ctx context.Context, a identity, b Object) (any, error) {
	p, e := s.product(ctx, a, str(b["product_id"]))
	if e != nil {
		return nil, e
	}
	if _, ok := b["expected_data_revision"]; !ok || integer(b["expected_data_revision"]) != integer(p["data_revision"]) {
		return nil, fail(409, "商品数据已变化")
	}
	if _, e = s.monthlyReference(ctx, a, p, b); e != nil {
		return nil, e
	}
	scenarios, ok := b["scenarios"].([]any)
	if !ok || len(scenarios) == 0 || len(scenarios) > 20 {
		return nil, fail(422, "每次试算1到20个方案")
	}
	items := []Object{}
	for _, v := range scenarios {
		in, ok := v.(map[string]any)
		if !ok {
			return nil, fail(422, "方案无效")
		}
		out, e := calculate(in)
		if e != nil {
			return nil, fail(422, e.Error())
		}
		items = append(items, Object{"name": in["name"], "inputs": in, "output": out})
	}
	return Object{"items": items, "formula_version": formulaVersion}, nil
}
func (s *service) monthlyReference(ctx context.Context, a identity, p, b Object) (Object, error) {
	id := str(b["monthly_id"])
	if id == "" {
		if b["expected_monthly_revision"] != nil {
			return nil, fail(422, "月度ID和版本必须同时提供")
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
	if b["monthly_id"] != nil && integer(b["expected_monthly_revision"]) != integer(m["revision"]) {
		return nil, fail(409, "月度数据已变化")
	}
	return m, nil
}
func (s *service) saveProfit(ctx context.Context, a identity, id string, b Object) (any, error) {
	// Lock the product while taking source and parameter snapshots.
	tx, e := s.db.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if previous, e := receipt(ctx, tx, a); e != nil || previous != nil {
		return previous, e
	}
	var raw []byte
	if e = tx.QueryRow(ctx, "SELECT to_jsonb(p) FROM ps_products p WHERE tenant_id=$1 AND id=$2::uuid FOR SHARE", a.Tenant, id).Scan(&raw); e != nil {
		return nil, e
	}
	p := decodeObject(raw)
	if _, ok := b["expected_data_revision"]; !ok || integer(b["expected_data_revision"]) != integer(p["data_revision"]) {
		return nil, fail(409, "商品数据已变化")
	}
	in, ok := b["inputs"].(map[string]any)
	if !ok {
		return nil, fail(422, "测算输入无效")
	}
	out, e := calculate(in)
	if e != nil {
		return nil, fail(422, e.Error())
	}
	m, e := s.monthlyReference(ctx, a, p, b)
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
			return nil, fail(409, "月度数据已变化")
		}
	}
	if tid := str(b["template_id"]); tid != "" {
		var templateRaw []byte
		if e = tx.QueryRow(ctx, "SELECT to_jsonb(c) FROM ps_configs c WHERE tenant_id=$1 AND id=$2::uuid AND kind='profit_template'", a.Tenant, tid).Scan(&templateRaw); e != nil {
			return nil, e
		}
		template = templateRaw
		templateID = tid
	}
	sid := uuid()
	_, e = tx.Exec(ctx, "INSERT INTO ps_profit_scenarios(tenant_id,id,product_id,template_id,name,inputs,output,template_snapshot,formula_version,product_data_revision,created_by,monthly_id,monthly_revision,monthly_snapshot,product_snapshot) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)", a.Tenant, sid, id, templateID, str(in["name"]), encoded(in), encoded(out), template, formulaVersion, integer(p["data_revision"]), a.User, mid, mrev, nullableJSON(m), raw)
	if e != nil {
		return nil, e
	}
	result := Object{"id": sid, "name": in["name"], "inputs": in, "output": out, "stale": false}
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
func (s *service) createConfig(ctx context.Context, a identity, path string, b Object) (any, error) {
	if a.Role != "owner" {
		return nil, fail(403, "只有所有者可管理公共配置")
	}
	name := strings.TrimSpace(str(b["name"]))
	if name == "" || len([]rune(name)) > 100 {
		return nil, fail(422, "名称无效")
	}
	kind := "screening"
	config, ok := b["config"].(map[string]any)
	if path == "profit/templates" {
		kind = "profit_template"
		config, ok = b["params"].(map[string]any)
		if ok {
			if _, e := calculate(config); e != nil {
				return nil, fail(422, e.Error())
			}
		}
	}
	if !ok {
		return nil, fail(422, "配置无效")
	}
	if kind == "screening" {
		if e := validatePolicy(config); e != nil {
			return nil, fail(422, e.Error())
		}
	}
	tx, e := s.db.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if previous, e := receipt(ctx, tx, a); e != nil || previous != nil {
		return previous, e
	}
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", a.Tenant); e != nil {
		return nil, e
	}
	var version int
	if e = tx.QueryRow(ctx, "SELECT COALESCE(max(version),0)+1 FROM ps_configs WHERE tenant_id=$1 AND kind=$2", a.Tenant, kind).Scan(&version); e != nil {
		return nil, e
	}
	id := uuid()
	hash := shaDigest(encoded(config))
	if _, e = tx.Exec(ctx, "INSERT INTO ps_configs(tenant_id,id,kind,version,name,config,config_hash,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8)", a.Tenant, id, kind, version, name, encoded(config), hash, a.User); e != nil {
		return nil, e
	}
	result := Object{"id": id, "name": name, "version": version, "config": config, "params": config, "active": false}
	if e = saveReceipt(ctx, tx, a, result); e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return result, nil
}
