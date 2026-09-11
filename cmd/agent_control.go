package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/chaozwn/infini-enterprise-cli/internal/agent"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/config"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/spf13/cobra"
)

// queued reports a fire-and-forget command honestly: the row is written and
// the wakeup is enqueued, and that is all that has happened yet.
func queued(command string, response *agent.EnqueueResponse, extra map[string]any) error {
	payload := map[string]any{
		"command":   command,
		"queued":    response.Queued,
		"commandId": response.CommandID,
	}
	if response.TaskID != "" {
		payload["taskId"] = response.TaskID
	}
	for key, value := range extra {
		payload[key] = value
	}
	return output.Success(payload, nil, nil)
}

func sendCommand(command agent.Command, label string, extra map[string]any) error {
	// Checked before the client exists so a bad type reports itself rather
	// than an unconfigured server.
	if err := agent.CheckType(command.Type); err != nil {
		return err
	}
	runner, err := agentRunnerOnly()
	if err != nil {
		return err
	}
	response, err := runner.Send(command)
	if err != nil {
		return err
	}
	return queued(label, response, extra)
}

var agentCancelCmd = &cobra.Command{
	Use:   "cancel <task-id>",
	Short: "Stop a running task",
	Long: `The command is queued: the worker stops the task when it next checks in, so
this returning does not mean the task has already stopped. Watch it settle
with ` + "`task status`" + ` or ` + "`events --task`" + `.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendCommand(
			agent.Command{Type: agent.TypeCancelTask, TaskID: args[0]},
			"cancelTask", map[string]any{"taskId": args[0]},
		)
	},
}

var agentClearCmd = &cobra.Command{
	Use:   "clear [task-id]",
	Short: "Drop a task from the worker, or every task with --all",
	Long: `Clearing releases the task's runtime: its worker slot, its shell sessions and
its in-memory state. The conversation stays in the database.

--all is not one command. The controller fans it out into one clearTask per
active task, because a user's tasks can be spread across several workers and a
single command would only ever reach the one that consumed it.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		if all == (len(args) == 1) {
			return cliexit.Usage("pass either a task id or --all")
		}

		runner, err := agentRunnerOnly()
		if err != nil {
			return err
		}

		if all {
			if err := confirm("Clear every active task of this account?"); err != nil {
				return err
			}
			response, err := runner.ClearAll()
			if err != nil {
				return err
			}
			return output.Success(map[string]any{
				"command":    "clearTask",
				"queued":     response.Queued,
				"commandIds": response.CommandIDs,
				"tasks":      len(response.CommandIDs),
			}, nil, nil)
		}

		return sendCommand(
			agent.Command{Type: agent.TypeClearTask, TaskID: args[0]},
			"clearTask", map[string]any{"taskId": args[0]},
		)
	},
}

var agentLoadCmd = &cobra.Command{
	Use:   "load <task-id>",
	Short: "Reopen a historical task server-side",
	Long: `Rebuilds a task's runtime from the database so its state can be read and it
can be continued. This is what the web UI does when you open an old
conversation.

The rebuilt state arrives over SSE, not in the response, so follow it with
` + "`agent state --task`" + ` or ` + "`events --task`" + `.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendCommand(
			agent.Command{Type: agent.TypeShowTaskWithID, TaskID: args[0]},
			"showTaskWithId", map[string]any{"taskId": args[0]},
		)
	},
}

var agentStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop something a task is running, without stopping the task",
	Long: `Each of these targets one piece of work inside a task: a shell session, a
native tool call, a SQL job or a subagent. To stop the task itself, use
` + "`agent cancel`" + `.`,
}

var agentStopShellCmd = &cobra.Command{
	Use:   "shell <task-id> <session-id>",
	Short: "Stop a shell session",
	Long:  `Session ids look like local:<task-id>:<suffix> and appear in the task's shell output.`,
	Args:  exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendCommand(
			agent.Command{Type: agent.TypeStopShellSession, TaskID: args[0], SessionID: args[1]},
			"stopShellSession", map[string]any{"taskId": args[0], "sessionId": args[1]},
		)
	},
}

var agentStopToolCmd = &cobra.Command{
	Use:   "tool <task-id> <tool-execution-id>",
	Short: "Stop a native tool call",
	Args:  exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendCommand(
			agent.Command{Type: agent.TypeStopToolExecution, TaskID: args[0], ToolExecutionID: args[1]},
			"stopToolExecution", map[string]any{"taskId": args[0], "toolExecutionId": args[1]},
		)
	},
}

