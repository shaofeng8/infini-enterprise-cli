package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/dashboard"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/spf13/cobra"
)

var dashRevisionsCmd = &cobra.Command{
	Use:     "revisions <id>",
	Aliases: []string{"revs"},
	Short:   "List a dashboard's version history",
	Args:    exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := dashAPI()
		if err != nil {
			return err
		}
		revisions, err := api.ListRevisions(args[0])
		if err != nil {
			return err
		}
		return output.Success(revisions, []string{"REV", "CREATED", "BY", "BRIEF"}, func() [][]string {
			rows := make([][]string, 0, len(revisions))
			for _, rev := range revisions {
				rows = append(rows, []string{
					strconv.Itoa(rev.Revision),
					rev.CreatedAt,
					deref(rev.CreatedBy),
					deref(rev.ChangeBrief),
				})
			}
			return rows
		})
	},
}

var dashRevisionCmd = &cobra.Command{
	Use:   "revision <id> <revision>",
	Short: "Print the spec of a specific revision",
	Args:  exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		revision, err := strconv.Atoi(args[1])
		if err != nil || revision < 1 {
			return cliexit.Usage("revision must be a positive integer, received %q", args[1])
		}
		api, err := dashAPI()
		if err != nil {
			return err
		}
		spec, err := api.GetRevision(args[0], revision)
		if err != nil {
			return err
		}
		return output.Success(spec, nil, nil)
	},
}

var dashRollbackCmd = &cobra.Command{
	Use:   "rollback <id>",
	Short: "Roll back to an earlier revision",
	Long: `Copies an earlier revision's spec into a new revision. History is never
deleted, so a rollback is itself reversible.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		revision, _ := cmd.Flags().GetInt("revision")
		if revision < 1 {
			return cliexit.Usage("--revision is required and must be a positive integer")
		}
		if err := confirm(fmt.Sprintf("Roll dashboard %s back to revision %d?", args[0], revision)); err != nil {
			return err
		}
		api, err := dashAPI()
		if err != nil {
			return err
		}
		result, err := api.Rollback(args[0], revision)
		if err != nil {
			return err
		}
		return output.Success(result, nil, nil)
	},
}

var dashLayoutCmd = &cobra.Command{
	Use:   "layout",
	Short: "Inspect and rearrange widget layout",
	Long: `Layout uses a 12-column grid: x + w must not exceed 12, and w and h are at
least 2.

` + "`layout set`" + ` persists positions without creating a revision, matching the
drag-and-drop behaviour in the web UI. Run ` + "`layout commit`" + ` to freeze the
current arrangement as a new revision.`,
}

var dashLayoutGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Print the current layout in the shape `layout set` accepts",
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
		layouts := spec.Layouts()
		return output.Success(layouts, []string{"WIDGET", "X", "Y", "W", "H"}, func() [][]string {
			rows := make([][]string, 0, len(layouts))
			for _, l := range layouts {
				rows = append(rows, []string{
					l.WidgetID,
					strconv.Itoa(l.X), strconv.Itoa(l.Y),
					strconv.Itoa(l.W), strconv.Itoa(l.H),
				})
			}
			return rows
		})
	},
}

var dashLayoutSetCmd = &cobra.Command{
	Use:   "set <id>",
	Short: "Move widgets on the grid",
	Long: `Accepts either repeated --widget flags or a JSON file from ` + "`layout get`" + `.

  infini-cli dash layout set abc --widget w_kpi=0,0,3,2 --widget w_trend=3,0,9,4
  infini-cli dash layout get abc --json > layout.json
  infini-cli dash layout set abc --layout @layout.json

Widget ids are validated against the spec before sending, so a typo fails
locally with the list of valid ids instead of as a server error.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		widgetPairs, _ := cmd.Flags().GetStringArray("widget")
		layoutJSON, _ := cmd.Flags().GetString("layout")

		if len(widgetPairs) == 0 && layoutJSON == "" {
			return cliexit.Usage("provide --widget id=x,y,w,h or --layout <json|@file>")
		}

		api, err := dashAPI()
		if err != nil {
			return err
		}
		_, spec, err := api.GetWithSpec(args[0])
		if err != nil {
			return err
		}

		var layouts []dashboard.WidgetLayout
		if layoutJSON != "" {
			raw, err := readJSONArg(layoutJSON)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(raw, &layouts); err != nil {
				return cliexit.Usage("--layout must be a JSON array of {widget_id,x,y,w,h}: %v", err)
			}
		}
		for _, pair := range widgetPairs {
			layout, err := parseWidgetLayout(pair)
			if err != nil {
				return err
			}
			layouts = append(layouts, layout)
		}

		for _, layout := range layouts {
			if spec.WidgetByID(layout.WidgetID) == nil {
				return cliexit.Usage("unknown widget %q; this dashboard has: %s",
					layout.WidgetID, strings.Join(spec.WidgetIDs(), ", "))
			}
			if layout.X+layout.W > 12 {
				return cliexit.Usage("widget %q overflows the 12-column grid (x=%d + w=%d)",
					layout.WidgetID, layout.X, layout.W)
			}
			if layout.W < 2 || layout.H < 2 {
				return cliexit.Usage("widget %q must be at least 2x2 (received w=%d h=%d)",
					layout.WidgetID, layout.W, layout.H)
			}
		}

		result, err := api.UpdateLayout(args[0], layouts)
		if err != nil {
			return err
		}
		return output.Success(map[string]any{
			"updated": len(layouts),
			"result":  normalizeRaw(result),
		}, nil, nil)
	},
}

var dashLayoutCommitCmd = &cobra.Command{
	Use:   "commit <id>",
	Short: "Freeze the current layout as a new revision",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := dashAPI()
		if err != nil {
			return err
		}
		result, err := api.CommitLayoutRevision(args[0])
		if err != nil {
			return err
		}
		return output.Success(result, nil, nil)
	},
}

// parseWidgetLayout reads `id=x,y,w,h`.
func parseWidgetLayout(pair string) (dashboard.WidgetLayout, error) {
	var layout dashboard.WidgetLayout

	id, coords, found := strings.Cut(pair, "=")
	if !found || id == "" {
		return layout, cliexit.Usage("--widget expects id=x,y,w,h, received %q", pair)
	}
	parts := strings.Split(coords, ",")
	if len(parts) != 4 {
		return layout, cliexit.Usage("--widget %s expects four numbers x,y,w,h, received %q", id, coords)
	}

	numbers := make([]int, 4)
	for i, part := range parts {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return layout, cliexit.Usage("--widget %s has a non-numeric coordinate %q", id, part)
		}
		numbers[i] = value
	}
	return dashboard.WidgetLayout{
		WidgetID: id,
		X:        numbers[0],
		Y:        numbers[1],
		W:        numbers[2],
		H:        numbers[3],
	}, nil
}

func init() {
	dashRollbackCmd.Flags().Int("revision", 0, "Revision to roll back to (required)")

	dashLayoutSetCmd.Flags().StringArray("widget", nil, "Widget position as id=x,y,w,h (repeatable)")
	dashLayoutSetCmd.Flags().String("layout", "", "Layout array as JSON, @file, or @- for stdin")

	dashLayoutCmd.AddCommand(dashLayoutGetCmd, dashLayoutSetCmd, dashLayoutCommitCmd)
	dashCmd.AddCommand(dashRevisionsCmd, dashRevisionCmd, dashRollbackCmd, dashLayoutCmd)
}
