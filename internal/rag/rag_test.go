package rag

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

// The paged listing is the controller's root route, while `all` is a sibling.
func TestListUsesRootRouteAndAllUsesSubroute(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": {
	  "items": [{"id": "r1", "name": "finance_docs", "enabled": 1, "docDir": "/data/finance"}],
	  "meta": {"totalItems": 1}
	}}`)

	result, err := api.List(ListQuery{Keyword: "财报"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].DocDir != "/data/finance" {
		t.Fatalf("got %+v", result.Items)
	}
	if recorded.path != BasePath {
		t.Fatalf("path: got %q, want %q", recorded.path, BasePath)
	}

	api, recorded = newTestAPI(t, http.StatusOK, `{"code": 200, "data": [{"id": "r1"}]}`)
	items, err := api.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %+v", items)
	}
	if recorded.path != BasePath+"/all" {
		t.Fatalf("path: got %q", recorded.path)
	}
}

// Create takes extensions as an array even though the record stores them as
// JSON text, so the two directions must not be confused.
func TestCreateSendsRequiredExtsAsArray(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": {"id": "r1"}}`)

	enabled := 1
	if _, err := api.Create(Spec{
		Name:         "finance_docs",
		RequiredExts: []string{"pdf", "md"},
		Enabled:      &enabled,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	exts, ok := recorded.body["requiredExts"].([]any)
	if !ok || len(exts) != 2 || exts[0] != "pdf" {
		t.Fatalf("requiredExts: got %v", recorded.body["requiredExts"])
	}
	if recorded.body["enabled"] != float64(1) {
		t.Fatalf("enabled: got %v", recorded.body["enabled"])
	}
}

// An unset Enabled must stay absent so the server keeps its default rather
// than reading a zero as "disabled".
func TestCreateOmitsUnsetEnabled(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": {"id": "r1"}}`)

	if _, err := api.Create(Spec{Name: "finance_docs"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, present := recorded.body["enabled"]; present {
		t.Fatalf("enabled should be omitted, got %v", recorded.body)
	}
}

func TestUpdatePutsIDInPath(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": {"id": "r1"}}`)

	if _, err := api.Update("r1", Spec{Name: "finance_docs"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if recorded.method != http.MethodPost || recorded.path != BasePath+"/update/r1" {
		t.Fatalf("got %s %s", recorded.method, recorded.path)
	}
}

// Credentials belong only to the remote file systems; a local listing must not
// carry empty credential fields.
func TestFileTreeOmitsEmptyCredentials(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": {"children": []}}`)

	if _, err := api.FileTree(Storage{FileSystem: "file"}, "/data/finance", "file"); err != nil {
		t.Fatalf("FileTree: %v", err)
	}
	if recorded.body["file_system"] != "file" || recorded.body["directory"] != "/data/finance" {
		t.Fatalf("body: got %v", recorded.body)
	}
	if recorded.body["filter"] != "file" {
		t.Fatalf("filter: got %v", recorded.body["filter"])
	}
	for _, key := range []string{"endpoint", "access_key_id", "access_key_secret"} {
		if _, present := recorded.body[key]; present {
			t.Fatalf("%s should be omitted for local storage, got %v", key, recorded.body)
		}
	}
}

func TestFileTreeSendsRemoteCredentials(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": {"children": []}}`)

	if _, err := api.FileTree(Storage{
		FileSystem:      "oss",
		Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
		AccessKeyID:     "AKID",
		AccessKeySecret: "SECRET",
	}, "docs/", ""); err != nil {
		t.Fatalf("FileTree: %v", err)
	}
	if recorded.body["access_key_id"] != "AKID" || recorded.body["access_key_secret"] != "SECRET" {
		t.Fatalf("credentials: got %v", recorded.body)
	}
	// An empty filter means "both"; sending "" would fail DTO validation.
	if _, present := recorded.body["filter"]; present {
		t.Fatalf("filter should be omitted when unset, got %v", recorded.body)
	}
}

func TestBindDatabasesSendsFullReplacementList(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": null}`)

	if _, err := api.BindDatabases("r1", []string{"db1", "db2"}); err != nil {
		t.Fatalf("BindDatabases: %v", err)
	}
	if recorded.path != BasePath+"/bindDatabases" || recorded.body["ragId"] != "r1" {
		t.Fatalf("got %s %v", recorded.path, recorded.body)
	}
	ids, _ := recorded.body["databaseIds"].([]any)
	if len(ids) != 2 || ids[1] != "db2" {
		t.Fatalf("databaseIds: got %v", recorded.body["databaseIds"])
	}
}

// Download is a POST whose body carries the storage descriptor, so the file
// path must not end up in the query string.
func TestDownloadPostsStorageDescriptor(t *testing.T) {
	var (
		method string
		query  string
		body   map[string]any
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		query = r.URL.RawQuery
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Disposition", `attachment; filename="q3.pdf"`)
		_, _ = w.Write([]byte("%PDF-1.7"))
	}))
	t.Cleanup(server.Close)

	api := NewAPI(client.NewAnonymous(server.URL))
	result, err := api.Download(Storage{FileSystem: "file"}, "docs/q3.pdf", t.TempDir())
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if method != http.MethodPost {
		t.Fatalf("method: got %s", method)
	}
	if query != "" {
		t.Fatalf("query should be empty, got %q", query)
	}
	if body["file_path"] != "docs/q3.pdf" {
		t.Fatalf("file_path: got %v", body["file_path"])
	}
	if result.Bytes != 8 {
		t.Fatalf("bytes: got %d", result.Bytes)
	}
}
