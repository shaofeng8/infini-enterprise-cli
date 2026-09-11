package task

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
)

// capture records what the CLI actually put on the wire, so request shape is
// asserted against the server DTOs rather than assumed.
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

func TestListSendsOnlySetFilters(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{
	  "code": 200,
	  "data": {"items": [{"id": "t1", "task_name": "Sales", "task_status": "running", "is_pinned": true}],
	           "meta": {"totalItems": 1, "currentPage": 1, "totalPages": 1}}
	}`)

	result, err := api.List(ListQuery{Page: 2, PageSize: 50, Status: "running", Field: "updated_at", Order: "desc"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].ID != "t1" || !result.Items[0].IsPinned {
		t.Fatalf("got %+v", result.Items)
	}
	if result.Meta.TotalItems != 1 {
		t.Fatalf("meta: got %+v", result.Meta)
	}

	query, err := parseQuery(recorded.query)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	for key, want := range map[string]string{
		"page": "2", "pageSize": "50", "task_status": "running",
		"field": "updated_at", "order": "desc",
	} {
		if query[key] != want {
			t.Fatalf("%s: got %q, want %q", key, query[key], want)
		}
	}
	// Empty filters must not reach the server: the DTO validates types and an
	// empty task_name would be a pointless fuzzy match on everything.
	for _, key := range []string{"task_name", "keyword", "pinned_only", "audit", "project_filter"} {
		if _, present := query[key]; present {
			t.Fatalf("%s should be omitted, query was %q", key, recorded.query)
		}
	}
}

func TestListJoinsProjectIDsAndUsesTruthyAudit(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": {"items": []}}`)

	if _, err := api.List(ListQuery{
		ProjectFilter: "project",
		ProjectIDs:    []string{"p1", "p2"},
		Audit:         true,
		PinnedOnly:    true,
	}); err != nil {
		t.Fatalf("List: %v", err)
	}

	query, err := parseQuery(recorded.query)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if query["project_ids"] != "p1,p2" {
		t.Fatalf("project_ids: got %q", query["project_ids"])
	}
	if query["audit"] != "1" {
		t.Fatalf("audit: got %q, want 1", query["audit"])
	}
	if query["pinned_only"] != "true" {
		t.Fatalf("pinned_only: got %q", query["pinned_only"])
	}
}

func TestStatusesUnwrapsItems(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK,
		`{"code": 200, "data": {"items": [{"id": "t1", "task_status": "completed"}]}}`)

	items, err := api.Statuses([]string{"t1", "t2"})
	if err != nil {
		t.Fatalf("Statuses: %v", err)
	}
	if len(items) != 1 || items[0].Status != "completed" {
		t.Fatalf("got %+v", items)
	}
	if recorded.method != http.MethodPost || recorded.path != BasePath+"/statuses" {
		t.Fatalf("got %s %s", recorded.method, recorded.path)
	}
	ids, _ := recorded.body["task_ids"].([]any)
	if len(ids) != 2 || ids[0] != "t1" {
		t.Fatalf("task_ids: got %v", recorded.body["task_ids"])
	}
}

