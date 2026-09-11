package hub

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
)

type capture struct {
	method string
	path   string
	query  string
	body   map[string]any
}

func newTestAPI(t *testing.T, status int, response string) (*API, *capture) {
	t.Helper()
	recorded := &capture{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorded.method = r.Method
		recorded.path = r.URL.Path
		recorded.query = r.URL.RawQuery
		if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
			_ = json.Unmarshal(raw, &recorded.body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(server.Close)

	return NewAPI(client.NewAnonymous(server.URL)), recorded
}

// The URL segment is not the entity type with underscores swapped, which is
// the easy mistake to make when adding a kind.
func TestKindRoutesMatchTheController(t *testing.T) {
	want := map[Kind]string{
		KindTable:      "table-data",
		KindColumn:     "table-column",
		KindPlaybook:   "playbook",
		KindKPI:        "kpi",
		KindPreference: "preference",
	}
	for kind, route := range want {
		if got := kind.route(); got != route {
			t.Fatalf("%s: got route %q, want %q", kind, got, route)
		}
	}
}

func TestListSendsOnlySetFilters(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": {
	  "items": [{"id": "t1", "database_id": "db1", "table_name": "invoices",
	             "table_description": "订单事实表"}],
	  "meta": {"totalItems": 1}
	}}`)

	page, err := List[TableData](api, KindTable, PageQuery{Page: 2, PageSize: 50, DatabaseID: "db1"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Description != "订单事实表" {
		t.Fatalf("got %+v", page.Items)
	}
	if recorded.path != BasePath+"/table-data/list" {
		t.Fatalf("path: got %q", recorded.path)
	}

	query, err := url.ParseQuery(recorded.query)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if query.Get("databaseId") != "db1" || query.Get("pageSize") != "50" {
		t.Fatalf("query: got %q", recorded.query)
	}
	// onlyMine is a boolean the server parses from the string; sending false
	// would be indistinguishable from asking for only your own records.
	for _, key := range []string{"onlyMine", "search", "tableId", "status"} {
		if _, present := query[key]; present {
			t.Fatalf("%s should be omitted, query was %q", key, recorded.query)
		}
	}
}

// Playbook database_ids and KPI tables come back parsed even though the columns
// store JSON text, so the typed fields must line up with the parsed names.
func TestListParsesPlaybookDatabaseIDsAndKpiTables(t *testing.T) {
	api, _ := newTestAPI(t, http.StatusOK, `{"code": 200, "data": {
	  "items": [{"id": "p1", "playbook_name": "月度复盘",
	             "database_ids_json": "[\"db1\",\"db2\"]",
	             "database_ids": ["db1", "db2"]}],
	  "meta": {}
	}}`)
	playbooks, err := List[Playbook](api, KindPlaybook, PageQuery{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(playbooks.Items) != 1 || len(playbooks.Items[0].DatabaseIDs) != 2 {
		t.Fatalf("got %+v", playbooks.Items)
	}

	api, _ = newTestAPI(t, http.StatusOK, `{"code": 200, "data": {
	  "items": [{"id": "k1", "kpi_name": "月度销售额", "creation_mode": "sql_playground",
	             "tables": [{"database_id": "db1", "database_name": "chinook", "table_name": "invoices"}]}],
	  "meta": {}
	}}`)
	kpis, err := List[KPI](api, KindKPI, PageQuery{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(kpis.Items) != 1 || len(kpis.Items[0].Tables) != 1 {
		t.Fatalf("got %+v", kpis.Items)
	}
	if kpis.Items[0].Tables[0].DatabaseName != "chinook" {
		t.Fatalf("table ref: got %+v", kpis.Items[0].Tables[0])
	}
}

func TestDeleteSendsIDList(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": null}`)

	if _, err := api.Delete(KindColumn, []string{"c1", "c2"}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if recorded.method != http.MethodPost || recorded.path != BasePath+"/table-column/delete" {
		t.Fatalf("got %s %s", recorded.method, recorded.path)
	}
	ids, _ := recorded.body["ids"].([]any)
	if len(ids) != 2 || ids[0] != "c1" {
		t.Fatalf("ids: got %v", recorded.body["ids"])
	}
}

// Drafts of different kinds have different payload shapes, so entity_type is
// not optional on the listing.
func TestListDraftsAlwaysSendsEntityType(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": {
	  "items": [{"id": "d1", "entity_type": "kpi", "operation_type": "update", "status": "pending"}],
	  "meta": {}
	}}`)

	page, err := ListDrafts(api, KindKPI, PageQuery{Status: "pending"})
	if err != nil {
		t.Fatalf("ListDrafts: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].OperationType != "update" {
		t.Fatalf("got %+v", page.Items)
	}
	if recorded.path != BasePath+"/draft/list" {
		t.Fatalf("path: got %q", recorded.path)
	}
	query, _ := url.ParseQuery(recorded.query)
	if query.Get("entity_type") != "kpi" || query.Get("status") != "pending" {
		t.Fatalf("query: got %q", recorded.query)
	}
}

func TestApproveOmitsUnsetNarrowingFields(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": {"ok": true}}`)

	if _, err := api.Approve(ReviewRequest{EntityType: "table_data", DraftID: "d1"}); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if recorded.path != BasePath+"/review/approve" {
		t.Fatalf("path: got %q", recorded.path)
	}
	// An empty approved_fields list would read as "approve nothing", and a null
	// field_values fails the DTO's object check.
	for _, key := range []string{"approved_fields", "field_values", "review_comment"} {
		if _, present := recorded.body[key]; present {
			t.Fatalf("%s should be omitted, got %v", key, recorded.body)
		}
	}
}

