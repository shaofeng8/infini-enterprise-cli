package hub

import (
	"encoding/json"
	"net/url"
	"slices"
)

// BatchModes decide how much of a data source a batch build regenerates.
var BatchModes = []string{"missing_only", "full_regenerate"}

// JobStatuses a memory build can report.
var JobStatuses = []string{"pending", "running", "succeeded", "failed", "cancelled"}

type MemoryColumn struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

type MemoryTable struct {
	TableName string         `json:"tableName"`
	Columns   []MemoryColumn `json:"columns"`
}

// StartRequest mirrors ContextHubMemoryBuildStartDto. TaskID names the
// standalone workspace the build runs in, so the CLI mints one per invocation.
type StartRequest struct {
	TaskID       string        `json:"taskId"`
	DatabaseID   string        `json:"databaseId"`
	DatabaseName string        `json:"databaseName"`
	DatabaseType string        `json:"databaseType,omitempty"`
	Tables       []MemoryTable `json:"tables"`
	// SkipExistingTableUpdates leaves already-described tables alone instead of
	// proposing updates to them.
	SkipExistingTableUpdates bool `json:"skipExistingTableUpdates,omitempty"`
}

type BuildStep struct {
	Key    string `json:"key"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type Job struct {
	JobID        string      `json:"jobId"`
	TaskID       string      `json:"taskId,omitempty"`
	DatabaseID   string      `json:"databaseId"`
	DatabaseName string      `json:"databaseName,omitempty"`
	DatabaseType string      `json:"databaseType,omitempty"`
	Status       string      `json:"status"`
	Progress     int         `json:"progress"`
	ActiveStep   string      `json:"activeStep,omitempty"`
	Steps        []BuildStep `json:"steps,omitempty"`
	Error        string      `json:"error,omitempty"`
}

// Terminal reports whether the job has stopped moving.
func (j *Job) Terminal() bool {
	return slices.Contains([]string{"succeeded", "failed", "cancelled"}, j.Status)
}

type BatchItem struct {
	DatabaseID   string `json:"databaseId"`
	DatabaseName string `json:"databaseName,omitempty"`
	Status       string `json:"status"`
	TableCount   int    `json:"tableCount"`
	ColumnCount  int    `json:"columnCount"`
	Job          *Job   `json:"job,omitempty"`
}

type BatchResult struct {
	Items   []BatchItem `json:"items"`
	Summary struct {
		Total     int `json:"total"`
		Started   int `json:"started"`
		Running   int `json:"running"`
		Skipped   int `json:"skipped"`
		Failed    int `json:"failed"`
		Cancelled int `json:"cancelled"`
	} `json:"summary"`
}

// StartMemoryBuild queues a build for one data source.
//
// The server returns the already-running job when one exists for the same data
// source, so calling this twice does not start two builds.
func (a *API) StartMemoryBuild(request StartRequest) (*Job, error) {
	raw, err := a.c.Post(BasePath+"/memory-build/start", request)
	job, err := decode[Job](raw, err, "memory build")
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// StartMemoryBuildBatch queues one build per data source, deriving the table
// and column selection from each schema on the server side.
func (a *API) StartMemoryBuildBatch(databaseIDs []string, mode string) (*BatchResult, error) {
	body := map[string]any{"databaseIds": databaseIDs}
	if mode != "" {
		body["mode"] = mode
	}
	raw, err := a.c.Post(BasePath+"/memory-build/batch-start", body)
	result, err := decode[BatchResult](raw, err, "batch memory build")
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (a *API) ActiveMemoryBuilds() ([]Job, error) {
	raw, err := a.c.Get(BasePath+"/memory-build/active", nil)
	return decode[[]Job](raw, err, "active memory builds")
}

func (a *API) CancelActiveMemoryBuilds() (json.RawMessage, error) {
	return a.c.Post(BasePath+"/memory-build/cancel-active", nil)
}

func (a *API) CancelMemoryBuild(jobID string) (*Job, error) {
	raw, err := a.c.Post(BasePath+"/memory-build/cancel/"+url.PathEscape(jobID), nil)
	job, err := decode[Job](raw, err, "memory build")
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// MemoryBuildStatus returns nil when no such job exists for this user.
func (a *API) MemoryBuildStatus(jobID string) (*Job, error) {
	raw, err := a.c.Get(BasePath+"/memory-build/status/"+url.PathEscape(jobID), nil)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	job, err := decode[Job](raw, nil, "memory build")
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// LatestMemoryBuild returns the most recent restorable job for a data source,
// which is how an interrupted review is picked back up.
func (a *API) LatestMemoryBuild(databaseID string) (*Job, error) {
	raw, err := a.c.Get(BasePath+"/memory-build/latest/"+url.PathEscape(databaseID), nil)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	job, err := decode[Job](raw, nil, "memory build")
	if err != nil {
		return nil, err
	}
	return &job, nil
}
