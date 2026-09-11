package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// Renderer turns the event stream into progress output.
//
// Everything it writes goes to stderr: stdout carries the single JSON result
// document, so a piped run stays parseable while a human still sees progress.
type Renderer struct {
	quiet         bool
	showReasoning bool
	out           *os.File
	partialActive bool
}

func NewRenderer(quiet, showReasoning bool) *Renderer {
	return &Renderer{quiet: quiet, showReasoning: showReasoning, out: os.Stderr}
}

func (r *Renderer) printf(format string, args ...any) {
	if r.quiet {
		return
	}
	fmt.Fprintf(r.out, format, args...)
}

func (r *Renderer) Queued(taskID, commandID string) {
	r.printf("task %s queued (command %s)\n", taskID, commandID)
}

func (r *Renderer) CommandState(state string) {
	if state == "queued" || state == "" {
		return
	}
	r.printf("command %s\n", state)
}

func (r *Renderer) Notification(event notificationEvent) {
	// Errors are worth showing even in quiet mode: they usually explain why a
	// run produced nothing.
	if event.Type == "error" {
		fmt.Fprintf(r.out, "[error] %s: %s\n", event.Title, event.Message)
		return
	}
	r.printf("[%s] %s: %s\n", event.Type, event.Title, event.Message)
}

// Message renders one conversation entry.
func (r *Renderer) Message(eventName string, message Message) {
	if r.quiet {
		return
	}

	// Partial messages stream token by token; print a single progress dot per
	// chunk instead of redrawing text that will be replaced anyway.
	if message.Partial {
		if eventName == "message.partial" {
			fmt.Fprint(r.out, ".")
			r.partialActive = true
		}
		return
	}
	r.endPartial()

	if message.Type == "ask" {
		fmt.Fprintf(r.out, "\n? [%s] %s\n", message.Ask, truncate(message.Text, 2000))
		return
	}

	switch message.Say {
	case "text", "completion_result":
		if text := strings.TrimSpace(message.Text); text != "" {
			fmt.Fprintf(r.out, "\n%s\n", text)
		}
	case "reasoning":
		if r.showReasoning {
			if text := strings.TrimSpace(message.Reasoning + message.Text); text != "" {
				fmt.Fprintf(r.out, "\n[thinking] %s\n", truncate(text, 2000))
			}
		}
	case "dashboard_read_result":
		r.dashboardRead(message)
	case "dashboard_submit", "dashboard_submit_result":
		r.dashboardSubmit(message)
	case "":
		// Messages without a say type carry no renderable content.
	default:
		r.printf("· %s\n", message.Say)
	}
}

func (r *Renderer) dashboardRead(message Message) {
	read, ok := parseDashboardRead(message.Text)
	if !ok {
		r.printf("· dashboard_read_result\n")
		return
	}
	switch {
	case read.Dashboard != nil:
		fmt.Fprintf(r.out, "· read dashboard %s (%s) at revision %d\n",
			read.Dashboard.Title, read.Dashboard.ID, read.Dashboard.Revision)
	case len(read.Dashboards) > 0:
		fmt.Fprintf(r.out, "· listed %d dashboard(s)\n", len(read.Dashboards))
	default:
		r.printf("· dashboard_read_result\n")
	}
}

func (r *Renderer) dashboardSubmit(message Message) {
	submit, ok := parseDashboardSubmit(message.Text)
	if !ok {
		// dashboard_submit streams in as partial JSON while the agent writes
		// the spec; an unparseable payload here is normal, not an error.
		r.printf("· submitting dashboard\n")
		return
	}

	switch submit.Status {
	case "accepted":
		fmt.Fprintf(r.out, "\n%s dashboard %s (%s) at revision %d\n",
			pastTense(submit.OperationType), submit.Title, submit.DashboardID, submit.Revision)
	case "rejected":
		// The errors array is the whole value of this event: it says exactly
		// which part of the spec the server refused.
		fmt.Fprintf(r.out, "\ndashboard submit rejected with %d error(s):\n", len(submit.Errors))
		for _, failure := range submit.Errors {
			location := failure.Path
			if location == "" {
				location = "(spec)"
			}
			if failure.Code != "" {
				fmt.Fprintf(r.out, "  %s [%s] %s\n", location, failure.Code, failure.Message)
				continue
			}
			fmt.Fprintf(r.out, "  %s %s\n", location, failure.Message)
		}
	default:
		r.printf("· submitting dashboard (%s)\n", submit.OperationType)
	}
}

func pastTense(operation string) string {
	switch operation {
	case "update":
		return "updated"
	case "delete":
		return "deleted"
	default:
		return "created"
	}
}

// PromptAsk asks the operator how to answer the agent.
//
// A single prompt handles both question shapes: "y"/"n" map to the approval
// buttons, anything else is sent as a text reply. That avoids having to track
// which ask types are approvals and which are questions.
func (r *Renderer) PromptAsk(ask *Ask) (text string, response string, err error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", "", fmt.Errorf("cannot answer %q without a terminal", ask.Ask)
	}

	fmt.Fprintf(r.out, "\nReply (y = approve, n = reject, or type a message; empty aborts): ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", "", fmt.Errorf("cannot read the reply: %w", err)
	}

	answer := strings.TrimSpace(line)
	switch strings.ToLower(answer) {
	case "":
		return "", "", fmt.Errorf("aborted without answering %q", ask.Ask)
	case "y", "yes":
		return "", AskResponseYes, nil
	case "n", "no":
		return "", AskResponseNo, nil
	default:
		return answer, AskResponseMessage, nil
	}
}

func (r *Renderer) endPartial() {
	if r.partialActive {
		fmt.Fprintln(r.out)
		r.partialActive = false
	}
}

// Done flushes any in-progress partial line.
func (r *Renderer) Done() { r.endPartial() }

func truncate(text string, max int) string {
	if len(text) <= max {
		return text
	}
	return text[:max] + "...(truncated)"
}

// captureDashboardPayload records dashboard tool results on the run result.
func captureDashboardPayload(message Message, result *Result) {
	switch message.Say {
	case "dashboard_submit_result":
		if submit, ok := parseDashboardSubmit(message.Text); ok {
			result.DashboardSubmits = append(result.DashboardSubmits, submit)
		}
	case "dashboard_read_result":
		if read, ok := parseDashboardRead(message.Text); ok {
			result.DashboardReads = append(result.DashboardReads, read)
		}
	}
}

func parseDashboardSubmit(text string) (DashboardSubmit, bool) {
	var submit DashboardSubmit
	if strings.TrimSpace(text) == "" {
		return submit, false
	}
	if err := json.Unmarshal([]byte(text), &submit); err != nil {
		return submit, false
	}
	submit.Raw = json.RawMessage(text)
	return submit, true
}

func parseDashboardRead(text string) (DashboardRead, bool) {
	var read DashboardRead
	if strings.TrimSpace(text) == "" {
		return read, false
	}
	if err := json.Unmarshal([]byte(text), &read); err != nil {
		return read, false
	}
	return read, true
}
