package cmd

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/config"
	"github.com/chaozwn/infini-enterprise-cli/internal/hub"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var hubMemoryCmd = &cobra.Command{
	Use:   "memory",
	Short: "Build the semantic layer from a data source's schema",
	Long: `A memory build reads a data source's schema and sample rows, then proposes
table and column descriptions. The proposals are drafts: nothing is written
until they are approved, so run ` + "`hub review pending`" + ` afterwards.

Builds run server-side and survive the CLI exiting. One build per data source
at a time: starting a second returns the one already running.`,
}

var hubMemoryStartCmd = &cobra.Command{
	Use:   "start <data-source>",
	Short: "Start a memory build for one data source",
	Long: `Takes a data source id or name. The table and column selection is derived
from the live schema, so no selection flags are needed for the common case.

  infini-cli hub memory start chinook
  infini-cli hub memory start chinook --missing-only --wait
  infini-cli hub memory start chinook --table artists --table albums
  infini-cli hub memory start chinook --table "invoices:id,total,date"

--missing-only restricts the build to tables the semantic layer does not cover
yet, which is what to use when adding tables to a described data source.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		selections, _ := flags.GetStringSlice("table")
		missingOnly, _ := flags.GetBool("missing-only")
		skipExisting, _ := flags.GetBool("skip-existing-table-updates")
		wait, _ := flags.GetBool("wait")

		source, err := resolveDatabaseID(args[0])
		if err != nil {
			return err
		}

		dbClient, err := dbAPI()
		if err != nil {
			return err
		}
		rawSchema, err := dbClient.Schema(source.ID)
		if err != nil {
			return err
		}
		tables, err := selectMemoryTables(rawSchema, selections, missingOnly)
		if err != nil {
			return err
		}

		api, err := hubAPI()
		if err != nil {
			return err
		}
		job, err := api.StartMemoryBuild(hub.StartRequest{
			// The task id names the workspace the build runs in; the server has
			// no opinion on its format beyond being non-empty.
			TaskID:                   "memory_build_" + uuid.NewString(),
			DatabaseID:               source.ID,
			DatabaseName:             source.Name,
			DatabaseType:             source.Type,
			Tables:                   tables,
			SkipExistingTableUpdates: skipExisting,
		})
		if err != nil {
			return err
		}

		if wait {
			job, err = waitForMemoryBuild(api, job.JobID)
			if err != nil {
				return err
			}
		}
		return reportMemoryJob(job, len(tables))
	},
}

// schemaTable is the subset of GET /ai_database/schema/:id this command needs.
// inContext is the server's own answer to "does the semantic layer cover this",
// which is more reliable than re-deriving it from the hub listings.
type schemaTable struct {
	TableName string `json:"tableName"`
	InContext bool   `json:"inContext"`
	Columns   []struct {
		Name      string `json:"name"`
		Type      string `json:"type"`
		InContext bool   `json:"inContext"`
	} `json:"columns"`
}

func selectMemoryTables(rawSchema json.RawMessage, selections []string, missingOnly bool) ([]hub.MemoryTable, error) {
	var schema struct {
		Tables []schemaTable `json:"tables"`
	}
	if err := json.Unmarshal(rawSchema, &schema); err != nil {
		return nil, cliexit.New(cliexit.CodeBusiness, "cannot parse the data source schema: %v", err)
	}
	if len(schema.Tables) == 0 {
		return nil, cliexit.New(cliexit.CodeBusiness, "the data source reports no tables")
	}

	wanted, err := parseTableSelections(selections)
	if err != nil {
		return nil, err
	}

	tables := make([]hub.MemoryTable, 0, len(schema.Tables))
	matched := map[string]bool{}
	for _, table := range schema.Tables {
		columnFilter, selected := wanted[table.TableName]
		if len(wanted) > 0 && !selected {
			continue
		}
		matched[table.TableName] = true
		// An explicit selection beats --missing-only: the caller already said
		// which tables they mean.
		if len(wanted) == 0 && missingOnly && table.InContext {
			continue
		}

		columns := make([]hub.MemoryColumn, 0, len(table.Columns))
		for _, column := range table.Columns {
			if column.Name == "" {
				continue
			}
			if len(columnFilter) > 0 && !slices.Contains(columnFilter, column.Name) {
				continue
			}
			columns = append(columns, hub.MemoryColumn{Name: column.Name, Type: column.Type})
		}
		if len(columns) == 0 {
			continue
		}
		tables = append(tables, hub.MemoryTable{TableName: table.TableName, Columns: columns})
	}

	for name := range wanted {
		if !matched[name] {
			return nil, cliexit.Usage("the data source has no table named %q", name)
		}
	}
	if len(tables) == 0 {
		if missingOnly {
			return nil, cliexit.New(cliexit.CodeBusiness,
				"every table is already covered by the semantic layer; drop --missing-only to rebuild them")
		}
		return nil, cliexit.Usage("the selection matched no tables with columns")
	}
	return tables, nil
}

// parseTableSelections accepts `table` and `table:col1,col2`.
func parseTableSelections(selections []string) (map[string][]string, error) {
	wanted := map[string][]string{}
	for _, selection := range selections {
		name, columns, hasColumns := strings.Cut(selection, ":")
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, cliexit.Usage("--table expects a table name, received %q", selection)
		}
		if !hasColumns {
			wanted[name] = nil
			continue
		}
		var list []string
		for _, column := range strings.Split(columns, ",") {
			if trimmed := strings.TrimSpace(column); trimmed != "" {
				list = append(list, trimmed)
			}
		}
		if len(list) == 0 {
			return nil, cliexit.Usage("--table %q lists no columns after the colon", selection)
		}
		wanted[name] = list
	}
	return wanted, nil
}

var hubMemoryBatchCmd = &cobra.Command{
	Use:   "batch <data-source> [data-source...]",
	Short: "Start memory builds for several data sources",
	Long: `The table and column selection is derived server-side, so this needs only
ids. Data sources are named by id here, not by name.

  infini-cli hub memory batch db_1 db_2 --mode missing_only

missing_only (the default) skips what the semantic layer already covers;
full_regenerate proposes fresh descriptions for everything.`,
	Args: minArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mode, _ := cmd.Flags().GetString("mode")
		if mode != "" && !slices.Contains(hub.BatchModes, mode) {
			return cliexit.Usage("--mode must be one of: %s", strings.Join(hub.BatchModes, ", "))
		}
		if mode == "full_regenerate" {
			if err := confirm(fmt.Sprintf("Regenerate descriptions for all %d data source(s)? Existing ones get update candidates.", len(args))); err != nil {
				return err
			}
		}

		api, err := hubAPI()
		if err != nil {
			return err
		}
		result, err := api.StartMemoryBuildBatch(args, mode)
		if err != nil {
			return err
		}

		return output.Success(result, []string{"DATABASE", "NAME", "STATUS", "JOB"}, func() [][]string {
			rows := make([][]string, 0, len(result.Items))
			for _, item := range result.Items {
				jobID := ""
				if item.Job != nil {
					jobID = item.Job.JobID
				}
				rows = append(rows, []string{item.DatabaseID, item.DatabaseName, item.Status, jobID})
			}
			return rows
		})
	},
}

var hubMemoryActiveCmd = &cobra.Command{
	Use:   "active",
	Short: "List memory builds that are still running",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := hubAPI()
		if err != nil {
			return err
		}
		jobs, err := api.ActiveMemoryBuilds()
		if err != nil {
			return err
		}
		return output.Success(jobs, []string{"JOB", "DATABASE", "STATUS", "PROGRESS", "STEP"}, func() [][]string {
			rows := make([][]string, 0, len(jobs))
			for _, job := range jobs {
				rows = append(rows, []string{
					job.JobID, job.DatabaseName, job.Status,
					strconv.Itoa(job.Progress) + "%", job.ActiveStep,
				})
			}
			return rows
		})
	},
}

var hubMemoryStatusCmd = &cobra.Command{
	Use:   "status <job-id>",
	Short: "Show a memory build's progress",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		wait, _ := cmd.Flags().GetBool("wait")

		api, err := hubAPI()
		if err != nil {
			return err
		}

		var job *hub.Job
		if wait {
			job, err = waitForMemoryBuild(api, args[0])
		} else {
			job, err = api.MemoryBuildStatus(args[0])
		}
		if err != nil {
			return err
		}
		if job == nil {
			return cliexit.New(cliexit.CodeBusiness, "no memory build job %s exists for this account", args[0])
		}
		return reportMemoryJob(job, 0)
	},
}

var hubMemoryLatestCmd = &cobra.Command{
	Use:   "latest <data-source>",
	Short: "Show the most recent restorable build for a data source",
	Long: `This is how an interrupted review is picked back up: the job keeps its
candidates so they can be reviewed after the fact.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		source, err := resolveDatabaseID(args[0])
		if err != nil {
			return err
		}
		api, err := hubAPI()
		if err != nil {
			return err
		}
		job, err := api.LatestMemoryBuild(source.ID)
		if err != nil {
			return err
		}
		if job == nil {
			return cliexit.New(cliexit.CodeBusiness, "no restorable memory build exists for %s", source.Name)
		}
		return reportMemoryJob(job, 0)
	},
}

