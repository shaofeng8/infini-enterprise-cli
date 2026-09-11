package dashboard

import (
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

// API binds the dashboard endpoints to a configured HTTP client.
type API struct {
	c *client.Client
}

func NewAPI(c *client.Client) *API { return &API{c: c} }

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

func (a *API) List(projectID string) ([]ListItem, error) {
	raw, err := a.c.Get(BasePath, map[string]string{"project_id": projectID})
	return decode[[]ListItem](raw, err, "dashboard list")
}

func (a *API) Get(id string) (*Detail, error) {
	raw, err := a.c.Get(BasePath+"/"+url.PathEscape(id), nil)
	detail, err := decode[Detail](raw, err, "dashboard detail")
	if err != nil {
		return nil, err
	}
	return &detail, nil
}

// GetWithSpec returns the detail plus its parsed spec, which most commands need
// together (filter types, query ids, widget layout).
func (a *API) GetWithSpec(id string) (*Detail, *Spec, error) {
	detail, err := a.Get(id)
	if err != nil {
		return nil, nil, err
	}
	spec, err := ParseSpec(detail.Spec)
	if err != nil {
		return nil, nil, err
	}
	return detail, spec, nil
}

func (a *API) Create(spec json.RawMessage, projectID, sourceTaskID string) (json.RawMessage, error) {
	body := map[string]any{"spec": spec}
	if projectID != "" {
		body["project_id"] = projectID
	}
	if sourceTaskID != "" {
		body["source_task_id"] = sourceTaskID
	}
	return a.c.Post(BasePath, body)
}

func (a *API) Update(id string, spec json.RawMessage, changeBrief, sourceTaskID string) (json.RawMessage, error) {
	body := map[string]any{"spec": spec}
	if changeBrief != "" {
		body["change_brief"] = changeBrief
	}
	if sourceTaskID != "" {
		body["source_task_id"] = sourceTaskID
	}
	return a.c.Put(BasePath+"/"+url.PathEscape(id), body)
}

func (a *API) Remove(id string) (json.RawMessage, error) {
	return a.c.Delete(BasePath+"/"+url.PathEscape(id), nil)
}

func (a *API) RemoveWidget(id, widgetID string) (json.RawMessage, error) {
	return a.c.Delete(fmt.Sprintf("%s/%s/widgets/%s", BasePath, url.PathEscape(id), url.PathEscape(widgetID)), nil)
}

func (a *API) ListRevisions(id string) ([]Revision, error) {
	raw, err := a.c.Get(BasePath+"/"+url.PathEscape(id)+"/revisions", nil)
	return decode[[]Revision](raw, err, "revision list")
}

func (a *API) GetRevision(id string, revision int) (json.RawMessage, error) {
	return a.c.Get(fmt.Sprintf("%s/%s/revisions/%d", BasePath, url.PathEscape(id), revision), nil)
}

func (a *API) Rollback(id string, revision int) (json.RawMessage, error) {
	return a.c.Post(BasePath+"/"+url.PathEscape(id)+"/rollback", map[string]int{"revision": revision})
}

func (a *API) UpdateLayout(id string, layouts []WidgetLayout) (json.RawMessage, error) {
	return a.c.Patch(BasePath+"/"+url.PathEscape(id)+"/layout", map[string]any{"layouts": layouts})
}

func (a *API) CommitLayoutRevision(id string) (json.RawMessage, error) {
	return a.c.Post(BasePath+"/"+url.PathEscape(id)+"/layout/revision", nil)
}

// QueryOptions controls cache behaviour shared by query and table-query.
type QueryOptions struct {
	ForceRefresh   bool
	PreferSnapshot bool
	SnapshotOnly   bool
}

func (o QueryOptions) apply(body map[string]any) {
	if o.ForceRefresh {
		body["force_refresh"] = true
	}
	if o.PreferSnapshot {
		body["prefer_snapshot"] = true
	}
	if o.SnapshotOnly {
		body["snapshot_only"] = true
	}
}

func (a *API) Query(id string, queryIDs []string, filters FilterValues, opts QueryOptions) (map[string]QueryResult, error) {
	body := map[string]any{
		"query_ids":     queryIDs,
		"filter_values": orEmpty(filters),
	}
	opts.apply(body)
	raw, err := a.c.Post(BasePath+"/"+url.PathEscape(id)+"/query", body)
	return decode[map[string]QueryResult](raw, err, "dashboard query")
}

// TableQuery pushes paging, search, sorting and column filters down to the data
// source for a single table widget.
type TableQueryRequest struct {
	WidgetID      string           `json:"widget_id"`
	FilterValues  map[string]any   `json:"filter_values"`
	PageIndex     int              `json:"page_index"`
	PageSize      int              `json:"page_size"`
	Search        *TableSearch     `json:"search,omitempty"`
	TimeRange     *TableTimeRange  `json:"time_range,omitempty"`
	ColumnFilters []map[string]any `json:"column_filters,omitempty"`
	Sort          *TableSort       `json:"sort,omitempty"`
	ForceRefresh  bool             `json:"force_refresh,omitempty"`
	PreferSnap    bool             `json:"prefer_snapshot,omitempty"`
	SnapshotOnly  bool             `json:"snapshot_only,omitempty"`
}

type TableSearch struct {
	Columns []string `json:"columns"`
	Value   string   `json:"value"`
}

type TableTimeRange struct {
	Columns []string `json:"columns"`
	Start   string   `json:"start"`
	End     string   `json:"end"`
}

type TableSort struct {
	Column    string `json:"column"`
	Direction string `json:"direction"`
}

func (a *API) TableQuery(id string, req TableQueryRequest) (*QueryResult, error) {
	if req.FilterValues == nil {
		req.FilterValues = map[string]any{}
	}
	raw, err := a.c.Post(BasePath+"/"+url.PathEscape(id)+"/table-query", req)
	result, err := decode[QueryResult](raw, err, "dashboard table query")
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (a *API) FilterOptions(id, filterName string) ([]FilterOption, error) {
	raw, err := a.c.Post(BasePath+"/"+url.PathEscape(id)+"/filter-options",
		map[string]string{"filter_name": filterName})
	return decode[[]FilterOption](raw, err, "filter options")
}

func (a *API) FilterRange(id, filterName string) (*FilterRange, error) {
	raw, err := a.c.Post(BasePath+"/"+url.PathEscape(id)+"/filter-range",
		map[string]string{"filter_name": filterName})
	result, err := decode[FilterRange](raw, err, "filter range")
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (a *API) CreateRefresh(id string, queryIDs []string, filters FilterValues, opts QueryOptions) (*RefreshJob, error) {
	body := map[string]any{
		"query_ids":     queryIDs,
		"filter_values": orEmpty(filters),
	}
	opts.apply(body)
	raw, err := a.c.Post(BasePath+"/"+url.PathEscape(id)+"/refreshes", body)
	job, err := decode[RefreshJob](raw, err, "refresh job")
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (a *API) GetRefresh(id, refreshID string) (*RefreshJob, error) {
	raw, err := a.c.Get(fmt.Sprintf("%s/%s/refreshes/%s", BasePath, url.PathEscape(id), url.PathEscape(refreshID)), nil)
	job, err := decode[RefreshJob](raw, err, "refresh job")
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// ActiveRefresh returns nil when the user has no refresh in flight.
func (a *API) ActiveRefresh(id string) (*RefreshJob, error) {
	raw, err := a.c.Get(BasePath+"/"+url.PathEscape(id)+"/refreshes/active", nil)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	job, err := decode[RefreshJob](raw, nil, "refresh job")
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (a *API) RefreshResult(id, refreshID, queryID string) (*QueryResult, error) {
	raw, err := a.c.Get(fmt.Sprintf("%s/%s/refreshes/%s/results/%s",
		BasePath, url.PathEscape(id), url.PathEscape(refreshID), url.PathEscape(queryID)), nil)
	result, err := decode[QueryResult](raw, err, "refresh result")
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (a *API) CancelRefresh(id, refreshID string) (*RefreshJob, error) {
	raw, err := a.c.Delete(fmt.Sprintf("%s/%s/refreshes/%s", BasePath, url.PathEscape(id), url.PathEscape(refreshID)), nil)
	job, err := decode[RefreshJob](raw, err, "refresh job")
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// Ask builds the context attachment used when asking the agent about a
// dashboard or a single widget.
func (a *API) Ask(id, scope, widgetID string, filters FilterValues) (*AskContext, error) {
	body := map[string]any{
		"scope":         scope,
		"filter_values": orEmpty(filters),
	}
	if widgetID != "" {
		body["widget_id"] = widgetID
	}
	raw, err := a.c.Post(BasePath+"/"+url.PathEscape(id)+"/ask", body)
	context, err := decode[AskContext](raw, err, "dashboard ask context")
	if err != nil {
		return nil, err
	}
	return &context, nil
}

// orEmpty keeps filter_values a JSON object; the DTO rejects null.
func orEmpty(filters FilterValues) map[string]any {
	if filters == nil {
		return map[string]any{}
	}
	return filters
}
