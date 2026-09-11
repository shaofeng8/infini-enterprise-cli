package cmd

import (
	"encoding/json"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/chaozwn/infini-enterprise-cli/internal/agent"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/config"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/spf13/cobra"
)

var agentModeCmd = &cobra.Command{
	Use:   "mode <act|plan|graph|fast>",
	Short: "Set the chat mode, for one task or as the default",
	Long: `Without --task this sets the account default for new tasks. With --task it
switches that task.

  infini-cli agent mode plan --task task_1
  infini-cli agent mode act

plan collects information and designs an approach; act executes. graph and
fast are create-only: they can be the default for new tasks but cannot be
switched into on a task that already exists.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mode := args[0]
		if !slices.Contains(agent.ChatModes, mode) {
			return cliexit.Usage("unknown mode %q, expected one of: %s",
				mode, strings.Join(agent.ChatModes, ", "))
		}

		taskID, _ := cmd.Flags().GetString("task")
		if taskID != "" && slices.Contains(agent.CreateOnlyChatModes, mode) {
			return cliexit.Hint(
				cliexit.Usage("%s mode can only be chosen when a task is created", mode),
				"start a new task with `%s agent new ... --mode %s`", config.AppName, mode,
			)
		}

		return sendCommand(
			agent.Command{
				Type:         agent.TypeUpdateChatMode,
				TaskID:       taskID,
				ChatSettings: &agent.ChatSettings{Mode: mode},
			},
			"updateChatMode", map[string]any{"mode": mode, "taskId": taskID},
		)
	},
}

var agentResourcesCmd = &cobra.Command{
	Use:   "resources <task-id>",
	Short: "Change which data sources, knowledge bases and projects a task can reach",
	Long: `  infini-cli agent resources task_1 --database db_1 --database db_2
  infini-cli agent resources task_1 --rag ""

An omitted group is left alone; an empty value clears it. At least one group
must be given, since a command that changes nothing is more likely a mistake
than an intent.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		databases := resourceFlag(cmd, "database")
		rags := resourceFlag(cmd, "rag")
		projects := resourceFlag(cmd, "project")
		if databases == nil && rags == nil && projects == nil {
			return cliexit.Usage("pass at least one of --database, --rag or --project")
		}

		command := agent.Command{Type: agent.TypeUpdateResources, TaskID: args[0]}
		if databases != nil {
			command.DatabaseIDs = &databases
		}
		if rags != nil {
			command.RagIDs = &rags
		}
		if projects != nil {
			command.ProjectIDs = &projects
		}

		return sendCommand(command, "updateTaskResources", map[string]any{"taskId": args[0]})
	},
}

var agentEngineCmd = &cobra.Command{
	Use:   "engine <task-id>",
	Short: "Set or clear a task's SQL engine",
	Long: `  infini-cli agent engine task_1 --engine engine_1
  infini-cli agent engine task_1 --clear`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		engine, _ := flags.GetString("engine")
		clear, _ := flags.GetBool("clear")
		if (engine == "") == !clear {
			return cliexit.Usage("pass either --engine <id> or --clear")
		}
		if clear {
			engine = ""
		}

		return sendCommand(
			agent.Command{Type: agent.TypeUpdateEngine, TaskID: args[0], EngineID: &engine},
			"updateTaskEngine", map[string]any{"taskId": args[0], "engineId": engine},
		)
	},
}

