// Package task wraps the /api/ai_task REST surface: task lifecycle, the
// notebook DAG behind a task, its workspace files, and public sharing.
package task

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

const (
	BasePath = "/api/ai_task"
	// Task files live under the tools router, not under ai_task.
	ToolsPath   = "/api/tools"
	StoragePath = "/api/tools/storage"
)

// Statuses a task can be in; `waiting` means it is blocked on user input.
var Statuses = []string{"pending", "running", "waiting", "completed", "error", "cancelled"}

type ListItem struct {
	ID         string `json:"id"`
	UserID     string `json:"user_id,omitempty"`
	OwnerName  string `json:"owner_username,omitempty"`
	TaskName   string `json:"task_name"`
	ProjectIDs string `json:"project_ids,omitempty"`
	Status     string `json:"task_status,omitempty"`
	IsPinned   bool   `json:"is_pinned,omitempty"`
	CreatedAt  string `json:"createdAt,omitempty"`
	UpdatedAt  string `json:"updatedAt,omitempty"`
}

type PagerMeta struct {
	ItemCount    int `json:"itemCount"`
	TotalItems   int `json:"totalItems"`
	ItemsPerPage int `json:"itemsPerPage"`
	TotalPages   int `json:"totalPages"`
	CurrentPage  int `json:"currentPage"`
}

type ListResponse struct {
	Items []ListItem `json:"items"`
	Meta  PagerMeta  `json:"meta"`
}

type StatusItem struct {
	ID     string `json:"id"`
	Status string `json:"task_status"`
}

type FileNode struct {
	Key       string     `json:"key"`
	Title     string     `json:"title"`
	IsDir     bool       `json:"isDir"`
	Size      string     `json:"size,omitempty"`
	SizeBytes int64      `json:"sizeBytes,omitempty"`
	ExtName   string     `json:"extName,omitempty"`
	CreatedAt string     `json:"createdAt,omitempty"`
	Children  []FileNode `json:"children,omitempty"`
}

type FileTree struct {
	Items []FileNode `json:"items"`
	// Truncated is set when the directory was too large or the walk timed out,
	// so an empty-looking tree is distinguishable from an incomplete one.
	Truncated bool `json:"truncated"`
}

// Flatten walks the tree depth-first so listings can be printed as a table.
func (t *FileTree) Flatten(filesOnly bool) []FileNode {
	var out []FileNode
	var walk func(nodes []FileNode)
	walk = func(nodes []FileNode) {
		for _, node := range nodes {
			if !filesOnly || !node.IsDir {
				out = append(out, node)
			}
			walk(node.Children)
		}
	}
	walk(t.Items)
	return out
}

type ShareStatus struct {
	IsPublic bool   `json:"isPublic"`
	URL      string `json:"url,omitempty"`
}

// ListQuery mirrors AiTaskQueryDto.
type ListQuery struct {
	Page          int
	PageSize      int
	Field         string
	Order         string
	Name          string
	Keyword       string
	Status        string
	TaskID        string
	OwnerUserID   string
	ProjectFilter string
	ProjectID     string
	ProjectIDs    []string
	CreatedFrom   string
	CreatedTo     string
	PinnedOnly    bool
	IncludeSubs   bool
	Audit         bool
}

func (q ListQuery) params() map[string]string {
	params := map[string]string{
		"task_name":      q.Name,
		"keyword":        q.Keyword,
		"task_status":    q.Status,
		"task_id":        q.TaskID,
		"owner_user_id":  q.OwnerUserID,
		"project_filter": q.ProjectFilter,
		"project_id":     q.ProjectID,
		"created_from":   q.CreatedFrom,
		"created_to":     q.CreatedTo,
		"field":          q.Field,
		"order":          q.Order,
	}
	if q.Page > 0 {
		params["page"] = strconv.Itoa(q.Page)
	}
	if q.PageSize > 0 {
		params["pageSize"] = strconv.Itoa(q.PageSize)
	}
	if len(q.ProjectIDs) > 0 {
		params["project_ids"] = joinCSV(q.ProjectIDs)
	}
	if q.PinnedOnly {
		params["pinned_only"] = "true"
	}
	if q.IncludeSubs {
		params["include_sub_tasks"] = "true"
	}
	if q.Audit {
		// The server reads audit as a truthy query value, matching the web client.
		params["audit"] = "1"
	}
	return params
}

type API struct {
	c *client.Client
}

func NewAPI(c *client.Client) *API { return &API{c: c} }

func (a *API) Client() *client.Client { return a.c }

func decode[T any](raw json.RawMessage, err error, what string) (T, error) {
	var value T
	if err != nil {
		return value, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return value, nil
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, cliexit.New(cliexit.CodeBusiness, "cannot parse %s response: %v", what, err)
	}
	return value, nil
}

