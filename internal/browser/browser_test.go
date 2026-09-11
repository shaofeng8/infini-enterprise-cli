package browser

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

func newAPI(t *testing.T, response string) (*API, *struct {
	path string
	body map[string]any
}) {
	t.Helper()
	recorded := &struct {
		path string
		body map[string]any
	}{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorded.path = r.URL.Path
		payload, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(payload, &recorded.body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(server.Close)
	return NewAPI(client.NewAnonymous(server.URL)), recorded
}

// Everything goes through the generic endpoint, because it is the only one
// that honours the session id and timeout.
func TestSendUsesTheGenericEndpoint(t *testing.T) {
	api, recorded := newAPI(t, `{"code":200,"data":{"success":true,"result":{}}}`)
	_, err := api.Send(Action{
		Type:      "navigate",
		Payload:   json.RawMessage(`{"url":"https://example.com"}`),
		SessionID: "tab-1",
		TimeoutMs: 5000,
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if recorded.path != "/api/ai_browser/action" {
		t.Fatalf("path: got %q", recorded.path)
	}
	if recorded.body["session_id"] != "tab-1" {
		t.Fatalf("session_id: got %v", recorded.body["session_id"])
	}
	if recorded.body["timeout_ms"] != float64(5000) {
		t.Fatalf("timeout_ms: got %v", recorded.body["timeout_ms"])
	}
}

// A disconnected browser answers HTTP 200 with success false, so the body is
// what decides the outcome.
func TestARefusalBecomesAnError(t *testing.T) {
	api, _ := newAPI(t, `{"code":200,"data":{"success":false,"error":"Browser extension not connected"}}`)
	result, err := api.Send(Action{Type: "view"})
	if err == nil {
		t.Fatal("a refusal must not read as success")
	}
	if cliexit.CodeOf(err) != cliexit.CodeBusiness {
		t.Fatalf("exit code: got %d", cliexit.CodeOf(err))
	}
	// The result still comes back so the caller can print the reason.
	if result == nil || result.Error == "" {
		t.Fatalf("result: got %v", result)
	}
}

// An unknown type would be answered with 200 and an "Unknown action type"
// body, so it is caught before the request goes out.
func TestSendRejectsAnUnknownTypeWithoutCallingTheServer(t *testing.T) {
	api, recorded := newAPI(t, `{"code":200,"data":{"success":true}}`)
	if _, err := api.Send(Action{Type: "teleport"}); cliexit.CodeOf(err) != cliexit.CodeUsage {
		t.Fatalf("got %v", err)
	}
	if recorded.path != "" {
		t.Fatal("nothing should have been sent")
	}
}

// The console actions have no dedicated route, which is why they only exist
// on the generic endpoint.
func TestConsoleActionsAreKnownTypes(t *testing.T) {
	for _, actionType := range []string{"browser_console_exec", "browser_console_view"} {
		if err := CheckType(actionType); err != nil {
			t.Fatalf("%s: %v", actionType, err)
		}
	}
}

func TestDirectTargetsThePerActionRoute(t *testing.T) {
	api, recorded := newAPI(t, `{"code":200,"data":{"success":true}}`)
	if _, err := api.Direct("navigate", map[string]string{"url": "https://example.com"}); err != nil {
		t.Fatalf("Direct: %v", err)
	}
	if recorded.path != "/api/ai_browser/action/navigate" {
		t.Fatalf("path: got %q", recorded.path)
	}
}

func TestSessionWithoutAUIDReadsYourOwn(t *testing.T) {
	api, recorded := newAPI(t, `{"code":200,"data":null}`)
	if _, err := api.Session(""); err != nil {
		t.Fatalf("Session: %v", err)
	}
	if recorded.path != "/api/ai_browser/session" {
		t.Fatalf("path: got %q", recorded.path)
	}
	if _, err := api.Session("u_9"); err != nil {
		t.Fatalf("Session: %v", err)
	}
	if recorded.path != "/api/ai_browser/session/u_9" {
		t.Fatalf("path: got %q", recorded.path)
	}
}
