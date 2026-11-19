package screening

import (
	"archive/zip"
	"bytes"
	"context"
	_ "embed"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

//go:embed data/normalization.json
var normalizationData []byte

type normalizationConfig struct {
	Aliases   map[string][]string `json:"ALIASES"`
	Simple    map[string]string   `json:"SIMPLE"`
	Materials [][2]string         `json:"MATERIALS"`
}

var normalConfig normalizationConfig

func init() {
	if e := json.Unmarshal(normalizationData, &normalConfig); e != nil {
		panic(e)
	}
}

//go:embed data/default-policy.json
var defaultPolicyData []byte

const maxUpload = 30 * 1024 * 1024
const maxExpanded = 250 * 1024 * 1024

type sourceRow struct {
	Member, Sheet   string
	Number          int
	Raw, Normalized Object
	Errors          []string
}

func normalizeRow(raw Object, mapping Object) (Object, []string) {
	data := normalConfig
	out := Object{}
	errs := []string{}
	for field, aliases := range data.Aliases {
		var value any
		if mapped := str(mapping[field]); mapped != "" {
			value = raw[mapped]
		} else {
			value = raw[field]
			if value == nil || str(value) == "" {
				for _, alias := range aliases {
					if v := raw[alias]; v != nil && str(v) != "" {
						value = v
						break
					}
				}
			}
		}
		if value == nil || str(value) == "" {
			out[field] = nil
			continue
		}
		text := strings.TrimSpace(str(value))
		out[field] = text
		switch field {
		case "price_usd", "monthly_revenue", "parent_monthly_revenue", "child_monthly_revenue", "rating", "weight_g", "package_weight_g", "package_length_cm", "package_width_cm", "package_height_cm", "carton_weight_g", "carton_length_cm", "carton_width_cm", "carton_height_cm":
			text = strings.ReplaceAll(strings.ReplaceAll(text, ",", ""), "$", "")
			if !decimalPattern.MatchString(text) {
				errs = append(errs, field+" 数值无效")
			} else {
				out[field] = text
				if field == "rating" && ratCompare(text, "5") > 0 {
					errs = append(errs, "评分超出范围")
				}
			}
		case "monthly_sales", "review_count", "bsr_rank", "units_per_pack", "carton_units", "parent_monthly_sales", "child_monthly_sales", "variant_count":
			text = strings.ReplaceAll(text, ",", "")
			n, e := parseNonnegativeInt(text)
			if e != nil {
				errs = append(errs, field+" 必须为非负整数")
			} else if (field == "units_per_pack" || field == "carton_units") && (n < 1 || n > 2147483647) {
				errs = append(errs, field+" 必须为正整数")
			} else {
				out[field] = n
			}
		}
	}
	out["asin"] = strings.ToUpper(str(out["asin"]))
	out["parent_asin"] = strings.ToUpper(str(out["parent_asin"]))
	role := strings.ToLower(str(out["listing_role"]))
	if role == "父体" || role == "parent" {
		role = "parent"
	} else if role == "" || role == "子体" || role == "child" {
		role = "child"
	} else {
		errs = append(errs, "未知父子体类型")
	}
	out["listing_role"] = role
	out["sales_scope_explicit"] = str(out["sales_scope"]) != ""
	scope := strings.ToLower(str(out["sales_scope"]))
	switch scope {
	case "子体", "子体销量", "child":
		scope = "child"
	case "父体", "父体销量", "父体汇总", "parent":
		scope = "parent"
	case "", "未知", "unknown":
		scope = "unknown"
	default:
		errs = append(errs, "未知销量口径")
	}
	out["sales_scope"] = scope
	if !asinPattern.MatchString(str(out["asin"])) {
		errs = append(errs, "ASIN必须为10位字母数字")
	}
	parent := str(out["parent_asin"])
	if role == "parent" {
		if parent != "" && parent != str(out["asin"]) {
			errs = append(errs, "父体行的父ASIN必须等于自身")
		}
		parent = str(out["asin"])
		out["parent_asin"] = parent
		if scope == "child" && out["sales_scope_explicit"] == true {
			errs = append(errs, "父体行不能是子体销量")
		}
		out["sales_scope"] = "parent"
		out["sales_scope_explicit"] = true
	}
	// 独立商品（没有子体）允许父ASIN等于自身，形成单成员家族。
	if parent != "" && !asinPattern.MatchString(parent) {
		errs = append(errs, "父ASIN无效")
	}
	if role != "parent" && str(out["title"]) == "" {
		errs = append(errs, "缺少商品标题")
	}
	// A self-parent ASIN with exactly one variant represents the same sales unit.
	// Never infer this for multi-variant listings or missing variant counts.
	if role != "parent" && parent == str(out["asin"]) && integer(out["variant_count"]) == 1 {
		sales := out["child_monthly_sales"]
		if sales == nil {
			sales = out["monthly_sales"]
		}
		if sales == nil {
			sales = out["parent_monthly_sales"]
		}
		if sales != nil {
			out["child_monthly_sales"] = sales
			out["parent_monthly_sales"] = sales
			out["sales_basis"] = "single_variant_self_parent"
			scope = "child"
			if out["child_monthly_revenue"] == nil {
				out["child_monthly_revenue"] = out["monthly_revenue"]
			}
		}
	}
	if out["child_monthly_sales"] != nil || out["child_monthly_revenue"] != nil {
		if scope == "parent" {
			errs = append(errs, "子体专用字段与父体口径冲突")
		}
		out["monthly_sales"] = out["child_monthly_sales"]
		out["monthly_revenue"] = out["child_monthly_revenue"]
		out["sales_scope"] = "child"
		out["sales_scope_explicit"] = true
	}
	if (out["parent_monthly_sales"] != nil || out["parent_monthly_revenue"] != nil || out["sales_scope"] == "parent") && parent == "" {
		errs = append(errs, "父体销量必须提供父ASIN")
	}
	if len([]rune(str(out["title"]))) > 2000 {
		errs = append(errs, "商品标题超过2000字")
	}
	if str(out["image_url"]) != "" && !strings.HasPrefix(str(out["image_url"]), "https://") {
		out["image_url"] = nil
	}
	out["specs"] = Object{"详细参数": str(raw["详细参数"])}
	return out, errs
}
func parseSource(data []byte, name string, mapping Object, emit func(sourceRow) error) error {
	return parseSourceReport(data, name, mapping, emit, nil)
}
func parseSourceReport(data []byte, name string, mapping Object, emit func(sourceRow) error, skipped func(Object)) error {
	if len(data) > maxUpload {
		return fmt.Errorf("文件超过30MB")
	}
	expanded := uint64(0)
	count := 0
	files := 0
	var parse func([]byte, string) error
	parse = func(content []byte, member string) error {
		ext := strings.ToLower(filepath.Ext(member))
		files++
		if files > 100 {
			return fmt.Errorf("ZIP最多100个文件")
		}
		send := func(sheet string, headers, values []string, row int) error {
			if len(values) == 0 {
				return nil
			}
			raw := Object{}
			nonempty := false
			for i, header := range headers {
				if i < len(values) {
					raw[header] = values[i]
					nonempty = nonempty || strings.TrimSpace(values[i]) != ""
				}
			}
			if !nonempty {
				return nil
			}
			count++
			if count > 100000 {
				return fmt.Errorf("每批最多10万行")
			}
			normalized, errs := normalizeRow(raw, mapping)
			return emit(sourceRow{member, sheet, row, raw, normalized, errs})
		}
		switch ext {
		case ".csv":
			reader := io.Reader(bytes.NewReader(content))
			if !utf8.Valid(content) {
				reader = transform.NewReader(reader, simplifiedchinese.GB18030.NewDecoder())
			}
			csvReader := csv.NewReader(reader)
			csvReader.FieldsPerRecord = -1
			headers, e := csvReader.Read()
			if e != nil {
				return e
			}
			if len(headers) > 1000 {
				return fmt.Errorf("表头列数超过1000")
			}
			headers[0] = strings.TrimPrefix(headers[0], "\ufeff")
			for row := 2; ; row++ {
				values, e := csvReader.Read()
				if e == io.EOF {
					break
				}
				if e != nil {
					return e
				}
				if e = send("", headers, values, row); e != nil {
					return e
				}
			}
		case ".xlsx":
			z, e := zip.NewReader(bytes.NewReader(content), int64(len(content)))
			if e != nil {
				return e
			}
			for _, f := range z.File {
				expanded += f.UncompressedSize64
				if expanded > maxExpanded {
					return fmt.Errorf("展开体积超过250MB")
				}
			}
			book, e := excelize.OpenReader(bytes.NewReader(content), excelize.Options{UnzipSizeLimit: maxExpanded, UnzipXMLSizeLimit: 16 * 1024 * 1024})
			if e != nil {
				return e
			}
			defer book.Close()
			for _, sheet := range book.GetSheetList() {
				rows, e := book.Rows(sheet)
				if e != nil {
					return e
				}
				var headers []string
				row := 0
				for rows.Next() {
					row++
					values, e := rows.Columns()
					if e != nil {
						rows.Close()
						return e
					}
					if headers == nil {
						headers = values
						if explanatorySheet(sheet, headers, mapping) {
							if skipped != nil {
								skipped(Object{"member_path": member, "sheet_name": sheet, "reason": "说明页，无 ASIN 字段，不作为商品导入"})
							}
							rows.Close()
							break
						}
						continue
					}
					if e = send(sheet, headers, values, row); e != nil {
						rows.Close()
						return e
					}
				}
				e = rows.Error()
				rows.Close()
				if e != nil {
					return e
				}
			}
		default:
			return fmt.Errorf("不支持的文件类型")
		}
		return nil
	}
	if strings.EqualFold(filepath.Ext(name), ".zip") {
		archive, e := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if e != nil {
			return e
		}
		if len(archive.File) > 200 {
			return fmt.Errorf("ZIP条目过多")
		}
		for _, f := range archive.File {
			if f.FileInfo().IsDir() {
				continue
			}
			name := strings.ReplaceAll(f.Name, "\\", "/")
			if strings.HasPrefix(name, "/") || strings.Contains(name, ":") || strings.Contains("/"+name+"/", "/../") || f.Flags&1 != 0 {
				return fmt.Errorf("ZIP路径或加密文件不支持")
			}
			ext := strings.ToLower(filepath.Ext(name))
			if ext == ".zip" {
				return fmt.Errorf("禁止嵌套ZIP")
			}
			if ext != ".xlsx" && ext != ".csv" {
				return fmt.Errorf("ZIP含不支持文件: %s", filepath.Base(name))
			}
			expanded += f.UncompressedSize64
			if expanded > maxExpanded {
				return fmt.Errorf("ZIP展开体积超过250MB")
			}
			reader, e := f.Open()
			if e != nil {
				return e
			}
			content, e := io.ReadAll(io.LimitReader(reader, maxExpanded+1))
			reader.Close()
			if e != nil {
				return e
			}
			if len(content) > maxExpanded {
				return fmt.Errorf("ZIP子文件过大")
			}
			if e = parse(content, name); e != nil {
				count++
				if count > 100000 {
					return e
				}
				if emitErr := emit(sourceRow{name, "", 1, Object{}, nil, []string{e.Error()}}); emitErr != nil {
					return emitErr
				}
			}
		}
		return nil
	}
	return parse(data, name)
}
func parseNonnegativeInt(s string) (int64, error) {
	var n int64
	if !regexp.MustCompile(`^[0-9]{1,12}$`).MatchString(s) {
		return 0, fmt.Errorf("invalid integer")
	}
	_, e := fmt.Sscan(s, &n)
	return n, e
}
func ratCompare(a, b string) int {
	x, _ := newRat(a)
	y, _ := newRat(b)
	if x == nil || y == nil {
		return 0
	}
	return x.Cmp(y)
}
func period(b Object, required bool) (Object, error) {
	kind := str(b["period_type"])
	if kind == "" {
		kind = "calendar_month"
	}
	observed := str(b["observed_on"])
	var date time.Time
	var e error
	if observed != "" {
		date, e = time.Parse("2006-01-02", observed)
		if e != nil {
			return nil, fail(422, "观察日期无效")
		}
	} else if required {
		return nil, fail(422, "确认导入前必须填写观察日期")
	}
	start, end := str(b["period_start"]), str(b["period_end"])
	sourcePeriod := str(b["source_period"])
	switch kind {
	case "calendar_month":
		month := sourcePeriod
		if !sourceMonthPattern.MatchString(month) {
			if sourcePeriod != "" || required {
				return nil, fail(422, "无法识别来源期，请填写YYYY-MM")
			}
			return pendingImportPeriod(b, kind, observed)
		}
		d, _ := time.Parse("2006-01", month)
		start = d.Format("2006-01-02")
		end = d.AddDate(0, 1, -1).Format("2006-01-02")
	case "rolling_30d":
		if date.IsZero() {
			if required {
				return nil, fail(422, "滚动30天必须提供观察日期")
			}
			return Object{"period_type": kind}, nil
		}
		end = date.Format("2006-01-02")
		start = date.AddDate(0, 0, -29).Format("2006-01-02")
	case "custom":
		if start == "" || end == "" {
			return nil, fail(422, "自定义周期须提供起止日期")
		}
	default:
		return nil, fail(422, "不支持的统计周期")
	}
	a, e := time.Parse("2006-01-02", start)
	if e != nil {
		return nil, fail(422, "开始日期无效")
	}
	z, e := time.Parse("2006-01-02", end)
	if e != nil || a.After(z) {
		return nil, fail(422, "统计日期无效")
	}
	source := str(b["source_type"])
	if source == "" {
		source = "sellersprite"
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,50}$`).MatchString(source) {
		return nil, fail(422, "来源标识无效")
	}
	scope := str(b["default_sales_scope"])
	if scope == "" {
		scope = "unknown"
	}
	if scope != "child" && scope != "parent" && scope != "unknown" {
		return nil, fail(422, "销量口径无效")
	}
	return Object{"period_type": kind, "period_start": start, "period_end": end, "stat_month": z.Format("2006-01") + "-01", "observed_on": nullableString(observed), "source_type": source, "source_period": sourcePeriod, "default_sales_scope": scope}, nil
}
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func (s *service) upload(w http.ResponseWriter, r *http.Request) (any, error) {
	a := actor(r)
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload+64*1024)
	if e := r.ParseMultipartForm(1024 * 1024); e != nil {
		return nil, fail(413, "文件请求无效或超过30MB")
	}
	defer r.MultipartForm.RemoveAll()
	f, header, e := r.FormFile("file")
	if e != nil {
		return nil, fail(422, "请选择文件")
	}
	defer f.Close()
	name := filepath.Base(header.Filename)
	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".zip" && ext != ".xlsx" && ext != ".csv" {
		return nil, fail(422, "只支持XLSX、CSV和ZIP")
	}
	data, e := io.ReadAll(io.LimitReader(f, maxUpload+1))
	if e != nil {
		return nil, e
	}
	if len(data) > maxUpload {
		return nil, fail(413, "文件超过30MB")
	}
	b := Object{}
	for _, key := range []string{"source_period", "observed_on", "period_type", "period_start", "period_end", "source_type", "default_sales_scope"} {
		b[key] = r.FormValue(key)
	}
	applyImportDefaults(b, name, time.Now())
	meta, e := period(b, false)
	if e != nil {
		return nil, e
	}
	for k, v := range meta {
		b[k] = v
	}
	if b["source_type"] == nil || str(b["source_type"]) == "" {
		b["source_type"] = "sellersprite"
	}
	if b["default_sales_scope"] == nil || str(b["default_sales_scope"]) == "" {
		b["default_sales_scope"] = "unknown"
	}
	for _, key := range []string{"observed_on", "period_start", "period_end"} {
		if str(b[key]) == "" {
			b[key] = nil
		}
	}
	hash := shaDigest(data)
	id := uuid()
	key := fmt.Sprintf("%d/imports/%s%s", a.Tenant, id, ext)
	target := filepath.Join(s.cfg.Storage, filepath.FromSlash(key))
	if e = os.MkdirAll(filepath.Dir(target), 0750); e != nil {
		return nil, e
	}
	if e = os.WriteFile(target, data, 0640); e != nil {
		return nil, e
	}
	tx, e := s.db.Begin(r.Context())
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(r.Context())
	mapping := Object{}
	_, e = tx.Exec(r.Context(), "INSERT INTO ps_imports(tenant_id,id,file_name,file_sha256,storage_key,file_size,mapping,mapping_hash,state,created_by,observed_on,source_period,source_type,period_type,period_start,period_end,default_sales_scope) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'parsing',$9,$10,$11,$12,$13,$14,$15,$16) ON CONFLICT DO NOTHING", a.Tenant, id, name, hash, key, len(data), encoded(mapping), shaDigest(encoded(mapping)), a.User, b["observed_on"], str(b["source_period"]), str(b["source_type"]), str(b["period_type"]), b["period_start"], b["period_end"], str(b["default_sales_scope"]))
	if e != nil {
		return nil, e
	}
	var actual string
	if e = tx.QueryRow(r.Context(), "SELECT id::text FROM ps_imports WHERE tenant_id=$1 AND file_sha256=$2 AND mapping_hash=$3 AND source_period=$4 AND observed_on IS NOT DISTINCT FROM $5::date AND source_type=$6 AND period_type=$7 AND period_start IS NOT DISTINCT FROM $8::date AND period_end IS NOT DISTINCT FROM $9::date AND default_sales_scope=$10", a.Tenant, hash, shaDigest(encoded(mapping)), str(b["source_period"]), b["observed_on"], str(b["source_type"]), str(b["period_type"]), b["period_start"], b["period_end"], str(b["default_sales_scope"])).Scan(&actual); e != nil {
		return nil, e
	}
	if actual != id {
		os.Remove(target)
		if e = tx.Commit(r.Context()); e != nil {
			return nil, e
		}
		var jobID string
		var state string
		e = s.db.QueryRow(r.Context(), "SELECT id::text,state FROM ps_jobs WHERE tenant_id=$1 AND kind='parse' AND payload->>'import_id'=$2 ORDER BY created_at DESC LIMIT 1", a.Tenant, actual).Scan(&jobID, &state)
		return Object{"import_id": actual, "job_id": jobID, "state": state}, e
	}
	job, e := insertJob(r.Context(), tx, a, "parse", Object{"import_id": id, "generation": 1})
	if e != nil {
		return nil, e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return nil, e
	}
	job["import_id"] = id
	return job, nil
}
func insertJob(ctx context.Context, tx pgx.Tx, a identity, kind string, payload Object) (Object, error) {
	id := uuid()
	payload["engine"] = goEngine
	_, e := tx.Exec(ctx, "INSERT INTO ps_jobs(tenant_id,id,kind,idempotency_key,payload,created_by) VALUES($1,$2::uuid,$3,$2::text,$4,$5)", a.Tenant, id, kind, encoded(payload), a.User)
	return Object{"job_id": id, "state": "queued"}, e
}

func explanatorySheet(name string, headers []string, mapping Object) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "notes", "说明", "说明页", "使用说明":
	default:
		return false
	}
	for _, header := range headers {
		header = strings.TrimSpace(strings.TrimPrefix(header, "\ufeff"))
		if strings.EqualFold(header, "asin") || header == str(mapping["asin"]) {
			return false
		}
		for _, alias := range normalConfig.Aliases["asin"] {
			if strings.EqualFold(header, alias) {
				return false
			}
		}
	}
	return true
}
