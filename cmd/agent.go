package cmd

import (
	"strings"

	"github.com/chaozwn/infini-enterprise-cli/internal/agent"
	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Drive the agent directly",
	Long: `Everything the web UI can ask the agent to do, minus the dashboard wrappers
in ` + "`infini-cli dash`" + `.

Two things shape how these commands behave.

First, the command channel is a queue, not an RPC. POST /api/ai/message writes
a row and returns; the worker holding the task's lease runs it afterwards and
the result arrives over SSE. Commands that produce conversation (new, reply,
options, resume, rollback --message, edit-first) therefore subscribe to the
event stream before they post, and stream until the agent stops. Everything
else reports "queued" and returns, because that is genuinely all that has
happened.

Second, a run stops at the agent's first question unless you pass
--interactive. That is deliberate: a CI job should fail with the question
rather than hang waiting for an answer nobody is there to give.`,
}

func agentRunnerOnly() (*agent.Runner, error) {
	c, err := client.New()
	if err != nil {
		return nil, err
	}
	return agent.NewRunner(c), nil
}

var agentNewCmd = &cobra.Command{
	Use:   "new <prompt>",
	Short: "Start a task",
	Long: `  infini-cli agent new "统计上季度各区域营收，输出一张表"
  infini-cli agent new "分析这批订单" --database db_1 --mode plan
  infini-cli agent new "跑通全流程" --interactive

The resources passed here are frozen into the task as a snapshot; changing
them later takes ` + "`agent resources`" + `.

graph and fast can only be chosen here, never switched into afterwards.`,
	Args: minArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		runner, renderer, err := agentRunner(cmd)
		if err != nil {
			return err
		}
		defer renderer.Done()

		opts := agentOptions(cmd, renderer)
		opts.Text = strings.Join(args, " ")
		opts.TaskID, _ = cmd.Flags().GetString("task-id")

		result, err := runner.Converse(func() (*agent.Result, error) {
			return runner.NewTask(opts)
		}, opts)
		if err != nil {
			return err
		}
		return reportAgentResult(result, "agent", "")
	},
}

var agentReplyCmd = &cobra.Command{
	Use:   "reply <task-id> [message]",
	Short: "Answer a question the agent stopped on",
	Long: `  infini-cli agent reply task_1 "用自然月口径"
  infini-cli agent reply task_1 --approve
  infini-cli agent reply task_1 --deny

A reply can also change the task's runtime configuration before execution
resumes: --database, --rag, --project, --engine, --model and --subagent-model
all apply on the way in. Passing an empty value clears that group, so
--database "" detaches every data source.`,
	Args: minArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		response, err := askResponseFor(cmd)
		if err != nil {
			return err
		}
		message := strings.Join(args[1:], " ")
		if response == agent.AskResponseMessage && message == "" {
			return cliexit.Usage("provide a message, or pass --approve or --deny")
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
		return reportAgentResult(result, "agent", "")
	},
}

// askResponseFor maps the approval flags onto the server's three response
// values. --approve and --deny are the same askResponse command with a
// different verdict, which is why they are flags rather than sibling commands.
func askResponseFor(cmd *cobra.Command) (string, error) {
	approve, _ := cmd.Flags().GetBool("approve")
	deny, _ := cmd.Flags().GetBool("deny")
	switch {
	case approve && deny:
		return "", cliexit.Usage("--approve and --deny are mutually exclusive")
	case approve:
		return agent.AskResponseYes, nil
	case deny:
		return agent.AskResponseNo, nil
	default:
		return agent.AskResponseMessage, nil
	}
}

var agentOptionsCmd = &cobra.Command{
	Use:   "options <task-id> <selection>",
	Short: "Answer a multiple-choice question",
	Long: `Some questions offer a list instead of free text. The selection is passed
through to the agent verbatim; the web UI sends JSON for multi-select
questions, and a plain string works for a single choice.

  infini-cli agent options task_1 "按自然月"
  infini-cli agent options task_1 '["华东","华南"]'`,
	Args: exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		runner, renderer, err := agentRunner(cmd)
		if err != nil {
			return err
		}
		defer renderer.Done()

		opts := agentOptions(cmd, renderer)
		opts.TaskID = args[0]
		opts.Text = args[1]

		result, err := runner.Converse(func() (*agent.Result, error) {
			return runner.Options(opts)
		}, opts)
		if err != nil {
			return err
		}
		return reportAgentResult(result, "agent", "")
	},
}

var agentResumeCmd = &cobra.Command{
	Use:   "resume <task-id>",
	Short: "Resume a task that stopped without finishing",
	Long: `Picks a task back up where it left off, for example after the worker that
held it restarted.

To reopen a completed task for reading rather than resuming it, use
` + "`agent load`" + `.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		runner, renderer, err := agentRunner(cmd)
		if err != nil {
			return err
		}
		defer renderer.Done()

		opts := agentOptions(cmd, renderer)
		opts.TaskID = args[0]

		result, err := runner.Resume(opts)
		if err != nil {
			return err
		}
		return reportAgentResult(result, "agent", "")
	},
}

func init() {
	agentNewCmd.Flags().String("task-id", "", "Use this id for the new task instead of a server-generated one")
	addAgentFlags(agentNewCmd)

	for _, cmd := range []*cobra.Command{agentReplyCmd, agentOptionsCmd, agentResumeCmd} {
		addAgentFlags(cmd)
	}
	agentReplyCmd.Flags().Bool("approve", false, "Answer an approval request with yes")
	agentReplyCmd.Flags().Bool("deny", false, "Answer an approval request with no")
	agentReplyCmd.Flags().String("engine", "", "Switch the task's SQL engine before resuming")

	agentCmd.AddCommand(agentNewCmd, agentReplyCmd, agentOptionsCmd, agentResumeCmd)
	rootCmd.AddCommand(agentCmd)
}
