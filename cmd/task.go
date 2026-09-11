package cmd

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/chaozwn/infini-enterprise-cli/internal/task"
	"github.com/spf13/cobra"
)

var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "Inspect and manage agent tasks",
	Long: `A task is one agent conversation together with everything it produced: the
notebook DAG of queries, its working directory, and the tool evidence behind
each result.

Starting a task is the job of the agent commands (` + "`dash new`" + `, and the
` + "`agent`" + ` commands to come). This command covers everything after that.`,
}

func taskAPI() (*task.API, error) {
	c, err := client.New()
	if err != nil {
		return nil, err
	}
	return task.NewAPI(c), nil
}

var taskLsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List tasks",
	Long: `Lists tasks newest-first.

  infini-cli task ls --table
  infini-cli task ls --status running --table
  infini-cli task ls --name 销售 --page-size 50
  infini-cli task ls --project proj_1 --include-sub-tasks`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		query, err := taskListQuery(cmd)
		if err != nil {
			return err
		}

		api, err := taskAPI()
		if err != nil {
			return err
		}
		result, err := api.List(query)
		if err != nil {
			return err
		}

		return output.Success(result, []string{"ID", "NAME", "STATUS", "PIN", "UPDATED"}, func() [][]string {
			rows := make([][]string, 0, len(result.Items))
			for _, item := range result.Items {
				pin := ""
				if item.IsPinned {
					pin = "*"
				}
				rows = append(rows, []string{
					item.ID,
					firstLine(item.TaskName),
					item.Status,
					pin,
					item.UpdatedAt,
				})
			}
			return rows
		})
	},
}

func taskListQuery(cmd *cobra.Command) (task.ListQuery, error) {
	flags := cmd.Flags()
	status, _ := flags.GetString("status")
	if status != "" && !slices.Contains(task.Statuses, status) {
		return task.ListQuery{}, cliexit.Usage("unknown status %q, expected one of: %s",
			status, strings.Join(task.Statuses, ", "))
	}

	order, _ := flags.GetString("order")
	if order != "" && order != "asc" && order != "desc" {
		return task.ListQuery{}, cliexit.Usage("--order must be asc or desc, received %q", order)
	}

	page, _ := flags.GetInt("page")
	pageSize, _ := flags.GetInt("page-size")
	sortField, _ := flags.GetString("sort")
	name, _ := flags.GetString("name")
	keyword, _ := flags.GetString("keyword")
	taskID, _ := flags.GetString("task-id")
	owner, _ := flags.GetString("owner")
	projectID, _ := flags.GetString("project")
	projectIDs, _ := flags.GetStringSlice("projects")
	createdFrom, _ := flags.GetString("created-from")
	createdTo, _ := flags.GetString("created-to")
	pinned, _ := flags.GetBool("pinned")
	includeSubs, _ := flags.GetBool("include-sub-tasks")
	audit, _ := flags.GetBool("audit")

	// The server only honors project_id / project_ids when the filter says to,
	// so derive the filter instead of making the caller set both.
	projectFilter, _ := flags.GetString("project-filter")
	if projectFilter == "" && (projectID != "" || len(projectIDs) > 0) {
		projectFilter = "project"
	}

	return task.ListQuery{
		Page:          page,
		PageSize:      pageSize,
		Field:         sortField,
		Order:         order,
		Name:          name,
		Keyword:       keyword,
		Status:        status,
		TaskID:        taskID,
		OwnerUserID:   owner,
		ProjectFilter: projectFilter,
		ProjectID:     projectID,
		ProjectIDs:    projectIDs,
		CreatedFrom:   createdFrom,
		CreatedTo:     createdTo,
		PinnedOnly:    pinned,
		IncludeSubs:   includeSubs,
		Audit:         audit,
	}, nil
}

var taskShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show a task with its conversation",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := taskAPI()
		if err != nil {
			return err
		}
		data, err := api.Show(args[0])
		if err != nil {
			return err
		}
		return output.Success(data, nil, nil)
	},
}

var taskInfoCmd = &cobra.Command{
	Use:   "info <id>",
	Short: "Show a task's metadata without the conversation",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := taskAPI()
		if err != nil {
			return err
		}
		data, err := api.Info(args[0])
		if err != nil {
			return err
		}
		return output.Success(data, nil, nil)
	},
}

var taskDataCmd = &cobra.Command{
	Use:   "data <id>",
	Short: "Show the raw task payload used by the chat view",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := taskAPI()
		if err != nil {
			return err
		}
		data, err := api.Data(args[0])
		if err != nil {
			return err
		}
		return output.Success(data, nil, nil)
	},
}

var taskStatusCmd = &cobra.Command{
	Use:   "status <id> [id...]",
	Short: "Check the status of one or more tasks",
	Long: `Reads status only, which is the cheap way to poll a batch of tasks without
pulling their conversations.`,
	Args: minArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := taskAPI()
		if err != nil {
			return err
		}
		items, err := api.Statuses(args)
		if err != nil {
			return err
		}
		return output.Success(items, []string{"ID", "STATUS"}, func() [][]string {
			rows := make([][]string, 0, len(items))
			for _, item := range items {
				rows = append(rows, []string{item.ID, item.Status})
			}
			return rows
		})
	},
}

