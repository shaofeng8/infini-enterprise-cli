package client

import (
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

// Upload posts a file as multipart/form-data.
//
// The body is streamed through a pipe rather than assembled in memory, because
// data source and knowledge base uploads are routinely hundreds of megabytes.
func (c *Client) Upload(path, fieldName, filePath string, fields map[string]string) (json.RawMessage, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, cliexit.Usage("cannot read %q: %v", filePath, err)
	}
	defer file.Close()

	reader, writer := io.Pipe()
	form := multipart.NewWriter(writer)

	go func() {
		// CloseWithError is what surfaces a mid-stream read failure to the
		// request instead of sending a silently truncated body.
		var writeErr error
		defer func() { _ = writer.CloseWithError(writeErr) }()

		for key, value := range fields {
			if writeErr = form.WriteField(key, value); writeErr != nil {
				return
			}
		}
		var part io.Writer
		if part, writeErr = form.CreateFormFile(fieldName, filepath.Base(filePath)); writeErr != nil {
			return
		}
		if _, writeErr = io.Copy(part, file); writeErr != nil {
			return
		}
		writeErr = form.Close()
	}()

	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, reader)
	if err != nil {
		return nil, cliexit.New(cliexit.CodeUsage, "cannot build request: %v", err)
	}
	c.applyHeaders(req)
	req.Header.Set("Content-Type", form.FormDataContentType())

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, transportError(http.MethodPost, c.baseURL+path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, transportError(http.MethodPost, c.baseURL+path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, httpStatusError(http.MethodPost, path, resp.StatusCode, raw)
	}
	return unwrap(raw)
}
