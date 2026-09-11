package agent

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

// Command types POST /api/ai/message accepts and something actually acts on.
//
// This list matters more than it looks. The endpoint takes its body as a bare
// interface type, so Nest's ValidationPipe has no metatype to check and the
// WEBVIEW_MESSAGE_TYPES enum in the DTO is Swagger documentation, not a
// runtime guard. An unrecognised type is accepted, queued, and then dropped by
// the worker's default branch with nothing but a server-side log line — the
// caller sees {"success": true, "queued": true} for a no-op. So the CLI
// validates the type itself rather than letting the server "accept" it.
const (
	TypeNewTask            = "newTask"
	TypeAskResponse        = "askResponse"
	TypeOptionsResponse    = "optionsResponse"
	TypeAutoResumeTask     = "autoResumeTask"
	TypeShowTaskWithID     = "showTaskWithId"
	TypeCancelTask         = "cancelTask"
	TypeClearTask          = "clearTask"
	TypeStopShellSession   = "stopShellSession"
	TypeStopToolExecution  = "stopToolExecution"
	TypeKillSubAgent       = "killSubAgent"
	TypeKillActiveJobs     = "killActiveJobs"
	TypeRollbackToSnapshot = "rollbackToSnapshot"
	TypeRollbackAndSend    = "rollbackAndSendMessage"
	TypeEditFirstAndResend = "editFirstMessageAndResend"
	TypeTogglePlanActMode  = "togglePlanActMode"
	TypeUpdateChatMode     = "updateChatMode"
	TypeUpdateResources    = "updateTaskResources"
	TypeUpdateToolParams   = "updateTaskToolParams"
	TypeUpdateEngine       = "updateTaskEngine"
	TypeAutoApproval       = "autoApprovalSettings"
	TypeUpdateSettings     = "updateSettings"
	TypeBrowserTakeOver    = "browserTakeOver"
	TypeBrowserResume      = "browserResume"
	TypeBrowserStop        = "browserStop"
)

// HandledTypes is every type the worker or the controller does something with.
//
// Six of these are absent from the DTO's documented enum but are handled all
// the same: autoResumeTask, showTaskWithId, updateSettings and the three
// browser ones. They are reachable because the body is never validated.
var HandledTypes = []string{
	TypeNewTask, TypeAskResponse, TypeOptionsResponse, TypeAutoResumeTask,
	TypeShowTaskWithID, TypeCancelTask, TypeClearTask, TypeStopShellSession,
	TypeStopToolExecution, TypeKillSubAgent, TypeKillActiveJobs,
	TypeRollbackToSnapshot, TypeRollbackAndSend, TypeEditFirstAndResend,
	TypeTogglePlanActMode, TypeUpdateChatMode, TypeUpdateResources,
	TypeUpdateToolParams, TypeUpdateEngine, TypeAutoApproval,
	TypeUpdateSettings, TypeBrowserTakeOver, TypeBrowserResume, TypeBrowserStop,
}

// StreamingTypes change the conversation, so their real result arrives over
// SSE rather than in the HTTP response.
var StreamingTypes = []string{
	TypeNewTask, TypeAskResponse, TypeOptionsResponse, TypeAutoResumeTask,
	TypeRollbackAndSend, TypeEditFirstAndResend,
}

// CheckType rejects a type nothing would act on, so the caller can fail before
// building a client and before anything is queued.
func CheckType(commandType string) error {
	if slices.Contains(HandledTypes, commandType) {
		return nil
	}
	return cliexit.Hint(
		cliexit.Usage("unknown command type %q", commandType),
		"the server would accept it and then silently drop it; known types: %s",
		strings.Join(HandledTypes, ", "),
	)
}

// Send posts a command and returns as soon as it is queued.
//
// Queued is not done: the worker holding the task's lease runs it afterwards.
// Commands whose effect is a state change (cancel, clear, resource updates)
// are fine with this; commands that produce conversation are not, and go
// through run instead.
func (r *Runner) Send(command Command) (*EnqueueResponse, error) {
	if err := CheckType(command.Type); err != nil {
		return nil, err
	}
	return r.enqueue(command)
}

// ClearAllResponse is what clearTask returns when no task id is given: the
// controller fans the request out into one command per active task, because a
// single command would only reach the one worker that consumed it.
type ClearAllResponse struct {
	Success    bool     `json:"success"`
	Queued     bool     `json:"queued"`
	CommandIDs []string `json:"commandIds"`
}

// ClearAll clears every task of the current user.
func (r *Runner) ClearAll() (*ClearAllResponse, error) {
	raw, err := r.post(Command{Type: TypeClearTask})
	if err != nil {
		return nil, err
	}
	var response ClearAllResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, cliexit.New(cliexit.CodeBusiness, "cannot parse clear response: %v", err)
	}
	return &response, nil
}

// Snapshot is a point a task can be rewound to.
type Snapshot struct {
	TS    int64  `json:"ts"`
	Kind  string `json:"kind"`
	Text  string `json:"text"`
	Index int    `json:"index"`
}

// Snapshots lists the rewind points of a task.
//
// There is no endpoint for this. The server creates a snapshot at each
// committed user turn and looks it up by exact ts, so the rewind points are
// the user messages in the conversation; anything else fails the lookup with
// "Snapshot <ts> not found". Deriving them here is what makes `rollback`
// usable without copying timestamps out of the web UI.
func (r *Runner) Snapshots(taskID string) ([]Snapshot, error) {
	if taskID == "" {
		return nil, cliexit.Usage("a task id is required to list snapshots")
	}
	state, err := r.FetchState(taskID)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, nil
	}

	var snapshots []Snapshot
	for _, message := range state.InfiniMessages {
		if message.Type != "say" {
			continue
		}
		// say=task is the opening prompt and say=user_feedback is every later
		// user turn; together they are the committed user turns.
		if message.Say != "task" && message.Say != "user_feedback" {
			continue
		}
		snapshots = append(snapshots, Snapshot{
			TS:    message.TS,
			Kind:  message.Say,
			Text:  message.Text,
			Index: len(snapshots),
		})
	}
	return snapshots, nil
}
