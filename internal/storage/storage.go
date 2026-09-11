// Package storage covers the file plane: the user's own directories, the
// object store, and the resumable chunked upload session.
//
// There are three distinct places a file can live, and they are not
// interchangeable:
//
//   - a user directory, which is what `fs` manages and what a data source
//     import reads from
//   - a task workspace, which `task file` handles
//   - an upload session's staging area, which only exists mid-transfer
package storage

import (
	"encoding/json"
	"net/url"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

// These endpoints hang off the API root rather than a controller prefix,
// because the upload controller is mounted at "/".
const (
	storageBase = "/api/storage"
	uploadBase  = "/api/file_upload"
)

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

// TreeNode is one entry in a user's file tree. Children is populated for
// directories, so the tree arrives whole rather than a level at a time.
type TreeNode struct {
	Key       string     `json:"key"`
	Title     string     `json:"title"`
	IsDir     bool       `json:"isDir"`
	Size      string     `json:"size,omitempty"`
	CreatedAt string     `json:"createdAt,omitempty"`
	Children  []TreeNode `json:"children,omitempty"`
}

// Flatten walks the tree depth-first so a listing prints one path per line.
func Flatten(nodes []TreeNode, filesOnly bool) []TreeNode {
	var flat []TreeNode
	for _, node := range nodes {
		if !node.IsDir || !filesOnly {
			flat = append(flat, node)
		}
		flat = append(flat, Flatten(node.Children, filesOnly)...)
	}
	return flat
}

func (a *API) Directories() ([]string, error) {
	raw, err := a.c.Get("/api/directories", nil)
	return decode[[]string](raw, err, "directory list")
}

func (a *API) FileTree(keyword string) ([]TreeNode, error) {
	params := map[string]string{}
	if keyword != "" {
		params["keyword"] = keyword
	}
	raw, err := a.c.Get("/api/fileTree", params)
	return decode[[]TreeNode](raw, err, "file tree")
}

func (a *API) CreateDirectory(name string) (json.RawMessage, error) {
	return a.c.Post("/api/createDirectory", map[string]string{"directoryName": name})
}

// DeleteDirectory sends its target in the body of a DELETE, which is unusual
// but is what the endpoint expects.
func (a *API) DeleteDirectory(name string) (json.RawMessage, error) {
	return a.c.Delete("/api/deleteDirectory", map[string]string{"directoryName": name})
}

// UploadConfig reports the size ceiling, which is worth reading before a large
// transfer: the limit is a deployment setting, not a fixed value.
func (a *API) UploadConfig() (json.RawMessage, error) {
	return a.c.Get("/api/uploadConfig", nil)
}

// UploadResult describes a stored file. AssetID and LogicalPath are only set
// by task uploads, which route through the content-addressed attachment store.
type UploadResult struct {
	Filename    string `json:"filename"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	Type        string `json:"type"`
	AssetID     string `json:"assetId,omitempty"`
	LogicalPath string `json:"logicalPath,omitempty"`
}

// Upload sends a file into one of the user's directories in a single request.
// Anything large enough to be worth resuming should go through a session
// instead; see Init.
func (a *API) Upload(directory, filePath string) (*UploadResult, error) {
	raw, err := a.c.Upload("/api/upload/"+url.PathEscape(directory), "file", filePath, nil)
	result, err := decode[UploadResult](raw, err, "upload result")
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// TaskNamingStrategies decide the stored file's name. Hash naming is
// content-addressed, so re-uploading identical bytes is a no-op; original
// naming overwrites a file of the same name.
var TaskNamingStrategies = []string{"original", "hash"}

func (a *API) UploadToTask(taskID, filePath, subdir, naming string) (*UploadResult, error) {
	params := map[string]string{}
	if subdir != "" {
		params["subdir"] = subdir
	}
	if naming != "" {
		params["naming"] = naming
	}
	path := client.WithQuery("/api/taskUpload/"+url.PathEscape(taskID), params)
	raw, err := a.c.Upload(path, "file", filePath, nil)
	result, err := decode[UploadResult](raw, err, "upload result")
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// Download fetches an object by id. The id may contain slashes and non-ASCII
// characters, so it is escaped rather than interpolated.
func (a *API) Download(id, dest string) (*client.DownloadResult, error) {
	path := storageBase + "/download/" + url.PathEscape(id)
	return a.c.Download("GET", path, nil, dest, client.SanitizeFilename(baseName(id)))
}

func (a *API) Delete(ids []string) (json.RawMessage, error) {
	if len(ids) == 0 {
		return nil, cliexit.Usage("at least one file id is required")
	}
	return a.c.Post(storageBase+"/delete", map[string]any{"ids": ids})
}

func baseName(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[i+1:]
		}
	}
	return path
}
