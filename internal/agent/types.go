// Package agent drives the asynchronous agent command channel.
//
// POST /api/ai/message does not execute anything: it writes a row into
// ai_task_command, enqueues a wakeup, and returns. The Worker holding the
// task's lease executes the command, and results arrive only over SSE. So every
// command follows the same five steps:
//
//  1. mint a clientOperationId (idempotency key for HTTP retries)
//  2. open the SSE stream and wait until it is established
//  3. POST the command
//  4. consume events until a terminal condition
//  5. reconcile against GET /api/ai/state, which is the source of truth
//
// Subscribing before posting is not an optimization: the command can finish
// before a later subscriber attaches, and the result would be lost.
package agent

import "encoding/json"

// ChatModes are the modes accepted by the server; graph and fast can only be
// chosen when a task is created.
var ChatModes = []string{"act", "plan", "graph", "fast"}

// CreateOnlyChatModes cannot be switched into on an existing task.
var CreateOnlyChatModes = []string{"graph", "fast"}

// Command is the subset of WebviewMessage the CLI sends.
type Command struct {
	Type            string        `json:"type"`
	ProtocolVersion int           `json:"protocolVersion,omitempty"`
	ClientOpID      string        `json:"clientOperationId,omitempty"`
	Text            string        `json:"text,omitempty"`
	TaskID          string        `json:"taskId,omitempty"`
	ConnID          string        `json:"connId,omitempty"`
	AskResponse     string        `json:"askResponse,omitempty"`
	ChatSettings    *ChatSettings `json:"chatSettings,omitempty"`
	APIProvider     string        `json:"apiProvider,omitempty"`
	APIModelID      string        `json:"apiModelId,omitempty"`

	// The resource lists are pointers to slices because the server reads an
	// absent field as "leave this alone" and an empty array as "clear it".
	DatabaseIDs *[]string `json:"databaseIds,omitempty"`
	RagIDs      *[]string `json:"ragIds,omitempty"`
	ProjectIDs  *[]string `json:"projectIds,omitempty"`

	// SubAgentInherit is a pointer because the server distinguishes "not
	// updating the subagent model" from "explicitly set to false", and rejects
	// the latter without an explicit subagent model.
	SubAgentInherit     *bool  `json:"subAgentModelInheritMain,omitempty"`
	SubAgentAPIProvider string `json:"subAgentApiProvider,omitempty"`
	SubAgentAPIModelID  string `json:"subAgentApiModelId,omitempty"`

	// SessionID addresses a shell session (stopShellSession) or a browser
	// session (browserTakeOver / browserResume / browserStop).
	SessionID       string `json:"sessionId,omitempty"`
	ToolExecutionID string `json:"toolExecutionId,omitempty"`
	// SnapshotTs is the ts of the user message to rewind to.
	SnapshotTs int64 `json:"snapshotTs,omitempty"`
	// EngineID is a pointer so that clearing the engine (empty string) is
	// distinguishable from not touching it.
	EngineID   *string      `json:"engineId,omitempty"`
	ToolParams *[]ToolParam `json:"toolParams,omitempty"`

	AutoApproval *AutoApprovalSettings `json:"autoApprovalSettings,omitempty"`

	Summary string `json:"summary,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// ToolParam carries per-task YAML for one installed external tool.
type ToolParam struct {
	ToolID string `json:"tool_id"`
	YAML   string `json:"yaml"`
}

type ChatSettings struct {
	Mode                   string `json:"mode,omitempty"`
	ReasoningHistoryPolicy string `json:"reasoningHistoryPolicy,omitempty"`
	ThemeMode              string `json:"themeMode,omitempty"`
}

// ReasoningHistoryPolicies decide how much reasoning is replayed to the model.
var ReasoningHistoryPolicies = []string{"last-only", "keep-all"}

// EnqueueResponse is what POST /api/ai/message returns. `queued` being true
// means the command was accepted, not that it ran.
type EnqueueResponse struct {
	Success   bool   `json:"success"`
	TaskID    string `json:"taskId,omitempty"`
	Queued    bool   `json:"queued"`
	CommandID string `json:"commandId,omitempty"`
	Protocol  int    `json:"protocolVersion,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Message mirrors InfiniMessage, one entry of the conversation.
type Message struct {
	TS        int64  `json:"ts"`
	Type      string `json:"type"` // say | ask
	Say       string `json:"say,omitempty"`
	Ask       string `json:"ask,omitempty"`
	Text      string `json:"text,omitempty"`
	Reasoning string `json:"reasoning,omitempty"`
	Partial   bool   `json:"partial,omitempty"`
}

// messageEvent is the payload of message.add / message.update / message.partial.
type messageEvent struct {
	TaskID  string  `json:"taskId"`
	Message Message `json:"message"`
}

// commandStateEvent is the payload of command.state. It is how a command that
// fails before producing any conversation output becomes visible.
type commandStateEvent struct {
	TaskID     string `json:"taskId"`
	CommandID  string `json:"commandId"`
	ClientOpID string `json:"clientOperationId"`
	State      string `json:"state"` // queued | processing | completed | failed
	Error      string `json:"error,omitempty"`
}

type notificationEvent struct {
	Type    string `json:"type"`
	Title   string `json:"title"`
	Message string `json:"message"`
}

// stateReadyEvent tells the client its cached state is stale; the CLI answers
// by fetching GET /api/ai/state.
type stateReadyEvent struct {
	TaskID       string `json:"taskId"`
	ForceReplace bool   `json:"forceReplace,omitempty"`
}

// TaskState is the part of ExtensionState the CLI reconciles against.
type TaskState struct {
	CurrentTaskItem struct {
		ID     string `json:"id"`
		Task   string `json:"task"`
		Status string `json:"task_status"`
	} `json:"currentTaskItem"`
	InfiniMessages []Message `json:"infiniMessages"`
}

// Ask is a question the agent is blocked on.
type Ask struct {
	Ask  string `json:"ask"`
	Text string `json:"text,omitempty"`
}

// Ask response values accepted by the server.
const (
	AskResponseMessage = "messageResponse"
	AskResponseYes     = "yesButtonClicked"
	AskResponseNo      = "noButtonClicked"
)

// Stop reasons for a run.
const (
	StopCompleted   = "completed"
	StopAsk         = "ask"
	StopFailed      = "failed"
	StopTimeout     = "timeout"
	StopInterrupted = "interrupted"
)

// Result is the outcome of one command run.
type Result struct {
	TaskID     string    `json:"taskId"`
	ConnID     string    `json:"connId"`
	CommandID  string    `json:"commandId,omitempty"`
	ClientOpID string    `json:"clientOperationId"`
	Stop       string    `json:"stop"`
	Ask        *Ask      `json:"ask,omitempty"`
	TaskStatus string    `json:"taskStatus,omitempty"`
	Summary    string    `json:"summary,omitempty"`
	Error      string    `json:"error,omitempty"`
	Events     int       `json:"events"`
	Messages   []Message `json:"messages,omitempty"`

	// DashboardSubmits and DashboardReads capture the dashboard tool calls the
	// agent made, so `dash new` can report the created dashboard without the
	// caller re-parsing the transcript.
	DashboardSubmits []DashboardSubmit `json:"dashboardSubmits,omitempty"`
	DashboardReads   []DashboardRead   `json:"dashboardReads,omitempty"`
}

// LastSubmit returns the final dashboard_submit_result, which is the one that
// decided the outcome.
func (r *Result) LastSubmit() *DashboardSubmit {
	if len(r.DashboardSubmits) == 0 {
		return nil
	}
	return &r.DashboardSubmits[len(r.DashboardSubmits)-1]
}

// DashboardSubmit is the payload of a dashboard_submit_result message.
type DashboardSubmit struct {
	Status        string           `json:"status"` // accepted | rejected
	OperationType string           `json:"operation_type"`
	DashboardID   string           `json:"dashboard_id,omitempty"`
	Title         string           `json:"title,omitempty"`
	Revision      int              `json:"revision,omitempty"`
	URL           string           `json:"url,omitempty"`
	Brief         string           `json:"brief,omitempty"`
	Errors        []DashboardError `json:"errors,omitempty"`
	Raw           json.RawMessage  `json:"-"`
}

type DashboardError struct {
	Path    string `json:"path,omitempty"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// DashboardRead is the payload of a dashboard_read_result message.
type DashboardRead struct {
	Brief     string `json:"brief,omitempty"`
	Dashboard *struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Revision int    `json:"revision"`
		SpecHash string `json:"spec_hash"`
	} `json:"dashboard,omitempty"`
	Dashboards []struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		UpdatedAt string `json:"updated_at"`
	} `json:"dashboards,omitempty"`
}
