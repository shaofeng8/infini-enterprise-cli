package project

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
)

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

func TestListReturnsProjectsWithRole(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK,
		`{"code": 200, "data": [{"id": "p1", "name": "销售分析", "role": "manager"}]}`)

	items, err := api.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || items[0].Role != "manager" {
		t.Fatalf("got %+v", items)
	}
	if recorded.path != BasePath+"/list" {
		t.Fatalf("path: got %q", recorded.path)
	}
}

// Create posts to the controller root, not to /create.
func TestCreatePostsToRoot(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": {"id": "p1"}}`)

	if _, err := api.Create(Spec{Name: "销售分析"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if recorded.method != http.MethodPost || recorded.path != BasePath {
		t.Fatalf("got %s %s", recorded.method, recorded.path)
	}
	if recorded.body["name"] != "销售分析" {
		t.Fatalf("name: got %v", recorded.body["name"])
	}
	// Empty optional fields stay out so a PATCH-style partial create is honest.
	if _, present := recorded.body["description"]; present {
		t.Fatalf("description should be omitted, got %v", recorded.body)
	}
}

// Update is a PATCH, so sending only the changed field is enough and there is
// no read-modify-write.
func TestUpdateIsPatchWithOnlyChangedFields(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": {"id": "p1"}}`)

	if _, err := api.Update("p1", Spec{Name: "销售分析 2026"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if recorded.method != http.MethodPatch || recorded.path != BasePath+"/p1" {
		t.Fatalf("got %s %s", recorded.method, recorded.path)
	}
	if len(recorded.body) != 1 {
		t.Fatalf("body should carry only the changed field, got %v", recorded.body)
	}
}

func TestMemberRoutesUseBothPathSegments(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": {"success": true}}`)

	if _, err := api.UpdateMember("p1", "u7", "editor"); err != nil {
		t.Fatalf("UpdateMember: %v", err)
	}
	if recorded.method != http.MethodPatch || recorded.path != BasePath+"/p1/members/u7" {
		t.Fatalf("got %s %s", recorded.method, recorded.path)
	}
	if recorded.body["role"] != "editor" {
		t.Fatalf("role: got %v", recorded.body["role"])
	}

	api, recorded = newTestAPI(t, http.StatusOK, `{"code": 200, "data": {"success": true}}`)
	if _, err := api.RemoveMember("p1", "u7"); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}
	if recorded.method != http.MethodDelete || recorded.path != BasePath+"/p1/members/u7" {
		t.Fatalf("got %s %s", recorded.method, recorded.path)
	}
}

// Deleting a path is a DELETE that carries a body, which is unusual enough to
// be worth pinning.
func TestDeletePathSendsBodyOnDelete(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": null}`)

	if _, err := api.DeletePath("p1", "data/old"); err != nil {
		t.Fatalf("DeletePath: %v", err)
	}
	if recorded.method != http.MethodDelete || recorded.path != BasePath+"/p1/files" {
		t.Fatalf("got %s %s", recorded.method, recorded.path)
	}
	if recorded.body["path"] != "data/old" {
		t.Fatalf("path: got %v", recorded.body["path"])
	}
}

func TestFileOperationsUseDistinctMethods(t *testing.T) {
	api, recorded := newTestAPI(t, http.StatusOK, `{"code": 200, "data": null}`)
	if _, err := api.MoveFile("p1", "a/x.csv", "b"); err != nil {
		t.Fatalf("MoveFile: %v", err)
	}
	if recorded.method != http.MethodPatch || recorded.path != BasePath+"/p1/files/move" {
		t.Fatalf("move: got %s %s", recorded.method, recorded.path)
	}

	api, recorded = newTestAPI(t, http.StatusOK, `{"code": 200, "data": null}`)
	if _, err := api.CopyFile("p1", "a/x.csv", "b"); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}
	if recorded.method != http.MethodPost || recorded.path != BasePath+"/p1/files/copy" {
		t.Fatalf("copy: got %s %s", recorded.method, recorded.path)
	}
}

func TestDownloadFileEscapesProjectIDAndPassesPathAsQuery(t *testing.T) {
	var escapedPath, query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		escapedPath = r.URL.EscapedPath()
		query = r.URL.RawQuery
		_, _ = w.Write([]byte("id,value\n"))
	}))
	t.Cleanup(server.Close)

	api := NewAPI(client.NewAnonymous(server.URL))
	if _, err := api.DownloadFile("p 1", "data/result.csv", t.TempDir()); err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	if escapedPath != BasePath+"/p%201/files/download" {
		t.Fatalf("path: got %q", escapedPath)
	}
	if query != "path=data%2Fresult.csv" {
		t.Fatalf("query: got %q", query)
	}
}
