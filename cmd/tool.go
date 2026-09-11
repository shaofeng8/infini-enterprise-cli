package cmd

import (
	"slices"
	"strconv"
	"strings"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/extension"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/spf13/cobra"
)

var toolCmd = &cobra.Command{
	Use:   "tool",
	Short: "Manage the agent's external tools",
	Long: `A tool is an external command the agent may invoke. Like skills, tools come
either from the catalog published to the proxy or from archives you upload.

Per-task parameters for a tool are set with ` + "`agent tool-params`" + `, not here:
this command manages which tools exist and whether they are on.`,
}

var toolListCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List installed tools",
	Long: `  infini-cli tool ls --table
  infini-cli tool ls --source local --table`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		query := keywordQuery(cmd)
		if source, _ := cmd.Flags().GetString("source"); source != "" {
			if !slices.Contains(extension.ToolSources, source) {
				return cliexit.Usage("unknown source %q, expected one of: %s",
					source, strings.Join(extension.ToolSources, ", "))
			}
			query.Source = source
		}

		api, err := extensionAPI()
		if err != nil {
			return err
		}
		page, err := api.Tools(query)
		if err != nil {
			return err
		}
		return output.Success(page, []string{"ID", "NAME", "ALIAS", "STATUS", "SOURCE", "PLUGIN ID"}, func() [][]string {
			rows := make([][]string, 0, len(page.List))
			for _, tool := range page.List {
				rows = append(rows, []string{
					tool.ID, tool.Name, tool.Alias, tool.Status, tool.Source, tool.PluginID,
				})
			}
			return rows
		})
	},
}

var toolLocalCmd = &cobra.Command{
	Use:   "local",
	Short: "List only the tools you uploaded",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := extensionAPI()
		if err != nil {
			return err
		}
		tools, err := api.LocalTools()
		if err != nil {
			return err
		}
		return output.Success(tools, []string{"ID", "NAME", "ALIAS", "STATUS", "EXECUTABLE"}, func() [][]string {
			rows := make([][]string, 0, len(tools))
			for _, tool := range tools {
				rows = append(rows, []string{
					tool.ID, tool.Name, tool.Alias, tool.Status, tool.ExecutableName,
				})
			}
			return rows
		})
	},
}

var toolInstalledCmd = &cobra.Command{
	Use:   "installed",
	Short: "List the catalog ids of installed tools",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := extensionAPI()
		if err != nil {
			return err
		}
		ids, err := api.InstalledToolIDs()
		if err != nil {
			return err
		}
		return output.Success(ids, []string{"PLUGIN ID"}, func() [][]string {
			rows := make([][]string, 0, len(ids))
			for _, id := range ids {
				rows = append(rows, []string{id})
			}
			return rows
		})
	},
}

var toolStateCmd = &cobra.Command{
	Use:   "state [plugin-id]",
	Short: "Print the installed-status map, or check one tool",
	Long: `With no argument this is the whole map keyed by catalog id. With one, it is a
single yes-or-no answer from the dedicated endpoint.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := extensionAPI()
		if err != nil {
			return err
		}
		if len(args) == 1 {
			installed, err := api.IsToolInstalled(args[0])
			if err != nil {
				return err
			}
			return output.Success(map[string]any{
				"pluginId":  args[0],
				"installed": installed,
			}, nil, nil)
		}

		state, err := api.ToolState()
		if err != nil {
			return err
		}
		ids := make([]string, 0, len(state))
		for id := range state {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		return output.Success(state, []string{"PLUGIN ID", "STATUS", "SOURCE", "SCOPE"}, func() [][]string {
			rows := make([][]string, 0, len(ids))
			for _, id := range ids {
				entry := state[id]
				rows = append(rows, []string{id, entry.Status, entry.Source, entry.Scope})
			}
			return rows
		})
	},
}

var toolInstallCmd = &cobra.Command{
	Use:   "install <plugin-id>",
	Short: "Install a tool from the catalog",
	Long: `  infini-cli tool install 507f1f77bcf86cd799439011 \
      --name "数据分析工具" --alias data-analysis --author admin

The server stores the display metadata with the installation instead of reading
it back from the catalog, which is why --name, --alias and --author are all
required rather than optional.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		body := map[string]string{"pluginId": args[0]}
		for _, flag := range []string{"name", "alias", "author"} {
			value, _ := flags.GetString(flag)
			if value == "" {
				return cliexit.Usage("--%s is required", flag)
			}
			body[flag] = value
		}
		// logo is required by the DTO but carries no meaning for a CLI
		// install, so an empty string stands in rather than forcing a flag.
		body["logo"], _ = flags.GetString("logo")
		for _, flag := range []string{"intro", "tags"} {
			if value, _ := flags.GetString(flag); value != "" {
				body[flag] = value
			}
		}

		api, err := extensionAPI()
		if err != nil {
			return err
		}
		tool, err := api.InstallTool(body)
		if err != nil {
			return err
		}
		return output.Success(tool, nil, nil)
	},
}

