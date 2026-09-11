package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"syscall"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// eventEnvelope covers the shared shape of message.* and state.ready payloads.
type eventEnvelope struct {
	TaskID  string          `json:"taskId"`
	Message json.RawMessage `json:"message"`
}

var eventsCmd = &cobra.Command{
	Use:   "events",
	Short: "Stream the SSE event feed",
	Long: `Opens GET /api/ai/events and prints every event as it arrives.

The agent command channel is asynchronous: POST /api/ai/message only enqueues a
command and returns, and results arrive exclusively over this stream. This
command is the raw view of that stream, useful for debugging what the agent is
doing and for confirming that events reach this machine at all.

Event types:
  message.add / message.update / message.partial   agent output
  state.ready                                      re-fetch task state from the API
  notification                                     global notice
  heartbeat                                        keep-alive, hidden unless --heartbeat`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		taskFilter, _ := cmd.Flags().GetString("task")
		eventFilter, _ := cmd.Flags().GetStringArray("event")
		showHeartbeat, _ := cmd.Flags().GetBool("heartbeat")
		limit, _ := cmd.Flags().GetInt("limit")
		connID, _ := cmd.Flags().GetString("conn-id")

		if connID == "" {
			connID = uuid.New().String()
		}

		c, err := client.New()
		if err != nil {
			return err
		}

		// Ctrl-C must end the stream cleanly rather than kill the process mid-write.
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		output.Note("Streaming %s/api/ai/events (connId=%s); press Ctrl-C to stop", c.BaseURL(), connID)

		seen := 0
		path := client.WithQuery("/api/ai/events", map[string]string{"connId": connID})

		err = client.NewSSEClient(c).SubscribeWithRetry(ctx, path, nil, func(event client.SSEEvent) bool {
			if event.Event == "heartbeat" && !showHeartbeat {
				return true
			}
			if len(eventFilter) > 0 && !slices.Contains(eventFilter, event.Event) {
				return true
			}
			if taskFilter != "" && !matchesTask(event, taskFilter) {
				return true
			}

			printEvent(event)
			seen++
			return limit <= 0 || seen < limit
		})

		if err != nil && ctx.Err() == nil {
			return err
		}
		output.Note("Stream closed after %d event(s)", seen)
		return nil
	},
}

func matchesTask(event client.SSEEvent, taskID string) bool {
	var payload eventEnvelope
	if err := json.Unmarshal([]byte(event.Data), &payload); err != nil {
		return false
	}
	return payload.TaskID == taskID
}

func printEvent(event client.SSEEvent) {
	var parsed any
	if err := json.Unmarshal([]byte(event.Data), &parsed); err != nil {
		// heartbeat sends a bare string rather than JSON.
		parsed = event.Data
	}
	record := map[string]any{"event": event.Event, "data": parsed}
	if event.ID != "" {
		record["id"] = event.ID
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return
	}
	fmt.Fprintln(os.Stdout, string(encoded))
}

func init() {
	eventsCmd.Flags().String("task", "", "Only print events for this task id")
	eventsCmd.Flags().StringArray("event", nil, "Only print these event types (repeatable)")
	eventsCmd.Flags().Bool("heartbeat", false, "Include heartbeat events")
	eventsCmd.Flags().Int("limit", 0, "Stop after N events (0 = unlimited)")
	eventsCmd.Flags().String("conn-id", "", "Connection id for targeted server pushes (default: random uuid)")

	rootCmd.AddCommand(eventsCmd)
}
