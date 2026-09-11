package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func countingServer(t *testing.T, hits *int) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"ok":true}}`))
	}))
	t.Cleanup(server.Close)
	return NewAnonymous(server.URL)
}

func reset(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		DryRun = false
		AuditLog = ""
	})
}

// A dry run has to keep reading. Most writes are preceded by a read of the
// current state so the write can be a patch, and a dry run that cannot look
// anything up would report nonsense.
func TestDryRunSuppressesWritesButNotReads(t *testing.T) {
	reset(t)
	hits := 0
	c := countingServer(t, &hits)
	DryRun = true

	if _, err := c.Get("/api/things", nil); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if hits != 1 {
		t.Fatalf("a read should still go out, hits=%d", hits)
	}

	raw, err := c.Post("/api/things", map[string]string{"name": "x"})
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if hits != 1 {
		t.Fatalf("a write must not go out, hits=%d", hits)
	}

	var described map[string]any
	if err := json.Unmarshal(raw, &described); err != nil {
		t.Fatalf("the suppressed write should describe itself: %v", err)
	}
	if described["dryRun"] != true || described["method"] != "POST" {
		t.Fatalf("got %v", described)
	}
}

// The description passes through the same redaction as --trace, so a dry run
// cannot become a way to print a secret.
func TestDryRunRedactsSecretsInTheDescription(t *testing.T) {
	reset(t)
	hits := 0
	c := countingServer(t, &hits)
	DryRun = true

	raw, err := c.Post("/api/login", map[string]string{
		"username": "alice",
		"password": "hunter2",
	})
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if strings.Contains(string(raw), "hunter2") {
		t.Fatalf("the password leaked into the dry-run output: %s", raw)
	}
}

func TestAuditLogRecordsOneLinePerRequest(t *testing.T) {
	reset(t)
	hits := 0
	c := countingServer(t, &hits)
	AuditLog = filepath.Join(t.TempDir(), "audit.jsonl")

	if _, err := c.Get("/api/things", nil); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if _, err := c.Post("/api/things", map[string]string{"name": "x"}); err != nil {
		t.Fatalf("Post: %v", err)
	}

	contents, err := os.ReadFile(AuditLog)
	if err != nil {
		t.Fatalf("cannot read the audit log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 entries, got %d: %s", len(lines), contents)
	}

	var entry map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &entry); err != nil {
		t.Fatalf("entry is not JSON: %v", err)
	}
	if entry["method"] != "POST" || entry["path"] != "/api/things" {
		t.Fatalf("got %v", entry)
	}
	if entry["status"] != float64(200) {
		t.Fatalf("status: got %v", entry["status"])
	}
	if entry["ts"] == nil {
		t.Fatal("an entry without a timestamp is not an audit record")
	}
}

// A suppressed write is still an attempt, and an audit trail that omits it
// would be misleading.
func TestAuditLogRecordsDryRunWrites(t *testing.T) {
	reset(t)
	hits := 0
	c := countingServer(t, &hits)
	AuditLog = filepath.Join(t.TempDir(), "audit.jsonl")
	DryRun = true

	if _, err := c.Post("/api/things", nil); err != nil {
		t.Fatalf("Post: %v", err)
	}
	contents, _ := os.ReadFile(AuditLog)
	if !strings.Contains(string(contents), `"dryRun":true`) {
		t.Fatalf("got %s", contents)
	}
}

// A trail with holes reads as complete, which is worse than no trail, so a
// write the log cannot record does not happen.
func TestAnUnwritableAuditLogFailsTheCommand(t *testing.T) {
	reset(t)
	hits := 0
	c := countingServer(t, &hits)
	AuditLog = filepath.Join(t.TempDir(), "missing-dir", "audit.jsonl")

	if _, err := c.Post("/api/things", nil); err == nil {
		t.Fatal("expected the command to fail")
	}
	if hits != 0 {
		t.Fatalf("nothing should have been sent, hits=%d", hits)
	}
}
