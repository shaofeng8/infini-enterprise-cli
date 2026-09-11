package cmd

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/config"
	"github.com/chaozwn/infini-enterprise-cli/internal/ops"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/spf13/cobra"
)

func opsAPI() (*ops.API, error) {
	c, err := client.New()
	if err != nil {
		return nil, err
	}
	return ops.NewAPI(c), nil
}

var scheduleCmd = &cobra.Command{
	Use:     "schedule",
	Aliases: []string{"sched"},
	Short:   "Manage recurring agent runs",
	Long: `A schedule fires a prompt on a cron and produces a normal task, so everything
under ` + "`task`" + ` and ` + "`agent`" + ` applies to the result.

The configuration a run executes with is frozen when you save the schedule,
not resolved when it fires. That is deliberate: a nightly report keeps using
the model and data sources it was set up with even after the account's
defaults move on. It also means changing your defaults does not update
existing schedules — use ` + "`schedule update`" + ` for that.

Cron expressions here are six-field Quartz (second minute hour day month
weekday), not the five-field Unix form: "0 30 9 * * ?" is 09:30 every day.`,
}

var scheduleListCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List schedules",
	Long: `  infini-cli schedule ls --table
  infini-cli schedule ls --status paused --table

Archived schedules are excluded; they stay readable through their run history.`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		page, _ := flags.GetInt("page")
		pageSize, _ := flags.GetInt("page-size")
		keyword, _ := flags.GetString("search")
		status, _ := flags.GetString("status")
		if status != "" && !slices.Contains(ops.ScheduleStatuses, status) {
			return cliexit.Usage("unknown status %q, expected one of: %s",
				status, strings.Join(ops.ScheduleStatuses, ", "))
		}

		api, err := opsAPI()
		if err != nil {
			return err
		}
		result, err := api.Schedules(ops.ScheduleQuery{
			Page: page, PageSize: pageSize, Keyword: keyword, Status: status,
		})
		if err != nil {
			return err
		}
		return output.Success(result, []string{"ID", "TITLE", "CRON", "ENABLED", "NEXT RUN", "LAST RUN"}, func() [][]string {
			rows := make([][]string, 0, len(result.Items))
			for _, schedule := range result.Items {
				rows = append(rows, []string{
					schedule.ScheduleID, schedule.Title, schedule.CronExpression,
					strconv.FormatBool(schedule.Enabled),
					stringOrDash(schedule.NextRunAt), stringOrDash(schedule.LastRunAt),
				})
			}
			return rows
		})
	},
}

func stringOrDash(value *string) string {
	if value == nil || *value == "" {
		return "-"
	}
	return *value
}

var scheduleShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show one schedule, including its frozen configuration",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := opsAPI()
		if err != nil {
			return err
		}
		schedule, err := api.Schedule(args[0])
		if err != nil {
			return err
		}
		return output.Success(schedule, nil, nil)
	},
}

var scheduleRunsCmd = &cobra.Command{
	Use:   "runs <id>",
	Short: "List a schedule's run history",
	Long: `A run carries the task it produced, so a failed nightly report can be traced
into the conversation that failed:

  infini-cli schedule runs s_1 --table
  infini-cli task show <taskId>

A run marked as a misfire fired late — the scheduler noticed it after the fact,
typically following a restart — rather than at its scheduled time.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		page, _ := flags.GetInt("page")
		pageSize, _ := flags.GetInt("page-size")

		api, err := opsAPI()
		if err != nil {
			return err
		}
		result, err := api.Runs(args[0], page, pageSize)
		if err != nil {
			return err
		}
		return output.Success(result, []string{"RUN ID", "SCHEDULED FOR", "STATUS", "TASK ID", "MISFIRE", "ERROR"}, func() [][]string {
			rows := make([][]string, 0, len(result.Items))
			for _, run := range result.Items {
				rows = append(rows, []string{
					run.RunID, run.ScheduledFor, run.Status, stringOrDash(run.TaskID),
					strconv.FormatBool(run.IsMisfire), firstLine(stringOrDash(run.ErrorMessage)),
				})
			}
			return rows
		})
	},
}

var scheduleCreateCmd = &cobra.Command{
	Use:   "create <title>",
	Short: "Create a schedule",
	Long: `  infini-cli schedule create "每日销售简报" \
      --prompt "汇总昨日销售数据并生成简报" \
      --cron "0 30 9 * * ?" \
      --database db_sales

--start defaults to now, so a schedule fires from the moment it is created
unless you push it out. --prompt accepts @file.

Capability flags (--shell, --browser, --web-search, --subagent) narrow the
run's envelope on top of your auto-approval defaults, which is how an
unattended run gets less reach than an interactive one.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := scheduleBody(cmd, nil)
		if err != nil {
			return err
		}
		body["title"] = args[0]
		if body["prompt"] == nil || body["prompt"] == "" {
			return cliexit.Usage("--prompt is required")
		}
		if body["cronExpression"] == nil || body["cronExpression"] == "" {
			return cliexit.Usage("--cron is required")
		}

		api, err := opsAPI()
		if err != nil {
			return err
		}
		schedule, err := api.CreateSchedule(body)
		if err != nil {
			return err
		}
		return output.Success(schedule, nil, nil)
	},
}

var scheduleUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a schedule",
	Long: `The server's save DTO is shared by create and update, so title, prompt, cron
and start time are all mandatory on the way in. The current schedule is read
first and whatever you did not pass is carried over, so a single flag really
does change a single thing.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := opsAPI()
		if err != nil {
			return err
		}
		current, err := api.Schedule(args[0])
		if err != nil {
			return err
		}

		body, err := scheduleBody(cmd, current)
		if err != nil {
			return err
		}
		if title, _ := cmd.Flags().GetString("title"); title != "" {
			body["title"] = title
		}

		schedule, err := api.UpdateSchedule(args[0], body)
		if err != nil {
			return err
		}
		return output.Success(schedule, nil, nil)
	},
}

// scheduleBody builds the save payload, filling the required fields from the
// current schedule when this is an update.
func scheduleBody(cmd *cobra.Command, current *ops.Schedule) (map[string]any, error) {
	flags := cmd.Flags()
	body := map[string]any{}

	if current != nil {
		body["title"] = current.Title
		body["prompt"] = current.Prompt
		body["cronExpression"] = current.CronExpression
		body["startAt"] = current.StartAt
		body["enabled"] = current.Enabled
		if current.EndAt != nil {
			body["endAt"] = *current.EndAt
		}
		if len(current.TaskConfig) > 0 {
			body["taskConfig"] = current.TaskConfig
		}
	} else {
		// A new schedule needs a start instant; now is the only default that
		// does not silently postpone the first run.
		body["startAt"] = time.Now().UTC().Format(time.RFC3339)
	}

	if flags.Changed("prompt") {
		prompt, _ := flags.GetString("prompt")
		if strings.HasPrefix(prompt, "@") {
			resolved, err := readTextFile(strings.TrimPrefix(prompt, "@"))
			if err != nil {
				return nil, err
			}
			prompt = resolved
		}
		body["prompt"] = prompt
	}
	if flags.Changed("cron") {
		cron, _ := flags.GetString("cron")
		if err := checkQuartzCron(cron); err != nil {
			return nil, err
		}
		body["cronExpression"] = cron
	}
	for flag, field := range map[string]string{"start": "startAt", "end": "endAt"} {
		if !flags.Changed(flag) {
			continue
		}
		value, _ := flags.GetString(flag)
		if value == "" && field == "endAt" {
			body["endAt"] = nil
			continue
		}
		instant, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return nil, cliexit.Usage("--%s expects an RFC 3339 timestamp like 2026-07-12T01:30:00Z, received %q", flag, value)
		}
		body[field] = instant.UTC().Format(time.RFC3339)
	}
	if flags.Changed("enabled") {
		enabled, _ := flags.GetBool("enabled")
		body["enabled"] = enabled
	}

	config, err := scheduleTaskConfig(cmd, current)
	if err != nil {
		return nil, err
	}
	if config != nil {
		body["taskConfig"] = config
	}
	return body, nil
}

// scheduleTaskConfig patches the frozen run configuration, preserving the
// fields no flag touched.
func scheduleTaskConfig(cmd *cobra.Command, current *ops.Schedule) (map[string]any, error) {
	flags := cmd.Flags()
	config := map[string]any{}
	if current != nil {
		for key, value := range current.TaskConfig {
			config[key] = value
		}
	}
	changed := false

	if flags.Changed("model") {
		value, _ := flags.GetString("model")
		provider, modelID, found := strings.Cut(value, "/")
		if !found || provider == "" || strings.TrimSpace(modelID) == "" {
			return nil, cliexit.Usage("--model expects provider/modelId, received %q", value)
		}
		config["apiProvider"] = provider
		config["apiModelId"] = strings.TrimSpace(modelID)
		changed = true
	}
	if flags.Changed("engine") {
		engine, _ := flags.GetString("engine")
		config["engineId"] = engine
		changed = true
	}
	for flag, field := range map[string]string{
		"database": "databaseIds",
		"rag":      "ragIds",
		"project":  "projectIds",
	} {
		if ids := resourceFlag(cmd, flag); ids != nil {
			config[field] = ids
			changed = true
		}
	}

	capabilities := map[string]any{}
	if existing, ok := config["capabilities"].(map[string]any); ok {
		for key, value := range existing {
			capabilities[key] = value
		}
	}
	for flag, field := range map[string]string{
		"shell":      "enableShell",
		"browser":    "enableBrowser",
		"web-search": "enableWebSearch",
		"subagent":   "enableSubAgent",
	} {
		if flags.Changed(flag) {
			value, _ := flags.GetBool(flag)
			capabilities[field] = value
			changed = true
		}
	}
	if len(capabilities) > 0 {
		config["capabilities"] = capabilities
	}

	if !changed {
		return nil, nil
	}
	return config, nil
}

// checkQuartzCron catches the five-field Unix form, which the server would
// accept as a string and then fail to parse into a schedule.
func checkQuartzCron(expression string) error {
	fields := strings.Fields(expression)
	if len(fields) == 6 || len(fields) == 7 {
		return nil
	}
	return cliexit.Hint(
		cliexit.Usage("--cron expects a six-field Quartz expression, received %d field(s)", len(fields)),
		"Quartz leads with seconds: \"0 30 9 * * ?\" is 09:30 daily, not \"30 9 * * *\"",
	)
}

var schedulePauseCmd = &cobra.Command{
	Use:   "pause <id>",
	Short: "Stop a schedule from firing",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return setScheduleEnabled(args[0], false)
	},
}

var scheduleResumeCmd = &cobra.Command{
	Use:   "resume <id>",
	Short: "Let a schedule fire again",
	Long: `Fails when the cron has no occurrence left in the future, which is the server
