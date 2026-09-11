package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/chaozwn/infini-enterprise-cli/internal/rag"
	"github.com/spf13/cobra"
)

// secretEnvVar is where object storage credentials are read from, since a
// secret passed as a flag would land in shell history and the process list.
const secretEnvVar = "INFINI_STORAGE_SECRET"

var ragCmd = &cobra.Command{
	Use:   "rag",
	Short: "Manage knowledge bases",
	Long: `A knowledge base is a document directory the agent can retrieve from. The
documents themselves stay where they are: a local path, or an OSS, S3 or COS
bucket.

Binding a knowledge base to a data source is what lets the agent use documents
to interpret that data source's tables.`,
}

func ragAPI() (*rag.API, error) {
	c, err := client.New()
	if err != nil {
		return nil, err
	}
	return rag.NewAPI(c), nil
}

var ragLsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List knowledge bases",
	Long: `  infini-cli rag ls --table
  infini-cli rag ls --keyword 财报 --table
  infini-cli rag ls --all --table    # every accessible one, unpaged`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		all, _ := flags.GetBool("all")

		source, _ := flags.GetString("source")
		if source != "" && !slices.Contains(rag.Sources, source) {
			return cliexit.Usage("--source must be one of: %s", strings.Join(rag.Sources, ", "))
		}

		api, err := ragAPI()
		if err != nil {
			return err
		}

		toRows := func(items []rag.Item) [][]string {
			rows := make([][]string, 0, len(items))
			for _, item := range items {
				rows = append(rows, []string{
					item.ID, item.Name, item.Nickname,
					intOrDash(item.Enabled), item.DocDir,
				})
			}
			return rows
		}
		headers := []string{"ID", "NAME", "NICKNAME", "ENABLED", "DOC DIR"}

		if all {
			items, err := api.ListAll()
			if err != nil {
				return err
			}
			return output.Success(items, headers, func() [][]string { return toRows(items) })
		}

		var enabled *int
		if flags.Changed("enabled") {
			value := 0
			if on, _ := flags.GetBool("enabled"); on {
				value = 1
			}
			enabled = &value
		}
		page, _ := flags.GetInt("page")
		pageSize, _ := flags.GetInt("page-size")
		keyword, _ := flags.GetString("keyword")

		result, err := api.List(rag.ListQuery{
			Page:     page,
			PageSize: pageSize,
			Keyword:  keyword,
			Enabled:  enabled,
			Source:   source,
		})
		if err != nil {
			return err
		}
		return output.Success(result, headers, func() [][]string { return toRows(result.Items) })
	},
}

var ragShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show a knowledge base",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := ragAPI()
		if err != nil {
			return err
		}
		item, err := api.Get(args[0])
		if err != nil {
			return err
		}
		return output.Success(item, []string{"FIELD", "VALUE"}, func() [][]string {
			linked := make([]string, 0, len(item.LinkedDatabases))
			for _, db := range item.LinkedDatabases {
				linked = append(linked, db.Name)
			}
			return [][]string{
				{"id", item.ID},
				{"name", item.Name},
				{"nickname", item.Nickname},
				{"enabled", intOrDash(item.Enabled)},
				{"docDir", item.DocDir},
				{"requiredExts", item.RequiredExts},
				{"ragDocFilterRelevance", item.DocFilterRelance},
				{"linkedDatabases", strings.Join(linked, ", ")},
				{"description", firstLine(item.Description)},
			}
		})
	},
}

var ragCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a knowledge base",
	Long: `  infini-cli rag create --name finance_docs --doc-dir /data/finance \
      --ext pdf --ext md --description "季度财报"`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		name, _ := flags.GetString("name")
		if name == "" {
			return cliexit.Usage("--name is required")
		}

		nickname, _ := flags.GetString("nickname")
		description, _ := flags.GetString("description")
		docDir, _ := flags.GetString("doc-dir")
		exts, _ := flags.GetStringSlice("ext")
		relevance, _ := flags.GetString("relevance")
		databaseIDs, _ := flags.GetStringSlice("database")
		disabled, _ := flags.GetBool("disabled")

		enabled := 1
		if disabled {
			enabled = 0
		}

		api, err := ragAPI()
		if err != nil {
			return err
		}
		result, err := api.Create(rag.Spec{
			Name:         name,
			Nickname:     nickname,
			Description:  description,
			DocDir:       docDir,
			RequiredExts: normalizeExts(exts),
			DocRelevance: relevance,
			Enabled:      &enabled,
			DatabaseIDs:  databaseIDs,
		})
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

var ragUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a knowledge base",
	Long: `The server's update endpoint reuses the create payload, so this reads the
current record first and overlays only the flags you passed.

  infini-cli rag update rag_1 --nickname 财报库
  infini-cli rag update rag_1 --ext pdf --ext docx`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		changeable := []string{"name", "nickname", "description", "doc-dir", "ext", "relevance", "database"}
		changed := false
		for _, flag := range changeable {
			if flags.Changed(flag) {
				changed = true
				break
			}
		}
		if !changed {
			return cliexit.Usage("nothing to update; pass at least one of --%s", strings.Join(changeable, ", --"))
		}

		api, err := ragAPI()
		if err != nil {
			return err
		}
		current, err := api.Get(args[0])
		if err != nil {
			return err
		}
		if current.ID == "" {
			return cliexit.New(cliexit.CodeBusiness, "knowledge base %s was not found", args[0])
		}

		spec := rag.Spec{
			Name:         current.Name,
			Nickname:     current.Nickname,
			Description:  current.Description,
			DocDir:       current.DocDir,
			DocRelevance: current.DocFilterRelance,
			Enabled:      current.Enabled,
			// The stored value is JSON text while the DTO wants an array.
			RequiredExts: parseStoredExts(current.RequiredExts),
		}
		for _, db := range current.LinkedDatabases {
			spec.DatabaseIDs = append(spec.DatabaseIDs, db.ID)
		}

		if flags.Changed("name") {
			spec.Name, _ = flags.GetString("name")
		}
		if flags.Changed("nickname") {
			spec.Nickname, _ = flags.GetString("nickname")
		}
		if flags.Changed("description") {
			spec.Description, _ = flags.GetString("description")
		}
		if flags.Changed("doc-dir") {
			spec.DocDir, _ = flags.GetString("doc-dir")
		}
		if flags.Changed("relevance") {
			spec.DocRelevance, _ = flags.GetString("relevance")
		}
		if flags.Changed("ext") {
			exts, _ := flags.GetStringSlice("ext")
			spec.RequiredExts = normalizeExts(exts)
		}
		if flags.Changed("database") {
			spec.DatabaseIDs, _ = flags.GetStringSlice("database")
		}

		result, err := api.Update(args[0], spec)
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

var ragRmCmd = &cobra.Command{
	Use:   "rm <id> [id...]",
	Short: "Delete knowledge bases",
	Long: `Deletes the knowledge base records. The documents themselves are left in
place; remove them with ` + "`rag file rm`" + ` if they live in object storage.`,
	Args: minArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := confirm(fmt.Sprintf("Delete %d knowledge base(s)?", len(args))); err != nil {
			return err
		}
		api, err := ragAPI()
		if err != nil {
			return err
		}
		result, err := api.Delete(args)
		if err != nil {
			return err
		}
		return output.Success(map[string]any{"deleted": args, "result": normalizeRaw(result)}, nil, nil)
	},
}

var ragEnableCmd = &cobra.Command{
	Use:   "enable <id> [id...]",
	Short: "Enable knowledge bases",
	Args:  minArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return setRagEnabled(args, true) },
}

var ragDisableCmd = &cobra.Command{
	Use:   "disable <id> [id...]",
	Short: "Disable knowledge bases",
	Args:  minArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return setRagEnabled(args, false) },
}

func setRagEnabled(ids []string, enabled bool) error {
	api, err := ragAPI()
	if err != nil {
		return err
	}
	if _, err := api.SetEnabled(ids, enabled); err != nil {
		return err
	}
	return output.Success(map[string]any{"ids": ids, "enabled": enabled}, nil, nil)
}

