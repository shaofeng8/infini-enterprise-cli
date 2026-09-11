// Package database wraps the /api/ai_database REST surface: data source CRUD,
// connection tests, schema inspection and knowledge base bindings.
package database

import (
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

const BasePath = "/api/ai_database"

// Types the server accepts, from DatabaseQueryDto.
var Types = []string{
	"mysql", "postgres", "snowflake", "gbase8a", "clickhouse", "dm", "supabase",
	"file", "deltalake", "sqlite", "duckdb", "doris", "starrocks", "kingbase",
	"sqlserver", "oracle",
}

var Sources = []string{"local", "remote", "all"}

type Item struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Nickname    string `json:"nickname,omitempty"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	// Config is a JSON document the server stores as text.
	Config string `json:"config,omitempty"`
	// Enabled lives in ai_database_mapping, not on the data source row, so it
	// is only meaningful on responses and can only be changed via SetEnabled.
	Enabled   *int   `json:"enabled,omitempty"`
	Source    string `json:"source,omitempty"`
	UserID    string `json:"user_id,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

type PagerMeta struct {
	ItemCount    int `json:"itemCount"`
	TotalItems   int `json:"totalItems"`
	ItemsPerPage int `json:"itemsPerPage"`
	TotalPages   int `json:"totalPages"`
	CurrentPage  int `json:"currentPage"`
}

type ListResponse struct {
	Items []Item    `json:"items"`
	Meta  PagerMeta `json:"meta"`
}

// ListQuery mirrors DatabaseQueryDto.
type ListQuery struct {
	Page            int
	PageSize        int
	Field           string
	Order           string
	Name            string
	Type            string
	Enabled         *int
	ContextHubState string
	Source          string
}

func (q ListQuery) params() map[string]string {
	params := map[string]string{
		"name":             q.Name,
		"type":             q.Type,
		"contextHubStatus": q.ContextHubState,
		"source":           q.Source,
		"field":            q.Field,
		"order":            q.Order,
	}
	if q.Page > 0 {
		params["page"] = strconv.Itoa(q.Page)
	}
	if q.PageSize > 0 {
		params["pageSize"] = strconv.Itoa(q.PageSize)
	}
	if q.Enabled != nil {
		params["enabled"] = strconv.Itoa(*q.Enabled)
	}
	return params
}

// Spec is the payload shared by add and update. Every field is required by the
// server DTO, which is why update reads the current record first and overlays
// changes onto it.
type Spec struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name"`
	Nickname    string `json:"nickname"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Config      string `json:"config"`
	Enabled     int    `json:"enabled"`
}

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

func (a *API) List(query ListQuery) (*ListResponse, error) {
	raw, err := a.c.Get(BasePath+"/list", query.params())
	result, err := decode[ListResponse](raw, err, "data source list")
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (a *API) Get(id string) (*Item, error) {
	raw, err := a.c.Get(BasePath+"/getDatabaseById/"+url.PathEscape(id), nil)
	item, err := decode[Item](raw, err, "data source")
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (a *API) GetByName(name string) (*Item, error) {
	raw, err := a.c.Get(BasePath+"/getDatabaseByName/"+url.PathEscape(name), nil)
	item, err := decode[Item](raw, err, "data source")
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (a *API) Add(spec Spec) (json.RawMessage, error) {
	spec.ID = ""
	return a.c.Post(BasePath+"/add", spec)
}

func (a *API) Update(spec Spec) (json.RawMessage, error) {
	return a.c.Post(BasePath+"/update", spec)
}

func (a *API) Delete(ids []string) (json.RawMessage, error) {
	return a.c.Post(BasePath+"/delete", map[string]any{"ids": ids})
}

// SetEnabled toggles the per-user mapping rather than the data source itself,
// which is why it takes a list and is separate from Update.
func (a *API) SetEnabled(ids []string, enabled bool) (json.RawMessage, error) {
	value := 0
	if enabled {
		value = 1
	}
	return a.c.Post(BasePath+"/enabled", map[string]any{"ids": ids, "enabled": value})
}

// TestConnection validates a config without saving it.
func (a *API) TestConnection(dbType, config string) (json.RawMessage, error) {
	return a.c.Post(BasePath+"/testConnection", map[string]string{
		"type":   dbType,
		"config": config,
	})
}

func (a *API) Schema(id string) (json.RawMessage, error) {
	return a.c.Get(BasePath+"/schema/"+url.PathEscape(id), nil)
}

// Upload adds a file to a `file` type data source.
func (a *API) Upload(id, filePath string) (json.RawMessage, error) {
	return a.c.Upload(BasePath+"/upload/"+url.PathEscape(id), "file", filePath, nil)
}

func (a *API) BindRags(id string, ragIDs []string) (json.RawMessage, error) {
	return a.c.Post(BasePath+"/bindRags", map[string]any{
		"databaseId": id,
		"ragIds":     ragIDs,
	})
}

func (a *API) BoundRags(id string) (json.RawMessage, error) {
	return a.c.Get(BasePath+"/getBindRags/"+url.PathEscape(id), nil)
}

// ReviewDatabases lists the data sources whose semantic layer this user can review.
func (a *API) ReviewDatabases() (json.RawMessage, error) {
	return a.c.Get(BasePath+"/context-hub-review-databases", nil)
}
