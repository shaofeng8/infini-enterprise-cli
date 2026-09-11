package cmd

import (
	"strconv"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/chaozwn/infini-enterprise-cli/internal/storage"
	"github.com/spf13/cobra"
)

func storageAPI() (*storage.API, error) {
	c, err := client.New()
	if err != nil {
		return nil, err
	}
	return storage.NewAPI(c), nil
}

var fsCmd = &cobra.Command{
	Use:   "fs",
	Short: "Manage your file directories",
	Long: `Files live in three unrelated places, and this command owns one of them.

` + "`fs`" + ` is your own directory tree — where you stage a CSV before importing
it as a data source, for example. A task's own outputs are under
` + "`task file`" + `, and mid-transfer chunks belong to an upload session
(` + "`fs session`" + `).

For anything large, prefer ` + "`fs session push`" + ` over ` + "`fs put`" + `: it can
resume, which a single request cannot.`,
}

var fsListCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List your directories",
	Args:    exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := storageAPI()
		if err != nil {
			return err
		}
		directories, err := api.Directories()
		if err != nil {
			return err
		}
		return output.Success(directories, []string{"DIRECTORY"}, func() [][]string {
			rows := make([][]string, 0, len(directories))
			for _, directory := range directories {
				rows = append(rows, []string{directory})
			}
			return rows
		})
	},
}

var fsTreeCmd = &cobra.Command{
	Use:   "tree",
	Short: "Print your file tree",
	Long: `  infini-cli fs tree --table
  infini-cli fs tree --search sales --files-only --table`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		keyword, _ := cmd.Flags().GetString("search")
		filesOnly, _ := cmd.Flags().GetBool("files-only")

		api, err := storageAPI()
		if err != nil {
			return err
		}
		tree, err := api.FileTree(keyword)
		if err != nil {
			return err
		}
		nodes := storage.Flatten(tree, filesOnly)
		return output.Success(tree, []string{"PATH", "KIND", "SIZE", "CREATED"}, func() [][]string {
			rows := make([][]string, 0, len(nodes))
			for _, node := range nodes {
				kind, size := "file", node.Size
				if node.IsDir {
					kind, size = "dir", ""
				}
				rows = append(rows, []string{node.Key, kind, size, node.CreatedAt})
			}
			return rows
		})
	},
}

var fsMkdirCmd = &cobra.Command{
	Use:   "mkdir <name>",
	Short: "Create a directory",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := storageAPI()
		if err != nil {
			return err
		}
		raw, err := api.CreateDirectory(args[0])
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

var fsRmdirCmd = &cobra.Command{
	Use:   "rmdir <name>",
	Short: "Delete a directory and everything in it",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := confirm("Delete directory " + args[0] + " and everything in it?"); err != nil {
			return err
		}
		api, err := storageAPI()
		if err != nil {
			return err
		}
		raw, err := api.DeleteDirectory(args[0])
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

var fsPutCmd = &cobra.Command{
	Use:     "put <directory> <file>",
	Aliases: []string{"upload"},
	Short:   "Upload a file to one of your directories",
	Long: `One request, no resume. The size ceiling is a deployment setting;
` + "`fs config`" + ` reports it.

  infini-cli fs put my-folder ./sales.csv`,
	Args: exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := storageAPI()
		if err != nil {
			return err
		}
		result, err := api.Upload(args[0], args[1])
		if err != nil {
			return err
		}
		return output.Success(result, nil, nil)
	},
}

var fsConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "Show the upload size limits this deployment enforces",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := storageAPI()
		if err != nil {
			return err
		}
		raw, err := api.UploadConfig()
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

var fsGetCmd = &cobra.Command{
	Use:     "get <id>",
	Aliases: []string{"download"},
	Short:   "Download a stored file by id",
	Long: `--out takes a file path or an existing directory; without it the file keeps
its own name in the current directory.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dest, _ := cmd.Flags().GetString("out")

		api, err := storageAPI()
		if err != nil {
			return err
		}
		result, err := api.Download(args[0], dest)
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

var fsRemoveCmd = &cobra.Command{
	Use:     "rm <id>...",
	Aliases: []string{"delete"},
	Short:   "Delete stored files",
	Args:    minArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := confirm("Delete " + strconv.Itoa(len(args)) + " file(s)?"); err != nil {
			return err
		}
		api, err := storageAPI()
		if err != nil {
			return err
		}
		raw, err := api.Delete(args)
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

var fsTaskPutCmd = &cobra.Command{
	Use:   "task-put <task-id> <file>",
	Short: "Upload a file into a task's workspace",
	Long: `Gives the agent something to work on mid-conversation.

  infini-cli fs task-put task_1 ./raw.csv --subdir data
  infini-cli fs task-put task_1 ./chart.png --naming hash

Task uploads go through a content-addressed store, so --naming hash makes
re-uploading identical bytes a no-op. The default keeps the original name and
overwrites.`,
	Args: exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		subdir, _ := flags.GetString("subdir")
		naming, _ := flags.GetString("naming")
		if naming != "" {
			if err := requireChoice("--naming", naming, storage.TaskNamingStrategies); err != nil {
				return err
			}
		}

		api, err := storageAPI()
		if err != nil {
			return err
		}
		result, err := api.UploadToTask(args[0], args[1], subdir, naming)
		if err != nil {
			return err
		}
		return output.Success(result, nil, nil)
	},
}

func init() {
	fsTreeCmd.Flags().String("search", "", "Filter by filename")
	fsTreeCmd.Flags().Bool("files-only", false, "Omit directories from the listing")
	fsGetCmd.Flags().String("out", "", "Destination file or directory")
	fsTaskPutCmd.Flags().String("subdir", "", "Subdirectory within the workspace, e.g. data/raw")
	fsTaskPutCmd.Flags().String("naming", "", "Naming strategy: original (default) or hash")

	fsCmd.AddCommand(
		fsListCmd, fsTreeCmd, fsMkdirCmd, fsRmdirCmd, fsPutCmd,
		fsConfigCmd, fsGetCmd, fsRemoveCmd, fsTaskPutCmd,
	)
	rootCmd.AddCommand(fsCmd)
}
