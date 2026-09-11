package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/google/uuid"
)

// sseReadyTimeout bounds how long we wait for the event stream before giving
// up. Posting the command without a live stream would strand the result.
const sseReadyTimeout = 15 * time.Second

// Options configures one command run.
type Options struct {
	Text        string
	TaskID      string
	Mode        string
	Model       string // provider/modelId
	SubAgent    string // provider/modelId, or "inherit"
	Databases   []string
	Rags        []string
	Projects    []string
	AskReply    string // set for askResponse commands
	Interactive bool
	Timeout     time.Duration
	Renderer    *Renderer
	// KeepMessages stores the streamed conversation in the result. Off by
	// default because a long run produces a lot of text.
	KeepMessages bool
}

// Runner owns the HTTP and SSE clients for a run.
type Runner struct {
	c *client.Client
}

func NewRunner(c *client.Client) *Runner { return &Runner{c: c} }

// NewTask starts a task and streams it to a terminal state.
func (r *Runner) NewTask(opts Options) (*Result, error) {
	if strings.TrimSpace(opts.Text) == "" {
		return nil, cliexit.Usage("a prompt is required to start a task")
	}
	command := Command{Type: "newTask", Text: opts.Text}
	if err := applyModel(&command, opts); err != nil {
		return nil, err
	}
	if opts.Mode != "" {
		if !slices.Contains(ChatModes, opts.Mode) {
			return nil, cliexit.Usage("unsupported mode %q, expected one of: %s",
				opts.Mode, strings.Join(ChatModes, ", "))
		}
		command.ChatSettings = &ChatSettings{Mode: opts.Mode}
	}
	command.DatabaseIDs = opts.Databases
	command.RagIDs = opts.Rags
	command.ProjectIDs = opts.Projects
	// The server accepts a client-supplied task id and echoes it back, which
	// lets a caller pre-register the id it will poll later.
	command.TaskID = opts.TaskID

	return r.run(command, opts)
}

// Reply answers a question the agent is waiting on.
func (r *Runner) Reply(opts Options) (*Result, error) {
	if opts.TaskID == "" {
		return nil, cliexit.Usage("a task id is required to reply")
	}
	response := opts.AskReply
	if response == "" {
		response = AskResponseMessage
	}
	if !slices.Contains([]string{AskResponseMessage, AskResponseYes, AskResponseNo}, response) {
		return nil, cliexit.Usage("unsupported ask response %q", response)
	}
	if response == AskResponseMessage && strings.TrimSpace(opts.Text) == "" {
		return nil, cliexit.Usage("a message is required for a %s reply", AskResponseMessage)
	}

	command := Command{
		Type:        "askResponse",
		TaskID:      opts.TaskID,
		Text:        opts.Text,
		AskResponse: response,
	}
	if err := applyModel(&command, opts); err != nil {
		return nil, err
	}
	return r.run(command, opts)
}

// Cancel stops a running task. It is fire-and-forget: the command is queued and
// the Worker acts on it when it next checks in.
func (r *Runner) Cancel(taskID string) error {
	if taskID == "" {
		return cliexit.Usage("a task id is required to cancel")
	}
	_, err := r.enqueue(Command{Type: "cancelTask", TaskID: taskID})
	return err
}

// applyModel parses the provider/modelId flags. The server rejects a provider
// without a model and vice versa, so they are validated as a pair here to fail
// before the round trip.
func applyModel(command *Command, opts Options) error {
	if opts.Model != "" {
		provider, modelID, err := splitModel(opts.Model, "--model")
		if err != nil {
			return err
		}
		command.APIProvider, command.APIModelID = provider, modelID
	}
	switch {
	case opts.SubAgent == "":
	case opts.SubAgent == "inherit":
		inherit := true
		command.SubAgentInherit = &inherit
	default:
		provider, modelID, err := splitModel(opts.SubAgent, "--subagent-model")
		if err != nil {
			return err
		}
		inherit := false
		command.SubAgentInherit = &inherit
		command.SubAgentAPIProvider, command.SubAgentAPIModelID = provider, modelID
	}
	return nil
}

func splitModel(value, flag string) (provider, modelID string, err error) {
	provider, modelID, found := strings.Cut(value, "/")
	if !found || provider == "" || strings.TrimSpace(modelID) == "" {
		return "", "", cliexit.Usage("%s expects provider/modelId, received %q", flag, value)
	}
	return provider, strings.TrimSpace(modelID), nil
}

