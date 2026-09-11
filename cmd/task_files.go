package cmd

import (
	"strconv"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/spf13/cobra"
)

var taskWorkspaceCmd = &cobra.Command{
	Use:   "workspace <id>",
	Short: "Show a task's workspace location and metadata",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := taskAPI()
		if err != nil {
			return err
		}
		data, err := api.Workspace(args[0])
		if err != nil {
			return err
		}
		return output.Success(data, nil, nil)
	},
}

var taskFileCmd = &cobra.Command{
	Use:   "file",
	Short: "Browse and fetch files a task produced",
	Long: `Each task gets its own working directory. Charts, exports and intermediate
data land there, and these commands are how you get them out.`,
}

var taskFileLsCmd = &cobra.Command{
	Use:   "ls <id>",
	Short: "List files in a task's workspace",
	Long: `Prints the workspace tree flattened to one path per line.

  infini-cli task file ls task_1 --table
  infini-cli task file ls task_1 --files-only --table`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		filesOnly, _ := cmd.Flags().GetBool("files-only")

		api, err := taskAPI()
		if err != nil {
			return err
		}
		tree, err := api.FileTree(args[0])
		if err != nil {
			return err
		}
		if tree.Truncated {
			output.Note("warning: the listing was truncated; the workspace is too large to walk in full")
		}

		nodes := tree.Flatten(filesOnly)
		return output.Success(tree, []string{"PATH", "KIND", "SIZE", "MODIFIED"}, func() [][]string {
			rows := make([][]string, 0, len(nodes))
			for _, node := range nodes {
				kind := "file"
				size := node.Size
				if node.IsDir {
					kind = "dir"
					size = ""
				}
				rows = append(rows, []string{node.Key, kind, size, node.CreatedAt})
			}
			return rows
		})
	},
}

var taskFilePreviewCmd = &cobra.Command{
	Use:   "preview <id> <file>",
	Short: "Preview a workspace file's contents",
	Long: `Returns a parsed preview rather than raw bytes, so a spreadsheet comes back
as rows. Use ` + "`task file get`" + ` when you want the file itself.

  infini-cli task file preview task_1 data/result.csv`,
	Args: exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := taskAPI()
		if err != nil {
			return err
		}
		data, err := api.PreviewFile(args[0], args[1])
		if err != nil {
			return err
		}
		return output.Success(data, nil, nil)
	},
}

var taskFileGetCmd = &cobra.Command{
	Use:     "get <id> <file>",
	Aliases: []string{"download"},
	Short:   "Download a file from a task's workspace",
	Long: `Streams the file to disk. --out takes a file path or an existing directory;
without it the file keeps its own name in the current directory.

  infini-cli task file get task_1 data/result.csv
  infini-cli task file get task_1 charts/sales.png --out ./downloads/`,
	Args: exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		dest, _ := cmd.Flags().GetString("out")

		api, err := taskAPI()
		if err != nil {
			return err
		}
		result, err := api.DownloadFile(args[0], args[1], dest)
		if err != nil {
			return err
		}
		return output.Success(result, []string{"FIELD", "VALUE"}, func() [][]string {
			return [][]string{
				{"path", result.Path},
				{"bytes", strconv.FormatInt(result.Bytes, 10)},
				{"contentType", result.ContentType},
			}
		})
	},
}

var taskZipCmd = &cobra.Command{
	Use:   "zip <id>",
	Short: "Download a task's whole workspace as a zip archive",
	Long: `Streams the archive to disk instead of buffering it, so a large workspace is
not limited by memory.

  infini-cli task zip task_1 --out ./task_1.zip`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dest, _ := cmd.Flags().GetString("out")

		api, err := taskAPI()
		if err != nil {
			return err
		}
		result, err := api.DownloadZip(args[0], dest)
		if err != nil {
			return err
		}
		if result.Bytes == 0 {
			return cliexit.New(cliexit.CodeBusiness, "the archive for %s is empty", args[0])
		}
		return output.Success(result, []string{"FIELD", "VALUE"}, func() [][]string {
			return [][]string{
				{"path", result.Path},
				{"bytes", strconv.FormatInt(result.Bytes, 10)},
			}
		})
	},
}

func init() {
	taskFileLsCmd.Flags().Bool("files-only", false, "Omit directories from the listing")
	taskFileGetCmd.Flags().String("out", "", "Destination file or directory")
	taskZipCmd.Flags().String("out", "", "Destination file or directory")

	taskFileCmd.AddCommand(taskFileLsCmd, taskFilePreviewCmd, taskFileGetCmd)
	taskCmd.AddCommand(taskWorkspaceCmd, taskFileCmd, taskZipCmd)
}