var toolUninstallCmd = &cobra.Command{
	Use:   "uninstall <plugin-id>",
	Short: "Uninstall a catalog tool",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := extensionAPI()
		if err != nil {
			return err
		}
		raw, err := api.UninstallTool(args[0])
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

var toolToggleCmd = &cobra.Command{
	Use:   "toggle <plugin-id> <active|inactive>",
	Short: "Enable or disable an installed tool",
	Args:  exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireStatus(args[1]); err != nil {
			return err
		}
		api, err := extensionAPI()
		if err != nil {
			return err
		}
		tool, err := api.ToggleTool(args[0], args[1])
		if err != nil {
			return err
		}
		return output.Success(tool, nil, nil)
	},
}

var toolUploadCmd = &cobra.Command{
	Use:   "upload <archive>",
	Short: "Upload a tool archive",
	Long: `  infini-cli tool upload ./mytool.zip --name mytool --alias mytool \
      --author me --status active`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fields, err := toolFormFields(cmd, true)
		if err != nil {
			return err
		}
		api, err := extensionAPI()
		if err != nil {
			return err
		}
		tool, err := api.UploadTool(args[0], fields)
		if err != nil {
			return err
		}
		return output.Success(tool, nil, nil)
	},
}

var toolEditCmd = &cobra.Command{
	Use:   "edit <id>",
	Short: "Edit an uploaded tool, optionally replacing its archive",
	Long: `  infini-cli tool edit t_1 --status inactive
  infini-cli tool edit t_1 --archive ./mytool.zip`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fields, err := toolFormFields(cmd, false)
		if err != nil {
			return err
		}
		fields["id"] = args[0]
		archive, _ := cmd.Flags().GetString("archive")
		if len(fields) == 1 && archive == "" {
			return cliexit.Usage("nothing to change; pass a metadata flag or --archive")
		}

		api, err := extensionAPI()
		if err != nil {
			return err
		}
		tool, err := api.EditTool(archive, fields)
		if err != nil {
			return err
		}
		return output.Success(tool, nil, nil)
	},
}

// toolFormFields collects the multipart metadata. Upload needs the full set
// because it creates the row; edit patches it, so everything is optional there.
func toolFormFields(cmd *cobra.Command, required bool) (map[string]string, error) {
	flags := cmd.Flags()
	fields := map[string]string{}

	for _, flag := range []string{"name", "alias", "author", "status"} {
		value, _ := flags.GetString(flag)
		if value == "" {
			if required {
				return nil, cliexit.Usage("--%s is required", flag)
			}
			continue
		}
		fields[flag] = value
	}
	if status, present := fields["status"]; present {
		if err := requireStatus(status); err != nil {
			return nil, err
		}
	}
	for _, flag := range []string{"logo", "intro", "tags"} {
		if value, _ := flags.GetString(flag); value != "" {
			fields[flag] = value
		}
	}
	return fields, nil
}

var toolRemoveCmd = &cobra.Command{
	Use:     "rm <id>...",
	Aliases: []string{"delete"},
	Short:   "Delete uploaded tools",
	Long: `Takes the installation id from ` + "`tool ls`" + `, not the catalog id. Catalog
installs come off with ` + "`tool uninstall`" + `.`,
	Args: minArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := confirm("Delete " + strconv.Itoa(len(args)) + " uploaded tool(s)?"); err != nil {
			return err
		}
		api, err := extensionAPI()
		if err != nil {
			return err
		}
		removed := make([]string, 0, len(args))
		for _, id := range args {
			if _, err := api.DeleteTool(id); err != nil {
				return err
			}
			removed = append(removed, id)
		}
		return output.Success(map[string]any{"deleted": removed}, nil, nil)
	},
}

func addToolMetadataFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.String("name", "", "Tool name")
	flags.String("alias", "", "Short handle the agent invokes")
	flags.String("author", "", "Author")
	flags.String("status", "", "Status: "+strings.Join(extension.Statuses, " or "))
	flags.String("logo", "", "Logo URL or data URL")
	flags.String("intro", "", "One-line description")
	flags.String("tags", "", "Tags as a JSON array, e.g. '[\"工具\"]'")
}

func init() {
	addKeywordPageFlags(toolListCmd)
	toolListCmd.Flags().String("source", "", "Filter by source: "+strings.Join(extension.ToolSources, " or "))

	toolInstallCmd.Flags().String("name", "", "Tool name (required)")
	toolInstallCmd.Flags().String("alias", "", "Short handle (required)")
	toolInstallCmd.Flags().String("author", "", "Author (required)")
	toolInstallCmd.Flags().String("logo", "", "Logo URL")
	toolInstallCmd.Flags().String("intro", "", "One-line description")
	toolInstallCmd.Flags().String("tags", "", "Tags as a JSON array")

	addToolMetadataFlags(toolUploadCmd)
	addToolMetadataFlags(toolEditCmd)
	toolEditCmd.Flags().String("archive", "", "Replace the tool's files with this archive")

	toolCmd.AddCommand(
		toolListCmd, toolLocalCmd, toolInstalledCmd, toolStateCmd, toolInstallCmd,
		toolUninstallCmd, toolToggleCmd, toolUploadCmd, toolEditCmd, toolRemoveCmd,
	)
	rootCmd.AddCommand(toolCmd)
}