// enqueue posts a command without streaming, for fire-and-forget types.
func (r *Runner) enqueue(command Command) (*EnqueueResponse, error) {
	command.ProtocolVersion = 2
	if command.ClientOpID == "" {
		command.ClientOpID = uuid.New().String()
	}

	raw, err := r.c.Post("/api/ai/message", command)
	if err != nil {
		return nil, err
	}
	var response EnqueueResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, cliexit.New(cliexit.CodeBusiness, "cannot parse command response: %v", err)
	}
	// A queued command reports failures in the body, not the HTTP status.
	if !response.Success {
		message := response.Error
		if message == "" {
			message = "server rejected the command"
		}
		return nil, cliexit.New(cliexit.CodeBusiness, "%s", message)
	}
	return &response, nil
}

// run performs the full subscribe → post → stream → reconcile cycle.
func (r *Runner) run(command Command, opts Options) (*Result, error) {
	command.ProtocolVersion = 2
	command.ClientOpID = uuid.New().String()
	command.ConnID = uuid.New().String()

	renderer := opts.Renderer
	if renderer == nil {
		renderer = NewRenderer(false, false)
	}

	result := &Result{
		TaskID:     command.TaskID,
		ConnID:     command.ConnID,
		ClientOpID: command.ClientOpID,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}
	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()

	ready := make(chan struct{})
	streamDone := make(chan error, 1)
	path := client.WithQuery("/api/ai/events", map[string]string{"connId": command.ConnID})

	go func() {
		streamDone <- client.NewSSEClient(r.c).Subscribe(streamCtx, path, ready, func(event client.SSEEvent) bool {
			return r.handleEvent(event, result, renderer, opts, command)
		})
	}()

	select {
	case <-ready:
	case err := <-streamDone:
		return nil, cliexit.Hint(
			cliexit.Wrap(cliexit.CodeNetwork, err),
			"the event stream must be live before a command is sent, otherwise the result is lost",
		)
	case <-time.After(sseReadyTimeout):
		cancelStream()
		return nil, cliexit.Hint(
			cliexit.New(cliexit.CodeNetwork, "the event stream did not open within %s", sseReadyTimeout),
			"check the server URL and whether a proxy buffers text/event-stream",
		)
	}

	response, err := r.enqueue(command)
	if err != nil {
		cancelStream()
		<-streamDone
		return nil, err
	}
	result.CommandID = response.CommandID
	if response.TaskID != "" {
		result.TaskID = response.TaskID
	}
	renderer.Queued(result.TaskID, response.CommandID)

	streamErr := <-streamDone
	if result.Stop == "" {
		switch {
		case errors.Is(streamErr, context.DeadlineExceeded):
			result.Stop = StopTimeout
		case errors.Is(streamErr, context.Canceled), ctx.Err() != nil:
			result.Stop = StopInterrupted
		case streamErr != nil:
			result.Stop = StopFailed
			result.Error = streamErr.Error()
		default:
			result.Stop = StopCompleted
		}
	}

	// Reconcile: the stream can be cut short, but the task's own status is
	// authoritative about what actually happened.
	if state, err := r.FetchState(result.TaskID); err == nil && state != nil {
		result.TaskStatus = state.CurrentTaskItem.Status
	}
	return result, nil
}

// handleEvent returns false to end the stream.
func (r *Runner) handleEvent(event client.SSEEvent, result *Result, renderer *Renderer, opts Options, command Command) bool {
	if event.Event == "heartbeat" {
		return true
	}
	result.Events++

	switch event.Event {
	case "message.add", "message.partial", "message.update":
		var payload messageEvent
		if err := json.Unmarshal([]byte(event.Data), &payload); err != nil {
			return true
		}
		if payload.TaskID != "" {
			result.TaskID = payload.TaskID
		}
		return r.handleMessage(event.Event, payload.Message, result, renderer, opts)

	case "command.state":
		var payload commandStateEvent
		if err := json.Unmarshal([]byte(event.Data), &payload); err != nil {
			return true
		}
		// Only react to this run's command: one SSE connection carries every
		// command of the user, including those from other terminals and tabs.
		if payload.ClientOpID != "" && payload.ClientOpID != command.ClientOpID {
			return true
		}
		renderer.CommandState(payload.State)
		if payload.State == "failed" {
			result.Stop = StopFailed
			result.Error = payload.Error
			if result.Error == "" {
				result.Error = "the worker reported the command as failed"
			}
			return false
		}
		return true

	case "notification":
		var payload notificationEvent
		if err := json.Unmarshal([]byte(event.Data), &payload); err == nil {
			renderer.Notification(payload)
		}
		return true

	case "state.ready":
		var payload stateReadyEvent
		if err := json.Unmarshal([]byte(event.Data), &payload); err == nil && payload.TaskID != "" {
			result.TaskID = payload.TaskID
		}
		return true

	default:
		return true
	}
}

