package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

func readAllStdin() (string, error) {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("cannot read stdin: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// normalizeRaw decodes raw JSON so it nests inside a result object instead of
// being emitted as an escaped string.
func normalizeRaw(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return string(raw)
	}
	return parsed
}

func unmarshalJSON(raw json.RawMessage, target any) error {
	return json.Unmarshal(raw, target)
}

// Table cells for optional values: an absent field reads better as a dash than
// as "0" or "false", which would look like real data.
func intOrDash(value *int) string {
	if value == nil {
		return "-"
	}
	return strconv.Itoa(*value)
}

func boolOrDash(value *bool) string {
	if value == nil {
		return "-"
	}
	return strconv.FormatBool(*value)
}

// firstLine keeps multi-line server errors from breaking table layout.
func firstLine(text string) string {
	if text == "" {
		return ""
	}
	line, _, _ := strings.Cut(text, "\n")
	if len(line) > 120 {
		return line[:120] + "..."
	}
	return line
}

// stringifyRows renders query result rows for table output.
func stringifyRows(datas [][]any) [][]string {
	rows := make([][]string, 0, len(datas))
	for _, record := range datas {
		cells := make([]string, 0, len(record))
		for _, value := range record {
			cells = append(cells, formatCell(value))
		}
		rows = append(rows, cells)
	}
	return rows
}

func formatCell(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		// JSON has one number type; print integers without a trailing ".0".
		if v == math.Trunc(v) && math.Abs(v) < 1e15 {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(encoded)
	}
}

// parseKeyValues turns repeated `k=v` flags into a map.
func parseKeyValues(pairs []string, flagName string) (map[string]string, error) {
	out := map[string]string{}
	for _, pair := range pairs {
		key, value, found := strings.Cut(pair, "=")
		if !found || key == "" {
			return nil, cliexit.Usage("--%s expects key=value, received %q", flagName, pair)
		}
		out[key] = value
	}
	return out, nil
}

// readJSONArg resolves a JSON payload from an inline string, a @file reference,
// or @- for stdin.
func readJSONArg(value string) (json.RawMessage, error) {
	if value == "" {
		return nil, nil
	}

	raw := []byte(value)
	if strings.HasPrefix(value, "@") {
		source := strings.TrimPrefix(value, "@")
		var (
			data []byte
			err  error
		)
		if source == "-" {
			data, err = io.ReadAll(os.Stdin)
		} else {
			data, err = os.ReadFile(source)
		}
		if err != nil {
			return nil, cliexit.Usage("cannot read JSON payload from %q: %v", source, err)
		}
		raw = data
	}

	if !json.Valid(raw) {
		return nil, cliexit.Usage("payload is not valid JSON")
	}
	return json.RawMessage(raw), nil
}
