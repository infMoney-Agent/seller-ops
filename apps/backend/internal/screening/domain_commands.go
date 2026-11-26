package screening

import (
	"fmt"
	"strings"
)

type ReviewDecision string

const (
	ReviewKept     ReviewDecision = "kept"
	ReviewExcluded ReviewDecision = "excluded"
	ReviewUnsure   ReviewDecision = "unsure"
)

func (d ReviewDecision) valid() bool {
	return d == ReviewKept || d == ReviewExcluded || d == ReviewUnsure
}

type BatchScope string

const (
	BatchScopeSelected         BatchScope = "selected"
	BatchScopeFamily           BatchScope = "family"
	BatchScopeSelectedFamilies BatchScope = "selected_families"
)

func (s BatchScope) valid() bool {
	return s == BatchScopeSelected || s == BatchScopeFamily || s == BatchScopeSelectedFamilies
}

type BatchAction string

const (
	BatchActionReview     BatchAction = "review"
	BatchActionCategorize BatchAction = "categorize"
)

func (a BatchAction) valid() bool {
	return a == BatchActionReview || a == BatchActionCategorize
}

type BatchMode string

const (
	BatchModeDirect    BatchMode = "direct"
	BatchModeReference BatchMode = "reference"
)

type ConfigKind string

const (
	ConfigScreening      ConfigKind = "screening"
	ConfigProfitTemplate ConfigKind = "profit_template"
)

func optionalRevision(b Object, key string) *int64 {
	raw, ok := b[key]
	if !ok || raw == nil {
		return nil
	}
	v := integer(raw)
	return &v
}

func stringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func sameOptionalString(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

type ReviewProductCommand struct {
	ExpectedDataRevision   *int64
	ExpectedReviewRevision *int64
	Status                 ReviewDecision
	Reason                 string
	Note                   string
}

func parseReviewCommand(b Object) ReviewProductCommand {
	return ReviewProductCommand{
		ExpectedDataRevision:   optionalRevision(b, "expected_data_revision"),
		ExpectedReviewRevision: optionalRevision(b, "expected_review_revision"),
		Status:                 ReviewDecision(str(b["status"])),
		Reason:                 str(b["reason"]),
		Note:                   str(b["note"]),
	}
}

func (c ReviewProductCommand) validate() error {
	if !c.Status.valid() {
		return invalid("复核结论无效")
	}
	if c.Status != ReviewKept && strings.TrimSpace(c.Reason) == "" {
		return invalid("排除和待确认必须填写原因")
	}
	if len(c.Reason) > 8000 || len(c.Note) > 40000 {
		return invalid("原因或备注过长")
	}
	return nil
}

type UpdateCategoryCommand struct {
	ExpectedDataRevision   *int64
	ExpectedReviewRevision *int64
	Category               string
}

func parseCategoryCommand(b Object) UpdateCategoryCommand {
	return UpdateCategoryCommand{
		ExpectedDataRevision:   optionalRevision(b, "expected_data_revision"),
		ExpectedReviewRevision: optionalRevision(b, "expected_review_revision"),
		Category:               strings.TrimSpace(str(b["category"])),
	}
}

func (c UpdateCategoryCommand) validate() error {
	if c.Category == "" || len([]rune(c.Category)) > 200 {
		return invalid("人工分类不能为空且不超过200字")
	}
	return nil
}

type ParameterPatch struct {
	Present bool
	Clear   bool
	Raw     string
}

type UpdateParametersCommand struct {
	ExpectedDataRevision *int64
	Patches              map[string]ParameterPatch
	UnknownKeys          []string
}

func parseParametersCommand(b Object) UpdateParametersCommand {
	cmd := UpdateParametersCommand{
		ExpectedDataRevision: optionalRevision(b, "expected_data_revision"),
		Patches:              map[string]ParameterPatch{},
	}
	for key, v := range b {
		if key == "expected_data_revision" {
			continue
		}
		if !containsColumn(parameterColumns, key) {
			cmd.UnknownKeys = append(cmd.UnknownKeys, key)
			continue
		}
		if v == nil {
			cmd.Patches[key] = ParameterPatch{Present: true, Clear: true}
		} else {
			cmd.Patches[key] = ParameterPatch{Present: true, Raw: str(v)}
		}
	}
	return cmd
}

func (c UpdateParametersCommand) buildUpdate(baseSet string, baseArgs []any) (string, []any, error) {
	if len(c.UnknownKeys) > 0 {
		return "", nil, invalid("未知商品参数")
	}
	set := baseSet
	args := baseArgs
	for _, column := range parameterColumns {
		patch, ok := c.Patches[column]
		if !ok || !patch.Present {
			continue
		}
		var sqlVal any
		if !patch.Clear {
			text := patch.Raw
			switch column {
			case "parent_asin":
				// 独立商品允许父ASIN等于自身。
				if !asinPattern.MatchString(text) {
					return "", nil, invalid("父ASIN无效")
				}
				sqlVal = text
			case "material":
				if len([]rune(text)) > 200 {
					return "", nil, invalid("材质过长")
				}
				sqlVal = text
			default:
				if !decimalPattern.MatchString(text) {
					return "", nil, invalid("参数必须是非负数且最多6位小数")
				}
				if column == "units_per_pack" || column == "carton_units" {
					n := integer(text)
					if n < 1 || n > 2147483647 || strings.Contains(text, ".") {
						return "", nil, invalid("包装数量必须是正整数")
					}
					sqlVal = n
				} else {
					sqlVal = text
				}
			}
		}
		args = append(args, sqlVal)
		set += fmt.Sprintf(",%s=$%d", column, len(args))
	}
	set += ",data_revision=data_revision+1,screen_generation=screen_generation+1,machine_decision=NULL,machine_group_key=NULL,current_group_rep_id=NULL"
	return set, args, nil
}

type MonthlyReferenceSpec struct {
	MonthlyID               string
	ExpectedMonthlyRevision *int64
}

func parseMonthlyReferenceSpec(b Object) MonthlyReferenceSpec {
	return MonthlyReferenceSpec{
		MonthlyID:               str(b["monthly_id"]),
		ExpectedMonthlyRevision: optionalRevision(b, "expected_monthly_revision"),
	}
}

type CalculateProfitCommand struct {
	ProductID            string
	ExpectedDataRevision *int64
	Monthly              MonthlyReferenceSpec
	Scenarios            []Object
	HasScenarioList      bool
	InvalidScenario      bool
}

func parseCalculateProfitCommand(b Object) CalculateProfitCommand {
	cmd := CalculateProfitCommand{
		ProductID:            str(b["product_id"]),
		ExpectedDataRevision: optionalRevision(b, "expected_data_revision"),
		Monthly:              parseMonthlyReferenceSpec(b),
	}
	raw, ok := b["scenarios"].([]any)
	if !ok {
		return cmd
	}
	cmd.HasScenarioList = true
	cmd.Scenarios = make([]Object, 0, len(raw))
	for _, entry := range raw {
		in, ok := entry.(map[string]any)
		if !ok {
			cmd.InvalidScenario = true
			break
		}
		cmd.Scenarios = append(cmd.Scenarios, in)
	}
	return cmd
}

type CalculatedScenarioItem struct {
	Name   *string `json:"name"`
	Inputs Object  `json:"inputs"`
	Output Object  `json:"output"`
}

type ProfitCalculationResult struct {
	Items          []CalculatedScenarioItem `json:"items"`
	FormulaVersion string                   `json:"formula_version"`
}

type SaveProfitScenarioCommand struct {
	ExpectedDataRevision *int64
	Monthly              MonthlyReferenceSpec
	TemplateID           string
	Inputs               Object
	HasInputs            bool
}

func parseSaveProfitCommand(b Object) SaveProfitScenarioCommand {
	in, ok := b["inputs"].(map[string]any)
	return SaveProfitScenarioCommand{
		ExpectedDataRevision: optionalRevision(b, "expected_data_revision"),
		Monthly:              parseMonthlyReferenceSpec(b),
		TemplateID:           str(b["template_id"]),
		Inputs:               in,
		HasInputs:            ok,
	}
}

type SavedProfitScenario struct {
	ID     string  `json:"id"`
	Name   *string `json:"name"`
	Inputs Object  `json:"inputs"`
	Output Object  `json:"output"`
	Stale  bool    `json:"stale"`
}

type CreateConfigCommand struct {
	Kind      ConfigKind
	Name      string
	Config    Object
	HasConfig bool
}

func parseCreateConfigCommand(kind ConfigKind, b Object) CreateConfigCommand {
	field := "config"
	if kind == ConfigProfitTemplate {
		field = "params"
	}
	cfg, ok := b[field].(map[string]any)
	return CreateConfigCommand{
		Kind:      kind,
		Name:      strings.TrimSpace(str(b["name"])),
		Config:    cfg,
		HasConfig: ok,
	}
}

type ConfigRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version int    `json:"version"`
	Config  Object `json:"config"`
	Params  Object `json:"params"`
	Active  bool   `json:"active"`
}

type TestPolicyCommand struct {
	ProductIDs []string
	ValidList  bool
}

func parseTestPolicyCommand(b Object) TestPolicyCommand {
	raw, ok := b["product_ids"].([]any)
	if !ok {
		return TestPolicyCommand{}
	}
	ids := make([]string, 0, len(raw))
	for _, v := range raw {
		ids = append(ids, str(v))
	}
	return TestPolicyCommand{ProductIDs: ids, ValidList: true}
}