var hubMemoryCancelCmd = &cobra.Command{
	Use:   "cancel [job-id]",
	Short: "Cancel a memory build, or all active ones with --all",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		if all == (len(args) == 1) {
			return cliexit.Usage("pass either a job id or --all")
		}

		api, err := hubAPI()
		if err != nil {
			return err
		}

		if all {
			if err := confirm("Cancel every active memory build?"); err != nil {
				return err
			}
			result, err := api.CancelActiveMemoryBuilds()
			if err != nil {
				return err
			}
			return output.Success(normalizeRaw(result), nil, nil)
		}

		job, err := api.CancelMemoryBuild(args[0])
		if err != nil {
			return err
		}
		if job == nil || job.JobID == "" {
			return cliexit.New(cliexit.CodeBusiness, "no memory build job %s exists for this account", args[0])
		}
		return reportMemoryJob(job, 0)
	},
}

func waitForMemoryBuild(api *hub.API, jobID string) (*hub.Job, error) {
	interval := 3 * time.Second
	deadline := time.Now().Add(30 * time.Minute)

	for {
		job, err := api.MemoryBuildStatus(jobID)
		if err != nil {
			return nil, err
		}
		if job == nil {
			return nil, cliexit.New(cliexit.CodeBusiness, "memory build %s disappeared while waiting", jobID)
		}
		if job.Terminal() {
			return job, nil
		}
		if time.Now().After(deadline) {
			// The build keeps running server-side, so this is a wait timeout
			// rather than a failure.
			return nil, cliexit.Hint(
				cliexit.New(cliexit.CodeBusiness, "memory build %s is still %s after 30m", jobID, job.Status),
				"the build continues server-side; check `%s hub memory status %s`", config.AppName, jobID,
			)
		}
		output.Note("%s %d%% (%s)", job.Status, job.Progress, job.ActiveStep)
		time.Sleep(interval)
	}
}

