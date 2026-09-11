package cmd

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/chaozwn/infini-enterprise-cli/internal/project"
	"github.com/spf13/cobra"
)

var projectCmd = &cobra.Command{
	Use:     "project",
	Aliases: []string{"proj"},
	Short:   "Manage projects and their members",
	Long: `A project scopes tasks, dashboards and files to a group of people, and its
member roles decide who can change what.

Roles: ` + strings.Join(project.Roles, ", ") + `.`,
}

func projectAPI() (*project.API, error) {
	c, err := client.New()
	if err != nil {
		return nil, err
	}
	return project.NewAPI(c), nil
}

var projectLsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List the projects you can access",
	Args:    exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := projectAPI()
		if err != nil {
			return err
		}
		items, err := api.List()
		if err != nil {
			return err
		}
		return output.Success(items, []string{"ID", "NAME", "ROLE", "DESCRIPTION"}, func() [][]string {
			rows := make([][]string, 0, len(items))
			for _, item := range items {
				rows = append(rows, []string{item.ID, item.Name, item.Role, firstLine(item.Description)})
			}
			return rows
		})
	},
}

var projectCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a project",
	Long:  `  infini-cli project create "销售分析" --description "季度复盘"`,
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		description, _ := cmd.Flags().GetString("description")
		metadata, _ := cmd.Flags().GetString("metadata")

		api, err := projectAPI()
		if err != nil {
			return err
		}
		result, err := api.Create(project.Spec{
			Name:        args[0],
			Description: description,
			Metadata:    metadata,
		})
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

var projectUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a project",
	Long: `Only the fields you pass are changed.

  infini-cli project update proj_1 --name "销售分析 2026"`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		if !flags.Changed("name") && !flags.Changed("description") && !flags.Changed("metadata") {
			return cliexit.Usage("nothing to update; pass at least one of --name, --description, --metadata")
		}

		spec := project.Spec{}
		if flags.Changed("name") {
			spec.Name, _ = flags.GetString("name")
		}
		if flags.Changed("description") {
			spec.Description, _ = flags.GetString("description")
		}
		if flags.Changed("metadata") {
			spec.Metadata, _ = flags.GetString("metadata")
		}

		api, err := projectAPI()
		if err != nil {
			return err
		}
		result, err := api.Update(args[0], spec)
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

var projectRmCmd = &cobra.Command{
	Use:   "rm <id>",
	Short: "Delete a project",
	Long:  `The project is soft-deleted, so its tasks and dashboards are not destroyed.`,
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := confirm(fmt.Sprintf("Delete project %s? Members lose access to everything scoped to it.", args[0])); err != nil {
			return err
		}
		api, err := projectAPI()
		if err != nil {
			return err
		}
		result, err := api.Delete(args[0])
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

var projectMemberCmd = &cobra.Command{
	Use:   "member",
	Short: "Manage project members",
}

var projectMemberLsCmd = &cobra.Command{
	Use:     "ls <project-id>",
	Aliases: []string{"list"},
	Short:   "List a project's members",
	Args:    exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := projectAPI()
		if err != nil {
			return err
		}
		members, err := api.Members(args[0])
		if err != nil {
			return err
		}
		return output.Success(members, []string{"USER ID", "USERNAME", "ROLE"}, func() [][]string {
			rows := make([][]string, 0, len(members))
			for _, member := range members {
				rows = append(rows, []string{member.MemberUserID, member.Username, member.Role})
			}
			return rows
		})
	},
}