refusing to enable a schedule that could never run.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return setScheduleEnabled(args[0], true)
	},
}

func setScheduleEnabled(id string, enabled bool) error {
	api, err := opsAPI()
	if err != nil {
		return err
	}
	schedule, err := api.SetScheduleEnabled(id, enabled)
	if err != nil {
		return err
	}
	return output.Success(schedule, nil, nil)
}

var scheduleRunCmd = &cobra.Command{
	Use:   "run <id>",
	Short: "Run a schedule once, now",
	Long: `Queues an extra run without touching the cron, so the next scheduled run
still happens as planned. The run produces a task like any other; follow it
with ` + "`schedule runs`" + ` or ` + "`task show`" + `.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := opsAPI()
		if err != nil {
			return err
		}
		run, err := api.RunSchedule(args[0])
		if err != nil {
			return err
		}
		if run.TaskID != nil && *run.TaskID != "" {
			output.Note("follow the run with `" + config.AppName + " task show " + *run.TaskID + "`")
		}
		return output.Success(run, nil, nil)
	},
}

var scheduleArchiveCmd = &cobra.Command{
	Use:   "archive <id>",
	Short: "Retire a schedule",
	Long: `Archiving hides the schedule and stops it firing. It is not a delete: the run
history stays queryable, which is what makes an unattended pipeline auditable
after the fact.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := confirm("Archive schedule " + args[0] + "?"); err != nil {
			return err
		}
		api, err := opsAPI()
		if err != nil {
			return err
		}
		raw, err := api.ArchiveSchedule(args[0])
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

func addScheduleWriteFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.String("prompt", "", "What to ask the agent; @file to read from a file")
	flags.String("cron", "", "Six-field Quartz cron, e.g. \"0 30 9 * * ?\"")
	flags.String("start", "", "When the schedule becomes active (RFC 3339)")
	flags.String("end", "", "When it expires (RFC 3339); empty clears it")
	flags.Bool("enabled", true, "Whether it fires")
	flags.String("model", "", "Model as provider/modelId")
	flags.String("engine", "", "SQL engine id")
	flags.StringSlice("database", nil, "Data source ids")
	flags.StringSlice("rag", nil, "Knowledge base ids")
	flags.StringSlice("project", nil, "Project ids")
	flags.Bool("shell", false, "Allow the shell tool")
	flags.Bool("browser", false, "Allow the browser tool")
	flags.Bool("web-search", false, "Allow web search")
	flags.Bool("subagent", false, "Allow subagent delegation")
}

func init() {
	scheduleListCmd.Flags().Int("page", 1, "Page number")
	scheduleListCmd.Flags().Int("page-size", 20, "Items per page")
	scheduleListCmd.Flags().String("search", "", "Filter by title")
	scheduleListCmd.Flags().String("status", "", "Filter by state: "+strings.Join(ops.ScheduleStatuses, ", "))

	scheduleRunsCmd.Flags().Int("page", 1, "Page number")
	scheduleRunsCmd.Flags().Int("page-size", 20, "Items per page (max 100)")

	addScheduleWriteFlags(scheduleCreateCmd)
	addScheduleWriteFlags(scheduleUpdateCmd)
	scheduleUpdateCmd.Flags().String("title", "", "New title")

	scheduleCmd.AddCommand(
		scheduleListCmd, scheduleShowCmd, scheduleRunsCmd, scheduleCreateCmd,
		scheduleUpdateCmd, schedulePauseCmd, scheduleResumeCmd, scheduleRunCmd,
		scheduleArchiveCmd,
	)
	rootCmd.AddCommand(scheduleCmd)
}
