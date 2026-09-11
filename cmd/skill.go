package cmd

import (
	"slices"
	"strconv"
	"strings"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/extension"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/spf13/cobra"
)

func extensionAPI() (*extension.API, error) {
	c, err := client.New()
	if err != nil {
		return nil, err
	}
	return extension.NewAPI(c), nil
}

// addKeywordPageFlags adds the pageNum/pageSize pair the skill and tool
// listings use, which differs from the page/pageSize of the other modules.
func addKeywordPageFlags(cmd *cobra.Command) {
	cmd.Flags().Int("page", 1, "Page number")
	cmd.Flags().Int("page-size", 12, "Items per page")
	cmd.Flags().String("search", "", "Filter by keyword")
}

func keywordQuery(cmd *cobra.Command) extension.KeywordQuery {
	page, _ := cmd.Flags().GetInt("page")
	pageSize, _ := cmd.Flags().GetInt("page-size")
	search, _ := cmd.Flags().GetString("search")
	return extension.KeywordQuery{PageNum: page, PageSize: pageSize, Keyword: search}
}

func requireStatus(value string) error {
	if !slices.Contains(extension.Statuses, value) {
		return cliexit.Usage("unknown status %q, expected one of: %s",
			value, strings.Join(extension.Statuses, ", "))
	}
	return nil
}

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Manage the agent's skills",
	Long: `A skill is procedural knowledge: a SKILL.md telling the agent how to do
something, plus whatever files it needs, shipped as a zip.

Skills come from two places. The catalog published to the proxy is what
` + "`skill available`" + ` lists and ` + "`skill install`" + ` enables. Your own archives go
through ` + "`skill upload`" + ` and live only in this deployment.

` + "`skill available`" + ` is context-dependent: the server filters the catalog by the
data source types a task has attached and by whether the browser is enabled, so
the same account sees a different list for different tasks.`,
}

var skillAvailableCmd = &cobra.Command{
	Use:   "available",
	Short: "List the skills the agent can reach",
	Long: `  infini-cli skill available --table
  infini-cli skill available --task t_1 --browser --table

Without --task this is the account-wide answer; with it, the answer the agent
would actually get for that task.`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		taskID, _ := cmd.Flags().GetString("task")
		browser, _ := cmd.Flags().GetBool("browser")

		api, err := extensionAPI()
		if err != nil {
			return err
		}
		skills, err := api.AvailableSkills(taskID, browser)
		if err != nil {
			return err
		}
		return output.Success(skills, []string{"NAME", "SOURCE", "DESCRIPTION"}, func() [][]string {
			rows := make([][]string, 0, len(skills))
			for _, skill := range skills {
				rows = append(rows, []string{skill.Name, skill.Source, firstLine(skill.Description)})
			}
			return rows
		})
	},
}

var skillListCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List installed skills",
	Args:    exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := extensionAPI()
		if err != nil {
			return err
		}
		page, err := api.InstalledSkills(keywordQuery(cmd))
		if err != nil {
			return err
		}
		return output.Success(page, []string{"ID", "NAME", "STATUS", "SOURCE", "SKILL ID"}, func() [][]string {
			rows := make([][]string, 0, len(page.List))
			for _, skill := range page.List {
				rows = append(rows, []string{
					skill.ID, skill.Name, skill.Status, skill.Source, skill.SkillID,
				})
			}
			return rows
		})
	},
}

var skillStateCmd = &cobra.Command{
	Use:   "state",
	Short: "Print the installed-status map, keyed by skill name",
	Long: `The cheap way to answer "is this skill installed and on", without paging
through the listing.`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := extensionAPI()
		if err != nil {
			return err
		}
		state, err := api.SkillState()
		if err != nil {
			return err
		}
		names := make([]string, 0, len(state))
		for name := range state {
			names = append(names, name)
		}
		slices.Sort(names)
		return output.Success(state, []string{"SKILL", "STATUS", "SOURCE", "SCOPE"}, func() [][]string {
			rows := make([][]string, 0, len(names))
			for _, name := range names {
				entry := state[name]
				rows = append(rows, []string{name, entry.Status, entry.Source, entry.Scope})
			}
			return rows
		})
	},
}

var skillInstallCmd = &cobra.Command{
	Use:   "install <skill-id> <name>",
	Short: "Install a skill from the catalog",
	Long: `The id identifies the catalog entry; the name is the handle the agent will use
to invoke it. ` + "`skill available`" + ` lists both.`,
	Args: exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := extensionAPI()
		if err != nil {
			return err
		}
		skill, err := api.InstallSkill(args[0], args[1])
		if err != nil {
			return err
		}
		return output.Success(skill, nil, nil)
	},
}