func (a *API) List(query ListQuery) (*ListResponse, error) {
	raw, err := a.c.Get(BasePath+"/list", query.params())
	result, err := decode[ListResponse](raw, err, "task list")
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (a *API) Statuses(taskIDs []string) ([]StatusItem, error) {
	raw, err := a.c.Post(BasePath+"/statuses", map[string]any{"task_ids": taskIDs})
	result, err := decode[struct {
		Items []StatusItem `json:"items"`
	}](raw, err, "task statuses")
	if err != nil {
		return nil, err
	}
	return result.Items, nil
}

// Show returns the task with its conversation.
func (a *API) Show(id string) (json.RawMessage, error) {
	return a.c.Get(BasePath+"/showTaskWithId/"+url.PathEscape(id), nil)
}

func (a *API) Info(id string) (json.RawMessage, error) {
	return a.c.Get(BasePath+"/getTaskInfo/"+url.PathEscape(id), nil)
}

// Data returns the task payload used to hydrate the chat view.
func (a *API) Data(id string) (json.RawMessage, error) {
	return a.c.Get(BasePath+"/tasks", map[string]string{"taskId": id})
}

func (a *API) Delete(ids []string) (json.RawMessage, error) {
	return a.c.Post(BasePath+"/deleteTaskWithId", map[string]any{"ids": ids})
}

// Cancel takes the task id as a query parameter even though it is a POST.
func (a *API) Cancel(id string) (json.RawMessage, error) {
	return a.c.Post(client.WithQuery(BasePath+"/cancelTask", map[string]string{"taskId": id}), nil)
}

func (a *API) SetPinned(id string, pinned bool) (json.RawMessage, error) {
	return a.c.Post(BasePath+"/setPinned", map[string]any{"taskId": id, "isPinned": pinned})
}

// NotebookGraph returns the DAG of notebook cells a task produced.
func (a *API) NotebookGraph(id string) (json.RawMessage, error) {
	return a.c.Get(fmt.Sprintf("%s/tasks/%s/notebook-graph", BasePath, url.PathEscape(id)), nil)
}

// NativeQuerySQL expands infini_ref references in a view into the SQL the data
// source actually receives.
func (a *API) NativeQuerySQL(taskID, viewName string) (json.RawMessage, error) {
	return a.c.Post(BasePath+"/native-query-sql", map[string]string{
		"task_id":   taskID,
		"view_name": viewName,
	})
}

// KpiSQLRequest mirrors AiRunKpiSqlDto, whose databases and tables fields are
// JSON encoded into strings rather than sent as arrays.
type KpiSQLRequest struct {
	Databases []string
	Tables    []KpiTable
	SQL       string
	SetValues string
}

type KpiTable struct {
	Database string `json:"database"`
	Table    string `json:"table"`
}

func (a *API) RunKpiSQL(request KpiSQLRequest) (json.RawMessage, error) {
	databases, err := json.Marshal(request.Databases)
	if err != nil {
		return nil, cliexit.New(cliexit.CodeUsage, "cannot encode databases: %v", err)
	}
	tables, err := json.Marshal(request.Tables)
	if err != nil {
		return nil, cliexit.New(cliexit.CodeUsage, "cannot encode tables: %v", err)
	}

	body := map[string]string{
		"databases": string(databases),
		"tables":    string(tables),
		"sql":       request.SQL,
	}
	if request.SetValues != "" {
		body["setValues"] = request.SetValues
	}
	return a.c.Post(BasePath+"/runKpiSql", body)
}

func (a *API) SetShare(id string, public bool) (json.RawMessage, error) {
	return a.c.Post(BasePath+"/setShare", map[string]any{"taskId": id, "isPublic": public})
}

func (a *API) ShareStatus(id string) (*ShareStatus, error) {
	raw, err := a.c.Get(BasePath+"/shareStatus", map[string]string{"taskId": id})
	status, err := decode[ShareStatus](raw, err, "share status")
	if err != nil {
		return nil, err
	}
	return &status, nil
}

// ToolEvidence returns the recorded inputs and outputs of tool calls, which is
// how a result gets audited without replaying the task.
func (a *API) ToolEvidence(id string, evidenceIDs []string, includeSubagent bool) (json.RawMessage, error) {
	body := map[string]any{"taskId": id, "evidenceIds": evidenceIDs}
	if includeSubagent {
		body["includeSubagent"] = true
	}
	return a.c.Post(BasePath+"/toolEvidence", body)
}

func (a *API) UIMessage(messageID string) (json.RawMessage, error) {
	return a.c.Get(BasePath+"/getUiMessageById", map[string]string{"id": messageID})
}

// MessagePayload returns the full, untruncated content of one message.
func (a *API) MessagePayload(taskID, messageTS string) (json.RawMessage, error) {
	return a.c.Get(BasePath+"/messagePayload", map[string]string{
		"taskId":    taskID,
		"messageTs": messageTS,
	})
}

func (a *API) Workspace(id string) (json.RawMessage, error) {
	return a.c.Get(BasePath+"/getTaskWorkspace/"+url.PathEscape(id), nil)
}

// FileTree lists the task's working directory.
func (a *API) FileTree(id string) (*FileTree, error) {
	raw, err := a.c.Get(ToolsPath+"/taskFileTree/"+url.PathEscape(id), nil)
	tree, err := decode[FileTree](raw, err, "task file tree")
	if err != nil {
		return nil, err
	}
	return &tree, nil
}

func (a *API) PreviewFile(taskID, fileName string) (json.RawMessage, error) {
	return a.c.Post(BasePath+"/previewFile", map[string]string{
		"taskId":   taskID,
		"fileName": fileName,
	})
}

func (a *API) DownloadFile(taskID, filePath, dest string) (*client.DownloadResult, error) {
	path := client.WithQuery(StoragePath+"/downloadTaskFile/"+url.PathEscape(taskID),
		map[string]string{"path": filePath})
	return a.c.Download("GET", path, nil, dest, client.SanitizeFilename(baseName(filePath)))
}

func (a *API) DownloadZip(taskID, dest string) (*client.DownloadResult, error) {
	path := client.WithQuery(BasePath+"/downloadZip", map[string]string{"taskId": taskID})
	return a.c.Download("GET", path, nil, dest, client.SanitizeFilename(taskID)+".zip")
}

func joinCSV(values []string) string {
	out := ""
	for i, value := range values {
		if i > 0 {
			out += ","
		}
		out += value
	}
	return out
}

func baseName(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[i+1:]
		}
	}
	return path
}