var agentToolParamsCmd = &cobra.Command{
	Use:   "tool-params <task-id>",
	Short: "Set the per-task YAML parameters of external tools",
	Long: `Each --tool pairs an installed tool id with a YAML file holding its
parameters, in the same format as the proxy's pluginCommandParams.

  infini-cli agent tool-params task_1 --tool tool_1=params.yaml
  infini-cli agent tool-params task_1 --clear

This replaces the whole set rather than merging into it, so list every tool
the task should keep. The server validates the tool ids before queueing, so a
bad id fails here rather than mid-run.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		pairs, _ := flags.GetStringSlice("tool")
		clear, _ := flags.GetBool("clear")
		if clear == (len(pairs) > 0) {
			return cliexit.Usage("pass --tool <id>=<file> at least once, or --clear")
		}

		params := make([]agent.ToolParam, 0, len(pairs))
		for _, pair := range pairs {
			toolID, path, found := strings.Cut(pair, "=")
			if !found || toolID == "" || path == "" {
				return cliexit.Usage("--tool expects <tool-id>=<yaml-file>, received %q", pair)
			}
			yaml, err := readTextFile(path)
			if err != nil {
				return err
			}
			params = append(params, agent.ToolParam{ToolID: toolID, YAML: yaml})
		}

		return sendCommand(
			agent.Command{Type: agent.TypeUpdateToolParams, TaskID: args[0], ToolParams: &params},
			"updateTaskToolParams", map[string]any{"taskId": args[0], "tools": len(params)},
		)
	},
}

var agentAutoApproveCmd = &cobra.Command{
	Use:   "auto-approve",
	Short: "Show or change the agent's budgets and capability switches",
	Long: `With no flags this prints the current settings. Any flag turns it into an
update.

  infini-cli agent auto-approve --table
  infini-cli agent auto-approve --max-requests 500 --browser=false

The endpoint stores whatever object it receives, so this reads the current
settings first and overlays only the flags you passed; otherwise omitting a
flag would reset that capability.

--db-return-limit is clamped server-side to the deployment's maximum.`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		runner, err := agentRunnerOnly()
		if err != nil {
			return err
		}
		state, err := runner.Settings("")
		if err != nil {
			return err
		}
		current := agent.AutoApprovalSettings{}
		if state.AutoApproval != nil {
			current = *state.AutoApproval
		}

		patch, changed := autoApprovalPatch(cmd)
		if !changed {
			return reportAutoApproval(current)
		}

		merged := current.Merge(patch)
		if _, err := runner.Send(agent.Command{
			Type:         agent.TypeAutoApproval,
			AutoApproval: &merged,
		}); err != nil {
			return err
		}
		return reportAutoApproval(merged)
	},
}

func autoApprovalPatch(cmd *cobra.Command) (agent.AutoApprovalSettings, bool) {
	flags := cmd.Flags()
	patch := agent.AutoApprovalSettings{}
	changed := false

	for flag, field := range map[string]**int{
		"max-requests":          &patch.MaxRequests,
		"max-subagent-requests": &patch.MaxSubAgentRequests,
		"db-return-limit":       &patch.DatabaseReturnLimit,
		"delegate-concurrency":  &patch.DelegateMaxConcurrency,
	} {
		if flags.Changed(flag) {
			value, _ := flags.GetInt(flag)
			*field = &value
			changed = true
		}
	}
	for flag, field := range map[string]**bool{
		"notifications": &patch.EnableNotifications,
		"debug":         &patch.DebugMode,
		"subagent":      &patch.EnableSubAgent,
		"web-search":    &patch.EnableWebSearch,
		"browser":       &patch.EnableBrowser,
		"shell":         &patch.EnableShell,
		"map":           &patch.EnableMap,
	} {
		if flags.Changed(flag) {
			value, _ := flags.GetBool(flag)
			*field = &value
			changed = true
		}
	}
	return patch, changed
}

func reportAutoApproval(settings agent.AutoApprovalSettings) error {
	return output.Success(settings, []string{"SETTING", "VALUE"}, func() [][]string {
		rows := [][]string{}
		add := func(name string, value string) {
			rows = append(rows, []string{name, value})
		}
		for _, entry := range []struct {
			name  string
			value *int
		}{
			{"maxRequests", settings.MaxRequests},
			{"maxSubAgentRequests", settings.MaxSubAgentRequests},
			{"databaseReturnLimit", settings.DatabaseReturnLimit},
			{"delegateMaxConcurrency", settings.DelegateMaxConcurrency},
		} {
			if entry.value != nil {
				add(entry.name, strconv.Itoa(*entry.value))
			}
		}
		for _, entry := range []struct {
			name  string
			value *bool
		}{
			{"enableNotifications", settings.EnableNotifications},
			{"debugMode", settings.DebugMode},
			{"enableSubAgent", settings.EnableSubAgent},
			{"enableWebSearch", settings.EnableWebSearch},
			{"enableBrowser", settings.EnableBrowser},
			{"enableShell", settings.EnableShell},
			{"enableMap", settings.EnableMap},
		} {
			if entry.value != nil {
				add(entry.name, strconv.FormatBool(*entry.value))
			}
		}
		return rows
	})
}

