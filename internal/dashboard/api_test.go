package dashboard

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

// capture records what the CLI actually put on the wire, so request shape is
// asserted against the server DTOs rather than assumed.
type capture struct {
	method      string
	path        string
	escapedPath string
	query       string
	body        map[string]any
}

func newTestAPI(t *testing.T, status int, response string) (*API, *capture) {
	t.Helper()
	recorded := &capture{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorded.method = r.Method
		recorded.path = r.URL.Path
		recorded.escapedPath = r.URL.EscapedPath()
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

func TestListUnwrapsEnvelope(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{
	  "code": 200, "message": "ok",
	  "data": [{"id": "d1", "title": "Sales", "currentRevision": 3, "updatedAt": "2026-01-01"}]
	}`)

	items, err := api.List("proj-1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || items[0].ID != "d1" || items[0].CurrentRevision != 3 {
		t.Fatalf("got %+v", items)
	}
	if recorded.path != BasePath {
		t.Fatalf("path: got %q", recorded.path)
	}
	if recorded.query != "project_id=proj-1" {
		t.Fatalf("query: got %q", recorded.query)
	}
}

func TestQuerySendsQueryIDsAndFilterValues(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK,
		`{"code": 200, "data": {"q_trend": {"status": "ok", "row_count": 12, "from_cache": true}}}`)

	results, err := api.Query("d1", []string{"q_trend"},
		FilterValues{"min_pv": float64(10)}, QueryOptions{ForceRefresh: true})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	result := results["q_trend"]
	if result.Status != "ok" || result.RowCount == nil || *result.RowCount != 12 {
		t.Fatalf("got %+v", result)
	}
	if recorded.path != BasePath+"/d1/query" {
		t.Fatalf("path: got %q", recorded.path)
	}
	if ids, ok := recorded.body["query_ids"].([]any); !ok || len(ids) != 1 || ids[0] != "q_trend" {
		t.Fatalf("query_ids: got %#v", recorded.body["query_ids"])
	}
	filters, ok := recorded.body["filter_values"].(map[string]any)
	if !ok || filters["min_pv"] != float64(10) {
		t.Fatalf("filter_values: got %#v", recorded.body["filter_values"])
	}
	if recorded.body["force_refresh"] != true {
		t.Fatalf("force_refresh: got %#v", recorded.body["force_refresh"])
	}
}

// filter_values must stay an object: the DTO validates it with @IsObject and
// rejects null.
func TestQuerySendsEmptyObjectForAbsentFilters(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": {}}`)

	if _, err := api.Query("d1", []string{"q_trend"}, nil, QueryOptions{}); err != nil {
		t.Fatalf("Query: %v", err)
	}
	filters, ok := recorded.body["filter_values"].(map[string]any)
	if !ok || len(filters) != 0 {
		t.Fatalf("filter_values: got %#v", recorded.body["filter_values"])
	}
	for _, absent := range []string{"force_refresh", "prefer_snapshot", "snapshot_only"} {
		if _, present := recorded.body[absent]; present {
			t.Fatalf("%s should be omitted when unset", absent)
		}
	}
}

func TestUpdateLayoutWrapsLayoutsArray(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": null}`)

	if _, err := api.UpdateLayout("d1", []WidgetLayout{{WidgetID: "w_kpi", X: 0, Y: 0, W: 3, H: 2}}); err != nil {
		t.Fatalf("UpdateLayout: %v", err)
	}
	if recorded.method != http.MethodPatch {
		t.Fatalf("method: got %q", recorded.method)
	}
	layouts, ok := recorded.body["layouts"].([]any)
	if !ok || len(layouts) != 1 {
		t.Fatalf("layouts: got %#v", recorded.body["layouts"])
	}
	entry := layouts[0].(map[string]any)
	if entry["widget_id"] != "w_kpi" || entry["w"] != float64(3) {
		t.Fatalf("layout entry: got %#v", entry)
	}
}

// GET .../refreshes/active returns null when nothing is running; that is not an
// error and must not be reported as one.
func TestActiveRefreshHandlesNull(t *testing.T) {
	api, _ := newTestAPI(t, http.StatusOK, `{"code": 200, "data": null}`)

	job, err := api.ActiveRefresh("d1")
	if err != nil {
		t.Fatalf("ActiveRefresh: %v", err)
	}
	if job != nil {
		t.Fatalf("got %+v, want nil", job)
	}
}

func TestRemoveWidgetEscapesPathSegments(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": {"revision": 4}}`)

	if _, err := api.RemoveWidget("d 1", "w/kpi"); err != nil {
		t.Fatalf("RemoveWidget: %v", err)
	}
	if recorded.escapedPath != BasePath+"/d%201/widgets/w%2Fkpi" {
		t.Fatalf("escaped path: got %q", recorded.escapedPath)
	}
}

// A server error code must become the matching exit code, not a generic failure.
func TestAuthErrorCodeMapsToAuthExit(t *testing.T) {
	api, _ := newTestAPI(t, http.StatusOK, `{"code": 1101, "message": "token expired"}`)

	_, err := api.List("")
	if err == nil {
		t.Fatal("expected an error")
	}
	if cliexit.CodeOf(err) != cliexit.CodeAuth {
		t.Fatalf("got exit code %d: %v", cliexit.CodeOf(err), err)
	}
}

func TestLicenseErrorCodeMapsToLicenseExit(t *testing.T) {
	api, _ := newTestAPI(t, http.StatusOK, `{"code": 3413, "message": "task quota exhausted"}`)

	_, err := api.List("")
	if cliexit.CodeOf(err) != cliexit.CodeLicense {
		t.Fatalf("got exit code %d: %v", cliexit.CodeOf(err), err)
	}
}

func TestHTTPErrorStatusMapsToExitCode(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   int
	}{
		{http.StatusUnauthorized, cliexit.CodeAuth},
		{http.StatusNotFound, cliexit.CodeBusiness},
		{http.StatusBadGateway, cliexit.CodeNetwork},
	} {
		api, _ := newTestAPI(t, tc.status, `{"statusCode": 0, "message": "boom"}`)
		_, err := api.List("")
		if got := cliexit.CodeOf(err); got != tc.want {
			t.Fatalf("HTTP %d: got exit code %d, want %d", tc.status, got, tc.want)
		}
	}
}