// Cancel is a POST whose id travels in the query string, not the body.
func TestCancelSendsTaskIDAsQueryParam(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": {"ok": true}}`)

	if _, err := api.Cancel("t 1"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if recorded.method != http.MethodPost || recorded.path != BasePath+"/cancelTask" {
		t.Fatalf("got %s %s", recorded.method, recorded.path)
	}
	if recorded.query != "taskId=t+1" {
		t.Fatalf("query: got %q", recorded.query)
	}
	if len(recorded.body) != 0 {
		t.Fatalf("body should be empty, got %v", recorded.body)
	}
}

// The KPI endpoint takes JSON encoded into strings, not arrays.
func TestRunKpiSQLEncodesDatabasesAndTablesAsStrings(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": {"datas": []}}`)

	if _, err := api.RunKpiSQL(KpiSQLRequest{
		Databases: []string{"chinook"},
		Tables:    []KpiTable{{Database: "chinook", Table: "artists"}},
		SQL:       "SELECT 1",
	}); err != nil {
		t.Fatalf("RunKpiSQL: %v", err)
	}

	if got, ok := recorded.body["databases"].(string); !ok || got != `["chinook"]` {
		t.Fatalf("databases: got %v", recorded.body["databases"])
	}
	if got, ok := recorded.body["tables"].(string); !ok || got != `[{"database":"chinook","table":"artists"}]` {
		t.Fatalf("tables: got %v", recorded.body["tables"])
	}
	if _, present := recorded.body["setValues"]; present {
		t.Fatalf("setValues should be omitted when empty, got %v", recorded.body)
	}
}

func TestFileTreeFlattensDepthFirst(t *testing.T) {
	api, _ := newTestAPI(t, http.StatusOK, `{"code": 200, "data": {
	  "items": [
	    {"key": "data", "title": "data", "isDir": true, "children": [
	      {"key": "data/result.csv", "title": "result.csv", "isDir": false, "size": "1.5 MB"}
	    ]},
	    {"key": "notes.md", "title": "notes.md", "isDir": false}
	  ],
	  "truncated": true
	}}`)

	tree, err := api.FileTree("t1")
	if err != nil {
		t.Fatalf("FileTree: %v", err)
	}
	if !tree.Truncated {
		t.Fatal("truncated should survive decoding")
	}

	all := tree.Flatten(false)
	if len(all) != 3 || all[0].Key != "data" || all[1].Key != "data/result.csv" || all[2].Key != "notes.md" {
		t.Fatalf("got %+v", all)
	}

	files := tree.Flatten(true)
	if len(files) != 2 || files[0].Key != "data/result.csv" {
		t.Fatalf("files only: got %+v", files)
	}
}

func TestDownloadZipWritesToDestination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="report.zip"`)
		_, _ = w.Write([]byte("PK\x03\x04payload"))
	}))
	t.Cleanup(server.Close)
	api := NewAPI(client.NewAnonymous(server.URL))

	dir := t.TempDir()
	result, err := api.DownloadZip("t1", dir)
	if err != nil {
		t.Fatalf("DownloadZip: %v", err)
	}

	// A directory destination must adopt the server's filename.
	want := filepath.Join(dir, "report.zip")
	if result.Path != want {
		t.Fatalf("path: got %q, want %q", result.Path, want)
	}
	if result.Bytes != 11 {
		t.Fatalf("bytes: got %d", result.Bytes)
	}
	if data, err := os.ReadFile(want); err != nil || string(data) != "PK\x03\x04payload" {
		t.Fatalf("file contents: %q (%v)", data, err)
	}
}

// A failed download must not leave a partial file behind, because a truncated
// archive looks like a valid result.
func TestDownloadReportsErrorsInsteadOfWritingFiles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code": 404, "message": "task not found"}`))
	}))
	t.Cleanup(server.Close)
	api := NewAPI(client.NewAnonymous(server.URL))

	dir := t.TempDir()
	if _, err := api.DownloadFile("t1", "data/result.csv", dir); err == nil {
		t.Fatal("expected an error for HTTP 404")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("no file should have been written, found %d", len(entries))
	}
}

func TestDownloadFilePassesPathAsQueryParam(t *testing.T) {
	var recordedPath, recordedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recordedPath = r.URL.Path
		recordedQuery = r.URL.RawQuery
		_, _ = w.Write([]byte("id,value\n"))
	}))
	t.Cleanup(server.Close)
	api := NewAPI(client.NewAnonymous(server.URL))

	result, err := api.DownloadFile("t1", "data/result.csv", t.TempDir())
	if err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	if recordedPath != StoragePath+"/downloadTaskFile/t1" {
		t.Fatalf("path: got %q", recordedPath)
	}
	if recordedQuery != "path=data%2Fresult.csv" {
		t.Fatalf("query: got %q", recordedQuery)
	}
	// Without Content-Disposition the file keeps its own base name.
	if filepath.Base(result.Path) != "result.csv" {
		t.Fatalf("fallback name: got %q", result.Path)
	}
}

func parseQuery(raw string) (map[string]string, error) {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(values))
	for key, list := range values {
		if len(list) > 0 {
			out[key] = list[0]
		}
	}
	return out, nil
}
