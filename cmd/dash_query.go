package cmd

import (
	"strconv"
	"strings"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/dashboard"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/spf13/cobra"
)

// filterFlags are shared by every command that submits filter values.
func addFilterFlags(cmd *cobra.Command) {
	cmd.Flags().StringArray("filter", nil, "Filter value as name=value (repeatable); see `dash filter ls`")
	cmd.Flags().String("filter-values", "", "Full filter payload as JSON, @file, or @- (overrides --filter)")
}

// resolveFilters parses --filter against the spec, then overlays --filter-values.
func resolveFilters(cmd *cobra.Command, spec *dashboard.Spec) (dashboard.FilterValues, error) {
	pairs, _ := cmd.Flags().GetStringArray("filter")
	rawFlag, _ := cmd.Flags().GetString("filter-values")

	values, err := dashboard.ParseFilterArgs(spec, pairs)
	if err != nil {
		return nil, err
	}
	raw, err := readJSONArg(rawFlag)
	if err != nil {
		return nil, err
	}
	return dashboard.MergeFilterValues(values, raw)
}

func queryOptions(cmd *cobra.Command) dashboard.QueryOptions {
	force, _ := cmd.Flags().GetBool("force-refresh")
	prefer, _ := cmd.Flags().GetBool("prefer-snapshot")
	only, _ := cmd.Flags().GetBool("snapshot-only")
	return dashboard.QueryOptions{ForceRefresh: force, PreferSnapshot: prefer, SnapshotOnly: only}
}

func addCacheFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("force-refresh", false, "Bypass the snapshot cache and re-run the query")
	cmd.Flags().Bool("prefer-snapshot", false, "Return an existing snapshot even if it is stale")
	cmd.Flags().Bool("snapshot-only", false, "Return only cached snapshots; never hit the data source")
}

