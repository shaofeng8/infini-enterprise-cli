package cmd

import (
	"strconv"

	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/chaozwn/infini-enterprise-cli/internal/task"
	"github.com/spf13/cobra"
)

var taskPublicCmd = &cobra.Command{
	Use:   "public",
	Short: "Read a task you do not own",
	Long: `Two readers share these commands, and they are not the same thing.

Without --audit this is the public path: no credentials at all, and it only
works on a task its owner shared with ` + "`task share`" + `. That is the same view a
shared link gives.

With --audit it is the compliance path: it needs a proxy super-administrator
token, reaches any task whether or not it was shared, and every read is logged
server-side with who read what. Use it when you have to answer for a result,
not as a way around an unshared task.

The point of this group is verifiability. ` + "`task public evidence`" + ` turns the
citations in a shared report back into the queries that produced them, so a
reader can check a number instead of trusting it.`,
}

// addAuditFlag is on every subcommand rather than the group, because audit is
// a per-read privileged act and the flag should be visible at the point of use.
func addAuditFlag(cmds ...*cobra.Command) {
	for _, cmd := range cmds {
		cmd.Flags().Bool("audit", false, "Read as a super-administrator; works on unshared tasks and is logged")
	}
}

func readonlyMode(cmd *cobra.Command) task.ReadonlyMode {
	audit, _ := cmd.Flags().GetBool("audit")
	return task.ReadonlyMode{Audit: audit}
}

var taskPublicShowCmd = &cobra.Command{
	Use:   "show <task-id>",
	Short: "Read a task's conversation",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := taskAPI()
		if err != nil {
			return err
		}
		raw, err := api.PublicTask(args[0], readonlyMode(cmd))
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

var taskPublicMessageCmd = &cobra.Command{
	Use:   "message <task-id> <ts>",
	Short: "Read one message in full",
	Long: `The timestamp is the message's ts, as it appears in the conversation. A
truncated conversation view is where you would go looking for one.`,
	Args: exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := taskAPI()
		if err != nil {
			return err
		}
		raw, err := api.PublicMessage(args[0], args[1], readonlyMode(cmd))
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

var taskPublicEvidenceCmd = &cobra.Command{
	Use:   "evidence <task-id> <ts>...",
	Short: "Resolve the tool calls a report cites",
	Long: `A shared report cites its evidence by message timestamp. This resolves those
citations into the actual tool calls — the query that ran, and what came back.

  infini-cli task public evidence task_1 1784807100587 1784807100999

Up to 100 citations per request. Pass --include-subagent when the report drew
on delegated work, since a subagent's evidence lives on its own task.`,
	Args: minArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		includeSubagent, _ := cmd.Flags().GetBool("include-subagent")

		api, err := taskAPI()
		if err != nil {
			return err
		}
		raw, err := api.PublicToolEvidence(args[0], args[1:], includeSubagent, readonlyMode(cmd))
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

var taskPublicFilesCmd = &cobra.Command{
	Use:   "files <task-id>",
	Short: "List the files a task produced",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := taskAPI()
		if err != nil {
			return err
		}
		raw, err := api.PublicFileTree(args[0], readonlyMode(cmd))
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

var taskPublicPreviewCmd = &cobra.Command{
	Use:   "preview <task-id> <file>",
	Short: "Preview a file's contents",
	Long: `Returns a parsed preview rather than raw bytes, so a spreadsheet comes back
as rows. Use ` + "`task public get`" + ` for the file itself.`,
	Args: exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := taskAPI()
		if err != nil {
			return err
		}
		raw, err := api.PublicPreviewFile(args[0], args[1], readonlyMode(cmd))
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

var taskPublicGetCmd = &cobra.Command{
	Use:     "get <task-id> <file>",
	Aliases: []string{"download"},
	Short:   "Download one file",
	Args:    exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		dest, _ := cmd.Flags().GetString("out")

		api, err := taskAPI()
		if err != nil {
			return err
		}
		result, err := api.PublicDownloadFile(args[0], args[1], dest, readonlyMode(cmd))
		if err != nil {
			return err
		}
		return reportDownload(result.Path, result.Bytes, result.ContentType)
	},
}

var taskPublicZipCmd = &cobra.Command{
	Use:   "zip <task-id>",
	Short: "Download the whole workspace as a zip archive",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dest, _ := cmd.Flags().GetString("out")

		api, err := taskAPI()
		if err != nil {
			return err
		}
		result, err := api.PublicDownloadZip(args[0], dest, readonlyMode(cmd))
		if err != nil {
			return err
		}
		return reportDownload(result.Path, result.Bytes, "application/zip")
	},
}

func reportDownload(path string, bytes int64, contentType string) error {
	return output.Success(map[string]any{
		"path":        path,
		"bytes":       bytes,
		"contentType": contentType,
	}, []string{"FIELD", "VALUE"}, func() [][]string {
		return [][]string{
			{"path", path},
			{"bytes", strconv.FormatInt(bytes, 10)},
			{"contentType", contentType},
		}
	})
}

func init() {
	taskPublicEvidenceCmd.Flags().Bool("include-subagent", false, "Also search delegated subagent tasks")
	taskPublicGetCmd.Flags().String("out", "", "Destination file or directory")
	taskPublicZipCmd.Flags().String("out", "", "Destination file or directory")

	addAuditFlag(
		taskPublicShowCmd, taskPublicMessageCmd, taskPublicEvidenceCmd,
		taskPublicFilesCmd, taskPublicPreviewCmd, taskPublicGetCmd, taskPublicZipCmd,
	)
	taskPublicCmd.AddCommand(
		taskPublicShowCmd, taskPublicMessageCmd, taskPublicEvidenceCmd,
		taskPublicFilesCmd, taskPublicPreviewCmd, taskPublicGetCmd, taskPublicZipCmd,
	)
	taskCmd.AddCommand(taskPublicCmd)
}