var agentSettingsCmd = &cobra.Command{
	Use:   "settings",
	Short: "Change the model configuration",
	Long: `Without --task this changes the account default; with --task it switches the
model of that one task through the command queue.

  infini-cli agent settings --provider openai --model gpt-4o
  infini-cli agent settings --task task_1 --provider anthropic --model claude-sonnet-4
  infini-cli agent settings --subagent-model openai/gpt-4o-mini
  infini-cli agent settings --instructions @house-style.md

The API key is read from INFINI_MODEL_API_KEY rather than a flag, so it stays
out of shell history and the process list.`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		request, err := settingsRequest(cmd)
		if err != nil {
			return err
		}

		runner, err := agentRunnerOnly()
		if err != nil {
			return err
		}
		response, err := runner.UpdateSettings(*request)
		if err != nil {
			return err
		}
		payload := map[string]any{"success": response.Success}
		if response.Notification != nil {
			payload["notification"] = response.Notification
		}
		if request.TaskID != "" {
			payload["taskId"] = request.TaskID
			// A task-level model change is queued like any other command.
			output.Note("the task's model change is queued; the worker applies it on its next turn")
		}
		return output.Success(payload, nil, nil)
	},
}

// modelKeyEnvVar keeps the model API key off the command line, where it would
// be visible in shell history and to anyone who can list processes.
const modelKeyEnvVar = "INFINI_MODEL_API_KEY"

func settingsRequest(cmd *cobra.Command) (*agent.SettingsRequest, error) {
	flags := cmd.Flags()
	request := &agent.SettingsRequest{}
	request.TaskID, _ = flags.GetString("task")

	provider, _ := flags.GetString("provider")
	model, _ := flags.GetString("model")
	baseURL, _ := flags.GetString("base-url")
	apiKey := os.Getenv(modelKeyEnvVar)

	// The server pairs these when it applies a task-level change, and rejects
	// a half-set pair; failing here keeps the account default consistent too.
	if (provider == "") != (model == "") {
		return nil, cliexit.Usage("--provider and --model must be given together")
	}
	if provider != "" && !slices.Contains(agent.APIProviders, provider) {
		return nil, cliexit.Usage("unknown provider %q, expected one of: %s",
			provider, strings.Join(agent.APIProviders, ", "))
	}

	if provider != "" || baseURL != "" || apiKey != "" {
		configuration := &agent.APIConfiguration{
			APIProvider:   provider,
			APIModelID:    model,
			OpenAiBaseURL: baseURL,
			OpenAiAPIKey:  apiKey,
		}
		if provider == "openai" {
			configuration.OpenAiModelID = model
		}
		request.APIConfiguration = configuration
	}

	if flags.Changed("subagent-model") {
		value, _ := flags.GetString("subagent-model")
		inherit := value == "inherit"
		request.SubAgentInherit = &inherit
		if !inherit {
			subProvider, subModel, found := strings.Cut(value, "/")
			if !found || subProvider == "" || strings.TrimSpace(subModel) == "" {
				return nil, cliexit.Usage(
					"--subagent-model expects provider/modelId or \"inherit\", received %q", value)
			}
			request.SubAgentAPIProvider = subProvider
			request.SubAgentAPIModelID = strings.TrimSpace(subModel)
		}
	}

	if flags.Changed("instructions") {
		value, _ := flags.GetString("instructions")
		if strings.HasPrefix(value, "@") {
			resolved, err := readTextFile(strings.TrimPrefix(value, "@"))
			if err != nil {
				return nil, err
			}
			value = resolved
		}
		request.CustomInstructions = &value
	}

	if flags.Changed("reasoning-history") {
		policy, _ := flags.GetString("reasoning-history")
		if !slices.Contains(agent.ReasoningHistoryPolicies, policy) {
			return nil, cliexit.Usage("--reasoning-history must be one of: %s",
				strings.Join(agent.ReasoningHistoryPolicies, ", "))
		}
		request.ChatSettings = &agent.ChatSettings{ReasoningHistoryPolicy: policy}
	}

	if request.APIConfiguration == nil && request.SubAgentInherit == nil &&
		request.CustomInstructions == nil && request.ChatSettings == nil {
		return nil, cliexit.Usage("nothing to change; pass --provider/--model, --subagent-model, --instructions or --reasoning-history")
	}
	return request, nil
}

