// Package ops covers the operational plane: scheduled runs, the SQL engine,
// the runtime fleet and the license.
//
// These are the parts an operator touches rather than an analyst, and they
// answer different questions from the rest of the CLI: not "what does the data
// say" but "is this deployment healthy, and what is it allowed to do".
package ops

import (
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

// The controller is mounted at ai_scheduler, not ai_schedule.
const scheduleBase = "/api/ai_scheduler"

// ScheduleStatuses filter the listing.
var ScheduleStatuses = []string{"enabled", "paused", "all"}

// RunStatuses a scheduled run can end in.
var RunStatuses = []string{"queued", "running", "completed", "failed", "skipped", "cancelled"}

type API struct {
	c *client.Client
}

func NewAPI(c *client.Client) *API { return &API{c: c} }

func decode[T any](raw json.RawMessage, err error, what string) (T, error) {
	var value T
	if err != nil {
		return value, err
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, cliexit.New(cliexit.CodeBusiness, "cannot parse %s: %v", what, err)
	}
	return value, nil
}

func fetch[T any](a *API, path string, params map[string]string, what string) (T, error) {
	raw, err := a.c.Get(path, params)
	return decode[T](raw, err, what)
}

func send[T any](a *API, path string, body any, what string) (T, error) {
	raw, err := a.c.Post(path, body)
	return decode[T](raw, err, what)
}

// TaskConfig is the snapshot a scheduled run executes with. It is frozen at
// save time rather than resolved at run time, so a schedule keeps working the
// way it was set up even after the account's defaults change.
type TaskConfig struct {
	APIProvider  string        `json:"apiProvider,omitempty"`
	APIModelID   string        `json:"apiModelId,omitempty"`
	EngineID     string        `json:"engineId,omitempty"`
	DatabaseIDs  []string      `json:"databaseIds,omitempty"`
	RagIDs       []string      `json:"ragIds,omitempty"`
	ProjectIDs   []string      `json:"projectIds,omitempty"`
	ToolParams   []ToolParam   `json:"toolParams,omitempty"`
	Capabilities *Capabilities `json:"capabilities,omitempty"`
}

type ToolParam struct {
	ToolID string `json:"tool_id"`
	YAML   string `json:"yaml"`
}

// Capabilities are merged into the user's auto-approval defaults for the run,
// which is how a scheduled run gets a narrower envelope than an interactive one.
type Capabilities struct {
	EnableShell     *bool `json:"enableShell,omitempty"`
	EnableBrowser   *bool `json:"enableBrowser,omitempty"`
	EnableWebSearch *bool `json:"enableWebSearch,omitempty"`
	EnableSubAgent  *bool `json:"enableSubAgent,omitempty"`
}

// Schedule is a recurring agent run.
type Schedule struct {
	ScheduleID     string         `json:"scheduleId"`
	UserID         string         `json:"userId"`
	Title          string         `json:"title"`
	Prompt         string         `json:"prompt"`
	CronExpression string         `json:"cronExpression"`
	StartAt        string         `json:"startAt"`
	EndAt          *string        `json:"endAt"`
	Timezone       string         `json:"timezone"`
	Enabled        bool           `json:"enabled"`
	ArchivedAt     *string        `json:"archivedAt"`
	NextRunAt      *string        `json:"nextRunAt"`
	LastRunAt      *string        `json:"lastRunAt"`
	ActiveRunID    *string        `json:"activeRunId"`
	Version        int            `json:"version"`
	TaskConfig     map[string]any `json:"taskConfig,omitempty"`
	CreatedAt      string         `json:"createdAt"`
	UpdatedAt      string         `json:"updatedAt"`
}

// Run is one execution of a schedule. TaskID links it to the conversation the
// run produced, which is how `task show` picks up from here.
type Run struct {
	RunID          string  `json:"runId"`
	ScheduleID     string  `json:"scheduleId"`
	UserID         string  `json:"userId"`
	ScheduledFor   string  `json:"scheduledFor"`
	TaskID         *string `json:"taskId"`
	CommandID      *string `json:"commandId"`
	Status         string  `json:"status"`
	DeliveryStatus string  `json:"deliveryStatus"`
	QueuedAt       *string `json:"queuedAt"`
	StartedAt      *string `json:"startedAt"`
	FinishedAt     *string `json:"finishedAt"`
	SkipReason     *string `json:"skipReason"`
	ErrorMessage   *string `json:"errorMessage"`
	// IsMisfire marks a run the scheduler noticed late, for example after a
	// restart, rather than one that fired on time.
	IsMisfire bool   `json:"isMisfire"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// SchedulePage is the {items, total, page, pageSize} envelope the scheduler
// uses, which is neither the Pagination nor the ListPage shape elsewhere.
type SchedulePage[T any] struct {
	Items    []T `json:"items"`
	Total    int `json:"total"`
	Page     int `json:"page"`
	PageSize int `json:"pageSize"`
}

type ScheduleQuery struct {
	Page     int
	PageSize int
	Keyword  string
	Status   string
}

func (q ScheduleQuery) params() map[string]string {
	params := map[string]string{}
	if q.Page > 0 {
		params["page"] = strconv.Itoa(q.Page)
	}
	if q.PageSize > 0 {
		params["pageSize"] = strconv.Itoa(q.PageSize)
	}
	if q.Keyword != "" {
		params["keyword"] = q.Keyword
	}
	if q.Status != "" {
		params["status"] = q.Status
	}
	return params
}

func (a *API) Schedules(query ScheduleQuery) (*SchedulePage[Schedule], error) {
	page, err := fetch[SchedulePage[Schedule]](a, scheduleBase, query.params(), "schedule list")
	if err != nil {
		return nil, err
	}
	return &page, nil
}

func (a *API) Schedule(id string) (*Schedule, error) {
	schedule, err := fetch[Schedule](a, scheduleBase+"/"+url.PathEscape(id), nil, "schedule")
	if err != nil {
		return nil, err
	}
	return &schedule, nil
}

func (a *API) Runs(id string, page, pageSize int) (*SchedulePage[Run], error) {
	params := map[string]string{}
	if page > 0 {
		params["page"] = strconv.Itoa(page)
	}
	if pageSize > 0 {
		params["pageSize"] = strconv.Itoa(pageSize)
	}
	result, err := fetch[SchedulePage[Run]](a, scheduleBase+"/"+url.PathEscape(id)+"/runs", params, "run list")
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (a *API) CreateSchedule(body map[string]any) (*Schedule, error) {
	schedule, err := send[Schedule](a, scheduleBase, body, "schedule")
	if err != nil {
		return nil, err
	}
	return &schedule, nil
}

// UpdateSchedule replaces the schedule; the save DTO is the same for create
// and update, so title, prompt, cron and startAt are always required.
func (a *API) UpdateSchedule(id string, body map[string]any) (*Schedule, error) {
	raw, err := a.c.Put(scheduleBase+"/"+url.PathEscape(id), body)
	schedule, err := decode[Schedule](raw, err, "schedule")
	if err != nil {
		return nil, err
	}
	return &schedule, nil
}

// SetScheduleEnabled pauses or resumes. Resuming fails when the cron has no
// future occurrence left, which is the server refusing to enable a schedule
// that could never fire.
func (a *API) SetScheduleEnabled(id string, enabled bool) (*Schedule, error) {
	action := "pause"
	if enabled {
		action = "resume"
	}
	raw, err := a.c.Patch(scheduleBase+"/"+url.PathEscape(id)+"/"+action, nil)
	schedule, err := decode[Schedule](raw, err, "schedule")
	if err != nil {
		return nil, err
	}
	return &schedule, nil
}

// RunSchedule queues one run immediately, without disturbing the cron.
func (a *API) RunSchedule(id string) (*Run, error) {
	run, err := send[Run](a, scheduleBase+"/"+url.PathEscape(id)+"/run", nil, "run")
	if err != nil {
		return nil, err
	}
	return &run, nil
}

// ArchiveSchedule retires a schedule. It is archived rather than deleted, so
// its run history stays auditable.
func (a *API) ArchiveSchedule(id string) (json.RawMessage, error) {
	return a.c.Delete(scheduleBase+"/"+url.PathEscape(id), nil)
}
