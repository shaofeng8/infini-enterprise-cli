// Package client wraps the Infini REST surface.
//
// Two base URLs matter and they are not interchangeable:
//   - server:  the Infini app backend, everything under /api
//   - console: the auth/proxy service, which issues JWTs and owns user/model data
package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/config"
)

// Tunables shared by every request; set once from global flags.
var (
	Timeout = 100 * time.Second
	Verbose bool
	Trace   bool
)

type Client struct {
	baseURL string
	token   string
	lang    string
	http    *http.Client
}

// BearerToken normalizes a credential into an Authorization header value.
func BearerToken(token string) string {
	if token == "" || strings.HasPrefix(token, "Bearer ") {
		return token
	}
	return "Bearer " + token
}

// New builds a client against the Infini app backend.
func New() (*Client, error) {
	server := config.Server()
	if server == "" {
		return nil, cliexit.Hint(
			cliexit.New(cliexit.CodeUsage, "server is not configured"),
			"run `%s config set server <url>` or pass --server", config.AppName,
		)
	}
	token, _ := config.Credential()
	if token == "" {
		return nil, cliexit.Hint(
			cliexit.New(cliexit.CodeAuth, "no credential configured"),
			"run `%s auth login` or pass --api-key", config.AppName,
		)
	}
	return newClient(server, token), nil
}

// NewConsole builds a client against the auth/proxy service.
func NewConsole() (*Client, error) {
	console := config.Console()
	if console == "" {
		return nil, cliexit.Hint(
			cliexit.New(cliexit.CodeUsage, "console (auth/proxy) URL is not configured"),
			"run `%s auth mode` to discover it, or pass --console", config.AppName,
		)
	}
	token, _ := config.Credential()
	return newClient(console, token), nil
}

// NewAnonymous targets a base URL without requiring a credential, for public
// endpoints such as /api/license/status.
func NewAnonymous(baseURL string) *Client {
	return newClient(baseURL, "")
}

func newClient(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		lang:    config.PreferLanguage(),
		http:    &http.Client{Timeout: Timeout},
	}
}

func (c *Client) BaseURL() string { return c.baseURL }
func (c *Client) Token() string   { return c.token }

