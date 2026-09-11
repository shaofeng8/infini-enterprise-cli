package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/config"
	"github.com/chaozwn/infini-enterprise-cli/internal/dashboard"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/spf13/cobra"
)

var dashCmd = &cobra.Command{
	Use:   "dash",
	Short: "Manage dashboards",
	Long: `Read, query, refresh and version dashboards.

Dashboard work splits in two:

  operations (this command)  list, inspect, export/import a spec, query data,
                             refresh, roll back, rearrange layout
  authoring (the agent)      turning a business request into a spec

Authoring goes through the agent because a spec carries an Infini-SQL DAG and a
filter contract that the server validates and hydrates; ` + "`dash import`" + ` exists for
moving an already-valid spec between deployments, not for writing one by hand.`,
}

// dashAPI is the shared constructor for every dash subcommand.
func dashAPI() (*dashboard.API, error) {
	c, err := client.New()
	if err != nil {
		return nil, err
	}
	return dashboard.NewAPI(c), nil
}

var dashLsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List dashboards visible to the current user",
	Args:    exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, _ := cmd.Flags().GetString("project")

		api, err := dashAPI()
		if err != nil {
			return err
		}
		items, err := api.List(projectID)
		if err != nil {
			return err
		}
		return output.Success(items, []string{"ID", "TITLE", "REV", "PROJECT", "UPDATED"}, func() [][]string {
			rows := make([][]string, 0, len(items))
			for _, item := range items {
				rows = append(rows, []string{
					item.ID,
					item.Title,
					strconv.Itoa(item.CurrentRevision),
					deref(item.ProjectID),
					item.UpdatedAt,
				})
			}
			return rows
		})
	},
}

var dashShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show a dashboard's metadata, filters, queries and widgets",
	Long: `Prints the dashboard summary. The spec itself is large, so it is omitted unless
--spec is passed; use ` + "`dash export`" + ` to write it to a file.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		withSpec, _ := cmd.Flags().GetBool("spec")

		api, err := dashAPI()
		if err != nil {
			return err
		}
		detail, spec, err := api.GetWithSpec(args[0])
		if err != nil {
			return err
		}

		summary := map[string]any{
			"id":        detail.ID,
			"title":     detail.Title,
			"revision":  detail.CurrentRevision,
			"specHash":  detail.SpecHash,
			"canWrite":  detail.CanWrite,
			"isPublic":  detail.IsPublic,
			"owner":     detail.UserID,
			"projectId": deref(detail.ProjectID),
			"updatedAt": detail.UpdatedAt,
			"filters":   filterSummaries(spec),
			"queries":   spec.DataQueryIDs(),
			"widgets":   widgetSummaries(spec),
		}
		if detail.SourceTaskID != nil {
			summary["sourceTaskId"] = *detail.SourceTaskID
		}
		if withSpec {
			summary["spec"] = normalizeRaw(detail.Spec)
		}

		return output.Success(summary, []string{"FIELD", "VALUE"}, func() [][]string {
			return [][]string{
				{"id", detail.ID},
				{"title", detail.Title},
				{"revision", strconv.Itoa(detail.CurrentRevision)},
				{"specHash", detail.SpecHash},
				{"canWrite", strconv.FormatBool(detail.CanWrite)},
				{"filters", strconv.Itoa(len(spec.Filters))},
				{"queries", strconv.Itoa(len(spec.Queries))},
				{"widgets", strconv.Itoa(len(spec.Widgets))},
				{"updatedAt", detail.UpdatedAt},
			}
		})
	},
}

func filterSummaries(spec *dashboard.Spec) []map[string]any {
	out := make([]map[string]any, 0, len(spec.Filters))
	for _, filter := range spec.Filters {
		entry := map[string]any{"name": filter.Name, "type": filter.Type, "label": filter.Label}
		if filter.Multiple {
			entry["multiple"] = true
		}
		if len(filter.Default) > 0 {
			entry["default"] = normalizeRaw(filter.Default)
		}
		out = append(out, entry)
	}
	return out
}

func widgetSummaries(spec *dashboard.Spec) []map[string]any {
	out := make([]map[string]any, 0, len(spec.Widgets))
	for _, widget := range spec.Widgets {
		out = append(out, map[string]any{
			"id":     widget.ID,
			"type":   widget.Type,
			"title":  widget.Title,
			"layout": widget.Layout,
		})
	}
	return out
}

var dashExportCmd = &cobra.Command{
	Use:   "export <id>",
	Short: "Write a dashboard spec to a file or stdout",
	Long: `Exports an editable bundle: the spec plus the id, revision and specHash needed