func TestApproveSendsNarrowingFieldsWhenGiven(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": {"ok": true}}`)

	if _, err := api.Approve(ReviewRequest{
		EntityType:     "kpi",
		DraftID:        "d2",
		ApprovedFields: []string{"kpi_description"},
		FieldValues:    json.RawMessage(`{"kpi_description":"季度口径已修正"}`),
	}); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	fields, _ := recorded.body["approved_fields"].([]any)
	if len(fields) != 1 || fields[0] != "kpi_description" {
		t.Fatalf("approved_fields: got %v", recorded.body["approved_fields"])
	}
	values, _ := recorded.body["field_values"].(map[string]any)
	if values["kpi_description"] != "季度口径已修正" {
		t.Fatalf("field_values: got %v", recorded.body["field_values"])
	}
}

func TestPendingCountsKeyedByEntityType(t *testing.T) {
	api, _ := newTestAPI(t, http.StatusOK, `{"code": 200, "data": {
	  "validationRequests": 0, "userRatings": 0,
	  "contextHubUpdates": {"table_data": 3, "kpi": 1},
	  "totalContextHubUpdates": 4
	}}`)

	counts, err := api.PendingCounts()
	if err != nil {
		t.Fatalf("PendingCounts: %v", err)
	}
	if counts.ContextHubUpdates["table_data"] != 3 || counts.TotalContextHubUpdates != 4 {
		t.Fatalf("got %+v", counts)
	}
}

func TestStartMemoryBuildSendsSelectionAndSkipsFalseFlag(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": {
	  "jobId": "j1", "databaseId": "db1", "status": "pending", "progress": 0,
	  "activeStep": "review_changes"
	}}`)

	job, err := api.StartMemoryBuild(StartRequest{
		TaskID:       "memory_build_1",
		DatabaseID:   "db1",
		DatabaseName: "chinook",
		Tables: []MemoryTable{{
			TableName: "invoices",
			Columns:   []MemoryColumn{{Name: "total", Type: "decimal"}},
		}},
	})
	if err != nil {
		t.Fatalf("StartMemoryBuild: %v", err)
	}
	if job.JobID != "j1" || job.Terminal() {
		t.Fatalf("got %+v", job)
	}
	if recorded.path != BasePath+"/memory-build/start" {
		t.Fatalf("path: got %q", recorded.path)
	}
	tables, _ := recorded.body["tables"].([]any)
	if len(tables) != 1 {
		t.Fatalf("tables: got %v", recorded.body["tables"])
	}
	if _, present := recorded.body["skipExistingTableUpdates"]; present {
		t.Fatalf("an unset flag should be omitted, got %v", recorded.body)
	}
}

func TestJobTerminalCoversEveryStopState(t *testing.T) {
	for _, status := range []string{"succeeded", "failed", "cancelled"} {
		if !(&Job{Status: status}).Terminal() {
			t.Fatalf("%s should be terminal", status)
		}
	}
	for _, status := range []string{"pending", "running"} {
		if (&Job{Status: status}).Terminal() {
			t.Fatalf("%s should not be terminal", status)
		}
	}
}

// A missing job is reported as a nil job rather than an error, so callers can
// tell "no such job" apart from a request failure.
func TestStatusReturnsNilForNullPayload(t *testing.T) {
	api, _ := newTestAPI(t, http.StatusOK, `{"code": 200, "data": null}`)

	job, err := api.MemoryBuildStatus("missing")
	if err != nil {
		t.Fatalf("MemoryBuildStatus: %v", err)
	}
	if job != nil {
		t.Fatalf("expected a nil job, got %+v", job)
	}
}

func TestBatchOmitsEmptyMode(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": {
	  "items": [{"databaseId": "db1", "status": "started", "job": {"jobId": "j1"}}],
	  "summary": {"total": 1, "started": 1}
	}}`)

	result, err := api.StartMemoryBuildBatch([]string{"db1"}, "")
	if err != nil {
		t.Fatalf("StartMemoryBuildBatch: %v", err)
	}
	if result.Summary.Started != 1 || result.Items[0].Job == nil {
		t.Fatalf("got %+v", result)
	}
	if _, present := recorded.body["mode"]; present {
		t.Fatalf("mode should be omitted so the server default applies, got %v", recorded.body)
	}
}

func TestCancelPutsJobIDInPath(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": {"jobId": "j 1", "status": "cancelled"}}`)

	job, err := api.CancelMemoryBuild("j 1")
	if err != nil {
		t.Fatalf("CancelMemoryBuild: %v", err)
	}
	if !job.Terminal() {
		t.Fatalf("a cancelled job should be terminal, got %+v", job)
	}
	if recorded.method != http.MethodPost || recorded.path != BasePath+"/memory-build/cancel/j 1" {
		t.Fatalf("got %s %s", recorded.method, recorded.path)
	}
}

func TestTranslateSendsFieldList(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": {"fields": []}}`)

	if _, err := api.Translate("zh-CN", []TranslateField{{Key: "table_description", Text: "Fact table"}}); err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if recorded.body["target_language"] != "zh-CN" {
		t.Fatalf("target_language: got %v", recorded.body["target_language"])
	}
	fields, _ := recorded.body["fields"].([]any)
	if len(fields) != 1 {
		t.Fatalf("fields: got %v", recorded.body["fields"])
	}
	field, _ := fields[0].(map[string]any)
	if field["key"] != "table_description" || field["text"] != "Fact table" {
		t.Fatalf("field: got %v", field)
	}
	// An absent label must not be sent as an empty string.
	if _, present := field["label"]; present {
		t.Fatalf("label should be omitted, got %v", field)
	}
}
