// Package rag wraps the /api/ai_rag_sdk REST surface: knowledge base CRUD,
// document storage browsing and data source bindings.
package rag

import (
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

const BasePath = "/api/ai_rag_sdk"

// FileSystems a knowledge base can read documents from. Only the remote three
// accept credentials, and only they can have files deleted remotely.
var FileSystems = []string{"file", "oss", "s3", "cos"}

var RemoteFileSystems = []string{"oss", "s3", "cos"}

var Sources = []string{"local", "remote", "all"}

type Item struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Nickname    string `json:"nickname,omitempty"`
	Description string `json:"description,omitempty"`
	// RequiredExts is stored as JSON text even though create and update accept
	// it as an array.
	RequiredExts     string `json:"requiredExts,omitempty"`
	DocDir           string `json:"docDir,omitempty"`
	DocFilterRelance string `json:"ragDocFilterRelevance,omitempty"`
	Enabled          *int   `json:"enabled,omitempty"`
	Source           string `json:"source,omitempty"`
	UserID           string `json:"user_id,omitempty"`
	ReviewStatus     string `json:"review_status,omitempty"`
	CreatedAt        string `json:"createdAt,omitempty"`
	UpdatedAt        string `json:"updatedAt,omitempty"`

	LinkedDatabases []LinkedDatabase `json:"linkedDatabases,omitempty"`
}

type LinkedDatabase struct {
	ID   string `json:"id"`
	Name string `json:"name"`
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

type ListQuery struct {
	Page     int
	PageSize int
	Field    string
	Order    string
	Keyword  string
	Enabled  *int
	Source   string
}

func (q ListQuery) params() map[string]string {
	params := map[string]string{
		"keyword": q.Keyword,
		"source":  q.Source,
		"field":   q.Field,
		"order":   q.Order,
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

// Spec is the create and update payload. Update reuses the create DTO, so it
// carries the whole record.
type Spec struct {
	Name         string   `json:"name"`
	Nickname     string   `json:"nickname"`
	Description  string   `json:"description"`
	RequiredExts []string `json:"requiredExts,omitempty"`
	DocDir       string   `json:"docDir,omitempty"`
	DocRelevance string   `json:"ragDocFilterRelevance,omitempty"`
	Enabled      *int     `json:"enabled,omitempty"`
	DatabaseIDs  []string `json:"database_ids,omitempty"`
}

// Storage identifies where documents live. The credentials are only read for
// the remote file systems.
type Storage struct {
	FileSystem      string `json:"file_system"`
	Endpoint        string `json:"endpoint,omitempty"`
	AccessKeyID     string `json:"access_key_id,omitempty"`
	AccessKeySecret string `json:"access_key_secret,omitempty"`
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
	raw, err := a.c.Get(BasePath, query.params())
	result, err := decode[ListResponse](raw, err, "knowledge base list")
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// ListAll returns every accessible knowledge base unpaged, which is what to use
// when resolving names to ids.
func (a *API) ListAll() ([]Item, error) {
	raw, err := a.c.Get(BasePath+"/all", nil)
	return decode[[]Item](raw, err, "knowledge base list")
}

func (a *API) Get(id string) (*Item, error) {
	raw, err := a.c.Get(BasePath+"/"+url.PathEscape(id), nil)
	item, err := decode[Item](raw, err, "knowledge base")
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (a *API) Create(spec Spec) (json.RawMessage, error) {
	return a.c.Post(BasePath+"/create", spec)
}

func (a *API) Update(id string, spec Spec) (json.RawMessage, error) {
	return a.c.Post(BasePath+"/update/"+url.PathEscape(id), spec)
}

func (a *API) Delete(ids []string) (json.RawMessage, error) {
	return a.c.Post(BasePath+"/delete", map[string]any{"ids": ids})
}

func (a *API) SetEnabled(ids []string, enabled bool) (json.RawMessage, error) {
	value := 0
	if enabled {
		value = 1
	}
	return a.c.Post(BasePath+"/enabled", map[string]any{"ids": ids, "enabled": value})
}

// FileTree lists a document directory. filter selects file, directory or both.
func (a *API) FileTree(storage Storage, directory, filter string) (json.RawMessage, error) {
	body := storageBody(storage)
	body["directory"] = directory
	if filter != "" {
		body["filter"] = filter
	}
	return a.c.Post(BasePath+"/fileTree", body)
}

func (a *API) Download(storage Storage, filePath, dest string) (*client.DownloadResult, error) {
	body := storageBody(storage)
	body["file_path"] = filePath
	return a.c.Download("POST", BasePath+"/download", body, dest, client.SanitizeFilename(baseName(filePath)))
}

func (a *API) DeleteRemoteFile(storage Storage, filePath string) (json.RawMessage, error) {
	body := storageBody(storage)
	body["file_path"] = filePath
	return a.c.Post(BasePath+"/deleteRemoteFile", body)
}

// BindDatabases replaces the data sources bound to a knowledge base.
func (a *API) BindDatabases(id string, databaseIDs []string) (json.RawMessage, error) {
	return a.c.Post(BasePath+"/bindDatabases", map[string]any{
		"ragId":       id,
		"databaseIds": databaseIDs,
	})
}

func (a *API) BoundDatabases(id string) (json.RawMessage, error) {
	return a.c.Get(BasePath+"/getBindDatabases/"+url.PathEscape(id), nil)
}

func storageBody(storage Storage) map[string]any {
	body := map[string]any{"file_system": storage.FileSystem}
	if storage.Endpoint != "" {
		body["endpoint"] = storage.Endpoint
	}
	if storage.AccessKeyID != "" {
		body["access_key_id"] = storage.AccessKeyID
	}
	if storage.AccessKeySecret != "" {
		body["access_key_secret"] = storage.AccessKeySecret
	}
	return body
}

func baseName(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[i+1:]
		}
	}
	return path
}
