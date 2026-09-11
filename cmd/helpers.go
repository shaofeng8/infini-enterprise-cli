package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
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