var ragFileCmd = &cobra.Command{
	Use:   "file",
	Short: "Browse a knowledge base's document storage",
	Long: `Document storage is addressed directly rather than through a knowledge base
id, because the same directory can back several knowledge bases.

Object storage credentials are read from the ` + secretEnvVar + ` environment
variable, never from a flag: a secret on the command line would land in shell
history and in the process list.

  export INFINI_STORAGE_SECRET=...
  infini-cli rag file ls --fs oss --dir docs/ \
      --endpoint oss-cn-hangzhou.aliyuncs.com --access-key AKID`,
}

var ragFileLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List files in a document directory",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		storage, err := resolveStorage(cmd)
		if err != nil {
			return err
		}
		directory, _ := cmd.Flags().GetString("dir")
		if directory == "" {
			return cliexit.Usage("--dir is required")
		}
		filter, _ := cmd.Flags().GetString("filter")
		if filter != "" && !slices.Contains([]string{"file", "directory", "both"}, filter) {
			return cliexit.Usage("--filter must be file, directory or both")
		}

		api, err := ragAPI()
		if err != nil {
			return err
		}
		data, err := api.FileTree(storage, directory, filter)
		if err != nil {
			return err
		}
		return output.Success(data, nil, nil)
	},
}

var ragFileGetCmd = &cobra.Command{
	Use:     "get <path>",
	Aliases: []string{"download"},
	Short:   "Download a document",
	Args:    exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		storage, err := resolveStorage(cmd)
		if err != nil {
			return err
		}
		dest, _ := cmd.Flags().GetString("out")

		api, err := ragAPI()
		if err != nil {
			return err
		}
		result, err := api.Download(storage, args[0], dest)
		if err != nil {
			return err
		}
		return output.Success(result, []string{"FIELD", "VALUE"}, func() [][]string {
			return [][]string{
				{"path", result.Path},
				{"bytes", strconv.FormatInt(result.Bytes, 10)},
			}
		})
	},
}