var projectMemberAddCmd = &cobra.Command{
	Use:   "add <project-id> <user-id>",
	Short: "Add a member to a project",
	Long:  `  infini-cli project member add proj_1 user_7 --role editor`,
	Args:  exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		role, err := requireRole(cmd)
		if err != nil {
			return err
		}
		api, err := projectAPI()
		if err != nil {
			return err
		}
		result, err := api.AddMember(args[0], args[1], role)
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

var projectMemberSetCmd = &cobra.Command{
	Use:   "set <project-id> <user-id>",
	Short: "Change a member's role",
	Args:  exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		role, err := requireRole(cmd)
		if err != nil {
			return err
		}
		api, err := projectAPI()
		if err != nil {
			return err
		}
		result, err := api.UpdateMember(args[0], args[1], role)
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

var projectMemberRmCmd = &cobra.Command{
	Use:   "rm <project-id> <user-id>",
	Short: "Remove a member from a project",
	Args:  exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := confirm(fmt.Sprintf("Remove %s from project %s?", args[1], args[0])); err != nil {
			return err
		}
		api, err := projectAPI()
		if err != nil {
			return err
		}
		result, err := api.RemoveMember(args[0], args[1])
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

func requireRole(cmd *cobra.Command) (string, error) {
	role, _ := cmd.Flags().GetString("role")
	if role == "" {
		return "", cliexit.Usage("--role is required, one of: %s", strings.Join(project.Roles, ", "))
	}
	if !slices.Contains(project.Roles, role) {
		return "", cliexit.Usage("unknown role %q, expected one of: %s", role, strings.Join(project.Roles, ", "))
	}
	return role, nil
}

var projectTreeCmd = &cobra.Command{
	Use:   "tree <id>",
	Short: "Show a project's file tree",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := projectAPI()
		if err != nil {
			return err
		}
		data, err := api.Tree(args[0])
		if err != nil {
			return err
		}
		return output.Success(data, nil, nil)
	},
}

var projectFileCmd = &cobra.Command{
	Use:   "file",
	Short: "Manage files in a project's shared directory",
}

var projectFilePreviewCmd = &cobra.Command{
	Use:   "preview <project-id> <path>",
	Short: "Preview a project file's contents",
	Args:  exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := projectAPI()
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

var projectFileGetCmd = &cobra.Command{
	Use:     "get <project-id> <path>",
	Aliases: []string{"download"},
	Short:   "Download a project file",
	Args:    exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		dest, _ := cmd.Flags().GetString("out")

		api, err := projectAPI()
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
			}
		})
	},
}

var projectFileMvCmd = &cobra.Command{
	Use:   "mv <project-id> <source> <target-dir>",
	Short: "Move a project file into another directory",
	Args:  exactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := projectAPI()
		if err != nil {
			return err
		}
		result, err := api.MoveFile(args[0], args[1], args[2])
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

var projectFileCpCmd = &cobra.Command{
	Use:   "cp <project-id> <source> <target-dir>",
	Short: "Copy a project file into another directory",
	Args:  exactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := projectAPI()
		if err != nil {
			return err
		}
		result, err := api.CopyFile(args[0], args[1], args[2])
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

var projectFileRmCmd = &cobra.Command{
	Use:   "rm <project-id> <path>",
	Short: "Delete a project file or directory",
	Long:  `A directory is removed with everything under it.`,
	Args:  exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := confirm(fmt.Sprintf("Delete %s from project %s? A directory is removed with everything under it.", args[1], args[0])); err != nil {
			return err
		}
		api, err := projectAPI()
		if err != nil {
			return err
		}
		result, err := api.DeletePath(args[0], args[1])
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

var projectMkdirCmd = &cobra.Command{
	Use:   "mkdir <project-id> <path>",
	Short: "Create a directory in a project",
	Args:  exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := projectAPI()
		if err != nil {
			return err
		}
		result, err := api.CreateDirectory(args[0], args[1])
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

func init() {
	projectCreateCmd.Flags().String("description", "", "Description")
	projectCreateCmd.Flags().String("metadata", "", "Opaque metadata string")

	projectUpdateCmd.Flags().String("name", "", "New name")
	projectUpdateCmd.Flags().String("description", "", "New description")
	projectUpdateCmd.Flags().String("metadata", "", "New metadata string")

	projectMemberAddCmd.Flags().String("role", "", "Role: "+strings.Join(project.Roles, ", "))
	projectMemberSetCmd.Flags().String("role", "", "Role: "+strings.Join(project.Roles, ", "))

	projectFileGetCmd.Flags().String("out", "", "Destination file or directory")

	projectMemberCmd.AddCommand(projectMemberLsCmd, projectMemberAddCmd, projectMemberSetCmd, projectMemberRmCmd)
	projectFileCmd.AddCommand(
		projectFilePreviewCmd, projectFileGetCmd, projectFileMvCmd,
		projectFileCpCmd, projectFileRmCmd,
	)
	projectCmd.AddCommand(
		projectLsCmd, projectCreateCmd, projectUpdateCmd, projectRmCmd,
		projectMemberCmd, projectTreeCmd, projectFileCmd, projectMkdirCmd,
	)
	rootCmd.AddCommand(projectCmd)
}
