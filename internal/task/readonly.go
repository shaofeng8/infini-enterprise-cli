package task

import (
	"encoding/json"
	"net/url"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

// The read-only surface serves two different readers through one set of
// endpoints, and the difference matters.
//
// Public mode needs no credentials at all, and only works on a task its owner
// marked public with `task share`. Audit mode needs a proxy super-admin token
// and reaches any task regardless of sharing — and every audit read is logged
// server-side with the actor and the task.
//
// So audit is not "public with a login". It is a privileged compliance path,
// which is why it is a separate flag rather than something the CLI turns on
// automatically when a public read is refused.

// ReadonlyMode selects which of the two readers a request presents as.
type ReadonlyMode struct {
	// Audit requests the privileged path. Leaving it false is the public
	// read, which is what a shared link does.
	Audit bool
}

func (m ReadonlyMode) params() map[string]string {
	if !m.Audit {
		return map[string]string{}
	}
	return map[string]string{"audit": "1"}
}

func withTask(taskID string, mode ReadonlyMode) map[string]string {
	params := mode.params()
	params["taskId"] = taskID
	return params
}

// PublicTask reads a task's conversation without owning it.
func (a *API) PublicTask(taskID string, mode ReadonlyMode) (json.RawMessage, error) {
	if taskID == "" {
		return nil, cliexit.Usage("a task id is required")
	}
	return a.c.Get(BasePath+"/publicTask", withTask(taskID, mode))
}

// PublicMessage reads one message in full. The timestamp identifies it, and is
// the ts shown by a task listing's messages.
func (a *API) PublicMessage(taskID, messageTs string, mode ReadonlyMode) (json.RawMessage, error) {
	params := withTask(taskID, mode)
	params["messageTs"] = messageTs
	return a.c.Get(BasePath+"/publicMessagePayload", params)
}

// PublicToolEvidence resolves the tool calls a report cites.
//
// This is what makes a shared answer checkable: the markdown references
// evidence by message timestamp, and this turns those references back into the
// queries and results behind them. Audit travels in the body here rather than
// the query string, unlike every other endpoint in this group.
func (a *API) PublicToolEvidence(taskID string, evidenceIDs []string, includeSubagent bool, mode ReadonlyMode) (json.RawMessage, error) {
	if len(evidenceIDs) == 0 {
		return nil, cliexit.Usage("at least one evidence id is required")
	}
	if len(evidenceIDs) > 100 {
		return nil, cliexit.Usage("at most 100 evidence ids per request, received %d", len(evidenceIDs))
	}
	body := map[string]any{
		"taskId":      taskID,
		"evidenceIds": evidenceIDs,
	}
	if includeSubagent {
		body["includeSubagent"] = true
	}
	if mode.Audit {
		body["audit"] = true
	}
	return a.c.Post(BasePath+"/publicToolEvidence", body)
}

func (a *API) PublicFileTree(taskID string, mode ReadonlyMode) (json.RawMessage, error) {
	return a.c.Get(BasePath+"/publicTaskFileTree/"+url.PathEscape(taskID), mode.params())
}

func (a *API) PublicPreviewFile(taskID, fileName string, mode ReadonlyMode) (json.RawMessage, error) {
	path := client.WithQuery(BasePath+"/publicPreviewFile", mode.params())
	return a.c.Post(path, map[string]string{"taskId": taskID, "fileName": fileName})
}

func (a *API) PublicDownloadFile(taskID, filePath, dest string, mode ReadonlyMode) (*client.DownloadResult, error) {
	params := mode.params()
	params["path"] = filePath
	path := client.WithQuery(BasePath+"/publicDownloadTaskFile/"+url.PathEscape(taskID), params)
	return a.c.Download("GET", path, nil, dest, client.SanitizeFilename(baseName(filePath)))
}

func (a *API) PublicDownloadZip(taskID, dest string, mode ReadonlyMode) (*client.DownloadResult, error) {
	path := client.WithQuery(BasePath+"/publicDownloadZip", withTask(taskID, mode))
	return a.c.Download("GET", path, nil, dest, client.SanitizeFilename(taskID)+".zip")
}
