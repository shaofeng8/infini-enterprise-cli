// Package hub wraps the /api/ai/context-hub REST surface: the semantic layer
// the agent reads before it writes SQL.
//
// Five entity kinds share one shape (list / add / update / delete) but each has
// its own payload:
//
//	table_data         table descriptions
//	table_column_data  column definitions and business meaning
//	playbook           analysis procedures scoped to data sources
//	kpi                metric definitions, either business logic or SQL
//	user_preference    presentation preferences
//
// Writes by a non-owner become drafts instead of taking effect, so the review
// commands are part of the normal editing path, not an admin afterthought.
package hub

import (
	"encoding/json"
	"strconv"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

const BasePath = "/api/ai/context-hub"

// Kind is the entity_type the server uses in draft and review payloads.
type Kind string

const (
	KindTable      Kind = "table_data"
	KindColumn     Kind = "table_column_data"
	KindPlaybook   Kind = "playbook"
	KindKPI        Kind = "kpi"
	KindPreference Kind = "user_preference"
)

var Kinds = []Kind{KindTable, KindColumn, KindPlaybook, KindKPI, KindPreference}

// EntityTypes is Kinds as plain strings, for flag validation and help text.
var EntityTypes = []string{
	string(KindTable), string(KindColumn), string(KindPlaybook),
	string(KindKPI), string(KindPreference),
}

// route maps an entity kind to its URL segment, which is not simply the kind
// with underscores replaced.
func (k Kind) route() string {
	switch k {
	case KindTable:
		return "table-data"
	case KindColumn:
		return "table-column"
	case KindPlaybook:
		return "playbook"
	case KindKPI:
		return "kpi"
	case KindPreference:
		return "preference"
	default:
		return string(k)
	}
}

// ReviewStatuses accepted by the draft and review listings.
var ReviewStatuses = []string{"pending", "reviewed", "approved", "rejected", "all"}

// DraftOperations and DraftSources, from the draft DTO enums.
var (
	DraftOperations = []string{"create", "update", "delete"}
	DraftSources    = []string{"manual", "ai_generated", "imported"}
)

// KpiCreationModes decide which KPI fields are meaningful: business_logic uses
// calculation_logic, sql_playground uses sql_query.
var KpiCreationModes = []string{"business_logic", "sql_playground"}

// PreferenceTypes currently has a single member, but the server validates it as
// an enum so it has to be sent.
var PreferenceTypes = []string{"data_table"}

type PagerMeta struct {
	ItemCount    int `json:"itemCount"`
	TotalItems   int `json:"totalItems"`
	ItemsPerPage int `json:"itemsPerPage"`
	TotalPages   int `json:"totalPages"`
	CurrentPage  int `json:"currentPage"`
}

// Page is the server's Pagination wrapper.
type Page[T any] struct {
	Items []T       `json:"items"`
	Meta  PagerMeta `json:"meta"`
}

// PageQuery mirrors ContextHubPageDto, plus the status filter the draft and
// review listings add.
type PageQuery struct {
	Page       int
	PageSize   int
	Field      string
	Order      string
	Search     string
	DatabaseID string
	TableID    string
	OnlyMine   bool
	Status     string
}

func (q PageQuery) params() map[string]string {
	params := map[string]string{
		"search":     q.Search,
		"databaseId": q.DatabaseID,
		"tableId":    q.TableID,
		"status":     q.Status,
		"field":      q.Field,
		"order":      q.Order,
	}
	if q.Page > 0 {
		params["page"] = strconv.Itoa(q.Page)
	}
	if q.PageSize > 0 {
		params["pageSize"] = strconv.Itoa(q.PageSize)
	}
	if q.OnlyMine {
		params["onlyMine"] = "true"
	}
	return params
}

// TableRef identifies a table for KPIs and preferences. All three fields are
// required by the DTO, which is why the CLI resolves a data source name into
// its id rather than making the caller supply both.
type TableRef struct {
	DatabaseID   string `json:"database_id"`
	DatabaseName string `json:"database_name"`
	TableName    string `json:"table_name"`
}

type TableData struct {
	ID          string `json:"id"`
	DatabaseID  string `json:"database_id"`
	TableName   string `json:"table_name"`
	Description string `json:"table_description,omitempty"`
	CreatorName string `json:"creatorName,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
}

type TableColumn struct {
	ID              string `json:"id"`
	TableID         string `json:"table_id"`
	ColumnName      string `json:"column_name"`
	Definition      string `json:"column_definition,omitempty"`
	BusinessMeaning string `json:"column_business_meaning,omitempty"`
	UpdatedAt       string `json:"updatedAt,omitempty"`
}

type Playbook struct {
	ID          string   `json:"id"`
	Name        string   `json:"playbook_name"`
	DatabaseIDs []string `json:"database_ids,omitempty"`
	Content     string   `json:"playbook_content,omitempty"`
	Description string   `json:"playbook_description,omitempty"`
	CreatorName string   `json:"creatorName,omitempty"`
	UpdatedAt   string   `json:"updatedAt,omitempty"`
}

type KPI struct {
	ID               string     `json:"id"`
	Name             string     `json:"kpi_name"`
	Alias            string     `json:"kpi_alias,omitempty"`
	Designations     string     `json:"relevant_for_designations,omitempty"`
	CreationMode     string     `json:"creation_mode"`
	Description      string     `json:"kpi_description,omitempty"`
	CalculationLogic string     `json:"calculation_logic,omitempty"`
	SQLQuery         string     `json:"sql_query,omitempty"`
	SetValues        string     `json:"set_values,omitempty"`
	Tables           []TableRef `json:"tables,omitempty"`
	CreatorName      string     `json:"creatorName,omitempty"`
	UpdatedAt        string     `json:"updatedAt,omitempty"`
}

type Preference struct {
	ID          string     `json:"id"`
	Name        string     `json:"preference_name"`
	Type        string     `json:"preference_type"`
	Value       string     `json:"preference_value,omitempty"`
	Tables      []TableRef `json:"tables,omitempty"`
	CreatorName string     `json:"creatorName,omitempty"`
	UpdatedAt   string     `json:"updatedAt,omitempty"`
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

// List reads one page of an entity kind. It is a free function because Go does
// not allow type parameters on methods.
func List[T any](a *API, kind Kind, query PageQuery) (*Page[T], error) {
	raw, err := a.c.Get(BasePath+"/"+kind.route()+"/list", query.params())
	page, err := decode[Page[T]](raw, err, string(kind)+" list")
	if err != nil {
		return nil, err
	}
	return &page, nil
}

// Add creates an entity, or a draft if the caller does not own the target
// data source.
func (a *API) Add(kind Kind, body any) (json.RawMessage, error) {
	return a.c.Post(BasePath+"/"+kind.route()+"/add", body)
}

func (a *API) Update(kind Kind, body any) (json.RawMessage, error) {
	return a.c.Post(BasePath+"/"+kind.route()+"/update", body)
}

func (a *API) Delete(kind Kind, ids []string) (json.RawMessage, error) {
	return a.c.Post(BasePath+"/"+kind.route()+"/delete", map[string]any{"ids": ids})
}

// TableDatabaseIDs lists the data sources that already have table definitions.
func (a *API) TableDatabaseIDs() ([]string, error) {
	raw, err := a.c.Get(BasePath+"/table-data/database-ids", nil)
	return decode[[]string](raw, err, "database ids")
}