var skillUninstallCmd = &cobra.Command{
	Use:   "uninstall <skill-id>",
	Short: "Uninstall a skill",
	Long: `Takes the catalog id, the same one ` + "`skill install`" + ` was given. To remove an
archive you uploaded yourself, use ` + "`skill rm`" + `.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := extensionAPI()
		if err != nil {
			return err
		}
		raw, err := api.UninstallSkill(args[0])
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

var skillToggleCmd = &cobra.Command{
	Use:   "toggle <skill-id> <active|inactive>",
	Short: "Enable or disable an installed skill",
	Args:  exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireStatus(args[1]); err != nil {
			return err
		}
		api, err := extensionAPI()
		if err != nil {
			return err
		}
		skill, err := api.ToggleSkill(args[0], args[1])
		if err != nil {
			return err
		}
		return output.Success(skill, nil, nil)
	},
}

var skillUploadCmd = &cobra.Command{
	Use:   "upload <archive.zip>",
	Short: "Upload a skill archive",
	Long: `  infini-cli skill upload ./my-skill.zip
  infini-cli skill upload ./my-skill.zip --id sk_1    # replace an existing upload

The name is read from the SKILL.md frontmatter, so --name is only needed to
override it. Uploading a name that already exists replaces it.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fields := map[string]string{}
		for _, flag := range []string{"id", "name", "status"} {
			if value, _ := cmd.Flags().GetString(flag); value != "" {
				fields[flag] = value
			}
		}
		if status, present := fields["status"]; present {
			if err := requireStatus(status); err != nil {
				return err
			}
		}

		api, err := extensionAPI()
		if err != nil {
			return err
		}
		skill, err := api.UploadSkill(args[0], fields)
		if err != nil {
			return err
		}
		return output.Success(skill, nil, nil)
	},
}

var skillEditCmd = &cobra.Command{
	Use:   "edit <id>",
	Short: "Edit an uploaded skill, optionally replacing its archive",
	Long: `  infini-cli skill edit sk_1 --status inactive
  infini-cli skill edit sk_1 --archive ./my-skill.zip`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		archive, _ := flags.GetString("archive")
		fields := map[string]string{"id": args[0]}
		for _, flag := range []string{"name", "status"} {
			if value, _ := flags.GetString(flag); value != "" {
				fields[flag] = value
			}
		}
		if status, present := fields["status"]; present {
			if err := requireStatus(status); err != nil {
				return err
			}
		}
		if len(fields) == 1 && archive == "" {
			return cliexit.Usage("nothing to change; pass --name, --status or --archive")
		}

		api, err := extensionAPI()
		if err != nil {
			return err
		}
		skill, err := api.EditSkill(archive, fields)
		if err != nil {
			return err
		}
		return output.Success(skill, nil, nil)
	},
}

var skillRemoveCmd = &cobra.Command{
	Use:     "rm <id>...",
	Aliases: []string{"delete"},
	Short:   "Delete uploaded skills",
	Long: `Takes the installation id from ` + "`skill ls`" + `, not the catalog id: an uploaded
skill has no catalog entry behind it. Catalog installs come off with
` + "`skill uninstall`" + `.`,
	Args: minArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := confirm("Delete " + strconv.Itoa(len(args)) + " uploaded skill(s)?"); err != nil {
			return err
		}
		api, err := extensionAPI()
		if err != nil {
			return err
		}
		removed := make([]string, 0, len(args))
		for _, id := range args {
			if _, err := api.DeleteSkill(id); err != nil {
				return err
			}
			removed = append(removed, id)
		}
		return output.Success(map[string]any{"deleted": removed}, nil, nil)
	},
}

func init() {
	skillAvailableCmd.Flags().String("task", "", "Answer as the agent would for this task")
	skillAvailableCmd.Flags().Bool("browser", false, "Include skills that need the browser tool")
	addKeywordPageFlags(skillListCmd)

	skillUploadCmd.Flags().String("id", "", "Replace this existing upload")
	skillUploadCmd.Flags().String("name", "", "Override the name from SKILL.md")
	skillUploadCmd.Flags().String("status", "", "Initial status: "+strings.Join(extension.Statuses, " or "))

	skillEditCmd.Flags().String("name", "", "New name")
	skillEditCmd.Flags().String("status", "", "New status: "+strings.Join(extension.Statuses, " or "))
	skillEditCmd.Flags().String("archive", "", "Replace the skill's files with this zip")

	skillCmd.AddCommand(
		skillAvailableCmd, skillListCmd, skillStateCmd, skillInstallCmd,
		skillUninstallCmd, skillToggleCmd, skillUploadCmd, skillEditCmd, skillRemoveCmd,
	)
	rootCmd.AddCommand(skillCmd)
}