to apply an edit back safely.

  infini-cli dash export abc -o board.json
  # edit board.json
  infini-cli dash apply abc board.json --change-brief "add conversion card"

` + "`dash apply`" + ` reads the bundle's specHash and refuses to overwrite a dashboard
that changed in the meantime. Use --spec-only for a bare spec (portable between
deployments, but without that safety check).`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		outPath, _ := cmd.Flags().GetString("output")
		specOnly, _ := cmd.Flags().GetBool("spec-only")
		revision, _ := cmd.Flags().GetInt("revision")

		api, err := dashAPI()
		if err != nil {
			return err
		}
		detail, err := api.Get(args[0])
		if err != nil {
			return err
		}

		spec := detail.Spec
		if revision > 0 {
			// An older revision's spec has a different hash than the live one,
			// so exporting it must not carry a hash that would pass the
			// conflict check on apply.
			spec, err = api.GetRevision(args[0], revision)
			if err != nil {
				return err
			}
		}

		var payload any
		if specOnly {
			payload = normalizeRaw(spec)
		} else {
			bundle := dashboard.Bundle{
				ID:       detail.ID,
				Title:    detail.Title,
				Revision: detail.CurrentRevision,
				Spec:     spec,
			}
			if revision == 0 {
				bundle.SpecHash = detail.SpecHash
			} else {
				bundle.Revision = revision
			}
			payload = bundle
		}

		encoded, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return cliexit.New(cliexit.CodeBusiness, "cannot serialize spec: %v", err)
		}

		if outPath == "" {
			fmt.Fprintln(os.Stdout, string(encoded))
			return nil
		}
		if err := os.WriteFile(outPath, append(encoded, '\n'), 0o644); err != nil {
			return cliexit.New(cliexit.CodeBusiness, "cannot write %s: %v", outPath, err)
		}
		return output.Success(map[string]any{
			"id":       detail.ID,
			"revision": detail.CurrentRevision,
			"specHash": detail.SpecHash,
			"path":     outPath,
		}, nil, nil)
	},
}

var dashImportCmd = &cobra.Command{
	Use:   "import <file>",
	Short: "Create a dashboard from an existing spec (import path, not authoring)",
	Long: `Creates a dashboard from a spec file, accepting either a bare spec or a bundle
produced by ` + "`dash export`" + `.

This is the import/fallback path. Regular creation goes through the agent
(` + "`dash new`" + `), which builds and validates the Infini-SQL DAG behind the spec.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, _ := cmd.Flags().GetString("project")
		sourceTaskID, _ := cmd.Flags().GetString("source-task")

		bundle, err := readBundleFile(args[0])
		if err != nil {
			return err
		}
		api, err := dashAPI()
		if err != nil {
			return err
		}
		created, err := api.Create(bundle.Spec, projectID, sourceTaskID)
		if err != nil {
			return err
		}
		return output.Success(created, nil, nil)
	},
}

var dashApplyCmd = &cobra.Command{
	Use:   "apply <id> <file>",
	Short: "Replace a dashboard spec from a file",
	Long: `Writes a full spec back, creating a new revision.

If the file is a bundle exported by ` + "`dash export`" + `, its specHash is compared
against the live dashboard first and the command aborts on a mismatch. The REST
API has no server-side conflict check, so this guard is client-side: it closes
the common "two people edited the same board" window, not a race.

Use --force to write regardless, or --expect-hash to supply the hash manually.`,
	Args: exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, path := args[0], args[1]
		changeBrief, _ := cmd.Flags().GetString("change-brief")
		sourceTaskID, _ := cmd.Flags().GetString("source-task")
		expectHash, _ := cmd.Flags().GetString("expect-hash")
		force, _ := cmd.Flags().GetBool("force")

		bundle, err := readBundleFile(path)
		if err != nil {
			return err
		}
		if expectHash == "" {
			expectHash = bundle.SpecHash
		}

		api, err := dashAPI()
		if err != nil {
			return err
		}

		if !force {
			if expectHash == "" {
				return cliexit.Hint(
					cliexit.New(cliexit.CodeUsage, "no expected specHash available for the conflict check"),
					"export with `%s dash export %s` to get one, or pass --force", config.AppName, id,
				)
			}
			current, err := api.Get(id)
			if err != nil {
				return err
			}
			if current.SpecHash != expectHash {
				return cliexit.Hint(
					cliexit.New(cliexit.CodeBusiness,
						"dashboard changed since export (expected specHash %s, found %s at revision %d)",
						expectHash, current.SpecHash, current.CurrentRevision),
					"re-export, reapply your edit, or pass --force to overwrite",
				)
			}
		}

		updated, err := api.Update(id, bundle.Spec, changeBrief, sourceTaskID)
		if err != nil {
			return err
		}
		return output.Success(updated, nil, nil)
	},
}