var agentStopJobCmd = &cobra.Command{
	Use:   "job <task-id> <job-id>",
	Short: "Stop a running SQL job",
	Long: `Unlike the other stop commands this one runs synchronously: killing an engine
job needs the caller's own access token, so the controller does it inline
instead of queueing it.`,
	Args: exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		engine, _ := cmd.Flags().GetString("engine")

		command := agent.Command{
			Type:   agent.TypeKillActiveJobs,
			TaskID: args[0],
			Text:   args[1],
		}
		if engine != "" {
			command.EngineID = &engine
		}

		runner, err := agentRunnerOnly()
		if err != nil {
			return err
		}
		// This type answers with {success, notification} rather than the queue
		// envelope, so it does not go through Send's queued reporting.
		response, err := runner.Send(command)
		if err != nil {
			return err
		}
		return output.Success(map[string]any{
			"command": "killActiveJobs",
			"taskId":  args[0],
			"jobId":   args[1],
			"success": response.Success,
		}, nil, nil)
	},
}

var agentStopSubagentCmd = &cobra.Command{
	Use:   "subagent <subagent-task-id>",
	Short: "Stop a subagent",
	Long: `Takes the subagent's own task id; the server routes the command to the parent
task, which is where the subagent actually lives.

Graph node conversations are read-only and are rejected: manage those from the
parent graph task.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if strings.Contains(args[0], "_graph_") {
			return cliexit.Hint(
				cliexit.Usage("%s is a graph node conversation, which is read-only", args[0]),
				"manage node execution from the parent graph task",
			)
		}
		// The id travels in text, not taskId: taskId routes the command to the
		// worker holding the parent's lease.
		return sendCommand(
			agent.Command{Type: agent.TypeKillSubAgent, Text: args[0]},
			"killSubAgent", map[string]any{"subAgentTaskId": args[0]},
		)
	},
}

var agentSnapshotsCmd = &cobra.Command{
	Use:   "snapshots <task-id>",
	Short: "List the points a task can be rewound to",
	Long: `The server snapshots a task at each committed user turn and looks a rollback
up by exact timestamp, so these are the only values ` + "`agent rollback --ts`" + `
accepts. There is no endpoint for this list; it is derived from the
conversation.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		runner, err := agentRunnerOnly()
		if err != nil {
			return err
		}
		snapshots, err := runner.Snapshots(args[0])
		if err != nil {
			return err
		}
		if len(snapshots) == 0 {
			return cliexit.New(cliexit.CodeBusiness, "task %s has no rewind points", args[0])
		}
		return output.Success(snapshots, []string{"#", "TS", "KIND", "TEXT"}, func() [][]string {
			rows := make([][]string, 0, len(snapshots))
			for _, snapshot := range snapshots {
				rows = append(rows, []string{
					strconv.Itoa(snapshot.Index),
					strconv.FormatInt(snapshot.TS, 10),
					snapshot.Kind,
					firstLine(snapshot.Text),
				})
			}
			return rows
		})
	},
}

