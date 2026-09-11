package cmd

import (
	"os"
	"strconv"
	"strings"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/ops"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/spf13/cobra"
)

var engineCmd = &cobra.Command{
	Use:   "engine",
	Short: "Manage the Infini-SQL engine",
	Long: `Two different things are called an engine, and this command covers both.

` + "`status`, `start`, `stop`, `ensure` and `logs`" + ` drive the engine process
embedded in this deployment — a single process, shared by everyone on it.

` + "`available`" + ` and ` + "`enabled`" + ` list the engines your account may bind a task
to, which come from the proxy and differ per user. Those are the ids
` + "`agent engine`" + ` and ` + "`schedule --engine`" + ` take.`,
}

var engineStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the embedded engine's status",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := opsAPI()
		if err != nil {
			return err
		}
		raw, err := api.EngineStatus()
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

var engineCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Is the embedded engine running?",
	Long: `Exits 0 when it is running and 3 when it is not, so this works as a shell
guard without parsing the output.`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := opsAPI()
		if err != nil {
			return err
		}
		running, err := api.EngineRunning()
		if err != nil {
			return err
		}
		if err := output.Success(map[string]bool{"running": running}, nil, nil); err != nil {
			return err
		}
		if !running {
			return cliexit.Hint(
				cliexit.New(cliexit.CodeBusiness, "the engine is not running"),
				"start it with `engine start`, or `engine ensure` to start it only if needed",
			)
		}
		return nil
	},
}

var engineStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the embedded engine",
	Long: `  infini-cli engine start
  infini-cli engine start --set spark.executor.memory=4g --set spark.executor.cores=2

Starting an engine that is already up is not idempotent; use ` + "`engine ensure`" + `
from scripts.`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		params, err := engineConfigParams(cmd)
		if err != nil {
			return err
		}
		api, err := opsAPI()
		if err != nil {
			return err
		}
		raw, err := api.StartEngine(params)
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

var engineEnsureCmd = &cobra.Command{
	Use:   "ensure",
	Short: "Start the embedded engine only if it is not already running",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		params, err := engineConfigParams(cmd)
		if err != nil {
			return err
		}
		api, err := opsAPI()
		if err != nil {
			return err
		}
		raw, err := api.EnsureEngine(params)
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

var engineStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the embedded engine",
	Long: `The engine is shared by everyone on this deployment, so stopping it breaks
every running query, not just yours.`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := confirm("Stop the engine? Every query running on this deployment fails."); err != nil {
			return err
		}
		api, err := opsAPI()
		if err != nil {
			return err
		}
		raw, err := api.StopEngine()
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

func engineConfigParams(cmd *cobra.Command) ([]ops.ConfigParam, error) {
	pairs, _ := cmd.Flags().GetStringSlice("set")
	params := make([]ops.ConfigParam, 0, len(pairs))
	for _, pair := range pairs {
		key, value, found := strings.Cut(pair, "=")
		if !found || key == "" {
			return nil, cliexit.Usage("--set expects key=value, received %q", pair)
		}
		params = append(params, ops.ConfigParam{Key: key, Value: value})
	}
	return params, nil
}

var engineLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Print the embedded engine's log tail",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		limit, _ := cmd.Flags().GetInt("limit")
		api, err := opsAPI()
		if err != nil {
			return err
		}
		logs, err := api.EngineLogs(limit)
		if err != nil {
			return err
		}
		// Log lines are read, not parsed, so plain text beats a JSON array of
		// strings when a terminal is watching.
		if output.CurrentFormat() == output.FormatTable {
			for _, line := range logs {
				if _, err := os.Stdout.WriteString(line + "\n"); err != nil {
					return err
				}
			}
			return nil
		}
		return output.Success(logs, nil, nil)
	},
}

var engineAvailableCmd = &cobra.Command{
	Use:   "available",
	Short: "List the engines your account can bind a task to",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := opsAPI()
		if err != nil {
			return err
		}
		engines, err := api.AvailableEngines()
		if err != nil {
			return err
		}
		return output.Success(engines, []string{"ID", "NAME", "TYPE", "STATUS", "ENABLED"}, func() [][]string {
			rows := make([][]string, 0, len(engines))
			for _, engine := range engines {
				rows = append(rows, []string{
					engine.ID, engine.Name, engine.Type, engine.Status,
					strconv.FormatBool(engine.Enabled),
				})
			}
			return rows
		})
	},
}

var engineEnabledCmd = &cobra.Command{
	Use:   "enabled",
	Short: "Show the engine your account currently uses",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := opsAPI()
		if err != nil {
			return err
		}
		raw, err := api.EnabledEngine()
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

func init() {
	engineStartCmd.Flags().StringSlice("set", nil, "Engine setting as key=value (repeatable)")
	engineEnsureCmd.Flags().StringSlice("set", nil, "Engine setting as key=value (repeatable)")
	engineLogsCmd.Flags().Int("limit", 1000, "How many trailing lines to fetch")

	engineCmd.AddCommand(
		engineStatusCmd, engineCheckCmd, engineStartCmd, engineEnsureCmd,
		engineStopCmd, engineLogsCmd, engineAvailableCmd, engineEnabledCmd,
	)
	rootCmd.AddCommand(engineCmd)
}
