package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

// fakeServer stands in for the async command channel: an SSE stream, the
// command endpoint, and the state endpoint used for reconciliation.
type fakeServer struct {
	server     *httptest.Server
	frames     chan string
	posted     chan Command
	streamOpen chan struct{}
	// postedBeforeStream records the ordering violation the protocol forbids:
	// a command accepted before the stream exists loses its result.
	postedBeforeStream bool
	taskStatus         string
	enqueue            func(Command) EnqueueResponse
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	fake := &fakeServer{
		frames:     make(chan string, 32),
		posted:     make(chan Command, 8),
		streamOpen: make(chan struct{}),
		taskStatus: "completed",
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/ai/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(fake.streamOpen)

		for {
			select {
			case <-r.Context().Done():
				return
			case frame, ok := <-fake.frames:
				if !ok {
					return
				}
				_, _ = io.WriteString(w, frame)
				w.(http.Flusher).Flush()
			}
		}
	})

	mux.HandleFunc("/api/ai/message", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-fake.streamOpen:
		default:
			fake.postedBeforeStream = true
		}

		var command Command
		_ = json.NewDecoder(r.Body).Decode(&command)
		fake.posted <- command

		response := EnqueueResponse{Success: true, Queued: true, TaskID: "task-1", CommandID: "cmd-1", Protocol: 2}
		if fake.enqueue != nil {
			response = fake.enqueue(command)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": response})
	})

	mux.HandleFunc("/api/ai/state", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"data": map[string]any{
				"currentTaskItem": map[string]any{
					"id":          r.URL.Query().Get("taskId"),
					"task_status": fake.taskStatus,
				},
			},
		})
	})

	fake.server = httptest.NewServer(mux)
	t.Cleanup(func() {
		fake.server.Close()
	})
	return fake
}

func (f *fakeServer) runner() *Runner {
	return NewRunner(client.NewAnonymous(f.server.URL))
}

// script pushes frames once the command has been posted, mimicking a Worker
// that only starts after it picks the command off the queue.
//
// It runs on its own goroutine, so it reports problems with Errorf rather than
// Fatal: Fatal only stops the goroutine it is called from, which would leave
// the test hanging on the stream instead of failing.
func (f *fakeServer) script(t *testing.T, frames ...string) Command {
	select {
	case command := <-f.posted:
		for _, frame := range frames {
			f.frames <- frame
		}
		close(f.frames)
		return command
	case <-time.After(5 * time.Second):
		t.Errorf("the command was never posted")
		close(f.frames)
		return Command{}
	}
}

func frame(event string, data any) string {
	encoded, err := json.Marshal(data)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("event: %s\ndata: %s\n\n", event, encoded)
}

func sayFrame(say, text string) string {
	return frame("message.add", messageEvent{
		TaskID:  "task-1",
		Message: Message{TS: 1, Type: "say", Say: say, Text: text},
	})
}

func quietOptions() Options {
	return Options{Renderer: NewRenderer(true, false), Timeout: 10 * time.Second}
}

func TestNewTaskStreamsToCompletion(t *testing.T) {
	fake := newFakeServer(t)
	submit := map[string]any{
		"type": "dashboard_submit_result", "status": "accepted",
		"operation_type": "create", "dashboard_id": "dash-9",
		"title": "销售看板", "revision": 1,
	}

	go fake.script(t,
		frame("heartbeat", "ping"),
		sayFrame("text", "正在读取数据模型"),
		sayFrame("dashboard_read_result", `{"brief":"读取看板列表","dashboards":[{"id":"d1","title":"旧看板"}]}`),
		sayFrame("dashboard_submit_result", mustJSON(submit)),
		sayFrame("completion_result", "看板已创建"),
	)

	opts := quietOptions()
	opts.Text = "近 30 天 GMV"
	result, err := fake.runner().NewTask(opts)
	if err != nil {
		t.Fatalf("NewTask: %v", err)
	}

	if result.Stop != StopCompleted {
		t.Fatalf("stop: got %q", result.Stop)
	}
	if result.TaskID != "task-1" || result.CommandID != "cmd-1" {
		t.Fatalf("identity: got %+v", result)
	}
	if result.Summary != "看板已创建" {
		t.Fatalf("summary: got %q", result.Summary)
	}
	if result.TaskStatus != "completed" {
		t.Fatalf("reconciled status: got %q", result.TaskStatus)
	}

	last := result.LastSubmit()
	if last == nil || last.DashboardID != "dash-9" || last.Revision != 1 {
		t.Fatalf("submit: got %+v", last)
	}
	if len(result.DashboardReads) != 1 || len(result.DashboardReads[0].Dashboards) != 1 {
		t.Fatalf("reads: got %+v", result.DashboardReads)
	}
}

