package task

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

type readonlyCapture struct {
	method string
	path   string
	query  url.Values
	body   map[string]any
}

func newReadonlyAPI(t *testing.T, response string) (*API, *readonlyCapture) {
	t.Helper()
	recorded := &readonlyCapture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorded.method = r.Method
		recorded.path = r.URL.Path
		recorded.query = r.URL.Query()
		payload, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(payload, &recorded.body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(server.Close)
	return NewAPI(client.NewAnonymous(server.URL)), recorded
}

// A public read must carry no audit marker at all: sending audit=0 would still
// look like an audit request to the server, which treats "1"/"true" as the
// trigger but reads the parameter's presence in its own way.
func TestPublicReadOmitsTheAuditParameter(t *testing.T) {
	api, recorded := newReadonlyAPI(t, `{"code":200,"data":{}}`)
	if _, err := api.PublicTask("t_1", ReadonlyMode{}); err != nil {
		t.Fatalf("PublicTask: %v", err)
	}
	if recorded.path != "/api/ai_task/publicTask" {
		t.Fatalf("path: got %q", recorded.path)
	}
	if _, present := recorded.query["audit"]; present {
		t.Fatal("audit must be absent for a public read")
	}
	if recorded.query.Get("taskId") != "t_1" {
		t.Fatalf("taskId: got %q", recorded.query.Get("taskId"))
	}
}

func TestAuditReadSetsTheAuditParameter(t *testing.T) {
	api, recorded := newReadonlyAPI(t, `{"code":200,"data":{}}`)
	if _, err := api.PublicTask("t_1", ReadonlyMode{Audit: true}); err != nil {
		t.Fatalf("PublicTask: %v", err)
	}
	if recorded.query.Get("audit") != "1" {
		t.Fatalf("audit: got %q", recorded.query.Get("audit"))
	}
}

// Evidence is the one endpoint in this group that reads audit from the body.
func TestEvidenceSendsAuditInTheBody(t *testing.T) {
	api, recorded := newReadonlyAPI(t, `{"code":200,"data":[]}`)
	if _, err := api.PublicToolEvidence("t_1", []string{"1784807100587"}, true, ReadonlyMode{Audit: true}); err != nil {
		t.Fatalf("PublicToolEvidence: %v", err)
	}
	if recorded.method != http.MethodPost || recorded.path != "/api/ai_task/publicToolEvidence" {
		t.Fatalf("got %s %s", recorded.method, recorded.path)
	}
	if recorded.body["audit"] != true {
		t.Fatalf("audit: got %v", recorded.body["audit"])
	}
	if recorded.body["includeSubagent"] != true {
		t.Fatalf("includeSubagent: got %v", recorded.body["includeSubagent"])
	}
	if _, present := recorded.query["audit"]; present {
		t.Fatal("this endpoint reads audit from the body, not the query string")
	}
}

func TestEvidenceOmitsUnsetFlags(t *testing.T) {
	api, recorded := newReadonlyAPI(t, `{"code":200,"data":[]}`)
	if _, err := api.PublicToolEvidence("t_1", []string{"1784807100587"}, false, ReadonlyMode{}); err != nil {
		t.Fatalf("PublicToolEvidence: %v", err)
	}
	for _, field := range []string{"audit", "includeSubagent"} {
		if _, present := recorded.body[field]; present {
			t.Fatalf("%s should be omitted when false", field)
		}
	}
}

// The server caps the batch at 100, so the excess is caught locally where the
// message can say what the limit is.
func TestEvidenceEnforcesTheBatchLimit(t *testing.T) {
	api, _ := newReadonlyAPI(t, `{}`)
	if _, err := api.PublicToolEvidence("t_1", nil, false, ReadonlyMode{}); cliexit.CodeOf(err) != cliexit.CodeUsage {
		t.Fatalf("empty list: got %v", err)
	}

	tooMany := make([]string, 101)
	for i := range tooMany {
		tooMany[i] = "1784807100587"
	}
	if _, err := api.PublicToolEvidence("t_1", tooMany, false, ReadonlyMode{}); cliexit.CodeOf(err) != cliexit.CodeUsage {
		t.Fatalf("101 ids: got %v", err)
	}
}

func TestPublicFileTreePutsTheTaskInThePath(t *testing.T) {
	api, recorded := newReadonlyAPI(t, `{"code":200,"data":{}}`)
	if _, err := api.PublicFileTree("t 1", ReadonlyMode{Audit: true}); err != nil {
		t.Fatalf("PublicFileTree: %v", err)
	}
	if recorded.query.Get("audit") != "1" {
		t.Fatalf("audit: got %q", recorded.query.Get("audit"))
	}
	if recorded.path != "/api/ai_task/publicTaskFileTree/t 1" {
		t.Fatalf("path: got %q", recorded.path)
	}
}

// Preview posts its body but still needs audit in the query string, since the
// preview DTO has no audit field.
func TestPreviewCarriesAuditInTheQueryString(t *testing.T) {
	api, recorded := newReadonlyAPI(t, `{"code":200,"data":{}}`)
	if _, err := api.PublicPreviewFile("t_1", "report.xlsx", ReadonlyMode{Audit: true}); err != nil {
		t.Fatalf("PublicPreviewFile: %v", err)
	}
	if recorded.query.Get("audit") != "1" {
		t.Fatalf("audit: got %q", recorded.query.Get("audit"))
	}
	if recorded.body["fileName"] != "report.xlsx" {
		t.Fatalf("body: got %v", recorded.body)
	}
}

func TestPublicTaskRequiresAnID(t *testing.T) {
	api, _ := newReadonlyAPI(t, `{}`)
	if _, err := api.PublicTask("", ReadonlyMode{}); cliexit.CodeOf(err) != cliexit.CodeUsage {
		t.Fatalf("got %v", err)
	}
}
