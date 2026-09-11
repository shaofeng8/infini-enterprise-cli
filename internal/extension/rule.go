package extension

import (
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

const ruleBase = "/api/ai_rule"

// RuleTypes decide where a rule applies: globally, or only when a particular
// data source is in play.
var RuleTypes = []string{"global", "database"}

// Rule is a standing instruction prepended to the agent's context.
type Rule struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Value string `json:"value"`
	// Enabled is 1 or 0 rather than a boolean, matching the column.
	Enabled     int      `json:"enabled"`
	RuleType    string   `json:"rule_type"`
	DatabaseIDs []string `json:"databaseIds,omitempty"`
	UserID      string   `json:"user_id,omitempty"`
	CreatedAt   string   `json:"created_at,omitempty"`
	UpdatedAt   string   `json:"updated_at,omitempty"`
}

// RuleQuery filters the rule listing. Enabled is a pointer because 0 is a
// meaningful filter value, not an absent one.
type RuleQuery struct {
	Page     int
	PageSize int
	Name     string
	Enabled  *int
	RuleType string
}

func (q RuleQuery) params() map[string]string {
	params := map[string]string{}
	if q.Page > 0 {
		params["page"] = strconv.Itoa(q.Page)
	}
	if q.PageSize > 0 {
		params["pageSize"] = strconv.Itoa(q.PageSize)
	}
	if q.Name != "" {
		params["name"] = q.Name
	}
	if q.Enabled != nil {
		params["enabled"] = strconv.Itoa(*q.Enabled)
	}
	if q.RuleType != "" {
		params["rule_type"] = q.RuleType
	}
	return params
}

func (a *API) Rules(query RuleQuery) (*Pagination[Rule], error) {
	page, err := fetch[Pagination[Rule]](a, ruleBase+"/list", query.params(), "rule list")
	if err != nil {
		return nil, err
	}
	return &page, nil
}

// EnabledRules is what the agent itself would receive, so it answers "which
// rules are actually in effect" without reconciling the listing by hand.
func (a *API) EnabledRules() ([]Rule, error) {
	return fetch[[]Rule](a, ruleBase+"/getEnabledRules", nil, "rule list")
}

func (a *API) AllRules() ([]Rule, error) {
	return fetch[[]Rule](a, ruleBase+"/getAllRules", nil, "rule list")
}

func (a *API) Rule(id string) (*Rule, error) {
	rule, err := fetch[Rule](a, ruleBase+"/getRuleById/"+url.PathEscape(id), nil, "rule")
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

func (a *API) RuleByName(name string) (*Rule, error) {
	rule, err := fetch[Rule](a, ruleBase+"/getRuleByName/"+url.PathEscape(name), nil, "rule")
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

// RuleDatabases lists the data sources a database-scoped rule may bind to.
func (a *API) RuleDatabases() (json.RawMessage, error) {
	return a.c.Get(ruleBase+"/getAvailableDatabases", nil)
}

func (a *API) AddRule(body map[string]any) (json.RawMessage, error) {
	return a.c.Post(ruleBase+"/add", body)
}

func (a *API) UpdateRule(body map[string]any) (json.RawMessage, error) {
	return a.c.Post(ruleBase+"/update", body)
}

func (a *API) DeleteRules(ids []string) (json.RawMessage, error) {
	if len(ids) == 0 {
		return nil, cliexit.Usage("at least one rule id is required")
	}
	return a.c.Post(ruleBase+"/delete", map[string]any{"ids": ids})
}

// SetRulesEnabled flips rules in bulk. The id list is numeric here while the
// delete endpoint takes strings, so the two are not interchangeable.
func (a *API) SetRulesEnabled(ids []int, enabled int) (json.RawMessage, error) {
	if len(ids) == 0 {
		return nil, cliexit.Usage("at least one rule id is required")
	}
	return a.c.Post(ruleBase+"/enabled", map[string]any{"ids": ids, "enabled": enabled})
}
