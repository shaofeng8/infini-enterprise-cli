package dashboard

import (
	"encoding/json"
	"testing"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

func testSpec() *Spec {
	return &Spec{
		Version: 1,
		Title:   "Sales",
		Filters: []Filter{
			{Name: "period", Type: "daterange"},
			{Name: "channel", Type: "enum"},
			{Name: "tags", Type: "enum", Multiple: true},
			{Name: "min_pv", Type: "number"},
			{Name: "keyword", Type: "text"},
		},
		Queries: []Query{
			{ID: "q_trend"},
			{ID: "q_kpi", Role: "data"},
			{ID: "q_channels", Role: "filter_options"},
		},
		Widgets: []Widget{
			{ID: "w_kpi", Type: "kpi_card", Layout: Layout{X: 0, Y: 0, W: 3, H: 2}},
		},
	}
}

// A number filter rejects the string "100" server-side, so the type must come
// from the spec rather than from the shape of the text.
func TestParseFilterArgsTypesValuesFromSpec(t *testing.T) {
	values, err := ParseFilterArgs(testSpec(), []string{
		"keyword=100",
		"min_pv=100",
		"channel=wechat",
		"tags=a,b",
		"period=2026-01-01..2026-01-31",
	})
	if err != nil {
		t.Fatalf("ParseFilterArgs: %v", err)
	}

	if got, ok := values["keyword"].(string); !ok || got != "100" {
		t.Fatalf("text filter: got %#v, want the string \"100\"", values["keyword"])
	}
	if got, ok := values["min_pv"].(float64); !ok || got != 100 {
		t.Fatalf("number filter: got %#v, want the number 100", values["min_pv"])
	}
	if got, ok := values["channel"].(string); !ok || got != "wechat" {
		t.Fatalf("single enum: got %#v", values["channel"])
	}
	tags, ok := values["tags"].([]string)
	if !ok || len(tags) != 2 || tags[0] != "a" || tags[1] != "b" {
		t.Fatalf("multi enum: got %#v", values["tags"])
	}
	period, ok := values["period"].(map[string]string)
	if !ok || period["start"] != "2026-01-01" || period["end"] != "2026-01-31" {
		t.Fatalf("daterange: got %#v", values["period"])
	}
}

func TestParseFilterArgsDaterangePreset(t *testing.T) {
	for _, input := range []string{"period=last_30d", "period=preset:last_30d"} {
		values, err := ParseFilterArgs(testSpec(), []string{input})
		if err != nil {
			t.Fatalf("%s: %v", input, err)
		}
		got, ok := values["period"].(map[string]string)
		if !ok || got["preset"] != "last_30d" {
			t.Fatalf("%s: got %#v", input, values["period"])
		}
	}
}

// Repeated flags accumulate, which is the natural form for shell loops.
func TestParseFilterArgsRepeatedMultiEnum(t *testing.T) {
	values, err := ParseFilterArgs(testSpec(), []string{"tags=a", "tags=b", "tags=c,d"})
	if err != nil {
		t.Fatalf("ParseFilterArgs: %v", err)
	}
	tags := values["tags"].([]string)
	if len(tags) != 4 {
		t.Fatalf("got %#v, want 4 values", tags)
	}
}

func TestParseFilterArgsRejectsBadInput(t *testing.T) {
	cases := map[string][]string{
		"unknown filter":          {"region=east"},
		"missing equals":          {"channel"},
		"non-numeric number":      {"min_pv=lots"},
		"list for single enum":    {"channel=a,b"},
		"daterange missing bound": {"period=2026-01-01.."},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseFilterArgs(testSpec(), args); err == nil {
				t.Fatal("expected an error")
			} else if cliexit.CodeOf(err) != cliexit.CodeUsage {
				t.Fatalf("expected a usage error, got exit code %d: %v", cliexit.CodeOf(err), err)
			}
		})
	}
}

func TestMergeFilterValuesOverlayWins(t *testing.T) {
	base, err := ParseFilterArgs(testSpec(), []string{"keyword=from-flag", "min_pv=1"})
	if err != nil {
		t.Fatalf("ParseFilterArgs: %v", err)
	}
	merged, err := MergeFilterValues(base, json.RawMessage(`{"keyword":"from-json"}`))
	if err != nil {
		t.Fatalf("MergeFilterValues: %v", err)
	}
	if merged["keyword"] != "from-json" {
		t.Fatalf("overlay should win, got %#v", merged["keyword"])
	}
	if merged["min_pv"] != float64(1) {
		t.Fatalf("unrelated keys should survive, got %#v", merged["min_pv"])
	}
}

func TestMergeFilterValuesRejectsNonObject(t *testing.T) {
	if _, err := MergeFilterValues(nil, json.RawMessage(`[1,2]`)); err == nil {
		t.Fatal("expected an error for a JSON array")
	}
}

// filter_options queries populate dropdowns and are rejected by the query
// endpoint, so they must never be requested by default.
func TestDataQueryIDsExcludesFilterOptions(t *testing.T) {
	ids := testSpec().DataQueryIDs()
	if len(ids) != 2 || ids[0] != "q_trend" || ids[1] != "q_kpi" {
		t.Fatalf("got %#v", ids)
	}
}