func (r *Runner) handleMessage(eventName string, message Message, result *Result, renderer *Renderer, opts Options) bool {
	if opts.KeepMessages && !message.Partial {
		result.Messages = append(result.Messages, message)
	}
	renderer.Message(eventName, message)

	if message.Type == "say" && !message.Partial {
		captureDashboardPayload(message, result)

		if message.Say == "completion_result" && eventName == "message.add" {
			result.Stop = StopCompleted
			result.Summary = message.Text
			return false
		}
		return true
	}

	if message.Type == "ask" && !message.Partial {
		if message.Ask == "completion_result" {
			result.Stop = StopCompleted
			if result.Summary == "" {
				result.Summary = message.Text
			}
			return false
		}

		// A question always ends the stream. Answering it is a new command on
		// the same task, with its own idempotency key and its own stream, so
		// the conversation loop lives in Converse rather than here. This keeps
		// interactive and non-interactive runs on one code path.
		result.Stop = StopAsk
		result.Ask = &Ask{Ask: message.Ask, Text: message.Text}
		return false
	}
	return true
}

// Converse runs a command and, when interactive, keeps answering the agent's
// questions until it finishes. Non-interactive runs return at the first
// question with StopAsk, which is the default so CI behaviour is predictable.
func (r *Runner) Converse(start func() (*Result, error), opts Options) (*Result, error) {
	result, err := start()
	if err != nil {
		return nil, err
	}
	if !opts.Interactive {
		return result, nil
	}

	renderer := opts.Renderer
	if renderer == nil {
		renderer = NewRenderer(false, false)
	}

	for result.Stop == StopAsk && result.Ask != nil {
		reply, response, err := renderer.PromptAsk(result.Ask)
		if err != nil {
			// No answer available (no TTY, EOF, user abort): report the pending
			// question rather than pretending the run finished.
			result.Error = err.Error()
			return result, nil
		}

		next, err := r.Reply(Options{
			TaskID:       result.TaskID,
			Text:         reply,
			AskReply:     response,
			Interactive:  true,
			Timeout:      opts.Timeout,
			Renderer:     renderer,
			KeepMessages: opts.KeepMessages,
		})
		if err != nil {
			return nil, err
		}
		result = mergeResult(result, next)
	}
	return result, nil
}

// mergeResult carries the artifacts of earlier turns into the latest one, so a
// dashboard created in turn 2 is still reported after turn 5.
func mergeResult(previous, next *Result) *Result {
	next.DashboardSubmits = append(previous.DashboardSubmits, next.DashboardSubmits...)
	next.DashboardReads = append(previous.DashboardReads, next.DashboardReads...)
	next.Messages = append(previous.Messages, next.Messages...)
	next.Events += previous.Events
	if next.Summary == "" {
		next.Summary = previous.Summary
	}
	return next
}

// FetchState reads the authoritative task state.
func (r *Runner) FetchState(taskID string) (*TaskState, error) {
	if taskID == "" {
		return nil, nil
	}
	raw, err := r.c.Get("/api/ai/state", map[string]string{"taskId": taskID})
	if err != nil {
		return nil, err
	}
	var state TaskState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, cliexit.New(cliexit.CodeBusiness, "cannot parse task state: %v", err)
	}
	return &state, nil
}

// TaskURL builds the web link for a task, for humans following up in the UI.
func TaskURL(server, taskID string) string {
	if server == "" || taskID == "" {
		return ""
	}
	return fmt.Sprintf("%s/tasks/%s", strings.TrimRight(server, "/"), url.PathEscape(taskID))
}
