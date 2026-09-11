// Package browser drives the browser the agent uses.
//
// The browser is not in this deployment. It is a Chrome extension connected
// over a websocket, so every command here is a message relayed to whatever
// browser your account currently has attached — and fails harmlessly when
// nothing is attached.
//
// Two things about this endpoint set are worth knowing before reading the
// rest of the package:
//
// The dedicated per-action routes (POST /action/navigate and friends) hardcode
// the session id to "test-browser" and accept no timeout. Only the generic
// POST /action honours session_id and timeout_ms, and only it can reach the
// console actions. So the generic route is a strict superset and everything
// here goes through it; Direct exists for parity with the dedicated routes.
//
// A disconnected browser is reported as HTTP 200 with {"success": false}, not
// as an error status. The caller has to read the body to know whether anything
// happened, which is why Send inspects success rather than trusting the
// status code.
package browser

import (
	"encoding/json"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

const base = "/api/ai_browser"

// ActionTypes the generic endpoint dispatches. The two console actions have
// no dedicated route.
var ActionTypes = []string{
	"navigate", "click", "input", "scroll", "press_key",
	"find_keyword", "view", "move_mouse",
	"browser_console_exec", "browser_console_view",
}

// ScrollDirections and ScrollTargets constrain a scroll action.
var (
	ScrollDirections = []string{"up", "down", "left", "right"}
	ScrollTargets    = []string{"page", "container"}
)

// DefaultSessionID is what the server falls back to, and what the dedicated
// routes always use. Sharing a session id means sharing a browser tab.
const DefaultSessionID = "test-browser"

// MaxTimeoutMs is the server's ceiling; the timeout can only be shortened.
const MaxTimeoutMs = 60000

type API struct {
	c *client.Client
}

func NewAPI(c *client.Client) *API { return &API{c: c} }

// SessionInfo describes an attached browser. A session found on another API
// instance is routed rather than held locally, which is why the fleet can
// report sessions this node has no socket for.
type SessionInfo struct {
	UID         string `json:"uid"`
	SessionID   string `json:"sessionId,omitempty"`
	InstanceID  string `json:"instanceId,omitempty"`
	Connected   bool   `json:"connected,omitempty"`
	ConnectedAt string `json:"connectedAt,omitempty"`
	UserAgent   string `json:"userAgent,omitempty"`
	Version     string `json:"version,omitempty"`
}

func (a *API) Sessions() ([]SessionInfo, error) {
	raw, err := a.c.Get(base+"/sessions", nil)
	if err != nil {
		return nil, err
	}
	var sessions []SessionInfo
	if err := json.Unmarshal(raw, &sessions); err != nil {
		return nil, cliexit.New(cliexit.CodeBusiness, "cannot parse session list: %v", err)
	}
	return sessions, nil
}

// Session reports one account's browser. An empty uid means the caller's own;
// the server answers null rather than 404 when nothing is attached.
func (a *API) Session(uid string) (json.RawMessage, error) {
	path := base + "/session"
	if uid != "" {
		path += "/" + uid
	}
	return a.c.Get(path, nil)
}

// Action is one instruction for the browser. Payload is left as raw JSON
// because its shape depends entirely on Type.
type Action struct {
	Type      string          `json:"action_type"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	TimeoutMs int             `json:"timeout_ms,omitempty"`
}

// Result is the envelope every browser action comes back in.
type Result struct {
	Success bool            `json:"success"`
	Error   string          `json:"error,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
}

// Send dispatches an action through the generic endpoint.
//
// A refusal arrives as HTTP 200 with success false, so the body is what
// decides the outcome. The most common refusal by far is no browser being
// attached, which is worth saying plainly rather than passing through as
// "Browser extension not connected".
func (a *API) Send(action Action) (*Result, error) {
	if err := CheckType(action.Type); err != nil {
		return nil, err
	}
	raw, err := a.c.Post(base+"/action", action)
	if err != nil {
		return nil, err
	}
	return readResult(raw)
}

// Direct uses a dedicated per-action route instead of the generic one.
//
// These routes exist for diagnostics and predate the generic endpoint. They
// ignore the session id and timeout, always acting on the default session, so
// prefer Send unless you are specifically testing one of these routes.
func (a *API) Direct(action string, payload any) (*Result, error) {
	raw, err := a.c.Post(base+"/action/"+action, payload)
	if err != nil {
		return nil, err
	}
	return readResult(raw)
}

func readResult(raw json.RawMessage) (*Result, error) {
	var result Result
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, cliexit.New(cliexit.CodeBusiness, "cannot parse action result: %v", err)
	}
	if !result.Success {
		message := result.Error
		if message == "" {
			message = "the browser refused the action"
		}
		err := cliexit.New(cliexit.CodeBusiness, "%s", message)
		if result.Error == "Browser extension not connected" {
			return &result, cliexit.Hint(err,
				"attach a browser first; `browser session` shows whether one is connected")
		}
		return &result, err
	}
	return &result, nil
}

// CheckType rejects an unknown action before it reaches the server, which
// would answer 200 with an "Unknown action type" body.
func CheckType(actionType string) error {
	for _, known := range ActionTypes {
		if actionType == known {
			return nil
		}
	}
	return cliexit.Hint(
		cliexit.Usage("unknown action type %q", actionType),
		"known types: %s", join(ActionTypes),
	)
}

func join(values []string) string {
	out := ""
	for i, value := range values {
		if i > 0 {
			out += ", "
		}
		out += value
	}
	return out
}