type PolicyTestResult struct {
	Items []Object `json:"items"`
}

type PreviewBatchCommand struct {
	ProductIDs         []string
	HasProductList     bool
	Scope              BatchScope
	Action             BatchAction
	Overwrite          bool
	InvalidOverwrite   bool
	Mode               BatchMode
	ReferenceProductID string
	Status             ReviewDecision
	Reason             string
	Note               string
	Category           *string
}

func parsePreviewBatchCommand(b Object) PreviewBatchCommand {
	scope := BatchScope(str(b["scope"]))
	if scope == "" {
		scope = BatchScopeSelected
	}
	action := BatchAction(str(b["action"]))
	if action == "" {
		action = BatchActionReview
	}
	mode := BatchMode(str(b["mode"]))
	if mode == "" {
		mode = BatchModeDirect
	}
	cmd := PreviewBatchCommand{
		Scope:              scope,
		Action:             action,
		Mode:               mode,
		ReferenceProductID: str(b["reference_product_id"]),
		Status:             ReviewDecision(str(b["status"])),
		Reason:             str(b["reason"]),
		Note:               str(b["note"]),
		Category:           optionalImportString(b["category"]),
	}
	if raw, present := b["overwrite"]; present {
		v, ok := raw.(bool)
		if !ok {
			cmd.InvalidOverwrite = true
		} else {
			cmd.Overwrite = v
		}
	}
	if rawIDs, ok := b["product_ids"].([]any); ok {
		cmd.HasProductList = true
		cmd.ProductIDs = make([]string, 0, len(rawIDs))
		for _, raw := range rawIDs {
			cmd.ProductIDs = append(cmd.ProductIDs, str(raw))
		}
	} else if typedIDs, ok := b["product_ids"].([]string); ok {
		cmd.HasProductList = true
		cmd.ProductIDs = append([]string{}, typedIDs...)
	}
	return cmd
}

func (c PreviewBatchCommand) validateInitial() ([]string, error) {
	if !c.HasProductList || len(c.ProductIDs) < 1 || len(c.ProductIDs) > 100 {
		return nil, invalid("每批须选择1到100个商品")
	}
	ids := make([]string, 0, len(c.ProductIDs))
	seen := map[string]bool{}
	for _, id := range c.ProductIDs {
		if seen[id] {
			return nil, invalid("不能重复选择商品")
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if !c.Scope.valid() || !c.Action.valid() {
		return nil, invalid("作用范围或动作无效")
	}
	if c.InvalidOverwrite {
		return nil, invalid("覆盖选项无效")
	}
	return ids, nil
}

type BatchAnchorSnapshot struct {
	ID           string  `json:"id"`
	ParentASIN   *string `json:"parent_asin"`
	DataRevision int64   `json:"data_revision"`
}

type BatchReferenceSnapshot struct {
	ID             string `json:"id"`
	DataRevision   int64  `json:"data_revision"`
	ReviewRevision int64  `json:"review_revision"`
}

type BatchTargetSnapshot struct {
	ID             string  `json:"id"`
	ASIN           string  `json:"asin"`
	Title          string  `json:"title"`
	Eligible       bool    `json:"eligible"`
	SkipReason     string  `json:"skip_reason"`
	DataRevision   int64   `json:"data_revision"`
	ReviewRevision int64   `json:"review_revision"`
	ReviewStatus   string  `json:"review_status"`
	ParentASIN     *string `json:"parent_asin"`
	Status         string  `json:"status"`
	Reason         string  `json:"reason"`
	Note           string  `json:"note"`
	Category       *string `json:"category"`
}

type ReviewBatchPayload struct {
	ID        string                  `json:"id"`
	ExpiresAt string                  `json:"expires_at"`
	Scope     BatchScope              `json:"scope"`
	Action    BatchAction             `json:"action"`
	Overwrite bool                    `json:"overwrite"`
	Status    string                  `json:"status"`
	Reason    string                  `json:"reason"`
	Note      string                  `json:"note"`
	Category  *string                 `json:"category"`
	Anchor    *BatchAnchorSnapshot    `json:"anchor,omitempty"`
	Reference *BatchReferenceSnapshot `json:"reference,omitempty"`
	Anchors   []BatchAnchorSnapshot   `json:"anchors,omitempty"`
	Targets   []BatchTargetSnapshot   `json:"targets"`
}

type BatchApplyItem struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Reason  string `json:"reason,omitempty"`
}

type BatchApplyResult struct {
	ID      string           `json:"id"`
	Success int              `json:"success"`
	Skipped int              `json:"skipped"`
	Items   []BatchApplyItem `json:"items"`
}
