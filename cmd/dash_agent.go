package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/chaozwn/infini-enterprise-cli/internal/agent"
	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/config"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/spf13/cobra"
)

// agentRunner builds the runner and the renderer shared by the agent commands.
func agentRunner(cmd *cobra.Command) (*agent.Runner, *agent.Renderer, error) {
	c, err := client.New()
	if err != nil {
		return nil, nil, err
	}
	quiet, _ := cmd.Flags().GetBool("quiet")
	reasoning, _ := cmd.Flags().GetBool("reasoning")
	return agent.NewRunner(c), agent.NewRenderer(quiet, reasoning), nil
}

func agentOptions(cmd *cobra.Command, renderer *agent.Renderer) agent.Options {
	mode, _ := cmd.Flags().GetString("mode")
	model, _ := cmd.Flags().GetString("model")
	subAgent, _ := cmd.Flags().GetString("subagent-model")
	interactive, _ := cmd.Flags().GetBool("interactive")
	timeout, _ := cmd.Flags().GetDuration("wait-timeout")
	keep, _ := cmd.Flags().GetBool("transcript")

	opts := agent.Options{
		Mode:         mode,
		Model:        model,
		SubAgent:     subAgent,
		Databases:    resourceFlag(cmd, "database"),
		Rags:         resourceFlag(cmd, "rag"),
		Projects:     resourceFlag(cmd, "project"),
		Interactive:  interactive,
		Timeout:      timeout,
		Renderer:     renderer,
		KeepMessages: keep,
	}
	if cmd.Flags().Changed("engine") {
		engine, _ := cmd.Flags().GetString("engine")
		opts.Engine = &engine
	}
	return opts
}

// resourceFlag distinguishes the three states the server cares about: absent
// (leave the group alone), empty (clear it), and a list (replace it). Cobra
// hands back an empty slice for an unset flag, which would read as "clear".
func resourceFlag(cmd *cobra.Command, name string) []string {
	if !cmd.Flags().Changed(name) {
		return nil
	}
	values, _ := cmd.Flags().GetStringSlice(name)
	ids := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			ids = append(ids, trimmed)
		}
	}
	return ids
}

func addAgentFlags(cmd *cobra.Command) {
	cmd.Flags().String("mode", "", "Chat mode: act, plan, graph or fast (default: the server's setting)")
	cmd.Flags().String("model", "", "Main model as provider/modelId (default: the account's configured model)")
	cmd.Flags().String("subagent-model", "", "Subagent model as provider/modelId, or \"inherit\"")
	cmd.Flags().StringSlice("database", nil, "Database ids to attach")
	cmd.Flags().StringSlice("rag", nil, "Knowledge base ids to attach")
	cmd.Flags().StringSlice("project", nil, "Project ids to attach")
	cmd.Flags().BoolP("interactive", "i", false, "Answer the agent's questions from the terminal instead of stopping")
	cmd.Flags().Duration("wait-timeout", 30*time.Minute, "Give up waiting after this long (0 = no limit)")
	cmd.Flags().Bool("quiet", false, "Suppress the progress stream on stderr")
	cmd.Flags().Bool("reasoning", false, "Show the agent's reasoning output")
	cmd.Flags().Bool("transcript", false, "Include the full conversation in the result")
}