var dashRmCmd = &cobra.Command{
	Use:   "rm <id>",
	Short: "Delete a dashboard (soft delete)",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := dashAPI()
		if err != nil {
			return err
		}
		// Name the dashboard in the prompt: ids are opaque and easy to mix up.
		detail, err := api.Get(args[0])
		if err != nil {
			return err
		}
		if err := confirm(fmt.Sprintf("Delete dashboard %q (%s)?", detail.Title, detail.ID)); err != nil {
			return err
		}
		removed, err := api.Remove(args[0])
		if err != nil {
			return err
		}
		return output.Success(removed, nil, nil)
	},
}

var dashWidgetCmd = &cobra.Command{
	Use:   "widget",
	Short: "Manage dashboard widgets",
}

var dashWidgetLsCmd = &cobra.Command{
	Use:   "ls <id>",
	Short: "List a dashboard's widgets",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := dashAPI()
		if err != nil {
			return err
		}
		_, spec, err := api.GetWithSpec(args[0])
		if err != nil {
			return err
		}
		return output.Success(widgetSummaries(spec), []string{"ID", "TYPE", "TITLE", "X", "Y", "W", "H"}, func() [][]string {
			rows := make([][]string, 0, len(spec.Widgets))
			for _, widget := range spec.Widgets {
				rows = append(rows, []string{
					widget.ID, widget.Type, widget.Title,
					strconv.Itoa(widget.Layout.X), strconv.Itoa(widget.Layout.Y),
					strconv.Itoa(widget.Layout.W), strconv.Itoa(widget.Layout.H),
				})
			}
			return rows
		})
	},
}

var dashWidgetRmCmd = &cobra.Command{
	Use:   "rm <id> <widgetId>",
	Short: "Delete one widget, creating a new revision",
	Args:  exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := confirm(fmt.Sprintf("Delete widget %q from dashboard %s?", args[1], args[0])); err != nil {
			return err
		}
		api, err := dashAPI()
		if err != nil {
			return err
		}
		removed, err := api.RemoveWidget(args[0], args[1])
		if err != nil {
			return err
		}
		return output.Success(removed, nil, nil)
	},
}

// readBundleFile loads a spec or bundle from a path, or from stdin for "-".
func readBundleFile(path string) (*dashboard.Bundle, error) {
	arg := path
	if path == "-" {
		arg = "@-"
	} else {
		arg = "@" + path
	}
	raw, err := readJSONArg(arg)
	if err != nil {
		return nil, err
	}
	return dashboard.ReadBundle(raw)
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func init() {
	dashLsCmd.Flags().String("project", "", "Only list dashboards in this project")

	dashShowCmd.Flags().Bool("spec", false, "Include the full spec in the output")

	dashExportCmd.Flags().StringP("output", "o", "", "Write to this file instead of stdout")
	dashExportCmd.Flags().Bool("spec-only", false, "Write a bare spec without id/revision/specHash")
	dashExportCmd.Flags().Int("revision", 0, "Export a historical revision instead of the live spec")

	dashImportCmd.Flags().String("project", "", "Attach the new dashboard to this project")
	dashImportCmd.Flags().String("source-task", "", "Record the task this dashboard came from")

	dashApplyCmd.Flags().String("change-brief", "", "Describe the change for the revision history")
	dashApplyCmd.Flags().String("source-task", "", "Record the task this change came from")
	dashApplyCmd.Flags().String("expect-hash", "", "Expected current specHash for the conflict check")
	dashApplyCmd.Flags().Bool("force", false, "Skip the specHash conflict check")

	dashWidgetCmd.AddCommand(dashWidgetLsCmd, dashWidgetRmCmd)
	dashCmd.AddCommand(
		dashLsCmd, dashShowCmd, dashExportCmd, dashImportCmd,
		dashApplyCmd, dashRmCmd, dashWidgetCmd,
	)
	rootCmd.AddCommand(dashCmd)
}
