package cmd

import (
	"encoding/json"
	"strings"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/chaozwn/infini-enterprise-cli/internal/task"
	"github.com/spf13/cobra"
)

var taskGraphCmd = &cobra.Command{
	Use:   "graph <id>",
	Short: "Show the notebook DAG a task produced",
	Long: `Every query a task runs becomes a notebook node, and nodes reference each
other, so the graph is what explains how a final number was derived.

Use ` + "`task sql`" + ` on a node's view name to see the SQL it actually ran.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := taskAPI()
		if err != nil {
			return err
		}
		data, err := api.NotebookGraph(args[0])
		if err != nil {
			return err
		}
		return output.Success(data, nil, nil)
	},
}

var taskSQLCmd = &cobra.Command{
	Use:   "sql <id>",
	Short: "Expand a notebook view into native SQL",
	Long: `Infini queries are written against views that reference other views through
infini_ref. This resolves those references down to the SQL the data source
receives, which is what you paste into a database console to reproduce a
result.

  infini-cli task sql task_1 --view kpi_monthly_sales`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		view, _ := cmd.Flags().GetString("view")
		if view == "" {
			return cliexit.Usage("--view is required; run `infini-cli task graph %s` to list node view names", args[0])
		}

		api, err := taskAPI()
		if err != nil {
			return err
		}
		data, err := api.NativeQuerySQL(args[0], view)
		if err != nil {
			return err
		}
		return output.Success(data, nil, nil)
	},
}

var taskKpiSQLCmd = &cobra.Command{
	Use:   "kpi-sql",
	Short: "Run a KPI SQL statement against registered source tables",
	Long: `Runs one statement with the source tables registered up front, the same way a
KPI definition is evaluated. This is the way to validate a KPI's SQL before
saving it to the semantic layer.

  infini-cli task kpi-sql --db chinook --table chinook.artists \
      --sql "SELECT count(*) FROM chinook_artists"

Tables are named <database>.<table> and are exposed to the statement as
<database>_<table>, matching the server's register convention.`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		databases, _ := flags.GetStringSlice("db")
		tableArgs, _ := flags.GetStringSlice("table")
		sql, _ := flags.GetString("sql")
		sqlFile, _ := flags.GetString("sql-file")
		setValues, _ := flags.GetString("set")

		if sql != "" && sqlFile != "" {
			return cliexit.Usage("pass either --sql or --sql-file, not both")
		}
		if sqlFile != "" {
			raw, err := readTextFile(sqlFile)
			if err != nil {
				return err
			}
			sql = raw
		}
		if strings.TrimSpace(sql) == "" {
			return cliexit.Usage("--sql or --sql-file is required")
		}
		if len(databases) == 0 {
			return cliexit.Usage("--db is required; it names the data sources the statement reads")
		}

		tables, err := parseKpiTables(tableArgs)
		if err != nil {
			return err
		}

		api, err := taskAPI()
		if err != nil {
			return err
		}
		data, err := api.RunKpiSQL(task.KpiSQLRequest{
			Databases: databases,
			Tables:    tables,
			SQL:       sql,
			SetValues: setValues,
		})
		if err != nil {
			return err
		}
		return output.Success(data, nil, nil)
	},
}

func parseKpiTables(values []string) ([]task.KpiTable, error) {
	tables := make([]task.KpiTable, 0, len(values))
	for _, value := range values {
		database, name, found := strings.Cut(value, ".")
		if !found || database == "" || name == "" {
			return nil, cliexit.Usage("--table expects <database>.<table>, received %q", value)
		}
		tables = append(tables, task.KpiTable{Database: database, Table: name})
	}
	return tables, nil
}

var taskEvidenceCmd = &cobra.Command{
	Use:   "evidence <id>",
	Short: "Fetch the recorded evidence behind tool calls",
	Long: `Evidence is the stored input and output of a tool call. Fetching it is how a
result is audited without re-running the task.

Evidence ids appear in the task's messages; ` + "`task data <id>`" + ` shows them.

  infini-cli task evidence task_1 --id ev_1 --id ev_2 --include-subagent`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ids, _ := cmd.Flags().GetStringSlice("id")
		includeSubagent, _ := cmd.Flags().GetBool("include-subagent")
		if len(ids) == 0 {
			return cliexit.Usage("--id is required at least once")
		}

		api, err := taskAPI()
		if err != nil {
			return err
		}
		data, err := api.ToolEvidence(args[0], ids, includeSubagent)
		if err != nil {
			return err
		}
		return output.Success(data, nil, nil)
	},
}

var taskMsgCmd = &cobra.Command{
	Use:   "msg <id>",
	Short: "Fetch a message's full payload",
	Long: `Task listings truncate long messages. This returns the stored payload in full.

Identify the message either by its timestamp within a task, or by its own id:

  infini-cli task msg task_1 --ts 1736200000000
  infini-cli task msg --message-id msg_1`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ts, _ := cmd.Flags().GetString("ts")
		messageID, _ := cmd.Flags().GetString("message-id")

		switch {
		case messageID != "" && (len(args) > 0 || ts != ""):
			return cliexit.Usage("--message-id already identifies the message; drop the task id and --ts")
		case messageID == "" && (len(args) != 1 || ts == ""):
			return cliexit.Usage("pass either <id> with --ts, or --message-id")
		}

		api, err := taskAPI()
		if err != nil {
			return err
		}

		var data json.RawMessage
		if messageID != "" {
			data, err = api.UIMessage(messageID)
		} else {
			data, err = api.MessagePayload(args[0], ts)
		}
		if err != nil {
			return err
		}
		return output.Success(data, nil, nil)
	},
}

func init() {
	taskSQLCmd.Flags().String("view", "", "Notebook view name to expand")

	kpiFlags := taskKpiSQLCmd.Flags()
	kpiFlags.StringSlice("db", nil, "Data source name the statement reads (repeatable)")
	kpiFlags.StringSlice("table", nil, "Source table to register as <database>.<table> (repeatable)")
	kpiFlags.String("sql", "", "KPI SQL statement")
	kpiFlags.String("sql-file", "", "Read the statement from a file")
	kpiFlags.String("set", "", "SET statements to run first, joined with ';'")

	taskEvidenceCmd.Flags().StringSlice("id", nil, "Evidence id (repeatable)")
	taskEvidenceCmd.Flags().Bool("include-subagent", false, "Include evidence produced by subagents")

	taskMsgCmd.Flags().String("ts", "", "Message timestamp within the task")
	taskMsgCmd.Flags().String("message-id", "", "Message id, looked up without a task id")

	taskCmd.AddCommand(taskGraphCmd, taskSQLCmd, taskKpiSQLCmd, taskEvidenceCmd, taskMsgCmd)
}
