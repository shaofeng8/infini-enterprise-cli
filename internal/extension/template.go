package extension

import (
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

const templateBase = "/api/ai_template"

// Template is a saved prompt. Variables is a JSON object stored as text, the
// way the server keeps it, and the placeholders in Text are {{name}}.
type Template struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Text      string `json:"text"`
	Variables string `json:"variables,omitempty"`
	UserID    string `json:"user_id,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type TemplateQuery struct {
	Page     int
	PageSize int
	Keyword  string
}

func (q TemplateQuery) params() map[string]string {
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
	return params
}

func (a *API) Templates(query TemplateQuery) (*Pagination[Template], error) {
	page, err := fetch[Pagination[Template]](a, templateBase, query.params(), "template list")
	if err != nil {
		return nil, err
	}
	return &page, nil
}

func (a *API) Template(id string) (*Template, error) {
	template, err := fetch[Template](a, templateBase+"/"+url.PathEscape(id), nil, "template")
	if err != nil {
		return nil, err
	}
	return &template, nil
}

func (a *API) CreateTemplate(body map[string]string) (*Template, error) {
	template, err := send[Template](a, templateBase+"/create", body, "template")
	if err != nil {
		return nil, err
	}
	return &template, nil
}

// UpdateTemplate replaces the template. Name and text are required even when
// only one of them changes, because the update DTO extends the create DTO.
func (a *API) UpdateTemplate(id string, body map[string]string) (*Template, error) {
	template, err := send[Template](a, templateBase+"/update/"+url.PathEscape(id), body, "template")
	if err != nil {
		return nil, err
	}
	return &template, nil
}

func (a *API) DeleteTemplates(ids []string) (json.RawMessage, error) {
	if len(ids) == 0 {
		return nil, cliexit.Usage("at least one template id is required")
	}
	return a.c.Post(templateBase+"/delete", map[string]any{"ids": ids})
}
