// Package dashboard wraps the /api/ai/dashboards REST surface.
//
// This package covers the deterministic half of dashboard work: reading,
// querying, refreshing, versioning and layout. Authoring a spec from a business
// request is the agent's job (dashboard_read + dashboard_submit), because the
// spec carries an Infini-SQL DAG and a filter contract that the server
// validates and hydrates.
package dashboard

import "encoding/json"

const BasePath = "/api/ai/dashboards"

// ListItem is one row of GET /api/ai/dashboards.
type ListItem struct {
	ID              string  `json:"id"`
	Title           string  `json:"title"`
	ProjectID       *string `json:"projectId"`
	CurrentRevision int     `json:"currentRevision"`
	UpdatedAt       string  `json:"updatedAt"`
	CreatedAt       string  `json:"createdAt"`
}

// Detail is GET /api/ai/dashboards/:id.
type Detail struct {
	ListItem
	Spec         json.RawMessage `json:"spec"`
	SpecHash     string          `json:"specHash"`
	UserID       string          `json:"userId"`
	IsPublic     bool            `json:"isPublic"`
	SourceTaskID *string         `json:"sourceTaskId"`
	CanWrite     bool            `json:"canWrite"`
}

// Revision is one entry of the version history.
type Revision struct {
	Revision     int     `json:"revision"`
	ChangeBrief  *string `json:"change_brief"`
	CreatedBy    *string `json:"created_by"`
	SourceTaskID *string `json:"source_task_id"`
	CreatedAt    string  `json:"createdAt"`
}

// QueryResult is one entry of POST /api/ai/dashboards/:id/query. A failed query
// is reported inside the result rather than as a transport error, so a partially
// broken dashboard still returns the widgets that do work.
type QueryResult struct {
	Status       string          `json:"status"` // ok | error | timeout
	Columns      []string        `json:"columns,omitempty"`
	Fields       []QueryField    `json:"fields,omitempty"`
	Datas        [][]any         `json:"datas,omitempty"`
	RowCount     *int            `json:"row_count,omitempty"`
	Truncated    *bool           `json:"truncated,omitempty"`
	FromCache    *bool           `json:"from_cache,omitempty"`
	ExecutedAt   string          `json:"executed_at,omitempty"`
	ErrorCode    string          `json:"error_code,omitempty"`
	ErrorMessage string          `json:"error_message,omitempty"`
	PageIndex    *int            `json:"page_index,omitempty"`
	PageSize     *int            `json:"page_size,omitempty"`
	HasMore      *bool           `json:"has_more,omitempty"`
	Raw          json.RawMessage `json:"-"`
}

type QueryField struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

// RefreshJob is the async refresh task created by POST .../refreshes.
type RefreshJob struct {
	ID               string          `json:"id"`
	DashboardID      string          `json:"dashboard_id"`
	Status           string          `json:"status"`
	SpecRevision     int             `json:"spec_revision"`
	QueryIDs         []string        `json:"query_ids"`
	FilterValues     json.RawMessage `json:"filter_values,omitempty"`
	ForceRefresh     bool            `json:"force_refresh"`
	PreferSnapshot   bool            `json:"prefer_snapshot"`
	TotalQueries     int             `json:"total_queries"`
	CompletedQueries int             `json:"completed_queries"`
	SucceededQueries int             `json:"succeeded_queries"`
	FailedQueries    int             `json:"failed_queries"`
	Error            string          `json:"error,omitempty"`
	Queries          []RefreshQuery  `json:"queries"`
	CreatedAt        string          `json:"created_at"`
	UpdatedAt        string          `json:"updated_at"`
	StartedAt        string          `json:"started_at,omitempty"`
	CompletedAt      string          `json:"completed_at,omitempty"`
}

type RefreshQuery struct {
	QueryID      string `json:"query_id"`
	Status       string `json:"status"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	DurationMs   *int   `json:"duration_ms,omitempty"`
}

// Terminal reports whether a refresh job has stopped moving.
func (j *RefreshJob) Terminal() bool {
	switch j.Status {
	case "completed", "completed_with_errors", "failed", "canceled":
		return true
	default:
		return false
	}
}

type FilterOption struct {
	Value string `json:"value"`
	Label string `json:"label,omitempty"`
}

type FilterRange struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// AskContext is the attachment payload the dashboard hands to a chat task.
type AskContext struct {
	ContextMarkdown string   `json:"context_markdown"`
	DatabaseIDs     []string `json:"database_ids"`
	ProjectIDs      []string `json:"project_ids"`
	DashboardID     string   `json:"dashboard_id"`
}

// WidgetLayout is one entry of PATCH .../layout.
type WidgetLayout struct {
	WidgetID string `json:"widget_id"`
	X        int    `json:"x"`
	Y        int    `json:"y"`
	W        int    `json:"w"`
	H        int    `json:"h"`
}