var dashNewCmd = &cobra.Command{
	Use:   "new",
	Short: "Create a dashboard by asking the agent to build it",
	Long: `Hands a brief to the agent, which reads the data model, writes the Infini-SQL
queries and submits a validated spec.

This is the normal way to create a dashboard. The REST endpoint behind
` + "`dash import`" + ` only stores a spec that is already valid; it does not build the
query DAG, infer datasource ids or satisfy the filter contract.

  infini-cli dash new --brief "近 30 天各渠道 GMV 与转化率趋势" --database db_sales

Non-interactive by default: if the agent asks a question, the command stops and
reports it, then you answer with ` + "`dash reply`" + `. Pass --interactive to answer
from the terminal instead, or --guided to have the agent walk you through the
requirements before it starts building.`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		brief, _ := cmd.Flags().GetString("brief")
		guided, _ := cmd.Flags().GetBool("guided")

		if guided {
			interactive, _ := cmd.Flags().GetBool("interactive")
			if !interactive {
				return cliexit.Hint(
					cliexit.New(cliexit.CodeUsage, "--guided needs --interactive"),
					"guided creation is a multi-turn conversation; without --interactive the first question would end the run",
				)
			}
		}
		if brief == "" && !guided {
			return cliexit.Usage("--brief is required (or pass --guided --interactive to be walked through it)")
		}

		prompt := brief
		if guided {
			prompt = guidedCreatePrompt(brief)
		}

		runner, renderer, err := agentRunner(cmd)
		if err != nil {
			return err
		}
		defer renderer.Done()

		opts := agentOptions(cmd, renderer)
		opts.Text = prompt

		result, err := runner.Converse(func() (*agent.Result, error) {
			return runner.NewTask(opts)
		}, opts)
		if err != nil {
			return err
		}
		return reportAgentResult(result, "dash", "create")
	},
}

var dashEditCmd = &cobra.Command{
	Use:   "edit <id>",
	Short: "Edit a dashboard by asking the agent to change it",
	Long: `Hands a change request to the agent, which reads the current spec
(dashboard_read), then submits a patch against the spec hash it read
(dashboard_submit). That read-then-patch cycle is why editing goes through the
agent: it is what makes concurrent edits safe.

  infini-cli dash edit dash_123 --brief "把转化率卡片换成折线图，并加上环比"`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		brief, _ := cmd.Flags().GetString("brief")
		if brief == "" {
			return cliexit.Usage("--brief is required to describe the change")
		}

		// Fail before starting a task if the dashboard is missing or read-only:
		// the agent would otherwise burn a full run to discover it.
		api, err := dashAPI()
		if err != nil {
			return err
		}
		detail, err := api.Get(args[0])
		if err != nil {
			return err
		}
		if !detail.CanWrite {
			return cliexit.Hint(
				cliexit.New(cliexit.CodeAuth, "you do not have write access to dashboard %s", args[0]),
				"ask the owner (%s) or a project admin for access", detail.UserID,
			)
		}

		runner, renderer, err := agentRunner(cmd)
		if err != nil {
			return err
		}
		defer renderer.Done()

		opts := agentOptions(cmd, renderer)
		opts.Text = fmt.Sprintf(
			"请修改看板 %q（id: %s，当前 revision %d）。\n\n"+
				"先用 dashboard_read 读取当前 spec，再用 dashboard_submit 提交 JSON Patch。\n\n"+
				"修改需求：%s",
			detail.Title, detail.ID, detail.CurrentRevision, brief)

		result, err := runner.Converse(func() (*agent.Result, error) {
			return runner.NewTask(opts)
		}, opts)
		if err != nil {
			return err
		}
		return reportAgentResult(result, "dash", "update")
	},
}