var dashQueryCmd = &cobra.Command{
	Use:   "query <id>",
	Short: "Run a dashboard's queries and print the results",
	Long: `Executes the dashboard's data queries with the given filter values.

Without --query-ids every data query runs; filter_options queries are excluded
because they exist to populate filter dropdowns and the endpoint rejects them.

Filter values are typed from the dashboard's own spec, so:
  --filter region=east                    text or single-select enum
  --filter tags=a,b                       multi-select enum
  --filter min_pv=100                     number
  --filter period=2026-01-01..2026-01-31  explicit date range
  --filter period=last_30d                date range preset

A query that fails is reported in its own result entry rather than aborting the
command, so a partially broken dashboard still returns the widgets that work.
The command exits non-zero if every requested query failed.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		queryIDs, _ := cmd.Flags().GetStringArray("query-ids")

		api, err := dashAPI()
		if err != nil {
			return err
		}
		_, spec, err := api.GetWithSpec(args[0])
		if err != nil {
			return err
		}

		if len(queryIDs) == 0 {
			queryIDs = spec.DataQueryIDs()
			if len(queryIDs) == 0 {
				return cliexit.New(cliexit.CodeBusiness, "dashboard %s has no data queries", args[0])
			}
		}
		filters, err := resolveFilters(cmd, spec)
		if err != nil {
			return err
		}

		results, err := api.Query(args[0], queryIDs, filters, queryOptions(cmd))
		if err != nil {
			return err
		}

		failed := 0
		for _, result := range results {
			if result.Status != "ok" {
				failed++
			}
		}

		if err := output.Success(results, []string{"QUERY", "STATUS", "ROWS", "CACHED", "ERROR"}, func() [][]string {
			rows := make([][]string, 0, len(results))
			for _, id := range queryIDs {
				result, ok := results[id]
				if !ok {
					continue
				}
				rows = append(rows, []string{
					id,
					result.Status,
					intOrDash(result.RowCount),
					boolOrDash(result.FromCache),
					firstLine(result.ErrorMessage),
				})
			}
			return rows
		}); err != nil {
			return err
		}

		if failed > 0 && failed == len(results) {
			return cliexit.Hint(
				cliexit.New(cliexit.CodeBusiness, "all %d query(s) failed", failed),
				"inspect error_code/error_message per query, or retry with --force-refresh",
			)
		}
		if failed > 0 {
			output.Note("%d of %d queries failed", failed, len(results))
		}
		return nil
	},
}

var dashTableQueryCmd = &cobra.Command{
	Use:   "table-query <id>",
	Short: "Page, search, sort and filter a single table widget",
	Long: `Runs one table widget's query with paging and column filters pushed down to the
data source.

  infini-cli dash table-query abc --widget w_orders --page 0 --page-size 50
  infini-cli dash table-query abc --widget w_orders \
      --search-columns customer,sku --search "acme" \
      --sort amount:desc

Timestamps use 'YYYY-MM-DD HH:MM:SS'. Column filters beyond search and sort are
passed as JSON via --column-filters.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		widgetID, _ := cmd.Flags().GetString("widget")
		if widgetID == "" {
			return cliexit.Usage("--widget is required")
		}
		pageIndex, _ := cmd.Flags().GetInt("page")
		pageSize, _ := cmd.Flags().GetInt("page-size")
		searchValue, _ := cmd.Flags().GetString("search")
		searchColumns, _ := cmd.Flags().GetStringSlice("search-columns")
		sortSpec, _ := cmd.Flags().GetString("sort")
		columnFiltersJSON, _ := cmd.Flags().GetString("column-filters")

		api, err := dashAPI()
		if err != nil {
			return err
		}
		_, spec, err := api.GetWithSpec(args[0])
		if err != nil {
			return err
		}
		if spec.WidgetByID(widgetID) == nil {
			return cliexit.Usage("unknown widget %q; this dashboard has: %s",
				widgetID, strings.Join(spec.WidgetIDs(), ", "))
		}
		filters, err := resolveFilters(cmd, spec)
		if err != nil {
			return err
		}

		req := dashboard.TableQueryRequest{
			WidgetID:     widgetID,
			FilterValues: filters,
			PageIndex:    pageIndex,
			PageSize:     pageSize,
		}
		opts := queryOptions(cmd)
		req.ForceRefresh, req.PreferSnap, req.SnapshotOnly = opts.ForceRefresh, opts.PreferSnapshot, opts.SnapshotOnly

		if searchValue != "" {
			if len(searchColumns) == 0 {
				return cliexit.Usage("--search requires --search-columns")
			}
			req.Search = &dashboard.TableSearch{Columns: searchColumns, Value: searchValue}
		}
		if sortSpec != "" {
			sort, err := parseSort(sortSpec)
			if err != nil {
				return err
			}
			req.Sort = sort
		}
		if columnFiltersJSON != "" {
			raw, err := readJSONArg(columnFiltersJSON)
			if err != nil {
				return err
			}
			var parsed []map[string]any
			if err := unmarshalJSON(raw, &parsed); err != nil {
				return cliexit.Usage("--column-filters must be a JSON array: %v", err)
			}
			req.ColumnFilters = parsed
		}

		result, err := api.TableQuery(args[0], req)
		if err != nil {
			return err
		}
		if result.Status != "ok" {
			return cliexit.New(cliexit.CodeBusiness, "table query failed (%s): %s",
				result.ErrorCode, result.ErrorMessage)
		}
		return output.Success(result, result.Columns, func() [][]string {
			return stringifyRows(result.Datas)
		})
	},
}

var dashFilterCmd = &cobra.Command{
	Use:   "filter",
	Short: "Inspect dashboard filters and their valid values",
}

var dashFilterLsCmd = &cobra.Command{
	Use:     "ls <id>",
	Aliases: []string{"list"},
	Short:   "List the dashboard's filters and how to pass them",
	Args:    exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := dashAPI()
		if err != nil {
			return err
		}
		_, spec, err := api.GetWithSpec(args[0])
		if err != nil {
			return err
		}
		return output.Success(filterSummaries(spec), []string{"NAME", "TYPE", "MULTIPLE", "LABEL", "EXAMPLE"}, func() [][]string {
			rows := make([][]string, 0, len(spec.Filters))
			for _, filter := range spec.Filters {
				rows = append(rows, []string{
					filter.Name,
					filter.Type,
					strconv.FormatBool(filter.Multiple),
					filter.Label,
					filterExample(filter),
				})
			}
			return rows
		})
	},
}

