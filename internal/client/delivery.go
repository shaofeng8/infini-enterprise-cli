package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

// DryRun stops writes from leaving the machine.
//
// Reads still go out, because a dry run that cannot look anything up is
// useless: half of what the CLI does before a write is read the current state
// so the write can be a patch. What is suppressed is exactly the set of
// requests that would change something.
var DryRun bool

// AuditLog is a path to append one JSON line per request to. Private
// deployments need to answer "who changed what, when" about the CLI as well
// as about the UI, and the server's own log cannot see which operator ran it.
var AuditLog string

var auditMu sync.Mutex

// auditPreflight fails before a write goes out if the trail cannot be
// written.
//
// The order matters. Recording after the request would mean an unwritable log
// silently drops entries for requests that already happened, and a trail with
// holes in it is worse than no trail because it reads as complete. So a write
// the log cannot record does not happen at all.
func auditPreflight() error {
	if AuditLog == "" {
		return nil
	}
	auditMu.Lock()
	defer auditMu.Unlock()
	file, err := os.OpenFile(AuditLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return cliexit.Usage("cannot write the audit log %q: %v", AuditLog, err)
	}
	return file.Close()
}

// isWrite reports whether a method would change server state. GET and HEAD
// are the only ones that would not.
func isWrite(method string) bool {
	return method != http.MethodGet && method != http.MethodHead
}

// dryRunResponse is what a suppressed write returns in place of a server
// answer: the request itself, so a caller can see what would have been sent.
func dryRunResponse(method, path string, body any) (json.RawMessage, error) {
	described := map[string]any{
		"dryRun": true,
		"method": method,
		"path":   path,
	}
	if body != nil {
		if raw, ok := body.(json.RawMessage); ok {
			described["body"] = json.RawMessage(redact(raw))
		} else if encoded, err := json.Marshal(body); err == nil {
			described["body"] = json.RawMessage(redact(encoded))
		}
	}
	encoded, err := json.Marshal(described)
	if err != nil {
		return nil, cliexit.New(cliexit.CodeUsage, "cannot describe the request: %v", err)
	}
	fmt.Fprintf(os.Stderr, "dry run: %s %s (not sent)\n", method, path)
	return encoded, nil
}

// auditRecord appends one line describing a request and its outcome.
//
// Failing to write the audit trail fails the command. A trail with holes in
// it is worse than no trail, because it reads as complete.
func auditRecord(method, path string, status int, requestErr error) error {
	if AuditLog == "" {
		return nil
	}

	entry := map[string]any{
		"ts":     time.Now().UTC().Format(time.RFC3339Nano),
		"method": method,
		"path":   path,
	}
	if status > 0 {
		entry["status"] = status
	}
	if requestErr != nil {
		entry["error"] = requestErr.Error()
	}
	if DryRun {
		entry["dryRun"] = true
	}

	line, err := json.Marshal(entry)
	if err != nil {
		return cliexit.New(cliexit.CodeUsage, "cannot serialize the audit entry: %v", err)
	}

	auditMu.Lock()
	defer auditMu.Unlock()
	file, err := os.OpenFile(AuditLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return cliexit.Usage("cannot write the audit log %q: %v", AuditLog, err)
	}
	defer file.Close()
	if _, err := file.Write(append(line, '\n')); err != nil {
		return cliexit.Usage("cannot write the audit log %q: %v", AuditLog, err)
	}
	return nil
}
