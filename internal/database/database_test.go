package database

import (
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
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

func TestListSendsEnabledOnlyWhenSet(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": {
	  "items": [{"id": "db1", "name": "chinook", "type": "sqlite", "enabled": 1}],
	  "meta": {"totalItems": 1}
	}}`)

	result, err := api.List(ListQuery{Type: "sqlite"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].Enabled == nil || *result.Items[0].Enabled != 1 {
		t.Fatalf("got %+v", result.Items)
	}

	query, err := url.ParseQuery(recorded.query)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if query.Get("type") != "sqlite" {
		t.Fatalf("type: got %q", query.Get("type"))
	}
	// Absent means "either", so an unset filter must not become enabled=0.
	if _, present := query["enabled"]; present {
		t.Fatalf("enabled should be omitted, query was %q", recorded.query)
	}

	disabled := 0
	api, recorded = newTestAPI(t, http.StatusOK, `{"code": 200, "data": {"items": []}}`)
	if _, err := api.List(ListQuery{Enabled: &disabled}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if recorded.query != "enabled=0" {
		t.Fatalf("query: got %q, want enabled=0", recorded.query)
	}
}

// Enablement lives in a per-user mapping table, so it is a batch toggle with
// its own endpoint rather than a field on update.
func TestSetEnabledSendsNumericFlag(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": null}`)

	if _, err := api.SetEnabled([]string{"db1", "db2"}, false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if recorded.path != BasePath+"/enabled" {
		t.Fatalf("path: got %q", recorded.path)
	}
	if recorded.body["enabled"] != float64(0) {
		t.Fatalf("enabled: got %v, want 0", recorded.body["enabled"])
	}
	ids, _ := recorded.body["ids"].([]any)
	if len(ids) != 2 {
		t.Fatalf("ids: got %v", recorded.body["ids"])
	}
}

// The config is stored as text, so it must reach the server as a JSON string
// and not as a nested object.
func TestAddSendsConfigAsString(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": {"id": "db1"}}`)

	if _, err := api.Add(Spec{
		Name:   "chinook",
		Type:   "sqlite",
		Config: `{"path":"/data/chinook.db"}`,
		// An id on add would be meaningless, so Add clears it.
		ID:      "ignored",
		Enabled: 1,
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if got, ok := recorded.body["config"].(string); !ok || got != `{"path":"/data/chinook.db"}` {
		t.Fatalf("config: got %v", recorded.body["config"])
	}
	if _, present := recorded.body["id"]; present {
		t.Fatalf("id should not be sent on add, got %v", recorded.body)
	}
	if recorded.body["enabled"] != float64(1) {
		t.Fatalf("enabled: got %v", recorded.body["enabled"])
	}
}

func TestUpdateKeepsID(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": {"id": "db1"}}`)

	if _, err := api.Update(Spec{ID: "db1", Name: "chinook", Type: "sqlite", Config: "{}"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if recorded.path != BasePath+"/update" {
		t.Fatalf("path: got %q", recorded.path)
	}
	if recorded.body["id"] != "db1" {
		t.Fatalf("id: got %v", recorded.body["id"])
	}
}

// The schema endpoint lives on a second controller that shares the ai_database
// prefix, which is easy to get wrong when reading only one controller.
func TestSchemaUsesDatabasePrefix(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": {"tables": []}}`)

	if _, err := api.Schema("db 1"); err != nil {
		t.Fatalf("Schema: %v", err)
	}
	if recorded.path != BasePath+"/schema/db 1" {
		t.Fatalf("path: got %q", recorded.path)
	}
}

func TestUploadSendsMultipartWithFilename(t *testing.T) {
	var (
		contentType string
		fieldName   string
		fileName    string
		contents    []byte
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		_, params, err := mime.ParseMediaType(contentType)
		if err != nil {
			t.Errorf("Content-Type: %v", err)
			return
		}
		reader := multipart.NewReader(r.Body, params["boundary"])
		part, err := reader.NextPart()
		if err != nil {
			t.Errorf("NextPart: %v", err)
			return
		}
		fieldName = part.FormName()
		fileName = part.FileName()
		contents, _ = io.ReadAll(part)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code": 200, "data": {"stored": true}}`))
	}))
	t.Cleanup(server.Close)

	source := filepath.Join(t.TempDir(), "sales.csv")
	if err := os.WriteFile(source, []byte("id,value\n1,2\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	api := NewAPI(client.NewAnonymous(server.URL))
	raw, err := api.Upload("db1", source)
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if string(raw) != `{"stored": true}` {
		t.Fatalf("response: got %s", raw)
	}
	if fieldName != "file" {
		t.Fatalf("field: got %q, want file", fieldName)
	}
	if fileName != "sales.csv" {
		t.Fatalf("filename: got %q", fileName)
	}
	if string(contents) != "id,value\n1,2\n" {
		t.Fatalf("contents: got %q", contents)
	}
}

func TestUploadRejectsMissingFileBeforeSending(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": null}`)

	if _, err := api.Upload("db1", filepath.Join(t.TempDir(), "absent.csv")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
	if recorded.method != "" {
		t.Fatalf("nothing should have been sent, got %s", recorded.method)
	}
}