func (c *Client) newRequest(method, path string, body any) (*http.Request, error) {
	var reader io.Reader
	var payload []byte
	if body != nil {
		if raw, ok := body.(json.RawMessage); ok {
			payload = raw
		} else {
			encoded, err := json.Marshal(body)
			if err != nil {
				return nil, cliexit.New(cliexit.CodeUsage, "cannot serialize request body: %v", err)
			}
			payload = encoded
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequest(method, c.baseURL+path, reader)
	if err != nil {
		return nil, cliexit.New(cliexit.CodeUsage, "cannot build request for %s %s: %v", method, path, err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", BearerToken(c.token))
	}
	req.Header.Set("Content-Type", "application/json")
	if c.lang != "" {
		req.Header.Set("x-lang", c.lang)
	}

	if Verbose || Trace {
		fmt.Fprintf(os.Stderr, "> %s %s%s\n", method, c.baseURL, path)
	}
	if Trace && len(payload) > 0 {
		fmt.Fprintf(os.Stderr, "> body: %s\n", redact(payload))
	}
	return req, nil
}

// envelope matches the server's global response wrapper. Code is a pointer so
// unwrapped payloads (arrays, plain objects, @Bypass routes) are distinguishable
// from a genuine `"code": 0`.
type envelope struct {
	Code    *int            `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// Do performs a request and unwraps the response envelope.
func (c *Client) Do(method, path string, body any) (json.RawMessage, error) {
	resp, raw, err := c.doRaw(method, path, body)
	if err != nil {
		return nil, err
	}

	if Trace {
		fmt.Fprintf(os.Stderr, "< HTTP %d: %s\n", resp.StatusCode, truncate(string(raw), 4096))
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, httpStatusError(method, path, resp.StatusCode, raw)
	}

	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil || env.Code == nil {
		// Not an envelope; hand the body back untouched.
		return raw, nil
	}
	if *env.Code != 200 {
		return nil, apiCodeError(*env.Code, env.Message)
	}
	return env.Data, nil
}

func (c *Client) doRaw(method, path string, body any) (*http.Response, []byte, error) {
	req, err := c.newRequest(method, path, body)
	if err != nil {
		return nil, nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, transportError(method, c.baseURL+path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, cliexit.New(cliexit.CodeNetwork, "cannot read response from %s %s: %v", method, path, err)
	}
	return resp, raw, nil
}

func (c *Client) Get(path string, params map[string]string) (json.RawMessage, error) {
	return c.Do(http.MethodGet, WithQuery(path, params), nil)
}

func (c *Client) Post(path string, body any) (json.RawMessage, error) {
	return c.Do(http.MethodPost, path, body)
}

func (c *Client) Put(path string, body any) (json.RawMessage, error) {
	return c.Do(http.MethodPut, path, body)
}

func (c *Client) Patch(path string, body any) (json.RawMessage, error) {
	return c.Do(http.MethodPatch, path, body)
}

func (c *Client) Delete(path string, body any) (json.RawMessage, error) {
	return c.Do(http.MethodDelete, path, body)
}

// RawResponse exposes the untouched status code and body, for the `api` escape
// hatch and for binary downloads.
func (c *Client) RawResponse(method, path string, body any) (int, []byte, error) {
	resp, raw, err := c.doRaw(method, path, body)
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, raw, nil
}

// WithQuery appends non-empty query parameters to a path.
func WithQuery(path string, params map[string]string) string {
	if len(params) == 0 {
		return path
	}
	values := url.Values{}
	for k, v := range params {
		if v != "" {
			values.Set(k, v)
		}
	}
	encoded := values.Encode()
	if encoded == "" {
		return path
	}
	if strings.Contains(path, "?") {
		return path + "&" + encoded
	}
	return path + "?" + encoded
}

// Server-side error codes worth a distinct exit code or hint. Values come from
// packages/server/src/constants/error-code.constant.ts.
var (
	authCodes = map[int]string{
		1101: "login invalid or token expired",
		1102: "no permission for this resource",
		1103: "only an admin can perform this operation",
		1105: "account logged in elsewhere, token invalidated",
		1106: "guest accounts cannot perform this operation",
	}
	licenseCodes = map[int]string{
		3400: "license missing",
		3401: "license expired",
		3402: "trial expired",
		3403: "license invalid",
		3410: "license user limit reached",
		3411: "license concurrent task limit reached",
		3412: "license token quota exhausted",
		3413: "license task quota exhausted",
		3414: "feature disabled by license",
	}
)

func apiCodeError(code int, message string) error {
	if message == "" {
		message = "request rejected"
	}
	if detail, ok := authCodes[code]; ok {
		return cliexit.Hint(
			cliexit.New(cliexit.CodeAuth, "%s (code %d: %s)", message, code, detail),
			"run `%s auth login` to refresh the credential", config.AppName,
		)
	}
	if detail, ok := licenseCodes[code]; ok {
		return cliexit.Hint(
			cliexit.New(cliexit.CodeLicense, "%s (code %d: %s)", message, code, detail),
			"run `%s license limits` to inspect quota usage", config.AppName,
		)
	}
	if code == 1201 {
		return cliexit.Hint(
			cliexit.New(cliexit.CodeBusiness, "%s (code %d: too many requests)", message, code),
			"retry with a lower request rate",
		)
	}
	return cliexit.New(cliexit.CodeBusiness, "%s (code %d)", message, code)
}

func httpStatusError(method, path string, status int, raw []byte) error {
	// An error envelope carries a more precise code than the HTTP status.
	var env envelope
	if err := json.Unmarshal(raw, &env); err == nil && env.Code != nil && *env.Code != 200 {
		return apiCodeError(*env.Code, env.Message)
	}

	body := truncate(strings.TrimSpace(string(raw)), 800)
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return cliexit.Hint(
			cliexit.New(cliexit.CodeAuth, "%s %s: HTTP %d: %s", method, path, status, body),
			"run `%s auth login` to refresh the credential", config.AppName,
		)
	case status >= 500:
		return cliexit.New(cliexit.CodeNetwork, "%s %s: HTTP %d: %s", method, path, status, body)
	default:
		return cliexit.New(cliexit.CodeBusiness, "%s %s: HTTP %d: %s", method, path, status, body)
	}
}

func transportError(method, target string, err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Timeout() {
		return cliexit.Hint(
			cliexit.New(cliexit.CodeNetwork, "%s %s timed out", method, target),
			"raise --timeout or check whether the server is reachable",
		)
	}
	return cliexit.Hint(
		cliexit.New(cliexit.CodeNetwork, "%s %s failed: %v", method, target, err),
		"verify the --server URL and network connectivity",
	)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}

// redact strips credential-looking values before tracing a request body.
func redact(payload []byte) string {
	var parsed map[string]any
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return truncate(string(payload), 4096)
	}
	for _, key := range []string{"password", "oldPassword", "newPassword", "api-key", "apiKey", "token", "access_token", "openAiApiKey"} {
		if _, ok := parsed[key]; ok {
			parsed[key] = "****"
		}
	}
	out, err := json.Marshal(parsed)
	if err != nil {
		return truncate(string(payload), 4096)
	}
	return truncate(string(out), 4096)
}
