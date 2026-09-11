package cmd

import (
	"strconv"
	"strings"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/extension"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/spf13/cobra"
)

var templateCmd = &cobra.Command{
	Use:   "template",
	Short: "Manage saved prompts",
	Long: `A template is a prompt you reuse, with {{name}} placeholders for the parts
that change.

Templates are storage only: nothing here expands or runs one. To use a
template, read its text and pass it to ` + "`agent new`" + `.`,
}

var templateListCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List templates",
	Args:    exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		page, _ := flags.GetInt("page")
		pageSize, _ := flags.GetInt("page-size")
		keyword, _ := flags.GetString("search")

		api, err := extensionAPI()
		if err != nil {
			return err
		}
		result, err := api.Templates(extension.TemplateQuery{
			Page: page, PageSize: pageSize, Keyword: keyword,
		})
		if err != nil {
			return err
		}
		return output.Success(result, []string{"ID", "NAME", "TEXT"}, func() [][]string {
			rows := make([][]string, 0, len(result.Items))
			for _, template := range result.Items {
				rows = append(rows, []string{template.ID, template.Name, firstLine(template.Text)})
			}
			return rows
		})
	},
}

var templateShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show one template",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := extensionAPI()
		if err != nil {
			return err
		}
		template, err := api.Template(args[0])
		if err != nil {
			return err
		}
		return output.Success(template, nil, nil)
	},
}

var templateCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a template",
	Long: `  infini-cli template create "月度复盘" --text @monthly.md
  infini-cli template create "区域对比" --text "对比 {{region}} 与 {{baseline}} 的 GMV" \
      --variables '{"region":"华东","baseline":"华南"}'

--text accepts @file. The name is capped at 64 characters by the server.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		text, err := templateText(cmd)
		if err != nil {
			return err
		}
		if text == "" {
			return cliexit.Usage("--text is required")
		}

		body := map[string]string{"name": args[0], "text": text}
		if variables, _ := cmd.Flags().GetString("variables"); variables != "" {
			if _, err := readJSONArg(variables); err != nil {
				return err
			}
			body["variables"] = variables
		}

		api, err := extensionAPI()
		if err != nil {
			return err
		}
		template, err := api.CreateTemplate(body)
		if err != nil {
			return err
		}
		return output.Success(template, nil, nil)
	},
}

var templateUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a template",
	Long: `The server's update DTO requires both name and text, so the current template
is read first and whichever field you did not pass is carried over.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		name, _ := flags.GetString("name")
		text, err := templateText(cmd)
		if err != nil {
			return err
		}
		variables, _ := flags.GetString("variables")
		if name == "" && text == "" && !flags.Changed("variables") {
			return cliexit.Usage("nothing to change; pass --name, --text or --variables")
		}

		api, err := extensionAPI()
		if err != nil {
			return err
		}
		current, err := api.Template(args[0])
		if err != nil {
			return err
		}

		body := map[string]string{
			"name": current.Name,
			"text": current.Text,
		}
		if name != "" {
			body["name"] = name
		}
		if text != "" {
			body["text"] = text
		}
		if flags.Changed("variables") {
			if variables != "" {
				if _, err := readJSONArg(variables); err != nil {
					return err
				}
			}
			body["variables"] = variables
		} else if current.Variables != "" {
			body["variables"] = current.Variables
		}

		template, err := api.UpdateTemplate(args[0], body)
		if err != nil {
			return err
		}
		return output.Success(template, nil, nil)
	},
}

// templateText resolves --text, which accepts @file because a prompt worth
// saving rarely fits comfortably on a command line.
func templateText(cmd *cobra.Command) (string, error) {
	value, _ := cmd.Flags().GetString("text")
	if value == "" {
		return "", nil
	}
	if strings.HasPrefix(value, "@") {
		return readTextFile(strings.TrimPrefix(value, "@"))
	}
	return value, nil
}

var templateRemoveCmd = &cobra.Command{
	Use:     "rm <id>...",
	Aliases: []string{"delete"},
	Short:   "Delete templates",
	Args:    minArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := confirm("Delete " + strconv.Itoa(len(args)) + " template(s)?"); err != nil {
			return err
		}
		api, err := extensionAPI()
		if err != nil {
			return err
		}
		raw, err := api.DeleteTemplates(args)
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

func init() {
	templateListCmd.Flags().Int("page", 1, "Page number")
	templateListCmd.Flags().Int("page-size", 10, "Items per page")
	templateListCmd.Flags().String("search", "", "Filter by name")

	templateCreateCmd.Flags().String("text", "", "Template body; @file to read from a file")
	templateCreateCmd.Flags().String("variables", "", "Default variable values as a JSON object")

	templateUpdateCmd.Flags().String("name", "", "New name")
	templateUpdateCmd.Flags().String("text", "", "New body; @file to read from a file")
	templateUpdateCmd.Flags().String("variables", "", "New variable values as a JSON object")

	templateCmd.AddCommand(
		templateListCmd, templateShowCmd, templateCreateCmd,
		templateUpdateCmd, templateRemoveCmd,
	)
	rootCmd.AddCommand(templateCmd)
}