var agentStateCmd = &cobra.Command{
	Use:   "state",
	Short: "Print the agent's state as the server sees it",
	Long: `With --task this is the whole ExtensionState of that task: its messages, its
model, its resources and its todos. Without one it is the account-level state.

This is the authoritative view; anything the CLI reports about a run is
reconciled against it.`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		taskID, _ := cmd.Flags().GetString("task")
		runner, err := agentRunnerOnly()
		if err != nil {
			return err
		}
		raw, err := runner.RawState(taskID)
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

var agentMessagesCmd = &cobra.Command{
	Use:   "messages <task-id>",
	Short: "Print a task's conversation",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		runner, err := agentRunnerOnly()
		if err != nil {
			return err
		}
		state, err := runner.FetchState(args[0])
		if err != nil {
			return err
		}
		if state == nil {
			return cliexit.New(cliexit.CodeBusiness, "task %s has no state", args[0])
		}
		messages := state.InfiniMessages
		return output.Success(messages, []string{"TS", "TYPE", "KIND", "TEXT"}, func() [][]string {
			rows := make([][]string, 0, len(messages))
			for _, message := range messages {
				kind := message.Say
				if message.Type == "ask" {
					kind = message.Ask
				}
				rows = append(rows, []string{
					strconv.FormatInt(message.TS, 10),
					message.Type, kind, firstLine(message.Text),
				})
			}
			return rows
		})
	},
}

var agentModelsCmd = &cobra.Command{
	Use:   "models",
	Short: "List the models the configured endpoint offers",
	Long: `Asks the OpenAI-compatible endpoint in the account's configuration for its
model list.

The server swallows every failure here and answers with an empty list, so an
empty result means either no models or a lookup that failed — it is not proof
that the endpoint is empty.`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		runner, err := agentRunnerOnly()
		if err != nil {
			return err
		}
		models, err := runner.Models()
		if err != nil {
			return err
		}
		if len(models) == 0 {
			output.Note("empty list: either the endpoint reports no models, or the lookup failed server-side")
		}
		return output.Success(models, []string{"MODEL"}, func() [][]string {
			rows := make([][]string, 0, len(models))
			for _, model := range models {
				rows = append(rows, []string{model})
			}
			return rows
		})
	},
}

var agentConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "Print the saved model configuration",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		runner, err := agentRunnerOnly()
		if err != nil {
			return err
		}
		raw, err := runner.Configuration()
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

