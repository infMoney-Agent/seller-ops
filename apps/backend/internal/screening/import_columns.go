package screening

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"math/big"
	"regexp"
	"strings"
)

//go:embed data/import-columns.json
var importedColumnsJSON []byte

type importedColumn struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Table string `json:"table"`
	Type  string `json:"type"`
}

var importedColumns = func() []importedColumn {
	var fields []importedColumn
	if err := json.Unmarshal(importedColumnsJSON, &fields); err != nil {
		panic(err)
	}
	return fields
}()

func containsColumn(columns []string, key string) bool {
	for _, column := range columns {
		if key == column {
			return true
		}
	}
	return false
}

var measurementNumber = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)

func measurement(text string, weight bool) []string {
	text = strings.ToLower(strings.TrimSpace(text))
	numbers := measurementNumber.FindAllString(text, -1)
	factor := "1"
	if weight {
		if len(numbers) != 1 {
			return nil
		}
		switch {
		case strings.Contains(text, "kg"), strings.Contains(text, "千克"):
			factor = "1000"
		case strings.Contains(text, "pound"), strings.Contains(text, "lb"):
			factor = "453.59237"
		case strings.Contains(text, "ounce"), strings.Contains(text, "oz"):
			factor = "28.349523125"
		case strings.Contains(text, "g"), strings.Contains(text, "克"):
		default:
			return nil
		}
	} else {
		if len(numbers) != 3 {
			return nil
		}
		switch {
		case strings.Contains(text, "cm"), strings.Contains(text, "厘米"):
		case strings.Contains(text, "inch"), strings.Contains(text, "inches"), strings.Contains(text, "英寸"), strings.HasSuffix(text, " in"):
			factor = "2.54"
		case strings.Contains(text, "mm"), strings.Contains(text, "毫米"):
			factor = "0.1"
		default:
			return nil
		}
	}
	multiplier, _ := new(big.Rat).SetString(factor)
	for i, n := range numbers {
		v, ok := new(big.Rat).SetString(n)
		if !ok {
			return nil
		}
		numbers[i] = new(big.Rat).Mul(v, multiplier).FloatString(6)
	}
	return numbers
}
func sourceDecimal(value any, percent bool) any {
	text := strings.TrimSpace(str(value))
	text = strings.ReplaceAll(strings.ReplaceAll(text, ",", ""), "$", "")
	hasPercent := strings.HasSuffix(text, "%")
	text = strings.TrimSuffix(text, "%")
	valueRat, ok := new(big.Rat).SetString(text)
	if !ok {
		return nil
	}
	if !percent && valueRat.Sign() < 0 {
		return nil
	}
	if hasPercent {
		valueRat.Quo(valueRat, big.NewRat(100, 1))
	}
	if new(big.Rat).Abs(valueRat).Cmp(big.NewRat(1000000000000, 1)) >= 0 {
		return nil
	}
	return valueRat.FloatString(6)
}
func normalizeImportedColumns(n Object) {
	for _, field := range importedColumns {
		if strings.HasPrefix(field.Type, "numeric") {
			n[field.Key] = sourceDecimal(n[field.Key], field.Key == "imported_gross_margin_rate")
		}
	}
	for _, target := range []struct{ raw, original, prefix string }{{"product_dimensions", "product_dimensions_original", "product"}, {"package_dimensions", "package_dimensions_original", "package"}} {
		values := measurement(str(n[target.raw]), false)
		if values == nil {
			values = measurement(str(n[target.original]), false)
		}
		if len(values) == 3 {
			for i, axis := range []string{"length", "width", "height"} {
				key := target.prefix + "_" + axis + "_cm"
				if n[key] == nil || str(n[key]) == "" {
					n[key] = values[i]
				}
			}
		}
	}
	for _, target := range []struct{ raw, original, key string }{{"product_weight_converted", "product_weight_original", "weight_g"}, {"package_weight_converted", "package_weight_original", "package_weight_g"}} {
		if n[target.key] != nil && str(n[target.key]) != "" {
			continue
		}
		values := measurement(str(n[target.raw]), true)
		if values == nil {
			values = measurement(str(n[target.original]), true)
		}
		if len(values) == 1 {
			n[target.key] = values[0]
		}
	}
}

