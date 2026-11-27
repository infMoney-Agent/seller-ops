package screening

import (
	"bytes"
	"encoding/json"
	"net/url"
)

type JobSubmission struct {
	JobID    string `json:"job_id"`
	RunID    string `json:"run_id,omitempty"`
	ImportID string `json:"import_id,omitempty"`
	State    string `json:"state"`
}

type ParseJobPayload struct {
	Engine     string `json:"engine"`
	ImportID   string `json:"import_id"`
	Generation int64  `json:"generation"`
}

type ImportJobPayload struct {
	Engine   string `json:"engine"`
	ImportID string `json:"import_id"`
}

type ExportJobPayload struct {
	Engine     string   `json:"engine"`
	ProductIDs []string `json:"product_ids"`
}

type ProfitBatchTarget struct {
	ID              string  `json:"id"`
	DataRevision    int64   `json:"data_revision"`
	MonthlyID       *string `json:"monthly_id"`
	MonthlyRevision *int64  `json:"monthly_revision"`
	PriceUSD        *string `json:"price_usd"`
	MissingPeriod   bool    `json:"missing_period"`
}

type ProfitBatchJobPayload struct {
	Engine     string              `json:"engine"`
	ProductIDs []string            `json:"product_ids"`
	Targets    []ProfitBatchTarget `json:"targets"`
	Inputs     Object              `json:"inputs"`
	TemplateID *string             `json:"template_id"`
}

type ScreenTargetSnapshot struct {
	ID         string `json:"id"`
	Revision   int64  `json:"revision"`
	Generation int64  `json:"generation"`
}

type ScreenJobPayload struct {
	Engine  string                 `json:"engine"`
	Policy  Object                 `json:"policy"`
	Targets []ScreenTargetSnapshot `json:"targets"`
}

func decodeJobPayload[T any](job Object) (T, error) {
	var out T
	dec := json.NewDecoder(bytes.NewReader(encoded(job["payload"])))
	dec.UseNumber()
	err := dec.Decode(&out)
	return out, err
}

type ProductSelectionCommand struct {
	ProductIDs    []string
	HasProductIDs bool
	Filters       url.Values
	HasFilterKeys bool
}

func parseSelectionCommand(b Object) ProductSelectionCommand {
	cmd := ProductSelectionCommand{Filters: url.Values{}}
	if raw, ok := b["product_ids"].([]any); ok {
		cmd.HasProductIDs = true
		cmd.ProductIDs = make([]string, 0, len(raw))
		for _, v := range raw {
			cmd.ProductIDs = append(cmd.ProductIDs, str(v))
		}
	} else if typed, ok := b["product_ids"].([]string); ok {
		cmd.HasProductIDs = true
		cmd.ProductIDs = append([]string{}, typed...)
	}
	if raw, ok := b["filters"].(map[string]any); ok {
		cmd.HasFilterKeys = len(raw) > 0
		for k, v := range raw {
			cmd.Filters.Set(k, str(v))
		}
	}
	return cmd
}

type EnqueueScreenCommand struct {
	Selection ProductSelectionCommand
}

func parseEnqueueScreenCommand(b Object) EnqueueScreenCommand {
	return EnqueueScreenCommand{Selection: parseSelectionCommand(b)}
}

type QueueProfitBatchCommand struct {
	Selection     ProductSelectionCommand
	Inputs        Object
	HasInputs     bool
	TemplateID    *string
	PeriodFilters url.Values
}

func parseQueueProfitCommand(b Object) QueueProfitBatchCommand {
	in, ok := b["inputs"].(map[string]any)
	periodFilters := url.Values{}
	if filters, ok := b["filters"].(map[string]any); ok {
		for _, key := range []string{"stat_month", "period_type", "source_type"} {
			if value := str(filters[key]); value != "" {
				periodFilters.Set(key, value)
			}
		}
	}
	return QueueProfitBatchCommand{
		Selection:     parseSelectionCommand(b),
		Inputs:        in,
		HasInputs:     ok,
		TemplateID:    optionalImportString(b["template_id"]),
		PeriodFilters: periodFilters,
	}
}

type ImportPeriodSpec struct {
	PeriodType        string
	PeriodStart       *string
	PeriodEnd         *string
	StatMonth         string
	ObservedOn        *string
	SourceType        string
	SourcePeriod      string
	DefaultSalesScope string
	MinimalPending    bool
}

func (m *ImportPeriodSpec) ToMap() Object {
	if m.MinimalPending {
		return Object{"period_type": m.PeriodType}
	}
	out := Object{
		"period_type":         m.PeriodType,
		"period_start":        nullableString(stringValue(m.PeriodStart)),
		"period_end":          nullableString(stringValue(m.PeriodEnd)),
		"observed_on":         nullableString(stringValue(m.ObservedOn)),
		"source_type":         m.SourceType,
		"source_period":       m.SourcePeriod,
		"default_sales_scope": m.DefaultSalesScope,
	}
	if m.StatMonth != "" {
		out["stat_month"] = m.StatMonth
	}
	return out
}

func (m *ImportPeriodSpec) ApplyToBatch(batch Object) {
	for k, v := range m.ToMap() {
		batch[k] = v
	}
}