var taskRmCmd = &cobra.Command{
	Use:   "rm <id> [id...]",
	Short: "Delete tasks",
	Args:  minArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := confirm(fmt.Sprintf("Delete %d task(s) and their workspaces?", len(args))); err != nil {
			return err
		}
		api, err := taskAPI()
		if err != nil {
			return err
		}
		result, err := api.Delete(args)
		if err != nil {
			return err
		}
		return output.Success(map[string]any{
			"deleted": args,
			"result":  normalizeRaw(result),
		}, nil, nil)
	},
}

var taskCancelCmd = &cobra.Command{
	Use:   "cancel <id>",
	Short: "Cancel a running task",
	Long: `Requests cancellation. The agent stops at its next checkpoint rather than
mid-tool, so a cancelled task can still take a moment to settle and may leave
completed side effects in place.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := taskAPI()
		if err != nil {
			return err
		}
		result, err := api.Cancel(args[0])
		if err != nil {
			return err
		}
		return output.Success(map[string]any{
			"taskId": args[0],
			"result": normalizeRaw(result),
		}, nil, nil)
	},
}

var taskPinCmd = &cobra.Command{
	Use:   "pin <id>",
	Short: "Pin a task to the top of the list",
	Args:  exactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return setPinned(args[0], true) },
}

var taskUnpinCmd = &cobra.Command{
	Use:   "unpin <id>",
	Short: "Remove a task's pin",
	Args:  exactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return setPinned(args[0], false) },
}

func setPinned(id string, pinned bool) error {
	api, err := taskAPI()
	if err != nil {
		return err
	}
	if _, err := api.SetPinned(id, pinned); err != nil {
		return err
	}
	return output.Success(map[string]any{"taskId": id, "pinned": pinned}, nil, nil)
}

var taskShareCmd = &cobra.Command{
	Use:   "share",
	Short: "Manage a task's public share link",
}

var taskShareGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Show whether a task is shared publicly",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := taskAPI()
		if err != nil {
			return err
		}
		status, err := api.ShareStatus(args[0])
		if err != nil {
			return err
		}
		return output.Success(status, []string{"FIELD", "VALUE"}, func() [][]string {
			return [][]string{
				{"isPublic", strconv.FormatBool(status.IsPublic)},
				{"url", status.URL},
			}
		})
	},
}

var taskShareSetCmd = &cobra.Command{
	Use:   "set <id>",
	Short: "Publish or unpublish a task",
	Long: `Publishing makes the task's conversation and results readable without a login.

  infini-cli task share set task_1 --public
  infini-cli task share set task_1 --private`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		public, _ := cmd.Flags().GetBool("public")
		private, _ := cmd.Flags().GetBool("private")
		if public == private {
			return cliexit.Usage("pass exactly one of --public or --private")
		}
		if public {
			if err := confirm(fmt.Sprintf("Publish task %s so anyone with the link can read it?", args[0])); err != nil {
				return err
			}
		}

		api, err := taskAPI()
		if err != nil {
			return err
		}
		result, err := api.SetShare(args[0], public)
		if err != nil {
			return err
		}
		return output.Success(map[string]any{
			"taskId":   args[0],
			"isPublic": public,
			"result":   normalizeRaw(result),
		}, nil, nil)
	},
}

func init() {
	flags := taskLsCmd.Flags()
	flags.Int("page", 1, "Page number")
	flags.Int("page-size", 20, "Items per page (max 100)")
	flags.String("sort", "updated_at", "Sort field")
	flags.String("order", "desc", "Sort direction: asc or desc")
	flags.String("name", "", "Filter by task name (fuzzy)")
	flags.String("keyword", "", "Filter by keyword across task content")
	flags.String("status", "", "Filter by status: "+strings.Join(task.Statuses, ", "))
	flags.String("task-id", "", "Filter by exact task id")
	flags.String("owner", "", "Filter by owner user id")
	flags.String("project", "", "Filter by a single project id")
	flags.StringSlice("projects", nil, "Filter by several project ids")
	flags.String("project-filter", "", "Project scope: all, none or project (inferred from --project)")
	flags.String("created-from", "", "Only tasks created at or after this ISO timestamp")
	flags.String("created-to", "", "Only tasks created at or before this ISO timestamp")
	flags.Bool("pinned", false, "Only pinned tasks")
	flags.Bool("include-sub-tasks", false, "Include subagent tasks")
	flags.Bool("audit", false, "Use the audit scope (requires permission)")

	taskShareSetCmd.Flags().Bool("public", false, "Make the task readable without a login")
	taskShareSetCmd.Flags().Bool("private", false, "Revoke public access")

	taskShareCmd.AddCommand(taskShareGetCmd, taskShareSetCmd)
	taskCmd.AddCommand(
		taskLsCmd, taskShowCmd, taskInfoCmd, taskDataCmd, taskStatusCmd,
		taskRmCmd, taskCancelCmd, taskPinCmd, taskUnpinCmd, taskShareCmd,
	)
	rootCmd.AddCommand(taskCmd)
}