// filterExample shows the flag form for each filter type, which is the question
// every operator asks first.
func filterExample(filter dashboard.Filter) string {
	switch filter.Type {
	case "daterange":
		return "--filter " + filter.Name + "=last_30d"
	case "number":
		return "--filter " + filter.Name + "=100"
	case "enum":
		if filter.Multiple {
			return "--filter " + filter.Name + "=a,b"
		}
		return "--filter " + filter.Name + "=value"
	default:
		return "--filter " + filter.Name + "=text"
	}
}

var dashFilterOptionsCmd = &cobra.Command{
	Use:   "options <id> <filterName>",
	Short: "List the allowed values of an enum filter",
	Args:  exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := dashAPI()
		if err != nil {
			return err
		}
		options, err := api.FilterOptions(args[0], args[1])
		if err != nil {
			return err
		}
		return output.Success(options, []string{"VALUE", "LABEL"}, func() [][]string {
			rows := make([][]string, 0, len(options))
			for _, option := range options {
				rows = append(rows, []string{option.Value, option.Label})
			}
			return rows
		})
	},
}

var dashFilterRangeCmd = &cobra.Command{
	Use:   "range <id> <filterName>",
	Short: "Show the real data boundary of a daterange filter",
	Args:  exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := dashAPI()
		if err != nil {
			return err
		}
		result, err := api.FilterRange(args[0], args[1])
		if err != nil {
			return err
		}
		return output.Success(result, nil, nil)
	},
}

var dashAskCmd = &cobra.Command{
	Use:   "ask <id>",
	Short: "Build the agent context attachment for a dashboard or widget",
	Long: `Produces the context pack the web UI sends when you ask the agent about a
dashboard: the rendered numbers, the bound databases and the active filters.

This command only builds the attachment. Feeding it into a conversation is the
job of the agent commands (` + "`dash chat`" + `), which arrive with the agent track.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		widgetID, _ := cmd.Flags().GetString("widget")
		scope := "dashboard"
		if widgetID != "" {
			scope = "widget"
		}

		api, err := dashAPI()
		if err != nil {
			return err
		}
		_, spec, err := api.GetWithSpec(args[0])
		if err != nil {
			return err
		}
		if widgetID != "" && spec.WidgetByID(widgetID) == nil {
			return cliexit.Usage("unknown widget %q; this dashboard has: %s",
				widgetID, strings.Join(spec.WidgetIDs(), ", "))
		}
		filters, err := resolveFilters(cmd, spec)
		if err != nil {
			return err
		}

		context, err := api.Ask(args[0], scope, widgetID, filters)
		if err != nil {
			return err
		}
		return output.Success(context, nil, nil)
	},
}

func parseSort(spec string) (*dashboard.TableSort, error) {
	column, direction, found := strings.Cut(spec, ":")
	if !found {
		direction = "asc"
	}
	if column == "" {
		return nil, cliexit.Usage("--sort expects column[:asc|desc], received %q", spec)
	}
	if direction != "asc" && direction != "desc" {
		return nil, cliexit.Usage("--sort direction must be asc or desc, received %q", direction)
	}
	return &dashboard.TableSort{Column: column, Direction: direction}, nil
}

func init() {
	dashQueryCmd.Flags().StringArray("query-ids", nil, "Query ids to run (default: every data query)")
	addFilterFlags(dashQueryCmd)
	addCacheFlags(dashQueryCmd)

	dashTableQueryCmd.Flags().String("widget", "", "Table widget id (required)")
	dashTableQueryCmd.Flags().Int("page", 0, "Page index, starting at 0")
	dashTableQueryCmd.Flags().Int("page-size", 50, "Rows per page (5-200)")
	dashTableQueryCmd.Flags().String("search", "", "Search text")
	dashTableQueryCmd.Flags().StringSlice("search-columns", nil, "Columns to search")
	dashTableQueryCmd.Flags().String("sort", "", "Sort as column[:asc|desc]")
	dashTableQueryCmd.Flags().String("column-filters", "", "Column filters as JSON, @file, or @-")
	addFilterFlags(dashTableQueryCmd)
	addCacheFlags(dashTableQueryCmd)

	dashAskCmd.Flags().String("widget", "", "Scope the context to a single widget")
	addFilterFlags(dashAskCmd)

	dashFilterCmd.AddCommand(dashFilterLsCmd, dashFilterOptionsCmd, dashFilterRangeCmd)
	dashCmd.AddCommand(dashQueryCmd, dashTableQueryCmd, dashFilterCmd, dashAskCmd)
}
