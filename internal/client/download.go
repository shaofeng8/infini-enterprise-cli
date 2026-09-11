package client

import (
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

type DownloadResult struct {
	Path        string `json:"path"`
	Bytes       int64  `json:"bytes"`
	ContentType string `json:"contentType,omitempty"`
}

// Download streams a response body to disk instead of buffering it, so task
// archives and data exports are not limited by available memory.
//
// dest may be a file path, a directory, or empty; in the latter two cases the
// filename comes from Content-Disposition, falling back to the given name.
func (c *Client) Download(method, path string, body any, dest, fallbackName string) (*DownloadResult, error) {
	req, err := c.newRequest(method, path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "*/*")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, transportError(method, c.baseURL+path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// An error response is small and is JSON, so reading it in full here is
		// what turns "HTTP 404" into an actionable message.
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return nil, httpStatusError(method, path, resp.StatusCode, raw)
	}

	target, err := resolveDownloadPath(dest, filenameFromResponse(resp, fallbackName))
	if err != nil {
		return nil, err
	}

	file, err := os.Create(target)
	if err != nil {
		return nil, cliexit.New(cliexit.CodeBusiness, "cannot create %s: %v", target, err)
	}
	defer file.Close()

	written, err := io.Copy(file, resp.Body)
	if err != nil {
		// A truncated file is worse than no file: it looks like a valid result.
		_ = os.Remove(target)
		return nil, cliexit.New(cliexit.CodeNetwork, "download of %s failed after %d bytes: %v", target, written, err)
	}

	return &DownloadResult{
		Path:        target,
		Bytes:       written,
		ContentType: resp.Header.Get("Content-Type"),
	}, nil
}

func resolveDownloadPath(dest, name string) (string, error) {
	if dest == "" {
		return name, nil
	}
	if info, err := os.Stat(dest); err == nil && info.IsDir() {
		return filepath.Join(dest, name), nil
	}
	if dir := filepath.Dir(dest); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", cliexit.New(cliexit.CodeBusiness, "cannot create %s: %v", dir, err)
		}
	}
	return dest, nil
}

func filenameFromResponse(resp *http.Response, fallback string) string {
	if disposition := resp.Header.Get("Content-Disposition"); disposition != "" {
		if _, params, err := mime.ParseMediaType(disposition); err == nil {
			if name := params["filename"]; name != "" {
				// Never let a server-provided name escape the target directory.
				if clean := filepath.Base(name); clean != "." && clean != string(filepath.Separator) {
					return clean
				}
			}
		}
	}
	if fallback != "" {
		return fallback
	}
	return "download.bin"
}

// SanitizeFilename turns an arbitrary identifier into a safe file name.
func SanitizeFilename(name string) string {
	cleaned := strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '_'
		default:
			return r
		}
	}, name)
	if cleaned = strings.TrimSpace(cleaned); cleaned == "" {
		return "download"
	}
	return cleaned
}
