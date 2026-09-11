package cmd

import (
	"os"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/spf13/cobra"
)

// drainTokenEnvVar keeps the internal token out of the command line, where it
// would sit in shell history and in the process list.
const drainTokenEnvVar = "INFINI_INTERNAL_TOKEN"

var runtimeCmd = &cobra.Command{
	Use:   "runtime",
	Short: "Inspect the running fleet",
	Long: `Tasks do not execute in the API process. A worker leases a task, runs it, and
renews the lease; if the worker dies the lease expires and another picks the
task up. That is why a task can look queued forever with nothing wrong in the
API logs — and why ` + "`runtime execution`" + ` is usually the first place to look.

` + "`instances`" + ` distinguishes what an instance says about itself from what the
registry concludes: an instance whose heartbeat has lapsed reads offline
regardless of the status it last reported.`,
}

var runtimeInstancesCmd = &cobra.Command{
	Use:   "instances",
	Short: "List registered API, worker and browser-gateway instances",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := opsAPI()
		if err != nil {
			return err
		}
		instances, err := api.Instances()
		if err != nil {
			return err
		}
		return output.Success(instances, []string{"INSTANCE", "ROLE", "REPORTED", "EFFECTIVE", "HOST", "HEARTBEAT"}, func() [][]string {
			rows := make([][]string, 0, len(instances))
			for _, instance := range instances {
				rows = append(rows, []string{
					instance.InstanceID, instance.Role, instance.Status,
					instance.EffectiveStatus, instance.Host, instance.HeartbeatAt,
				})
			}
			return rows
		})
	},
}

var runtimeExecutionCmd = &cobra.Command{
	Use:   "execution <task-id>",
	Short: "Show which worker holds a task, and its lease",
	Long: `Answers why a task is not progressing: whether anything has leased it, which
worker did, when the lease expires, and how many times it has been retried.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := opsAPI()
		if err != nil {
			return err
		}
		raw, err := api.TaskExecution(args[0])
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

var runtimeReadyCmd = &cobra.Command{
	Use:   "ready",
	Short: "Probe worker readiness",
	Long: `This is the endpoint Kubernetes probes. It answers 503 rather than an error
body when the worker is not ready, so a non-zero exit here means "not ready",
not "the request failed".`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := opsAPI()
		if err != nil {
			return err
		}
		raw, err := api.WorkerReady()
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

var runtimeAutoscalingCmd = &cobra.Command{
	Use:   "autoscaling",
	Short: "Print the workload metric an autoscaler consumes",
	Long: `The workload figure combines queued agent tasks with queued memory builds,
because both compete for the same workers.`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := opsAPI()
		if err != nil {
			return err
		}
		raw, err := api.Autoscaling()
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

var runtimeDrainCmd = &cobra.Command{
	Use:   "drain",
	Short: "Tell a worker to stop accepting work",
	Long: `Draining is what a graceful shutdown does: the worker finishes the tasks it
holds and takes no new ones. It does not come back on its own — the process is
expected to exit.

Which worker drains depends on which one your request reaches. Against a
load-balanced endpoint that is effectively arbitrary, so point --server at a
specific instance when you mean a specific worker.

The token comes from ` + drainTokenEnvVar + ` rather than a flag. A deployment
that has not configured INTERNAL_HTTP_TOKEN accepts an unauthenticated drain,
so treat reachability of this endpoint as privileged either way.`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := confirm("Drain this worker? It stops taking new tasks and is expected to exit."); err != nil {
			return err
		}
		api, err := opsAPI()
		if err != nil {
			return err
		}
		token := os.Getenv(drainTokenEnvVar)
		raw, err := api.Drain(token)
		if err != nil {
			if token == "" {
				return cliexit.Hint(err, "if the deployment sets INTERNAL_HTTP_TOKEN, export "+drainTokenEnvVar)
			}
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

func init() {
	runtimeCmd.AddCommand(
		runtimeInstancesCmd, runtimeExecutionCmd, runtimeReadyCmd,
		runtimeAutoscalingCmd, runtimeDrainCmd,
	)
	rootCmd.AddCommand(runtimeCmd)
}