var dashChatCmd = &cobra.Command{
	Use:   "chat <id>",
	Short: "Ask the agent a question about a dashboard's current numbers",
	Long: `Builds the dashboard context (the rendered numbers, bound databases and active
filters) and starts a task with it, so the agent answers about what the
dashboard actually shows rather than querying from scratch.

  infini-cli dash chat dash_123 --question "为什么华东转化率比上月低？" \
      --filter period=last_30d

The context is inlined into the prompt. The web UI uploads it as a file into the
task workspace instead; inlining keeps the CLI to one request and behaves the
same for the agent, but a very large context will consume prompt budget.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		question, _ := cmd.Flags().GetString("question")
		if question == "" {
			return cliexit.Usage("--question is required")
		}
		widgetID, _ := cmd.Flags().GetString("widget")

		api, err := dashAPI()
		if err != nil {
			return err
		}
		_, spec, err := api.GetWithSpec(args[0])
		if err != nil {
			return err
		}
		if widgetID != "" && spec.WidgetByID(widgetID) == nil {
			return cliexit.Usage("unknown widget %q; this dashboard has: %s",
				widgetID, strings.Join(spec.WidgetIDs(), ", "))
		}
		filters, err := resolveFilters(cmd, spec)
		if err != nil {
			return err
		}

		scope := "dashboard"
		if widgetID != "" {
			scope = "widget"
		}
		context, err := api.Ask(args[0], scope, widgetID, filters)
		if err != nil {
			return err
		}

		runner, renderer, err := agentRunner(cmd)
		if err != nil {
			return err
		}
		defer renderer.Done()

		opts := agentOptions(cmd, renderer)
		opts.Text = fmt.Sprintf("%s\n\n---\n\n%s", question, context.ContextMarkdown)
		// The context pack names the data the dashboard is built on; attaching
		// it lets the agent drill into the source instead of only reasoning
		// over the rendered numbers.
		opts.Databases = append(opts.Databases, context.DatabaseIDs...)
		opts.Projects = append(opts.Projects, context.ProjectIDs...)

		result, err := runner.Converse(func() (*agent.Result, error) {
			return runner.NewTask(opts)
		}, opts)
		if err != nil {
			return err
		}
		return reportAgentResult(result, "dash", "")
	},
}

var dashReplyCmd = &cobra.Command{
	Use:   "reply <taskId> [message]",
	Short: "Answer a question the agent stopped on",
	Long: `Continues a task that is waiting for input.

  infini-cli dash reply task_123 "用自然月，不要滚动 30 天"
  infini-cli dash reply task_123 --approve
  infini-cli dash reply task_123 --reject`,
	Args: minArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		approve, _ := cmd.Flags().GetBool("approve")
		reject, _ := cmd.Flags().GetBool("reject")
		if approve && reject {
			return cliexit.Usage("--approve and --reject are mutually exclusive")
		}

		message := strings.Join(args[1:], " ")
		response := agent.AskResponseMessage
		switch {
		case approve:
			response = agent.AskResponseYes
		case reject:
			response = agent.AskResponseNo
		case message == "":
			return cliexit.Usage("provide a message, or pass --approve or --reject")
		}

		runner, renderer, err := agentRunner(cmd)
		if err != nil {
			return err
		}
		defer renderer.Done()

		opts := agentOptions(cmd, renderer)
		opts.TaskID = args[0]
		opts.Text = message
		opts.AskReply = response

		result, err := runner.Converse(func() (*agent.Result, error) {
			return runner.Reply(opts)
		}, opts)
		if err != nil {
			return err
		}
		return reportAgentResult(result, "dash", "")
	},
}

var dashCancelCmd = &cobra.Command{
	Use:   "cancel <taskId>",
	Short: "Cancel a running dashboard task",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.New()
		if err != nil {
			return err
		}
		if err := agent.NewRunner(c).Cancel(args[0]); err != nil {
			return err
		}
		// The command is queued, not applied: the Worker acts on it when it
		// next checks in, so claiming the task is already stopped would lie.
		return output.Success(map[string]string{
			"taskId": args[0],
			"status": "cancel queued",
		}, nil, nil)
	},
}

// reportAgentResult prints the run result and picks the exit code.
//
// family is the command group the hints should point at ("dash" or "agent"),
// so a stopped run tells you how to continue it from where you were.
//
// expectSubmit names the dashboard operation the command was supposed to
// perform; when set, a run that never produced an accepted submit is a failure
// even if the agent thinks it finished.
func reportAgentResult(result *agent.Result, family, expectSubmit string) error {
	payload := map[string]any{
		"taskId":     result.TaskID,
		"stop":       result.Stop,
		"taskStatus": result.TaskStatus,
		"events":     result.Events,
	}
	if url := agent.TaskURL(config.Server(), result.TaskID); url != "" {
		payload["taskUrl"] = url
	}
	if result.Summary != "" {
		payload["summary"] = result.Summary
	}
	if result.Ask != nil {
		payload["ask"] = result.Ask
	}
	if result.Error != "" {
		payload["error"] = result.Error
	}
	if len(result.DashboardReads) > 0 {
		payload["dashboardReads"] = result.DashboardReads
	}
	if len(result.DashboardSubmits) > 0 {
		payload["dashboardSubmits"] = result.DashboardSubmits
	}
	if len(result.Messages) > 0 {
		payload["transcript"] = result.Messages
	}
	if submit := result.LastSubmit(); submit != nil && submit.Status == "accepted" {
		payload["dashboardId"] = submit.DashboardID
		payload["revision"] = submit.Revision
	}

	if err := output.Success(payload, nil, nil); err != nil {
		return err
	}

	switch result.Stop {
	case agent.StopAsk:
		return cliexit.Hint(
			cliexit.New(cliexit.CodeBusiness, "the agent is waiting for an answer (%s)", askLabel(result.Ask)),
			"answer with `%s %s reply %s \"...\"`, or rerun with --interactive",
			config.AppName, family, result.TaskID,
		)
	case agent.StopTimeout:
		return cliexit.Hint(
			cliexit.New(cliexit.CodeBusiness, "stopped waiting for task %s", result.TaskID),
			"the task keeps running server-side; follow it with `%s events --task %s`",
			config.AppName, result.TaskID,
		)
	case agent.StopInterrupted:
		return cliexit.Hint(
			cliexit.New(cliexit.CodeBusiness, "interrupted while task %s was running", result.TaskID),
			"the task keeps running server-side; cancel it with `%s %s cancel %s`",
			config.AppName, family, result.TaskID,
		)
	case agent.StopFailed:
		message := result.Error
		if message == "" {
			message = "the task failed"
		}
		return cliexit.New(cliexit.CodeBusiness, "%s", message)
	}

	if expectSubmit == "" {
		return nil
	}

	submit := result.LastSubmit()
	switch {
	case submit == nil:
		return cliexit.Hint(
			cliexit.New(cliexit.CodeBusiness, "the agent finished without submitting a dashboard"),
			"read the transcript with `%s events --task %s`, or restate the brief",
			config.AppName, result.TaskID,
		)
	case submit.Status == "rejected":
		return cliexit.Hint(
			cliexit.New(cliexit.CodeBusiness, "the dashboard submit was rejected with %d error(s)", len(submit.Errors)),
			"the errors above name the exact spec paths the server refused",
		)
	default:
		return nil
	}
}

