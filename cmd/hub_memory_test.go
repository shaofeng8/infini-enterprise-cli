package cmd

import (
	"encoding/json"
	"testing"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

const testSchema = `{"tables": [
  {"tableName": "invoices", "inContext": true, "columns": [
    {"name": "id", "type": "int", "inContext": true},
    {"name": "total", "type": "decimal", "inContext": false}
  ]},
  {"tableName": "artists", "inContext": false, "columns": [
    {"name": "id", "type": "int"},
    {"name": "name", "type": "varchar"}
  ]},
  {"tableName": "empty_meta", "inContext": false, "columns": []}
]}`

func TestSelectMemoryTablesDefaultsToEverythingWithColumns(t *testing.T) {
	tables, err := selectMemoryTables(json.RawMessage(testSchema), nil, false)
	if err != nil {
		t.Fatalf("selectMemoryTables: %v", err)
	}
	// A table with no columns is dropped: the server rejects an empty column
	// selection for it and would fail the whole build.
	if len(tables) != 2 {
		t.Fatalf("got %+v", tables)
	}
	if tables[0].TableName != "invoices" || len(tables[0].Columns) != 2 {
		t.Fatalf("got %+v", tables[0])
	}
	if tables[0].Columns[1].Type != "decimal" {
		t.Fatalf("column type should survive: %+v", tables[0].Columns[1])
	}
}

func TestSelectMemoryTablesMissingOnlySkipsCoveredTables(t *testing.T) {
	tables, err := selectMemoryTables(json.RawMessage(testSchema), nil, true)
	if err != nil {
		t.Fatalf("selectMemoryTables: %v", err)
	}
	if len(tables) != 1 || tables[0].TableName != "artists" {
		t.Fatalf("got %+v", tables)
	}
}

// An explicit selection beats --missing-only: the caller already said which
// tables they mean, so silently dropping them would be surprising.
func TestExplicitSelectionOverridesMissingOnly(t *testing.T) {
	tables, err := selectMemoryTables(json.RawMessage(testSchema), []string{"invoices"}, true)
	if err != nil {
		t.Fatalf("selectMemoryTables: %v", err)
	}
	if len(tables) != 1 || tables[0].TableName != "invoices" {
		t.Fatalf("got %+v", tables)
	}
}

func TestSelectMemoryTablesNarrowsColumns(t *testing.T) {
	tables, err := selectMemoryTables(json.RawMessage(testSchema), []string{"invoices:total"}, false)
	if err != nil {
		t.Fatalf("selectMemoryTables: %v", err)
	}
	if len(tables) != 1 || len(tables[0].Columns) != 1 || tables[0].Columns[0].Name != "total" {
		t.Fatalf("got %+v", tables)
	}
}

func TestSelectMemoryTablesRejectsBadInput(t *testing.T) {
	cases := []struct {
		name       string
		schema     string
		selections []string
		missing    bool
		code       int
	}{
		{
			name:       "unknown table",
			schema:     testSchema,
			selections: []string{"nosuch"},
			code:       cliexit.CodeUsage,
		},
		{
			name:       "colon with no columns",
			schema:     testSchema,
			selections: []string{"invoices:"},
			code:       cliexit.CodeUsage,
		},
		{
			name:       "column that does not exist leaves the table empty",
			schema:     testSchema,
			selections: []string{"invoices:nosuch"},
			code:       cliexit.CodeUsage,
		},
		{
			name:   "schema reports no tables",
			schema: `{"tables": []}`,
			code:   cliexit.CodeBusiness,
		},
		{
			name:    "everything already covered",
			schema:  `{"tables": [{"tableName": "invoices", "inContext": true, "columns": [{"name": "id"}]}]}`,
			missing: true,
			code:    cliexit.CodeBusiness,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := selectMemoryTables(json.RawMessage(testCase.schema), testCase.selections, testCase.missing)
			if err == nil {
				t.Fatal("expected an error")
			}
			if got := cliexit.CodeOf(err); got != testCase.code {
				t.Fatalf("exit code: got %d, want %d (%v)", got, testCase.code, err)
			}
		})
	}
}

func TestParseTableSelectionsKeepsColumnOrderAndTrimsSpace(t *testing.T) {
	wanted, err := parseTableSelections([]string{" invoices : total , id ", "artists"})
	if err != nil {
		t.Fatalf("parseTableSelections: %v", err)
	}
	columns, present := wanted["invoices"]
	if !present || len(columns) != 2 || columns[0] != "total" || columns[1] != "id" {
		t.Fatalf("got %v", wanted)
	}
	// A table with no colon means "all of its columns", which is a nil filter
	// rather than an empty one.
	if columns, present := wanted["artists"]; !present || columns != nil {
		t.Fatalf("artists should have a nil filter, got %v", wanted)
	}
}