// Each source estimate is immutable and is never selected as a calculated profit.
func saveImportedMonthly(ctx context.Context, tx pgx.Tx, a identity, productID, rowID string, n Object) error {
	args := []any{a.Tenant, productID, rowID}
	sets := []string{}
	for _, field := range importedColumns {
		if field.Table != "monthly" || n[field.Key] == nil || str(n[field.Key]) == "" {
			continue
		}
		args = append(args, n[field.Key])
		sets = append(sets, fmt.Sprintf("%s=$%d", field.Key, len(args)))
	}
	if len(sets) > 0 {
		if _, err := tx.Exec(ctx, "UPDATE ps_product_monthly SET "+strings.Join(sets, ",")+" WHERE tenant_id=$1 AND product_id=$2::uuid AND source_row_id=$3::uuid", args...); err != nil {
			return err
		}
	}
	if n["imported_fba_fee_usd"] == nil && n["imported_gross_margin_rate"] == nil {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO ps_profit_scenarios(tenant_id,id,product_id,name,inputs,output,formula_version,product_data_revision,created_by,monthly_id,monthly_revision,monthly_snapshot,product_snapshot,record_kind,source_row_id,imported_fba_fee_usd,imported_gross_margin_rate)
 SELECT p.tenant_id,$4::uuid,p.id,'卖家精灵来源估算',$5::jsonb,'{"state":"source_estimate"}'::jsonb,'sellersprite-source-v1',p.data_revision,$6,m.id,m.revision,to_jsonb(m),$5::jsonb,'imported',$3::uuid,$7::numeric,$8::numeric
 FROM ps_products p JOIN ps_product_monthly m ON m.tenant_id=p.tenant_id AND m.product_id=p.id WHERE p.tenant_id=$1 AND p.id=$2::uuid AND m.source_row_id=$3::uuid
 ON CONFLICT(tenant_id,source_row_id) WHERE record_kind='imported' DO NOTHING`, a.Tenant, productID, rowID, uuid(), encoded(n), a.User, n["imported_fba_fee_usd"], n["imported_gross_margin_rate"])
	return err
}

// Migrate source fields in bounded batches. Existing manual parameters, reviews,
// normalized observations and saved profit snapshots are never overwritten.
func backfillImportedColumns(ctx context.Context, tx pgx.Tx) error {
	for _, kind := range []string{"product", "monthly"} {
		var tenant int64
		last := "00000000-0000-0000-0000-000000000000"
		for {
			selectSQL := `SELECT p.tenant_id,p.id::text,p.current_row_id::text,r.raw_data,i.mapping,i.created_by FROM ps_products p JOIN ps_import_rows r ON r.tenant_id=p.tenant_id AND r.id=p.current_row_id JOIN ps_imports i ON i.tenant_id=r.tenant_id AND i.id=r.import_id WHERE (p.tenant_id,p.id)>($1,$2::uuid) ORDER BY p.tenant_id,p.id LIMIT 500`
			if kind == "monthly" {
				selectSQL = `SELECT m.tenant_id,m.id::text,m.source_row_id::text,r.raw_data,i.mapping,i.created_by,m.product_id::text FROM ps_product_monthly m JOIN ps_import_rows r ON r.tenant_id=m.tenant_id AND r.id=m.source_row_id JOIN ps_imports i ON i.tenant_id=r.tenant_id AND i.id=r.import_id WHERE (m.tenant_id,m.id)>($1,$2::uuid) ORDER BY m.tenant_id,m.id LIMIT 500`
			}
			rows, err := tx.Query(ctx, selectSQL, tenant, last)
			if err != nil {
				return err
			}
			type item struct {
				tenant, user     int64
				id, row, product string
				raw, mapping     []byte
			}
			items := []item{}
			for rows.Next() {
				var v item
				if kind == "monthly" {
					err = rows.Scan(&v.tenant, &v.id, &v.row, &v.raw, &v.mapping, &v.user, &v.product)
				} else {
					err = rows.Scan(&v.tenant, &v.id, &v.row, &v.raw, &v.mapping, &v.user)
					v.product = v.id
				}
				if err != nil {
					break
				}
				items = append(items, v)
			}
			if err == nil {
				err = rows.Err()
			}
			rows.Close()
			if err != nil {
				return err
			}
			if len(items) == 0 {
				break
			}
			for _, v := range items {
				n, _ := normalizeRow(decodeObject(v.raw), decodeObject(v.mapping))
				if kind == "monthly" {
					if err = saveImportedMonthly(ctx, tx, identity{Tenant: v.tenant, User: v.user}, v.product, v.row, n); err != nil {
						return err
					}
				} else {
					columns := []string{}
					for _, f := range importedColumns {
						if f.Table == "product" {
							columns = append(columns, f.Key)
						}
					}
					for _, key := range []string{"weight_g", "package_weight_g", "package_length_cm", "package_width_cm", "package_height_cm"} {
						columns = append(columns, key)
					}
					args := []any{v.tenant, v.id}
					sets, missing := []string{}, []string{}
					for _, key := range columns {
						if n[key] == nil || str(n[key]) == "" {
							continue
						}
						args = append(args, n[key])
						sets = append(sets, fmt.Sprintf("%s=COALESCE(%s,$%d)", key, key, len(args)))
						missing = append(missing, key+" IS NULL")
					}
					if len(sets) > 0 {
						_, err = tx.Exec(ctx, "UPDATE ps_products SET "+strings.Join(sets, ",")+",data_revision=data_revision+1 WHERE tenant_id=$1 AND id=$2::uuid AND ("+strings.Join(missing, " OR ")+")", args...)
						if err != nil {
							return err
						}
					}
				}
				tenant, last = v.tenant, v.id
			}
		}
	}
	return nil
}