func askLabel(ask *agent.Ask) string {
	if ask == nil {
		return "unknown question"
	}
	return ask.Ask
}

// guidedCreatePrompt mirrors the prompt the web UI sends when a user picks
// guided dashboard creation, so both paths produce the same conversation.
func guidedCreatePrompt(brief string) string {
	prompt := "我想创建一个数据看板，请默认开启创建引导。先让我选择创建方式，再逐步确认使用人和决策目标、" +
		"数据范围、指标口径、时间范围与粒度、筛选维度和展示偏好；每次只问一组相关问题，并提供\"由你推荐\"的选项。" +
		"请先给出看板蓝图让我确认，再开始查询和创建。"
	if brief != "" {
		prompt += "\n\n初始需求：" + brief
	}
	return prompt
}

func init() {
	dashNewCmd.Flags().String("brief", "", "What the dashboard should show")
	dashNewCmd.Flags().Bool("guided", false, "Have the agent walk through the requirements first (needs --interactive)")
	addAgentFlags(dashNewCmd)

	dashEditCmd.Flags().String("brief", "", "What to change (required)")
	addAgentFlags(dashEditCmd)

	dashChatCmd.Flags().String("question", "", "What to ask about the dashboard (required)")
	dashChatCmd.Flags().String("widget", "", "Scope the question to a single widget")
	addFilterFlags(dashChatCmd)
	addAgentFlags(dashChatCmd)

	dashReplyCmd.Flags().Bool("approve", false, "Answer an approval request with yes")
	dashReplyCmd.Flags().Bool("reject", false, "Answer an approval request with no")
	addAgentFlags(dashReplyCmd)

	dashCmd.AddCommand(dashNewCmd, dashEditCmd, dashChatCmd, dashReplyCmd, dashCancelCmd)
}
