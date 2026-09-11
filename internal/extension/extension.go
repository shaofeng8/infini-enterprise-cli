// Package extension covers the four things that extend what the agent can do.
//
//   - skills: procedural knowledge, a SKILL.md plus its files, shipped as a zip
//   - tools: external commands the agent may invoke
//   - rules: standing instructions, globally or per data source
//   - templates: saved prompts with variables
//
// Skills and tools share a shape the other two do not: a catalog published to
// the proxy that you install from, plus local archives you upload yourself.
// "Installed" therefore means a row in this deployment's table pointing at
// either a remote catalog entry or a local upload, which is why uninstall takes
// the catalog's id while deleting a local one takes the row's id.
package extension

import (
	"encoding/json"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

// Statuses an installed skill or tool can be in.
var Statuses = []string{"active", "inactive"}

// Sources a tool listing can be filtered by.
var ToolSources = []string{"local", "remote"}

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

// fetch, send and form are free functions rather than methods because Go does
// not allow type parameters on methods.
func fetch[T any](a *API, path string, params map[string]string, what string) (T, error) {
	raw, err := a.c.Get(path, params)
	return decode[T](raw, err, what)
}

func send[T any](a *API, path string, body any, what string) (T, error) {
	raw, err := a.c.Post(path, body)
	return decode[T](raw, err, what)
}

func form[T any](a *API, path, field, file string, fields map[string]string, what string) (T, error) {
	raw, err := a.c.Upload(path, field, file, fields)
	return decode[T](raw, err, what)
}

// ListPage is the {list, total} envelope the skill and tool listings use.
// It is not the Pagination shape the rule and template listings return.
type ListPage[T any] struct {
	List  []T `json:"list"`
	Total int `json:"total"`
}

// PagerMeta accompanies the paginated listings.
type PagerMeta struct {
	ItemCount    int `json:"itemCount"`
	TotalItems   int `json:"totalItems"`
	ItemsPerPage int `json:"itemsPerPage"`
	TotalPages   int `json:"totalPages"`
	CurrentPage  int `json:"currentPage"`
}

type Pagination[T any] struct {
	Items []T       `json:"items"`
	Meta  PagerMeta `json:"meta"`
}
