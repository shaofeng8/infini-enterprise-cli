package extension

import (
	"encoding/json"
	"net/url"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

const toolBase = "/api/ai_tool"

// Tool is an installed external command.
type Tool struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Alias  string `json:"alias"`
	Author string `json:"author"`
	Intro  string `json:"intro"`
	// Tags is a JSON array stored as text, the way the server keeps it.
	Tags   string `json:"tags"`
	Logo   string `json:"logo"`
	Status string `json:"status"`
	Source string `json:"source"`
	// PluginID is the catalog id, present only for remote tools.
	PluginID       string `json:"pluginId,omitempty"`
	UserID         string `json:"userId"`
	ExecutableName string `json:"executableName,omitempty"`
	Scope          string `json:"scope,omitempty"`
	RuntimePath    string `json:"runtimePath,omitempty"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
}

// ToolState is the status map keyed by plugin id.
type ToolState struct {
	Status string `json:"status"`
	Source string `json:"source,omitempty"`
	Scope  string `json:"scope,omitempty"`
}

func (a *API) Tools(query KeywordQuery) (*ListPage[Tool], error) {
	page, err := fetch[ListPage[Tool]](a, toolBase+"/list", query.params(), "tool list")
	if err != nil {
		return nil, err
	}
	return &page, nil
}

// LocalTools lists the tools this user uploaded, which the paginated listing
// mixes in with the installed catalog entries.
func (a *API) LocalTools() ([]Tool, error) {
	return fetch[[]Tool](a, toolBase+"/local", nil, "tool list")
}

// InstalledToolIDs returns just the catalog ids, which is the cheap check.
func (a *API) InstalledToolIDs() ([]string, error) {
	return fetch[[]string](a, toolBase+"/installed", nil, "tool ids")
}

func (a *API) ToolState() (map[string]ToolState, error) {
	return fetch[map[string]ToolState](a, toolBase+"/installedState", nil, "tool state")
}

func (a *API) IsToolInstalled(pluginID string) (bool, error) {
	response, err := fetch[struct {
		Installed bool `json:"installed"`
	}](a, toolBase+"/isInstalled/"+url.PathEscape(pluginID), nil, "tool state")
	return response.Installed, err
}

// InstallTool enables a catalog entry. The server stores the display metadata
// alongside the installation rather than reading it back from the catalog, so
// name, logo, alias and author are all required.
func (a *API) InstallTool(body map[string]string) (*Tool, error) {
	tool, err := send[Tool](a, toolBase+"/install", body, "tool")
	if err != nil {
		return nil, err
	}
	return &tool, nil
}

func (a *API) UninstallTool(pluginID string) (json.RawMessage, error) {
	return a.c.Post(toolBase+"/uninstall", map[string]string{"pluginId": pluginID})
}

func (a *API) ToggleTool(pluginID, status string) (*Tool, error) {
	tool, err := send[Tool](a, toolBase+"/toggleStatus", map[string]string{
		"pluginId": pluginID,
		"status":   status,
	}, "tool")
	if err != nil {
		return nil, err
	}
	return &tool, nil
}

func (a *API) UploadTool(archive string, fields map[string]string) (*Tool, error) {
	tool, err := form[Tool](a, toolBase+"/upload", "file", archive, fields, "tool")
	if err != nil {
		return nil, err
	}
	return &tool, nil
}

func (a *API) EditTool(archive string, fields map[string]string) (*Tool, error) {
	tool, err := form[Tool](a, toolBase+"/editLocal", "file", archive, fields, "tool")
	if err != nil {
		return nil, err
	}
	return &tool, nil
}

// DeleteTool removes a locally uploaded tool by its row id.
func (a *API) DeleteTool(id string) (json.RawMessage, error) {
	if id == "" {
		return nil, cliexit.Usage("a tool id is required")
	}
	return a.c.Post(toolBase+"/deleteLocal/"+url.PathEscape(id), nil)
}
