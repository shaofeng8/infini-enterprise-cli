package extension

import (
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

const skillBase = "/api/ai_skill"

// AvailableSkill is a catalog entry the agent could be given.
type AvailableSkill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
	// Source is "system" for the built-ins, otherwise the owning user's id.
	Source string `json:"source"`
}

// InstalledSkill is a skill installed into this deployment.
type InstalledSkill struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	// SkillID is the catalog id, or local:<name> for an uploaded archive.
	SkillID     string `json:"skillId"`
	UserID      string `json:"userId"`
	Source      string `json:"source"`
	Scope       string `json:"scope,omitempty"`
	InstallPath string `json:"installPath"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

// SkillState is the status map keyed by skill name, which is what the UI reads
// to decide whether a catalog entry is already installed.
type SkillState struct {
	Status string `json:"status"`
	Source string `json:"source,omitempty"`
	Scope  string `json:"scope,omitempty"`
}

// KeywordQuery is the page filter the skill and tool listings accept. They use
// pageNum/pageSize rather than the page/pageSize of the other modules.
type KeywordQuery struct {
	PageNum  int
	PageSize int
	Keyword  string
	Source   string
}

func (q KeywordQuery) params() map[string]string {
	params := map[string]string{}
	if q.PageNum > 0 {
		params["pageNum"] = strconv.Itoa(q.PageNum)
	}
	if q.PageSize > 0 {
		params["pageSize"] = strconv.Itoa(q.PageSize)
	}
	if q.Keyword != "" {
		params["keyword"] = q.Keyword
	}
	if q.Source != "" {
		params["source"] = q.Source
	}
	return params
}

// AvailableSkills lists what the agent can reach right now.
//
// The answer depends on context: the server filters the catalog by the data
// source types the task has attached and by whether the browser is enabled, so
// the same account sees different skills for different tasks.
func (a *API) AvailableSkills(taskID string, supportsBrowser bool) ([]AvailableSkill, error) {
	params := map[string]string{}
	if taskID != "" {
		params["taskId"] = taskID
	}
	if supportsBrowser {
		params["supportsBrowser"] = "true"
	}
	return fetch[[]AvailableSkill](a, skillBase+"/available", params, "skill list")
}

func (a *API) InstalledSkills(query KeywordQuery) (*ListPage[InstalledSkill], error) {
	page, err := fetch[ListPage[InstalledSkill]](a, skillBase+"/list", query.params(), "skill list")
	if err != nil {
		return nil, err
	}
	return &page, nil
}

func (a *API) SkillState() (map[string]SkillState, error) {
	return fetch[map[string]SkillState](a, skillBase+"/installedState", nil, "skill state")
}

// InstallSkill enables a catalog entry. Both the catalog id and the name are
// required: the id identifies the entry, the name is the handle the agent uses.
func (a *API) InstallSkill(skillID, name string) (*InstalledSkill, error) {
	skill, err := send[InstalledSkill](a, skillBase+"/install", map[string]string{
		"skillId": skillID,
		"name":    name,
	}, "skill")
	if err != nil {
		return nil, err
	}
	return &skill, nil
}

func (a *API) UninstallSkill(skillID string) (json.RawMessage, error) {
	return a.c.Post(skillBase+"/uninstall", map[string]string{"skillId": skillID})
}

func (a *API) ToggleSkill(skillID, status string) (*InstalledSkill, error) {
	skill, err := send[InstalledSkill](a, skillBase+"/toggleStatus", map[string]string{
		"skillId": skillID,
		"status":  status,
	}, "skill")
	if err != nil {
		return nil, err
	}
	return &skill, nil
}

// UploadSkill sends a zip archive. The name comes from the SKILL.md
// frontmatter, so it does not have to be given; passing id replaces an
// existing upload instead of adding one.
func (a *API) UploadSkill(archive string, fields map[string]string) (*InstalledSkill, error) {
	skill, err := form[InstalledSkill](a, skillBase+"/upload", "file", archive, fields, "skill")
	if err != nil {
		return nil, err
	}
	return &skill, nil
}

// EditSkill updates a local skill's metadata, optionally replacing its archive.
func (a *API) EditSkill(archive string, fields map[string]string) (*InstalledSkill, error) {
	skill, err := form[InstalledSkill](a, skillBase+"/editLocal", "file", archive, fields, "skill")
	if err != nil {
		return nil, err
	}
	return &skill, nil
}

// DeleteSkill removes a locally uploaded skill by its row id, not its
// catalog id: a local skill has no catalog entry to point back to.
func (a *API) DeleteSkill(id string) (json.RawMessage, error) {
	if id == "" {
		return nil, cliexit.Usage("a skill id is required")
	}
	return a.c.Post(skillBase+"/deleteLocal/"+url.PathEscape(id), nil)
}
