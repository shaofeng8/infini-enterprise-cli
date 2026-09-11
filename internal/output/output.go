// Package output renders command results as JSON (default, pipeline-friendly)
// or as a table (human-friendly for list commands).
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/olekukonko/tablewriter"
)

type Format string

const (
	FormatJSON  Format = "json"
	FormatTable Format = "table"
)

// Envelope is the stable stdout contract: every command emits this shape in
// JSON mode so callers can branch on `success` without parsing stderr.
type Envelope struct {
	Success bool   `json:"success"`
	Data    any    `json:"data"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

var (
	current           = FormatJSON
	out     io.Writer = os.Stdout
	errOut  io.Writer = os.Stderr
)

func SetFormat(f Format)    { current = f }
func CurrentFormat() Format { return current }

// SetWriters is used by tests to capture output.
func SetWriters(stdout, stderr io.Writer) { out, errOut = stdout, stderr }

// Success prints a result payload. headers/toRows are optional; when supplied
// and the format is table, the payload is rendered as a table instead.
func Success(data any, headers []string, toRows func() [][]string) error {
	if current == FormatTable && headers != nil && toRows != nil {
		Table(headers, toRows())
		return nil
	}
	return JSON(Envelope{Success: true, Data: normalize(data)})
}

// Raw prints a payload without the envelope, for passthrough commands.
func Raw(data any) error {
	return JSON(normalize(data))
}

// Failure prints an error. JSON mode keeps stdout machine-readable; table mode
// writes a human line to stderr.
func Failure(err error, hint string) {
	if current == FormatTable {
		fmt.Fprintf(errOut, "Error: %v\n", err)
		if hint != "" {
			fmt.Fprintf(errOut, "Hint:  %s\n", hint)
		}
		return
	}
	_ = JSON(Envelope{Success: false, Data: nil, Message: err.Error(), Hint: hint})
}

func JSON(payload any) error {
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot serialize output: %w", err)
	}
	fmt.Fprintln(out, string(encoded))
	return nil
}

func Table(headers []string, rows [][]string) {
	table := tablewriter.NewWriter(out)
	table.Header(headers)
	table.Bulk(rows)
	table.Render()
}

// Note writes progress information that must never pollute stdout.
func Note(format string, args ...any) {
	fmt.Fprintf(errOut, format+"\n", args...)
}

// normalize turns raw JSON bytes into a value so output stays pretty-printed
// rather than an escaped string.
func normalize(data any) any {
	var raw []byte
	switch v := data.(type) {
	case nil:
		return nil
	case json.RawMessage:
		raw = []byte(v)
	case []byte:
		raw = v
	default:
		return data
	}
	if len(raw) == 0 {
		return nil
	}
	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return string(raw)
	}
	return parsed
}