func reportMemoryJob(job *hub.Job, selectedTables int) error {
	if job.Status == "failed" {
		message := job.Error
		if message == "" {
			message = "memory build failed"
		}
		return cliexit.New(cliexit.CodeBusiness, "memory build %s failed: %s", job.JobID, message)
	}
	if job.Status == "succeeded" {
		output.Note("build finished; run `%s hub review pending` to review the candidates", config.AppName)
	}

	payload := map[string]any{"job": job}
	if selectedTables > 0 {
		payload["selectedTables"] = selectedTables
	}
	return output.Success(payload, []string{"FIELD", "VALUE"}, func() [][]string {
		rows := [][]string{
			{"jobId", job.JobID},
			{"database", job.DatabaseName},
			{"status", job.Status},
			{"progress", strconv.Itoa(job.Progress) + "%"},
			{"activeStep", job.ActiveStep},
		}
		if selectedTables > 0 {
			rows = append(rows, []string{"selectedTables", strconv.Itoa(selectedTables)})
		}
		if job.Error != "" {
			rows = append(rows, []string{"error", firstLine(job.Error)})
		}
		return rows
	})
}

func init() {
	startFlags := hubMemoryStartCmd.Flags()
	startFlags.StringSlice("table", nil, "Table to include, optionally as table:col1,col2 (repeatable; default all)")
	startFlags.Bool("missing-only", false, "Only tables the semantic layer does not cover yet")
	startFlags.Bool("skip-existing-table-updates", false, "Do not propose updates to already-described tables")
	startFlags.Bool("wait", false, "Poll until the build reaches a terminal state")

	hubMemoryBatchCmd.Flags().String("mode", "", "Batch mode: "+strings.Join(hub.BatchModes, ", "))
	hubMemoryStatusCmd.Flags().Bool("wait", false, "Poll until the build reaches a terminal state")
	hubMemoryCancelCmd.Flags().Bool("all", false, "Cancel every active build")

	hubMemoryCmd.AddCommand(
		hubMemoryStartCmd, hubMemoryBatchCmd, hubMemoryActiveCmd,
		hubMemoryStatusCmd, hubMemoryLatestCmd, hubMemoryCancelCmd,
	)
	hubCmd.AddCommand(hubMemoryCmd)
}