// The stream must be live before the command is posted, or a fast command can
// finish before anyone is listening and its result is lost for good.
func TestCommandIsPostedOnlyAfterStreamIsLive(t *testing.T) {
	fake := newFakeServer(t)
	go fake.script(t, sayFrame("completion_result", "done"))

	opts := quietOptions()
	opts.Text = "hello"
	if _, err := fake.runner().NewTask(opts); err != nil {
		t.Fatalf("NewTask: %v", err)
	}
	if fake.postedBeforeStream {
		t.Fatal("the command was posted before the event stream was established")
	}
}

func TestNewTaskSendsProtocolAndIdempotencyKey(t *testing.T) {
	fake := newFakeServer(t)
	commands := make(chan Command, 1)
	go func() { commands <- fake.script(t, sayFrame("completion_result", "done")) }()

	opts := quietOptions()
	opts.Text = "hello"
	opts.Mode = "act"
	opts.Model = "openai/gpt-4o"
	opts.Databases = []string{"db_1"}
	result, err := fake.runner().NewTask(opts)
	if err != nil {
		t.Fatalf("NewTask: %v", err)
	}

	command := <-commands
	if command.ProtocolVersion != 2 {
		t.Fatalf("protocolVersion: got %d", command.ProtocolVersion)
	}
	if command.ClientOpID == "" || command.ClientOpID != result.ClientOpID {
		t.Fatalf("clientOperationId: command %q, result %q", command.ClientOpID, result.ClientOpID)
	}
	if command.ConnID == "" || command.ConnID != result.ConnID {
		t.Fatalf("connId: command %q, result %q", command.ConnID, result.ConnID)
	}
	if command.APIProvider != "openai" || command.APIModelID != "gpt-4o" {
		t.Fatalf("model: got %q/%q", command.APIProvider, command.APIModelID)
	}
	if command.ChatSettings == nil || command.ChatSettings.Mode != "act" {
		t.Fatalf("chatSettings: got %+v", command.ChatSettings)
	}
	if len(command.DatabaseIDs) != 1 || command.DatabaseIDs[0] != "db_1" {
		t.Fatalf("databaseIds: got %#v", command.DatabaseIDs)
	}
}

// A non-interactive run stops at the first question instead of guessing, so CI
// behaviour stays predictable.
func TestAskStopsNonInteractiveRun(t *testing.T) {
	fake := newFakeServer(t)
	fake.taskStatus = "waiting"
	go fake.script(t,
		sayFrame("text", "需要确认口径"),
		frame("message.add", messageEvent{
			TaskID:  "task-1",
			Message: Message{TS: 2, Type: "ask", Ask: "followup", Text: "用自然月还是滚动 30 天？"},
		}),
	)

	opts := quietOptions()
	opts.Text = "建个看板"
	result, err := fake.runner().NewTask(opts)
	if err != nil {
		t.Fatalf("NewTask: %v", err)
	}
	if result.Stop != StopAsk {
		t.Fatalf("stop: got %q", result.Stop)
	}
	if result.Ask == nil || result.Ask.Ask != "followup" {
		t.Fatalf("ask: got %+v", result.Ask)
	}
	if result.TaskStatus != "waiting" {
		t.Fatalf("reconciled status: got %q", result.TaskStatus)
	}
}

// An ask that is still streaming is not a question yet; stopping on it would
// cut the run short mid-sentence.
func TestPartialAskDoesNotStopRun(t *testing.T) {
	fake := newFakeServer(t)
	go fake.script(t,
		frame("message.partial", messageEvent{
			TaskID:  "task-1",
			Message: Message{TS: 2, Type: "ask", Ask: "followup", Text: "用自然", Partial: true},
		}),
		sayFrame("completion_result", "done"),
	)

	opts := quietOptions()
	opts.Text = "建个看板"
	result, err := fake.runner().NewTask(opts)
	if err != nil {
		t.Fatalf("NewTask: %v", err)
	}
	if result.Stop != StopCompleted {
		t.Fatalf("stop: got %q, want the run to continue past a partial ask", result.Stop)
	}
}

func TestFailedCommandStateEndsRun(t *testing.T) {
	fake := newFakeServer(t)
	fake.taskStatus = "error"
	commands := make(chan Command, 1)

	go func() {
		command := <-fake.posted
		commands <- command
		// A command.state for somebody else's operation shares this stream and
		// must not end our run.
		fake.frames <- frame("command.state", commandStateEvent{
			TaskID: "task-1", CommandID: "other", ClientOpID: "someone-else", State: "failed",
			Error: "not our command",
		})
		fake.frames <- sayFrame("text", "still working")
		fake.frames <- frame("command.state", commandStateEvent{
			TaskID: "task-1", CommandID: "cmd-1", ClientOpID: command.ClientOpID,
			State: "failed", Error: "worker crashed",
		})
		close(fake.frames)
	}()

	opts := quietOptions()
	opts.Text = "hello"
	result, err := fake.runner().NewTask(opts)
	if err != nil {
		t.Fatalf("NewTask: %v", err)
	}
	if result.Stop != StopFailed {
		t.Fatalf("stop: got %q", result.Stop)
	}
	if result.Error != "worker crashed" {
		t.Fatalf("error: got %q", result.Error)
	}
}

