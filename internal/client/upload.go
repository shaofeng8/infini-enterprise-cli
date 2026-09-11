package client

import (
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

// Stream sends a reader as the whole request body, with no multipart wrapper.
//
// The chunked upload endpoint pipes the request straight to a file, so a
// multipart envelope would be written into the chunk itself and corrupt the
// assembled result. Length matters as well: without it the request goes out
// chunked and the server has no size to check against.
func (c *Client) Stream(method, path string, body io.Reader, length int64) (json.RawMessage, error) {
	if err := auditPreflight(); err != nil {
		return nil, err
	}
	if DryRun {
		if err := auditRecord(method, path, 0, nil); err != nil {
			return nil, err
		}
		return dryRunResponse(method, path, map[string]any{"bytes": length})
	}

	req, err := http.NewRequest(method, c.baseURL+path, body)
	if err != nil {
		return nil, cliexit.New(cliexit.CodeUsage, "cannot build request: %v", err)
	}
	c.applyHeaders(req)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = length

	if Verbose || Trace {
		fmt.Fprintf(os.Stderr, "> %s %s%s (%d bytes)\n", method, c.baseURL, path, length)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		if auditErr := auditRecord(method, path, 0, err); auditErr != nil {
			return nil, auditErr
		}
		return nil, transportError(method, c.baseURL+path, err)
	}
	defer resp.Body.Close()
	if auditErr := auditRecord(method, path, resp.StatusCode, nil); auditErr != nil {
		return nil, auditErr
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, transportError(method, c.baseURL+path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, httpStatusError(method, path, resp.StatusCode, raw)
	}
	return unwrap(raw)
}

// Upload posts a file as multipart/form-data.
//
// The body is streamed through a pipe rather than assembled in memory, because
// data source and knowledge base uploads are routinely hundreds of megabytes.
//
// An empty filePath sends the fields alone, which some endpoints need: editing
// a local skill or tool takes the same multipart form whether or not a new
// archive comes with it.
func (c *Client) Upload(path, fieldName, filePath string, fields map[string]string) (json.RawMessage, error) {
	if err := auditPreflight(); err != nil {
		return nil, err
	}
	if DryRun {
		if err := auditRecord(http.MethodPost, path, 0, nil); err != nil {
			return nil, err
		}
		return dryRunResponse(http.MethodPost, path, map[string]any{
			"file":   filePath,
			"fields": fields,
		})
	}

	var file *os.File
	if filePath != "" {
		opened, err := os.Open(filePath)
		if err != nil {
			return nil, cliexit.Usage("cannot read %q: %v", filePath, err)
		}
		defer opened.Close()
		file = opened
	}

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
		if file != nil {
			var part io.Writer
			if part, writeErr = form.CreateFormFile(fieldName, filepath.Base(filePath)); writeErr != nil {
				return
			}
			if _, writeErr = io.Copy(part, file); writeErr != nil {
				return
			}
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
		if auditErr := auditRecord(http.MethodPost, path, 0, err); auditErr != nil {
			return nil, auditErr
		}
		return nil, transportError(http.MethodPost, c.baseURL+path, err)
	}
	defer resp.Body.Close()
	if auditErr := auditRecord(http.MethodPost, path, resp.StatusCode, nil); auditErr != nil {
		return nil, auditErr
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, transportError(http.MethodPost, c.baseURL+path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, httpStatusError(http.MethodPost, path, resp.StatusCode, raw)
	}
	return unwrap(raw)
}