var ragFileRmCmd = &cobra.Command{
	Use:   "rm <path>",
	Short: "Delete a document from object storage",
	Long: `Only the remote file systems (` + "oss, s3, cos" + `) support deletion; a local
path is managed outside Infini.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		storage, err := resolveStorage(cmd)
		if err != nil {
			return err
		}
		if !slices.Contains(rag.RemoteFileSystems, storage.FileSystem) {
			return cliexit.Usage("--fs must be one of %s to delete a file", strings.Join(rag.RemoteFileSystems, ", "))
		}
		if err := confirm(fmt.Sprintf("Permanently delete %s from %s?", args[0], storage.FileSystem)); err != nil {
			return err
		}

		api, err := ragAPI()
		if err != nil {
			return err
		}
		result, err := api.DeleteRemoteFile(storage, args[0])
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

var ragBindsCmd = &cobra.Command{
	Use:   "binds <id>",
	Short: "List the data sources bound to a knowledge base",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := ragAPI()
		if err != nil {
			return err
		}
		data, err := api.BoundDatabases(args[0])
		if err != nil {
			return err
		}
		return output.Success(data, nil, nil)
	},
}

var ragBindDBCmd = &cobra.Command{
	Use:   "bind-db <id>",
	Short: "Replace the data sources bound to a knowledge base",
	Long: `The list is replaced, not merged. Passing no --db unbinds all of them.

  infini-cli rag bind-db rag_1 --db db_1 --db db_2`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		databaseIDs, _ := cmd.Flags().GetStringSlice("db")
		if len(databaseIDs) == 0 {
			if err := confirm(fmt.Sprintf("Unbind every data source from knowledge base %s?", args[0])); err != nil {
				return err
			}
		}

		api, err := ragAPI()
		if err != nil {
			return err
		}
		result, err := api.BindDatabases(args[0], databaseIDs)
		if err != nil {
			return err
		}
		return output.Success(map[string]any{
			"ragId":       args[0],
			"databaseIds": databaseIDs,
			"result":      normalizeRaw(result),
		}, nil, nil)
	},
}

func resolveStorage(cmd *cobra.Command) (rag.Storage, error) {
	flags := cmd.Flags()
	fileSystem, _ := flags.GetString("fs")
	if fileSystem == "" {
		fileSystem = "file"
	}
	if !slices.Contains(rag.FileSystems, fileSystem) {
		return rag.Storage{}, cliexit.Usage("--fs must be one of: %s", strings.Join(rag.FileSystems, ", "))
	}

	endpoint, _ := flags.GetString("endpoint")
	accessKey, _ := flags.GetString("access-key")
	secret := os.Getenv(secretEnvVar)

	if slices.Contains(rag.RemoteFileSystems, fileSystem) && (accessKey == "" || secret == "") {
		return rag.Storage{}, cliexit.Hint(
			cliexit.Usage("%s storage needs --access-key and the %s environment variable", fileSystem, secretEnvVar),
			"export %s=<secret> so it stays out of shell history", secretEnvVar,
		)
	}

	return rag.Storage{
		FileSystem:      fileSystem,
		Endpoint:        endpoint,
		AccessKeyID:     accessKey,
		AccessKeySecret: secret,
	}, nil
}

// normalizeExts drops a leading dot so `--ext .pdf` and `--ext pdf` behave the
// same.
func normalizeExts(exts []string) []string {
	out := make([]string, 0, len(exts))
	for _, ext := range exts {
		if trimmed := strings.TrimPrefix(strings.TrimSpace(ext), "."); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func parseStoredExts(stored string) []string {
	if stored == "" {
		return nil
	}
	var exts []string
	if err := json.Unmarshal([]byte(stored), &exts); err != nil {
		// Fall back to a comma separated list rather than losing the value.
		return normalizeExts(strings.Split(stored, ","))
	}
	return exts
}

func addRagSpecFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.String("name", "", "Unique knowledge base name")
	flags.String("nickname", "", "Display name")
	flags.String("description", "", "Description")
	flags.String("doc-dir", "", "Document directory")
	flags.StringSlice("ext", nil, "Allowed document extension, e.g. pdf (repeatable)")
	flags.String("relevance", "", "Retrieval relevance threshold, e.g. 0.5")
	flags.StringSlice("database", nil, "Data source id to bind on write (repeatable)")
}

func addStorageFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.String("fs", "file", "File system: "+strings.Join(rag.FileSystems, ", "))
	flags.String("endpoint", "", "Object storage endpoint")
	flags.String("access-key", "", "Object storage access key id")
}

func init() {
	lsFlags := ragLsCmd.Flags()
	lsFlags.Bool("all", false, "Return every accessible knowledge base, unpaged")
	lsFlags.Int("page", 1, "Page number")
	lsFlags.Int("page-size", 20, "Items per page")
	lsFlags.String("keyword", "", "Filter by keyword")
	lsFlags.Bool("enabled", false, "Filter by enablement; --enabled=false lists disabled ones")
	lsFlags.String("source", "", "Scope: "+strings.Join(rag.Sources, ", "))

	addRagSpecFlags(ragCreateCmd)
	ragCreateCmd.Flags().Bool("disabled", false, "Create the knowledge base disabled")
	addRagSpecFlags(ragUpdateCmd)

	addStorageFlags(ragFileLsCmd)
	ragFileLsCmd.Flags().String("dir", "", "Directory to list")
	ragFileLsCmd.Flags().String("filter", "", "Only file, directory or both")

	addStorageFlags(ragFileGetCmd)
	ragFileGetCmd.Flags().String("out", "", "Destination file or directory")

	addStorageFlags(ragFileRmCmd)

	ragBindDBCmd.Flags().StringSlice("db", nil, "Data source id (repeatable)")

	ragFileCmd.AddCommand(ragFileLsCmd, ragFileGetCmd, ragFileRmCmd)
	ragCmd.AddCommand(
		ragLsCmd, ragShowCmd, ragCreateCmd, ragUpdateCmd, ragRmCmd,
		ragEnableCmd, ragDisableCmd, ragFileCmd, ragBindsCmd, ragBindDBCmd,
	)
	rootCmd.AddCommand(ragCmd)
}
