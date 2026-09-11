// Package project wraps the /api/ai_project REST surface: projects, their
// members and their shared file tree.
package project

import (
	"encoding/json"
	"net/url"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

const BasePath = "/api/ai_project"

// Roles a member can hold, from AI_PROJECT_MEMBER_ROLES.
var Roles = []string{"viewer", "editor", "manager"}

type Item struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Metadata    string `json:"metadata,omitempty"`
	OwnerUserID string `json:"owner_user_id,omitempty"`
	Role        string `json:"role,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
}

type Member struct {
	ID           string `json:"id,omitempty"`
	ProjectID    string `json:"project_id,omitempty"`
	MemberUserID string `json:"member_user_id"`
	Username     string `json:"username,omitempty"`
	Role         string `json:"role"`
	CreatedAt    string `json:"createdAt,omitempty"`
}

type Spec struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Metadata    string `json:"metadata,omitempty"`
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

func (a *API) List() ([]Item, error) {
	raw, err := a.c.Get(BasePath+"/list", nil)
	return decode[[]Item](raw, err, "project list")
}

func (a *API) Create(spec Spec) (json.RawMessage, error) {
	return a.c.Post(BasePath, spec)
}

// Update is a PATCH, so omitted fields are left alone by the server and no
// read-modify-write is needed.
func (a *API) Update(id string, spec Spec) (json.RawMessage, error) {
	return a.c.Patch(BasePath+"/"+url.PathEscape(id), spec)
}

// Delete soft-deletes the project.
func (a *API) Delete(id string) (json.RawMessage, error) {
	return a.c.Delete(BasePath+"/"+url.PathEscape(id), nil)
}

func (a *API) Members(id string) ([]Member, error) {
	raw, err := a.c.Get(BasePath+"/"+url.PathEscape(id)+"/members", nil)
	return decode[[]Member](raw, err, "project members")
}

func (a *API) AddMember(id, userID, role string) (json.RawMessage, error) {
	return a.c.Post(BasePath+"/"+url.PathEscape(id)+"/members", map[string]string{
		"userId": userID,
		"role":   role,
	})
}

func (a *API) UpdateMember(id, userID, role string) (json.RawMessage, error) {
	return a.c.Patch(memberPath(id, userID), map[string]string{"role": role})
}

func (a *API) RemoveMember(id, userID string) (json.RawMessage, error) {
	return a.c.Delete(memberPath(id, userID), nil)
}

func (a *API) Tree(id string) (json.RawMessage, error) {
	return a.c.Get(BasePath+"/"+url.PathEscape(id)+"/tree", nil)
}

func (a *API) PreviewFile(id, path string) (json.RawMessage, error) {
	return a.c.Post(filesPath(id)+"/preview", map[string]string{"path": path})
}

func (a *API) DownloadFile(id, path, dest string) (*client.DownloadResult, error) {
	target := client.WithQuery(filesPath(id)+"/download", map[string]string{"path": path})
	return a.c.Download("GET", target, nil, dest, client.SanitizeFilename(baseName(path)))
}

func (a *API) MoveFile(id, sourcePath, targetDirectory string) (json.RawMessage, error) {
	return a.c.Patch(filesPath(id)+"/move", map[string]string{
		"sourcePath":      sourcePath,
		"targetDirectory": targetDirectory,
	})
}

func (a *API) CopyFile(id, sourcePath, targetDirectory string) (json.RawMessage, error) {
	return a.c.Post(filesPath(id)+"/copy", map[string]string{
		"sourcePath":      sourcePath,
		"targetDirectory": targetDirectory,
	})
}

// DeletePath removes a file or a whole directory.
func (a *API) DeletePath(id, path string) (json.RawMessage, error) {
	return a.c.Delete(filesPath(id), map[string]string{"path": path})
}

func (a *API) CreateDirectory(id, path string) (json.RawMessage, error) {
	return a.c.Post(BasePath+"/"+url.PathEscape(id)+"/directories", map[string]string{"path": path})
}

func filesPath(id string) string {
	return BasePath + "/" + url.PathEscape(id) + "/files"
}

func memberPath(id, userID string) string {
	return BasePath + "/" + url.PathEscape(id) + "/members/" + url.PathEscape(userID)
}

func baseName(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[i+1:]
		}
	}
	return path
}
