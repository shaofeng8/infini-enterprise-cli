package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

// An unknown type must never reach the server. The endpoint takes its body as
// an untyped object, so Nest validates nothing: the command would be queued
// and then dropped by the worker's default branch, and the caller would read
// {"queued": true} as success.
func TestSendRejectsUnknownTypeWithoutCallingTheServer(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"code":200,"data":{"success":true,"queued":true}}`))
	}))
	defer server.Close()

	runner := NewRunner(client.NewAnonymous(server.URL))
	_, err := runner.Send(Command{Type: "summary_task_start"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := cliexit.CodeOf(err); got != cliexit.CodeUsage {
		t.Fatalf("exit code: got %d, want %d", got, cliexit.CodeUsage)
	}
	if called {
		t.Fatal("the request should not have been sent")
	}
}

// The six types the DTO's enum omits are still handled by the worker, and are
// reachable precisely because the body is not validated.
func TestHandledTypesCoverTheUndocumentedWorkerCases(t *testing.T) {
	for _, undocumented := range []string{
		TypeAutoResumeTask, TypeShowTaskWithID, TypeUpdateSettings,
		TypeBrowserTakeOver, TypeBrowserResume, TypeBrowserStop,
	} {
		if !slices.Contains(HandledTypes, undocumented) {
			t.Fatalf("%s should be sendable", undocumented)
		}
	}
	for _, streaming := range StreamingTypes {
		if !slices.Contains(HandledTypes, streaming) {
			t.Fatalf("%s streams but is not in HandledTypes", streaming)
		}
	}
}

func TestSendPostsAKnownType(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"code":200,"data":{"success":true,"queued":true,"commandId":"c1"}}`))
	}))
	defer server.Close()

	runner := NewRunner(client.NewAnonymous(server.URL))
	response, err := runner.Send(Command{Type: TypeCancelTask, TaskID: "task-1"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !response.Queued || response.CommandID != "c1" {
		t.Fatalf("got %+v", response)
	}
	if body["type"] != TypeCancelTask || body["taskId"] != "task-1" {
		t.Fatalf("body: got %v", body)
	}
	// Every command carries the protocol version and an idempotency key, so an
	// HTTP retry cannot enqueue the work twice.
	if body["protocolVersion"] != float64(2) || body["clientOperationId"] == "" {
		t.Fatalf("body: got %v", body)
	}
}

// clearTask without a task id answers with a list of command ids, not the
// single-command envelope, because the controller fans it out per task.
func TestClearAllReturnsOneCommandPerTask(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"code":200,"data":{"success":true,"queued":true,"commandIds":["c1","c2"]}}`))
	}))
	defer server.Close()

	response, err := NewRunner(client.NewAnonymous(server.URL)).ClearAll()
	if err != nil {
		t.Fatalf("ClearAll: %v", err)
	}
	if len(response.CommandIDs) != 2 {
		t.Fatalf("got %+v", response)
	}
	if _, present := body["taskId"]; present {
		t.Fatalf("taskId must be absent to mean \"all\", got %v", body)
	}
}

func TestSnapshotsAreTheCommittedUserTurns(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":200,"data":{"infiniMessages":[
		  {"ts":1,"type":"say","say":"task","text":"统计上季度营收"},
		  {"ts":2,"type":"say","say":"text","text":"好的，我先看表结构"},
		  {"ts":3,"type":"ask","ask":"followup","text":"按自然月还是滚动 30 天？"},
		  {"ts":4,"type":"say","say":"user_feedback","text":"自然月"},
		  {"ts":5,"type":"say","say":"completion_result","text":"完成"}
		]}}`))
	}))
	defer server.Close()

	snapshots, err := NewRunner(client.NewAnonymous(server.URL)).Snapshots("task-1")
	if err != nil {
		t.Fatalf("Snapshots: %v", err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("got %+v", snapshots)
	}
	if snapshots[0].TS != 1 || snapshots[0].Kind != "task" {
		t.Fatalf("first: got %+v", snapshots[0])
	}
	if snapshots[1].TS != 4 || snapshots[1].Kind != "user_feedback" || snapshots[1].Index != 1 {
		t.Fatalf("second: got %+v", snapshots[1])
	}
}