var agentRollbackCmd = &cobra.Command{
	Use:   "rollback <task-id>",
	Short: "Rewind a task to an earlier turn",
	Long: `Rewinds the conversation, the workspace files and the SQL notebook to the
state before the given user turn. This is destructive: everything after that
point is discarded.

  infini-cli agent snapshots task_1
  infini-cli agent rollback task_1 --ts 1700000000000
  infini-cli agent rollback task_1 --ts 1700000000000 --message "换个口径重来"

With --message the task also resumes from that point with the new message, so
it streams like a normal turn. Without it, the rewind is queued and the new
state arrives over SSE.

--ts must be an exact snapshot timestamp; ` + "`agent snapshots`" + ` lists them.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		ts, _ := flags.GetInt64("ts")
		if ts <= 0 {
			return cliexit.Hint(
				cliexit.Usage("--ts is required"),
				"list the rewind points with `%s agent snapshots %s`", config.AppName, args[0],
			)
		}
		message, _ := flags.GetString("message")

		if err := confirm(fmt.Sprintf(
			"Rewind task %s to %d? Everything after that turn is discarded.", args[0], ts)); err != nil {
			return err
		}

		if message == "" {
			return sendCommand(
				agent.Command{Type: agent.TypeRollbackToSnapshot, TaskID: args[0], SnapshotTs: ts},
				"rollbackToSnapshot", map[string]any{"taskId": args[0], "snapshotTs": ts},
			)
		}

		runner, renderer, err := agentRunner(cmd)
		if err != nil {
			return err
		}
		defer renderer.Done()

		opts := agentOptions(cmd, renderer)
		opts.TaskID = args[0]
		opts.Text = message

		result, err := runner.Converse(func() (*agent.Result, error) {
			return runner.RollbackAndSend(opts, ts)
		}, opts)
		if err != nil {
			return err
		}
		return reportAgentResult(result, "agent", "")
	},
}

var agentEditFirstCmd = &cobra.Command{
	Use:   "edit-first <task-id> <message>",
	Short: "Rewrite the opening prompt and run the task again",
	Long: `Replaces the first user message and re-runs the whole task from there, which
discards everything the task has produced.

Use this when the original brief was wrong; use ` + "`agent rollback`" + ` when only
a later turn was.`,
	Args: minArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		message := strings.Join(args[1:], " ")
		if err := confirm(fmt.Sprintf(
			"Replace the opening prompt of %s and re-run it? The whole conversation is discarded.",
			args[0])); err != nil {
			return err
		}

		runner, renderer, err := agentRunner(cmd)
		if err != nil {
			return err
		}
		defer renderer.Done()

		opts := agentOptions(cmd, renderer)
		opts.TaskID = args[0]
		opts.Text = message

		result, err := runner.Converse(func() (*agent.Result, error) {
			return runner.EditFirstAndResend(opts)
		}, opts)
		if err != nil {
			return err
		}
		return reportAgentResult(result, "agent", "")
	},
}

var agentBrowserCmd = &cobra.Command{
	Use:   "browser",
	Short: "Intervene in a browser session the agent is driving",
	Long: `When the agent hits something it cannot get past in the browser — a login, a
captcha — you can take the session over, do it by hand, and hand control back.

These types are absent from the documented message enum but the worker handles
them, and the endpoint does not validate the type.`,
}

var agentBrowserTakeOverCmd = &cobra.Command{
	Use:   "takeover <task-id> <session-id>",
	Short: "Take manual control of a browser session",
	Args:  exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendCommand(
			agent.Command{Type: agent.TypeBrowserTakeOver, TaskID: args[0], SessionID: args[1]},
			"browserTakeOver", map[string]any{"taskId": args[0], "sessionId": args[1]},
		)
	},
}

var agentBrowserResumeCmd = &cobra.Command{
	Use:   "resume <task-id> <session-id>",
	Short: "Hand a browser session back to the agent",
	Long: `--summary tells the agent what you did while you had control, which is what
keeps it from repeating the step it could not do.`,
	Args: exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		summary, _ := cmd.Flags().GetString("summary")
		return sendCommand(
			agent.Command{
				Type: agent.TypeBrowserResume, TaskID: args[0],
				SessionID: args[1], Summary: summary,
			},
			"browserResume", map[string]any{"taskId": args[0], "sessionId": args[1]},
		)
	},
}

var agentBrowserStopCmd = &cobra.Command{
	Use:   "stop <task-id> <session-id>",
	Short: "Close a browser session",
	Args:  exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		reason, _ := cmd.Flags().GetString("reason")
		return sendCommand(
			agent.Command{
				Type: agent.TypeBrowserStop, TaskID: args[0],
				SessionID: args[1], Reason: reason,
			},
			"browserStop", map[string]any{"taskId": args[0], "sessionId": args[1]},
		)
	},
}

func init() {
	agentClearCmd.Flags().Bool("all", false, "Clear every active task of this account")
	agentStopJobCmd.Flags().String("engine", "", "Engine id, when the task is not bound to one")

	agentRollbackCmd.Flags().Int64("ts", 0, "Snapshot timestamp to rewind to (required)")
	agentRollbackCmd.Flags().String("message", "", "Resume from that point with this message instead of only rewinding")
	addAgentFlags(agentRollbackCmd)
	addAgentFlags(agentEditFirstCmd)

	agentBrowserResumeCmd.Flags().String("summary", "", "What you did while you had control")
	agentBrowserStopCmd.Flags().String("reason", "", "Why the session was closed")

	agentStopCmd.AddCommand(agentStopShellCmd, agentStopToolCmd, agentStopJobCmd, agentStopSubagentCmd)
	agentBrowserCmd.AddCommand(agentBrowserTakeOverCmd, agentBrowserResumeCmd, agentBrowserStopCmd)
	agentCmd.AddCommand(
		agentCancelCmd, agentClearCmd, agentLoadCmd, agentStopCmd,
		agentSnapshotsCmd, agentRollbackCmd, agentEditFirstCmd, agentBrowserCmd,
	)
}
