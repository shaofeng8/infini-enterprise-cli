package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/dashboard"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/spf13/cobra"
)

var dashRefreshCmd = &cobra.Command{
	Use:   "refresh <id>",
	Short: "Submit an async refresh of a dashboard's queries",
	Long: `Queues a refresh job and returns immediately (HTTP 202).

With --wait the command polls until the job reaches a terminal state, which is
what a scheduled or CI-driven refresh usually wants:

  infini-cli dash refresh abc --wait --force-refresh

Exit status with --wait: 0 when every query succeeded, 1 when any query failed
or the job was cancelled.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		queryIDs, _ := cmd.Flags().GetStringArray("query-ids")
		wait, _ := cmd.Flags().GetBool("wait")
		interval, _ := cmd.Flags().GetDuration("poll-interval")
		deadline, _ := cmd.Flags().GetDuration("wait-timeout")

		api, err := dashAPI()
		if err != nil {
			return err
		}
		_, spec, err := api.GetWithSpec(args[0])
		if err != nil {
			return err
		}
		if len(queryIDs) == 0 {
			queryIDs = spec.DataQueryIDs()
			if len(queryIDs) == 0 {
				return cliexit.New(cliexit.CodeBusiness, "dashboard %s has no data queries", args[0])
			}
		}
		filters, err := resolveFilters(cmd, spec)
		if err != nil {
			return err
		}

		job, err := api.CreateRefresh(args[0], queryIDs, filters, queryOptions(cmd))
		if err != nil {
			return err
		}
		if !wait {
			output.Note("Refresh %s queued for %d query(s)", job.ID, job.TotalQueries)
			return output.Success(job, nil, nil)
		}

		final, err := waitForRefresh(api, args[0], job, interval, deadline)
		if err != nil {
			return err
		}
		if err := output.Success(final, refreshHeaders, func() [][]string {
			return refreshRows(final)
		}); err != nil {
			return err
		}
		return refreshExit(final)
	},
}

// waitForRefresh polls until the job stops moving. Progress goes to stderr so
// stdout stays a single parseable document.
func waitForRefresh(api *dashboard.API, dashboardID string, job *dashboard.RefreshJob, interval, deadline time.Duration) (*dashboard.RefreshJob, error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if deadline > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, deadline)
		defer cancel()
	}

	lastCompleted := -1
	for {
		if job.Terminal() {
			return job, nil
		}
		if completed := job.CompletedQueries; completed != lastCompleted {
			output.Note("refresh %s: %s %d/%d done (%d failed)",
				job.ID, job.Status, completed, job.TotalQueries, job.FailedQueries)
			lastCompleted = completed
		}

		select {
		case <-ctx.Done():
			// Leave the server-side job running: cancelling it here would throw
			// away work the next poll could have collected.
			return nil, cliexit.Hint(
				cliexit.New(cliexit.CodeBusiness, "stopped waiting for refresh %s (still %s)", job.ID, job.Status),
				"check it later with `dash refresh status %s %s`", dashboardID, job.ID,
			)
		case <-time.After(interval):
		}

		updated, err := api.GetRefresh(dashboardID, job.ID)
		if err != nil {
			return nil, err
		}
		job = updated
	}
}

var refreshHeaders = []string{"QUERY", "STATUS", "MS", "ERROR"}

func refreshRows(job *dashboard.RefreshJob) [][]string {
	rows := make([][]string, 0, len(job.Queries))
	for _, query := range job.Queries {
		rows = append(rows, []string{
			query.QueryID,
			query.Status,
			intOrDash(query.DurationMs),
			firstLine(query.ErrorMessage),
		})
	}
	return rows
}

func refreshExit(job *dashboard.RefreshJob) error {
	switch job.Status {
	case "completed":
		return nil
	case "completed_with_errors":
		return cliexit.Hint(
			cliexit.New(cliexit.CodeBusiness, "refresh finished with %d failed query(s)", job.FailedQueries),
			"inspect a failure with `dash refresh result <id> %s <queryId>`", job.ID,
		)
	case "canceled":
		return cliexit.New(cliexit.CodeBusiness, "refresh %s was cancelled", job.ID)
	default:
		message := job.Error
		if message == "" {
			message = "refresh failed"
		}
		return cliexit.New(cliexit.CodeBusiness, "refresh %s: %s", job.ID, message)
	}
}

var dashRefreshStatusCmd = &cobra.Command{
	Use:   "status <id> <refreshId>",
	Short: "Show a refresh job's progress",
	Args:  exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := dashAPI()
		if err != nil {
			return err
		}
		job, err := api.GetRefresh(args[0], args[1])
		if err != nil {
			return err
		}
		return output.Success(job, refreshHeaders, func() [][]string { return refreshRows(job) })
	},
}

var dashRefreshActiveCmd = &cobra.Command{
	Use:   "active <id>",
	Short: "Show the refresh job currently running for this user, if any",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := dashAPI()
		if err != nil {
			return err
		}
		job, err := api.ActiveRefresh(args[0])
		if err != nil {
			return err
		}
		if job == nil {
			output.Note("No active refresh for dashboard %s", args[0])
			return output.Success(nil, nil, nil)
		}
		return output.Success(job, refreshHeaders, func() [][]string { return refreshRows(job) })
	},
}

var dashRefreshResultCmd = &cobra.Command{
	Use:   "result <id> <refreshId> <queryId>",
	Short: "Read one query's result from a refresh job",
	Args:  exactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := dashAPI()
		if err != nil {
			return err
		}
		result, err := api.RefreshResult(args[0], args[1], args[2])
		if err != nil {
			return err
		}
		if result.Status != "ok" {
			return cliexit.New(cliexit.CodeBusiness, "query %s failed (%s): %s",
				args[2], result.ErrorCode, result.ErrorMessage)
		}
		return output.Success(result, result.Columns, func() [][]string {
			return stringifyRows(result.Datas)
		})
	},
}

var dashRefreshCancelCmd = &cobra.Command{
	Use:   "cancel <id> <refreshId>",
	Short: "Cancel a running refresh job",
	Args:  exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := confirm(fmt.Sprintf("Cancel refresh %s on dashboard %s?", args[1], args[0])); err != nil {
			return err
		}
		api, err := dashAPI()
		if err != nil {
			return err
		}
		job, err := api.CancelRefresh(args[0], args[1])
		if err != nil {
			return err
		}
		output.Note("Refresh %s is now %s (%d/%d queries had finished)",
			job.ID, job.Status, job.CompletedQueries, job.TotalQueries)
		return output.Success(job, nil, nil)
	},
}

func init() {
	dashRefreshCmd.Flags().StringArray("query-ids", nil, "Query ids to refresh (default: every data query)")
	dashRefreshCmd.Flags().Bool("wait", false, "Poll until the job finishes")
	dashRefreshCmd.Flags().Duration("poll-interval", 2*time.Second, "Polling interval used with --wait")
	dashRefreshCmd.Flags().Duration("wait-timeout", 10*time.Minute, "Give up waiting after this long (0 = no limit)")
	addFilterFlags(dashRefreshCmd)
	addCacheFlags(dashRefreshCmd)

	dashRefreshCmd.AddCommand(dashRefreshStatusCmd, dashRefreshActiveCmd, dashRefreshResultCmd, dashRefreshCancelCmd)
	dashCmd.AddCommand(dashRefreshCmd)
}
