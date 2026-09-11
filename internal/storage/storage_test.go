package storage

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

type request struct {
	method      string
	path        string
	query       url.Values
	contentType string
	body        []byte
}

// recorder answers a scripted sequence of responses and keeps every request,
// which is what a multi-step session needs.
type recorder struct {
	mu        sync.Mutex
	requests  []request
	responses []string
}

func newRecorder(t *testing.T, responses ...string) (*API, *recorder) {
	t.Helper()
	rec := &recorder{responses: responses}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		defer rec.mu.Unlock()
		rec.requests = append(rec.requests, request{
			method:      r.Method,
			path:        r.URL.Path,
			query:       r.URL.Query(),
			contentType: r.Header.Get("Content-Type"),
			body:        payload,
		})
		response := `{"code":200,"data":{}}`
		if index := len(rec.requests) - 1; index < len(rec.responses) {
			response = rec.responses[index]
		} else if len(rec.responses) > 0 {
			response = rec.responses[len(rec.responses)-1]
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(server.Close)
	return NewAPI(client.NewAnonymous(server.URL)), rec
}

func (r *recorder) at(index int) request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.requests[index]
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

// The upload controller is mounted at the API root, so these paths have no
// module prefix.
func TestDirectoryEndpointsSitAtTheAPIRoot(t *testing.T) {
	api, rec := newRecorder(t, `{"code":200,"data":[]}`)
	if _, err := api.Directories(); err != nil {
		t.Fatalf("Directories: %v", err)
	}
	if rec.at(0).path != "/api/directories" {
		t.Fatalf("path: got %q", rec.at(0).path)
	}
}

// The delete route takes its target in the body of a DELETE.
func TestDeleteDirectorySendsABody(t *testing.T) {
	api, rec := newRecorder(t, `{"code":200,"data":{"message":"ok"}}`)
	if _, err := api.DeleteDirectory("scratch"); err != nil {
		t.Fatalf("DeleteDirectory: %v", err)
	}
	sent := rec.at(0)
	if sent.method != http.MethodDelete {
		t.Fatalf("method: got %s", sent.method)
	}
	if !strings.Contains(string(sent.body), "scratch") {
		t.Fatalf("body: got %q", sent.body)
	}
}

func TestUploadToTaskPassesNamingAsAQueryParameter(t *testing.T) {
	api, rec := newRecorder(t, `{"code":200,"data":{"filename":"/w/a.csv"}}`)
	file := writeTempFile(t, "a.csv", "x")
	if _, err := api.UploadToTask("t_1", file, "data/raw", "hash"); err != nil {
		t.Fatalf("UploadToTask: %v", err)
	}
	sent := rec.at(0)
	if sent.path != "/api/taskUpload/t_1" {
		t.Fatalf("path: got %q", sent.path)
	}
	if sent.query.Get("naming") != "hash" || sent.query.Get("subdir") != "data/raw" {
		t.Fatalf("query: got %v", sent.query)
	}
	if !strings.HasPrefix(sent.contentType, "multipart/form-data") {
		t.Fatalf("content type: got %q", sent.contentType)
	}
}

func TestDeleteRejectsAnEmptyList(t *testing.T) {
	api, _ := newRecorder(t)
	if _, err := api.Delete(nil); cliexit.CodeOf(err) != cliexit.CodeUsage {
		t.Fatalf("got %v", err)
	}
}

func TestRemainingSkipsUploadedChunks(t *testing.T) {
	session := Session{TotalChunks: 5, UploadedChunks: []int{0, 2, 4}}
	remaining := session.Remaining()
	if len(remaining) != 2 || remaining[0] != 1 || remaining[1] != 3 {
		t.Fatalf("got %v, want [1 3]", remaining)
	}
}

// A chunk is the whole request body. A multipart wrapper would be written
// into the chunk and corrupt the assembled file.
func TestSendChunkPutsRawBytes(t *testing.T) {
	api, rec := newRecorder(t, `{"code":200,"data":{"uploadId":"u_1","uploadedChunks":[0]}}`)
	if _, err := api.SendChunk("u_1", 0, strings.NewReader("abc"), 3); err != nil {
		t.Fatalf("SendChunk: %v", err)
	}
	sent := rec.at(0)
	if sent.method != http.MethodPut || sent.path != "/api/file_upload/u_1/chunks/0" {
		t.Fatalf("got %s %s", sent.method, sent.path)
	}
	if string(sent.body) != "abc" {
		t.Fatalf("body: got %q, want the raw bytes", sent.body)
	}
	if strings.Contains(sent.contentType, "multipart") {
		t.Fatalf("content type: got %q, must not be multipart", sent.contentType)
	}
}

// Resuming is the point of a session: only the missing chunks go out.
func TestSendFileSkipsChunksTheServerAlreadyHas(t *testing.T) {
	api, rec := newRecorder(t,
		`{"code":200,"data":{"uploadId":"u_1","uploadedChunks":[0,2]}}`,
		`{"code":200,"data":{"uploadId":"u_1","status":"completed"}}`,
	)
	file := writeTempFile(t, "big.bin", "aabbcc")
	session := &Session{
		UploadID: "u_1", FileSize: 6, ChunkSize: 2,
		TotalChunks: 3, UploadedChunks: []int{0, 2},
	}

	if _, err := api.SendFile(session, file, nil); err != nil {
		t.Fatalf("SendFile: %v", err)
	}
	// One chunk plus the completion call.
	if rec.count() != 2 {
		t.Fatalf("expected 2 requests, got %d", rec.count())
	}
	chunk := rec.at(0)
	if chunk.path != "/api/file_upload/u_1/chunks/1" {
		t.Fatalf("chunk path: got %q, want index 1", chunk.path)
	}
	if string(chunk.body) != "bb" {
		t.Fatalf("chunk body: got %q, want the second chunk", chunk.body)
	}
	if rec.at(1).path != "/api/file_upload/u_1/complete" {
		t.Fatalf("completion: got %q", rec.at(1).path)
	}
}

// A file that changed since init would produce a corrupt assembly, so the
// mismatch is caught locally rather than at completion time.
func TestSendFileRejectsASizeMismatch(t *testing.T) {
	api, rec := newRecorder(t, `{"code":200,"data":{}}`)
	file := writeTempFile(t, "changed.bin", "abcdef")
	session := &Session{UploadID: "u_1", FileSize: 99, ChunkSize: 2, TotalChunks: 50}

	if _, err := api.SendFile(session, file, nil); cliexit.CodeOf(err) != cliexit.CodeUsage {
		t.Fatalf("got %v", err)
	}
	if rec.count() != 0 {
		t.Fatal("nothing should have been sent")
	}
}

func TestSendFileReportsProgressPerChunk(t *testing.T) {
	api, _ := newRecorder(t,
		`{"code":200,"data":{"uploadId":"u_1","uploadedChunks":[0]}}`,
		`{"code":200,"data":{"uploadId":"u_1","uploadedChunks":[0,1]}}`,
		`{"code":200,"data":{"uploadId":"u_1","status":"completed"}}`,
	)
	file := writeTempFile(t, "two.bin", "abcd")
	session := &Session{UploadID: "u_1", FileSize: 4, ChunkSize: 2, TotalChunks: 2}

	var seen []int
	if _, err := api.SendFile(session, file, func(_ *Session, index int) {
		seen = append(seen, index)
	}); err != nil {
		t.Fatalf("SendFile: %v", err)
	}
	if len(seen) != 2 || seen[0] != 0 || seen[1] != 1 {
		t.Fatalf("progress: got %v, want [0 1]", seen)
	}
}

func TestFlattenWalksDepthFirst(t *testing.T) {
	tree := []TreeNode{{
		Key: "dir", IsDir: true,
		Children: []TreeNode{{Key: "dir/a.csv"}, {Key: "dir/b.csv"}},
	}}
	if all := Flatten(tree, false); len(all) != 3 || all[0].Key != "dir" {
		t.Fatalf("got %v", all)
	}
	files := Flatten(tree, true)
	if len(files) != 2 || files[0].Key != "dir/a.csv" {
		t.Fatalf("files only: got %v", files)
	}
}

func TestInitPostsTheSessionRequest(t *testing.T) {
	api, rec := newRecorder(t, `{"code":200,"data":{"uploadId":"u_1","totalChunks":2}}`)
	session, err := api.Init(InitRequest{
		TargetType: "database", TargetID: "db_1",
		FileName: "dump.csv", FileSize: 100, PostAction: "import_database",
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if session.UploadID != "u_1" {
		t.Fatalf("got %v", session)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.at(0).body, &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	if body["postAction"] != "import_database" || body["targetType"] != "database" {
		t.Fatalf("body: got %v", body)
	}
	// Optional fields left unset must not travel as empty strings, which the
	// server's enum validation would reject.
	if _, present := body["conflictPolicy"]; present {
		t.Fatal("conflictPolicy should be omitted when unset")
	}
}

func writeTempFile(t *testing.T, name, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("cannot write %s: %v", path, err)
	}
	return path
}