var agentPingCmd = &cobra.Command{
	Use:   "ping",
	Short: "Check that the agent endpoint answers",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		runner, err := agentRunnerOnly()
		if err != nil {
			return err
		}
		raw, err := runner.Ping()
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

var agentSendCmd = &cobra.Command{
	Use:   "send <type>",
	Short: "Send a raw agent command",
	Long: `The escape hatch for command types the named commands do not cover.

  infini-cli agent send togglePlanActMode --task task_1 \
      --field chatSettings='{"mode":"plan"}'

The type is checked against the set the server actually acts on. That check is
the point of this command: the endpoint takes its body as an untyped object,
so an unknown type is accepted, queued, and then dropped by the worker with
nothing but a log line — you would get {"queued": true} for a no-op.

This does not stream. Types that produce conversation have their own commands.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		taskID, _ := flags.GetString("task")
		text, _ := flags.GetString("text")
		fields, _ := flags.GetStringSlice("field")

		body := map[string]any{"type": args[0]}
		if taskID != "" {
			body["taskId"] = taskID
		}
		if text != "" {
			body["text"] = text
		}
		for _, pair := range fields {
			key, value, found := strings.Cut(pair, "=")
			if !found || key == "" {
				return cliexit.Usage("--field expects key=value, received %q", pair)
			}
			// A value that parses as JSON is sent as JSON, so nested objects
			// like chatSettings work; anything else goes as a string.
			var decoded any
			if err := json.Unmarshal([]byte(value), &decoded); err != nil {
				decoded = value
			}
			body[key] = decoded
		}

		encoded, err := json.Marshal(body)
		if err != nil {
			return cliexit.Usage("cannot encode the command: %v", err)
		}
		var command agent.Command
		if err := json.Unmarshal(encoded, &command); err != nil {
			return cliexit.Usage("cannot build the command: %v", err)
		}

		return sendCommand(command, args[0], nil)
	},
}

func init() {
	agentModeCmd.Flags().String("task", "", "Apply to this task instead of the account default")

	agentResourcesCmd.Flags().StringSlice("database", nil, "Data source id (repeatable; \"\" clears the group)")
	agentResourcesCmd.Flags().StringSlice("rag", nil, "Knowledge base id (repeatable; \"\" clears the group)")
	agentResourcesCmd.Flags().StringSlice("project", nil, "Project id (repeatable; \"\" clears the group)")

	agentEngineCmd.Flags().String("engine", "", "SQL engine id")
	agentEngineCmd.Flags().Bool("clear", false, "Unbind the task's engine")

	agentToolParamsCmd.Flags().StringSlice("tool", nil, "Tool parameters as <tool-id>=<yaml-file> (repeatable)")
	agentToolParamsCmd.Flags().Bool("clear", false, "Remove every tool parameter from the task")

	approve := agentAutoApproveCmd.Flags()
	approve.Int("max-requests", 0, "Model request budget for the main agent")
	approve.Int("max-subagent-requests", 0, "Model request budget for subagents")
	approve.Int("db-return-limit", 0, "Row cap on query results (clamped server-side)")
	approve.Int("delegate-concurrency", 0, "How many delegated runs may go at once")
	approve.Bool("notifications", false, "Send notifications")
	approve.Bool("debug", false, "Debug mode")
	approve.Bool("subagent", false, "Allow delegating to subagents")
	approve.Bool("web-search", false, "Allow web search")
	approve.Bool("browser", false, "Allow the browser tool")
	approve.Bool("shell", false, "Allow the shell tool")
	approve.Bool("map", false, "Allow map delegation")

	settings := agentSettingsCmd.Flags()
	settings.String("task", "", "Change this task's model instead of the account default")
	settings.String("provider", "", "Model provider: "+strings.Join(agent.APIProviders, ", "))
	settings.String("model", "", "Model id (required with --provider)")
	settings.String("base-url", "", "OpenAI-compatible base URL")
	settings.String("subagent-model", "", "Subagent model as provider/modelId, or \"inherit\"")
	settings.String("instructions", "", "Custom instructions; @file to read from a file")
	settings.String("reasoning-history", "", "Reasoning replay policy: "+strings.Join(agent.ReasoningHistoryPolicies, ", "))

	agentStateCmd.Flags().String("task", "", "Scope the state to one task")

	sendFlags := agentSendCmd.Flags()
	sendFlags.String("task", "", "Task id")
	sendFlags.String("text", "", "The command's text field")
	sendFlags.StringSlice("field", nil, "Extra field as key=value; a JSON value is sent as JSON (repeatable)")

	agentCmd.AddCommand(
		agentModeCmd, agentResourcesCmd, agentEngineCmd, agentToolParamsCmd,
		agentAutoApproveCmd, agentSettingsCmd, agentStateCmd, agentMessagesCmd,
		agentModelsCmd, agentConfigCmd, agentPingCmd, agentSendCmd,
	)
}
