package dashboard

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

// FilterValues is the runtime filter payload sent with every query.
type FilterValues map[string]any

// ParseFilterArgs turns `--filter name=value` pairs into a payload the server
// accepts, using the dashboard's own spec to decide each value's JSON type.
//
// Guessing the type from the text would be wrong in both directions: a number
// filter rejects the string "10", while a text filter rejects the number 10.
// Reading the spec removes the guess and lets an unknown filter name fail
// locally with the list of valid names.
//
// Accepted forms:
//
//	region=east                    text / single enum
//	tags=a,b       tags=a --filter tags=b     multi-select enum (repeatable)
//	min_pv=100                     number
//	period=2026-01-01..2026-01-31  daterange, explicit
//	period=last_30d                daterange, preset
//	region=__all__                 enum, select everything
func ParseFilterArgs(spec *Spec, pairs []string) (FilterValues, error) {
	values := FilterValues{}
	multi := map[string][]string{}

	for _, pair := range pairs {
		name, raw, found := strings.Cut(pair, "=")
		if !found || name == "" {
			return nil, cliexit.Usage("--filter expects name=value, received %q", pair)
		}

		filter := spec.FilterByName(name)
		if filter == nil {
			known := spec.FilterNames()
			if len(known) == 0 {
				return nil, cliexit.Usage("this dashboard declares no filters, but --filter %s was given", name)
			}
			sort.Strings(known)
			return nil, cliexit.Usage("unknown filter %q; this dashboard declares: %s", name, strings.Join(known, ", "))
		}

		switch filter.Type {
		case "enum":
			if filter.Multiple {
				multi[name] = append(multi[name], splitList(raw)...)
				continue
			}
			if strings.Contains(raw, ",") {
				return nil, cliexit.Usage("filter %q is single-select; %q looks like a list", name, raw)
			}
			values[name] = raw
		case "number":
			number, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				return nil, cliexit.Usage("filter %q expects a number, received %q", name, raw)
			}
			values[name] = number
		case "text":
			values[name] = raw
		case "daterange":
			parsed, err := parseDaterange(name, raw)
			if err != nil {
				return nil, err
			}
			values[name] = parsed
		default:
			// An unknown filter type means the server schema moved ahead of the
			// CLI; pass the value through rather than blocking the command.
			values[name] = raw
		}
	}

	for name, list := range multi {
		values[name] = list
	}
	return values, nil
}

// MergeFilterValues overlays raw JSON onto parsed --filter values. The raw form
// wins, so --filter-values is the escape hatch for anything the flags cannot say.
func MergeFilterValues(base FilterValues, rawJSON json.RawMessage) (FilterValues, error) {
	if len(rawJSON) == 0 {
		return base, nil
	}
	var overlay map[string]any
	if err := json.Unmarshal(rawJSON, &overlay); err != nil {
		return nil, cliexit.Usage("--filter-values must be a JSON object: %v", err)
	}
	if base == nil {
		base = FilterValues{}
	}
	for k, v := range overlay {
		base[k] = v
	}
	return base, nil
}

func splitList(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// parseDaterange accepts `start..end` or a preset name. The server resolves
// presets against its own clock, so they are forwarded verbatim.
func parseDaterange(name, raw string) (map[string]string, error) {
	if start, end, found := strings.Cut(raw, ".."); found {
		start, end = strings.TrimSpace(start), strings.TrimSpace(end)
		if start == "" || end == "" {
			return nil, cliexit.Usage("filter %q expects start..end, received %q", name, raw)
		}
		return map[string]string{"start": start, "end": end}, nil
	}
	preset := strings.TrimPrefix(raw, "preset:")
	if preset == "" {
		return nil, cliexit.Usage("filter %q expects a preset or start..end, received %q", name, raw)
	}
	return map[string]string{"preset": preset}, nil
}
