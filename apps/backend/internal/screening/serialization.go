package screening

import "encoding/json"

// PostgreSQL JSON numeric values must not become binary floating point amounts
// in browser clients. Revisions, quantities and counts remain JSON integers.
func publicJSON(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := Object{}
		for key, item := range v {
			switch key {
			case "price_usd", "monthly_revenue", "rating", "weight_g", "package_weight_g", "package_length_cm", "package_width_cm", "package_height_cm", "carton_weight_g", "carton_length_cm", "carton_width_cm", "carton_height_cm":
				if number, ok := item.(json.Number); ok {
					out[key] = string(number)
					continue
				}
			}
			out[key] = publicJSON(item)
		}
		return out
	case []Object:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = publicJSON(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = publicJSON(item)
		}
		return out
	default:
		return value
	}
}