func TestRejectedSubmitKeepsErrors(t *testing.T) {
	fake := newFakeServer(t)
	rejected := map[string]any{
		"status":         "rejected",
		"operation_type": "create",
		"errors": []map[string]string{
			{"path": "widgets[0].query_id", "code": "UNKNOWN_QUERY", "message": "query q_x does not exist"},
			{"path": "filters[1]", "code": "BAD_NAME", "message": "filter name must be snake_case"},
		},
	}
	go fake.script(t,
		sayFrame("dashboard_submit_result", mustJSON(rejected)),
		sayFrame("completion_result", "提交被拒"),
	)

	opts := quietOptions()
	opts.Text = "建个看板"
	result, err := fake.runner().NewTask(opts)
	if err != nil {
		t.Fatalf("NewTask: %v", err)
	}

	submit := result.LastSubmit()
	if submit == nil || submit.Status != "rejected" || len(submit.Errors) != 2 {
		t.Fatalf("submit: got %+v", submit)
	}
	if submit.Errors[0].Code != "UNKNOWN_QUERY" {
		t.Fatalf("first error: got %+v", submit.Errors[0])
	}
}

// A queued command reports rejection in the body, not the HTTP status.
func TestUnsuccessfulEnqueueIsAnError(t *testing.T) {
	fake := newFakeServer(t)
	fake.enqueue = func(Command) EnqueueResponse {
		return EnqueueResponse{Success: false, Error: "taskId and connId are required"}
	}
	go func() {
		<-fake.posted
		close(fake.frames)
	}()

	opts := quietOptions()
	opts.Text = "hello"
	if _, err := fake.runner().NewTask(opts); err == nil {
		t.Fatal("expected an error")
	} else if cliexit.CodeOf(err) != cliexit.CodeBusiness {
		t.Fatalf("got exit code %d: %v", cliexit.CodeOf(err), err)
	}
}

func TestReplyRequiresTaskAndMessage(t *testing.T) {
	runner := NewRunner(client.NewAnonymous("http://127.0.0.1:1"))

	if _, err := runner.Reply(Options{Text: "hi"}); cliexit.CodeOf(err) != cliexit.CodeUsage {
		t.Fatalf("missing task id: got %v", err)
	}
	if _, err := runner.Reply(Options{TaskID: "t1"}); cliexit.CodeOf(err) != cliexit.CodeUsage {
		t.Fatalf("missing message: got %v", err)
	}
	// An approval needs no message text.
	if _, err := runner.Reply(Options{TaskID: "t1", AskReply: AskResponseYes, Timeout: time.Second}); cliexit.CodeOf(err) == cliexit.CodeUsage {
		t.Fatalf("an approval should not be a usage error: %v", err)
	}
}

// The server rejects a provider without a model and a subagent model that is
// neither inherited nor specified; both are caught before the round trip.
func TestApplyModelValidatesPairs(t *testing.T) {
	var command Command
	if err := applyModel(&command, Options{Model: "openai"}); cliexit.CodeOf(err) != cliexit.CodeUsage {
		t.Fatalf("provider without model: got %v", err)
	}

	command = Command{}
	if err := applyModel(&command, Options{SubAgent: "inherit"}); err != nil {
		t.Fatalf("inherit: %v", err)
	}
	if command.SubAgentInherit == nil || !*command.SubAgentInherit {
		t.Fatalf("inherit flag: got %+v", command.SubAgentInherit)
	}

	command = Command{}
	if err := applyModel(&command, Options{SubAgent: "anthropic/claude"}); err != nil {
		t.Fatalf("explicit subagent: %v", err)
	}
	if command.SubAgentInherit == nil || *command.SubAgentInherit {
		t.Fatal("an explicit subagent model must set inheritance to false")
	}
	if command.SubAgentAPIProvider != "anthropic" || command.SubAgentAPIModelID != "claude" {
		t.Fatalf("subagent model: got %q/%q", command.SubAgentAPIProvider, command.SubAgentAPIModelID)
	}

	// Absent flags must leave the model fields unset so the server keeps using
	// the account's configuration.
	command = Command{}
	if err := applyModel(&command, Options{}); err != nil {
		t.Fatalf("no model flags: %v", err)
	}
	if command.APIProvider != "" || command.SubAgentInherit != nil {
		t.Fatalf("unset flags must not populate the command: %+v", command)
	}
}

func TestNewTaskRejectsUnknownMode(t *testing.T) {
	runner := NewRunner(client.NewAnonymous("http://127.0.0.1:1"))
	_, err := runner.NewTask(Options{Text: "hi", Mode: "turbo"})
	if cliexit.CodeOf(err) != cliexit.CodeUsage {
		t.Fatalf("got %v", err)
	}
}

func mustJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}