func TestSnapshotsRequireATaskID(t *testing.T) {
	_, err := NewRunner(client.NewAnonymous("http://127.0.0.1:1")).Snapshots("")
	if cliexit.CodeOf(err) != cliexit.CodeUsage {
		t.Fatalf("got %v", err)
	}
}

// A partial update must not reset the capabilities it does not mention: the
// endpoint stores whatever object it is handed.
func TestAutoApprovalMergeKeepsUnsetFields(t *testing.T) {
	limit, budget := 500, 1000
	enabled, disabled := true, false
	current := AutoApprovalSettings{
		MaxRequests:         &budget,
		DatabaseReturnLimit: &limit,
		EnableBrowser:       &enabled,
		EnableSubAgent:      &enabled,
	}

	newLimit := 100
	merged := current.Merge(AutoApprovalSettings{
		DatabaseReturnLimit: &newLimit,
		EnableBrowser:       &disabled,
	})

	if merged.DatabaseReturnLimit == nil || *merged.DatabaseReturnLimit != 100 {
		t.Fatalf("databaseReturnLimit: got %v", merged.DatabaseReturnLimit)
	}
	if merged.EnableBrowser == nil || *merged.EnableBrowser {
		t.Fatalf("enableBrowser should be false, got %v", merged.EnableBrowser)
	}
	if merged.MaxRequests == nil || *merged.MaxRequests != 1000 {
		t.Fatalf("maxRequests should survive, got %v", merged.MaxRequests)
	}
	if merged.EnableSubAgent == nil || !*merged.EnableSubAgent {
		t.Fatalf("enableSubAgent should survive, got %v", merged.EnableSubAgent)
	}
	// Fields neither side set stay absent rather than becoming zero values.
	if merged.EnableShell != nil || merged.MaxSubAgentRequests != nil {
		t.Fatalf("untouched fields should stay nil, got %+v", merged)
	}
}

func TestAutoApprovalMarshalsOnlySetFields(t *testing.T) {
	enabled := true
	encoded, err := json.Marshal(AutoApprovalSettings{EnableWebSearch: &enabled})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(encoded) != `{"enableWebSearch":true}` {
		t.Fatalf("got %s", encoded)
	}
}

// graph and fast exist only at creation time; the server enforces this after
// the command is queued, which would surface as a dead run rather than an error.
func TestCheckSwitchableModeRejectsCreateOnlyModes(t *testing.T) {
	for _, mode := range CreateOnlyChatModes {
		if err := checkSwitchableMode(mode); err == nil {
			t.Fatalf("%s should be rejected", mode)
		} else if cliexit.CodeOf(err) != cliexit.CodeUsage {
			t.Fatalf("%s: got exit code %d", mode, cliexit.CodeOf(err))
		}
	}
	for _, mode := range []string{"act", "plan"} {
		if err := checkSwitchableMode(mode); err != nil {
			t.Fatalf("%s should be allowed: %v", mode, err)
		}
	}
	if err := checkSwitchableMode("turbo"); err == nil {
		t.Fatal("an unknown mode should be rejected")
	}
}

func TestReplyCarriesRuntimeChangesAndOmitsUnsetGroups(t *testing.T) {
	fake := newFakeServer(t)
	commands := make(chan Command, 1)
	go func() { commands <- fake.script(t, sayFrame("completion_result", "done")) }()

	engine := ""
	databases := []string{"db_1"}
	opts := quietOptions()
	opts.TaskID = "task-1"
	opts.Text = "继续"
	opts.Databases = databases
	opts.Engine = &engine

	if _, err := fake.runner().Reply(opts); err != nil {
		t.Fatalf("Reply: %v", err)
	}

	command := <-commands
	if command.Type != TypeAskResponse || command.AskResponse != AskResponseMessage {
		t.Fatalf("got %+v", command)
	}
	if command.DatabaseIDs == nil || len(*command.DatabaseIDs) != 1 {
		t.Fatalf("databaseIds: got %#v", command.DatabaseIDs)
	}
	// An empty engine id clears the binding, so it has to be sent rather than
	// omitted as a zero value.
	if command.EngineID == nil || *command.EngineID != "" {
		t.Fatalf("engineId should be present and empty, got %#v", command.EngineID)
	}
	if command.RagIDs != nil {
		t.Fatalf("an unset group must be omitted, got %#v", command.RagIDs)
	}
}
