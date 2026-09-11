package cmd

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/hub"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/spf13/cobra"
)

// Editing a data source you do not own produces a draft instead of a change,
// which is worth saying once at the top of each write command's help.
const draftNotice = `Writing to a data source you do not own produces a draft for review rather
than an immediate change.`

// ---------- table definitions ----------

var hubTableCmd = &cobra.Command{
	Use:   "table",
	Short: "Manage table descriptions",
}

var hubTableLsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List table descriptions",
	Long: `  infini-cli hub table ls --table
  infini-cli hub table ls --database db_1 --search 销售 --table`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := hubAPI()
		if err != nil {
			return err
		}
		page, err := hub.List[hub.TableData](api, hub.KindTable, hubPageQuery(cmd))
		if err != nil {
			return err
		}
		return output.Success(page, []string{"ID", "DATABASE", "TABLE", "DESCRIPTION"}, func() [][]string {
			rows := make([][]string, 0, len(page.Items))
			for _, item := range page.Items {
				rows = append(rows, []string{item.ID, item.DatabaseID, item.TableName, firstLine(item.Description)})
			}
			return rows
		})
	},
}

var hubTableDbsCmd = &cobra.Command{
	Use:   "dbs",
	Short: "List data sources that already have table descriptions",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := hubAPI()
		if err != nil {
			return err
		}
		ids, err := api.TableDatabaseIDs()
		if err != nil {
			return err
		}
		return output.Success(ids, []string{"DATABASE ID"}, func() [][]string {
			rows := make([][]string, 0, len(ids))
			for _, id := range ids {
				rows = append(rows, []string{id})
			}
			return rows
		})
	},
}

var hubTableAddCmd = &cobra.Command{
	Use:   "add <data-source> <table>",
	Short: "Describe a table",
	Long: `  infini-cli hub table add chinook artists --description "艺人主表，一行一个艺人"

` + draftNotice,
	Args: exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		description, err := requireDescription(cmd)
		if err != nil {
			return err
		}
		source, err := resolveDatabaseID(args[0])
		if err != nil {
			return err
		}

		api, err := hubAPI()
		if err != nil {
			return err
		}
		result, err := api.Add(hub.KindTable, map[string]string{
			"database_id":       source.ID,
			"table_name":        args[1],
			"table_description": description,
		})
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

var hubTableUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Change a table description",
	Long: `The server's update payload carries the whole record, so this reads the
current description first and overlays only what you passed.

  infini-cli hub table update t_1 --description "订单事实表，按天分区"

` + draftNotice,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		if !flags.Changed("description") && !flags.Changed("table-name") {
			return cliexit.Usage("nothing to update; pass --description or --table-name")
		}

		current, err := findHubEntity(hub.KindTable, args[0], "table definition",
			func(item *hub.TableData) string { return item.ID })
		if err != nil {
			return err
		}
		api, err := hubAPI()
		if err != nil {
			return err
		}

		body := map[string]string{
			"id":                current.ID,
			"database_id":       current.DatabaseID,
			"table_name":        current.TableName,
			"table_description": current.Description,
		}
		if flags.Changed("description") {
			body["table_description"], _ = flags.GetString("description")
		}
		if flags.Changed("table-name") {
			body["table_name"], _ = flags.GetString("table-name")
		}

		result, err := api.Update(hub.KindTable, body)
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

// findHubEntity locates one entity by id. The hub has no get-by-id route, and
// its search filter matches the id column, so a narrow search stands in for one.
func findHubEntity[T any](kind hub.Kind, id, label string, idOf func(*T) string) (*T, error) {
	api, err := hubAPI()
	if err != nil {
		return nil, err
	}
	page, err := hub.List[T](api, kind, hub.PageQuery{Search: id, PageSize: 100})
	if err != nil {
		return nil, err
	}
	for i := range page.Items {
		if idOf(&page.Items[i]) == id {
			return &page.Items[i], nil
		}
	}
	return nil, cliexit.New(cliexit.CodeBusiness, "no %s %s is visible to this account", label, id)
}

var hubTableRmCmd = &cobra.Command{
	Use:   "rm <id> [id...]",
	Short: "Delete table descriptions",
	Long:  `Deleting a table description also removes the column definitions under it.`,
	Args:  minArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return deleteHubEntities(hub.KindTable, args,
			"Delete %d table description(s) and the column definitions under them?")
	},
}

// ---------- column definitions ----------

var hubColumnCmd = &cobra.Command{
	Use:     "column",
	Aliases: []string{"col"},
	Short:   "Manage column definitions",
	Long: `Column definitions hang off a table description, so they are addressed by
that table's id rather than by data source and table name.

  infini-cli hub table ls --database db_1 --table   # find the table id first`,
}

var hubColumnLsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List column definitions",
	Args:    exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := hubAPI()
		if err != nil {
			return err
		}
		page, err := hub.List[hub.TableColumn](api, hub.KindColumn, hubPageQuery(cmd))
		if err != nil {
			return err
		}
		return output.Success(page, []string{"ID", "TABLE ID", "COLUMN", "MEANING"}, func() [][]string {
			rows := make([][]string, 0, len(page.Items))
			for _, item := range page.Items {
				rows = append(rows, []string{item.ID, item.TableID, item.ColumnName, firstLine(item.BusinessMeaning)})
			}
			return rows
		})
	},
}

var hubColumnAddCmd = &cobra.Command{
	Use:   "add <table-id> <column>",
	Short: "Define a column",
	Long: `  infini-cli hub column add t_1 total --meaning "订单金额，含税，单位元"

` + draftNotice,
	Args: exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		definition, _ := flags.GetString("definition")
		meaning, _ := flags.GetString("meaning")
		if definition == "" && meaning == "" {
			return cliexit.Usage("pass --meaning or --definition; a column entry with neither carries no information")
		}

		api, err := hubAPI()
		if err != nil {
			return err
		}
		result, err := api.Add(hub.KindColumn, map[string]string{
			"table_id":                args[0],
			"column_name":             args[1],
			"column_definition":       definition,
			"column_business_meaning": meaning,
		})
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

var hubColumnUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Change a column definition",
	Long:  draftNotice,
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		if !flags.Changed("definition") && !flags.Changed("meaning") && !flags.Changed("column-name") {
			return cliexit.Usage("nothing to update; pass --meaning, --definition or --column-name")
		}

		current, err := findHubEntity(hub.KindColumn, args[0], "column definition",
			func(item *hub.TableColumn) string { return item.ID })
		if err != nil {
			return err
		}
		api, err := hubAPI()
		if err != nil {
			return err
		}

		body := map[string]string{
			"id":                      current.ID,
			"table_id":                current.TableID,
			"column_name":             current.ColumnName,
			"column_definition":       current.Definition,
			"column_business_meaning": current.BusinessMeaning,
		}
		if flags.Changed("definition") {
			body["column_definition"], _ = flags.GetString("definition")
		}
		if flags.Changed("meaning") {
			body["column_business_meaning"], _ = flags.GetString("meaning")
		}
		if flags.Changed("column-name") {
			body["column_name"], _ = flags.GetString("column-name")
		}

		result, err := api.Update(hub.KindColumn, body)
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

var hubColumnRmCmd = &cobra.Command{
	Use:   "rm <id> [id...]",
	Short: "Delete column definitions",
	Args:  minArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return deleteHubEntities(hub.KindColumn, args, "Delete %d column definition(s)?")
	},
}

// ---------- playbooks ----------

var hubPlaybookCmd = &cobra.Command{
	Use:   "playbook",
	Short: "Manage analysis playbooks",
	Long: `A playbook is a procedure the agent follows for a recurring question, scoped
to the data sources it applies to.`,
}

var hubPlaybookLsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List playbooks",
	Args:    exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := hubAPI()
		if err != nil {
			return err
		}
		page, err := hub.List[hub.Playbook](api, hub.KindPlaybook, hubPageQuery(cmd))
		if err != nil {
			return err
		}
		return output.Success(page, []string{"ID", "NAME", "DATABASES", "DESCRIPTION"}, func() [][]string {
			rows := make([][]string, 0, len(page.Items))
			for _, item := range page.Items {
				rows = append(rows, []string{
					item.ID, item.Name,
					strconv.Itoa(len(item.DatabaseIDs)),
					firstLine(item.Description),
				})
			}
			return rows
		})
	},
}

var hubPlaybookAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Create a playbook",
	Long: `At least one data source is required: a playbook that applies to nothing
would never be selected.

  infini-cli hub playbook add "月度复盘" --database db_1 --content @playbook.md

` + draftNotice,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := playbookBody(cmd, args[0], nil)
		if err != nil {
			return err
		}

		api, err := hubAPI()
		if err != nil {
			return err
		}
		result, err := api.Add(hub.KindPlaybook, body)
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

var hubPlaybookUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Change a playbook",
	Long:  draftNotice,
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		if !flags.Changed("name") && !flags.Changed("database") &&
			!flags.Changed("content") && !flags.Changed("description") {
			return cliexit.Usage("nothing to update; pass at least one of --name, --database, --content, --description")
		}

		current, err := findHubEntity(hub.KindPlaybook, args[0], "playbook",
			func(item *hub.Playbook) string { return item.ID })
		if err != nil {
			return err
		}
		api, err := hubAPI()
		if err != nil {
			return err
		}

		name := current.Name
		if flags.Changed("name") {
			name, _ = flags.GetString("name")
		}
		body, err := playbookBody(cmd, name, current)
		if err != nil {
			return err
		}
		body["id"] = current.ID

		result, err := api.Update(hub.KindPlaybook, body)
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

func playbookBody(cmd *cobra.Command, name string, current *hub.Playbook) (map[string]any, error) {
	flags := cmd.Flags()
	databases, _ := flags.GetStringSlice("database")
	content, _ := flags.GetString("content")
	description, _ := flags.GetString("description")

	if content != "" && strings.HasPrefix(content, "@") {
		resolved, err := readTextFile(strings.TrimPrefix(content, "@"))
		if err != nil {
			return nil, err
		}
		content = resolved
	}

	body := map[string]any{"playbook_name": name}
	if current != nil {
		body["database_ids"] = current.DatabaseIDs
		body["playbook_content"] = current.Content
		body["playbook_description"] = current.Description
	}
	if len(databases) > 0 {
		ids := make([]string, 0, len(databases))
		for _, value := range databases {
			source, err := resolveDatabaseID(value)
			if err != nil {
				return nil, err
			}
			ids = append(ids, source.ID)
		}
		body["database_ids"] = ids
	}
	if flags.Changed("content") {
		body["playbook_content"] = content
	}
	if flags.Changed("description") {
		body["playbook_description"] = description
	}

	ids, _ := body["database_ids"].([]string)
	if len(ids) == 0 {
		return nil, cliexit.Usage("--database is required at least once; a playbook scoped to nothing is never selected")
	}
	return body, nil
}

var hubPlaybookRmCmd = &cobra.Command{
	Use:   "rm <id> [id...]",
	Short: "Delete playbooks",
	Args:  minArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return deleteHubEntities(hub.KindPlaybook, args, "Delete %d playbook(s)?")
	},
}

// ---------- KPIs ----------

var hubKpiCmd = &cobra.Command{
	Use:   "kpi",
	Short: "Manage KPI definitions",
	Long: `A KPI is defined either as business logic or as SQL, and --mode decides which
field carries the definition:

  business_logic   --logic describes the calculation in words
  sql_playground   --sql holds the statement, --set holds its SET prelude

Validate a SQL KPI before saving it with ` + "`task kpi-sql`" + `.`,
}

var hubKpiLsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List KPIs",
	Args:    exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := hubAPI()
		if err != nil {
			return err
		}
		page, err := hub.List[hub.KPI](api, hub.KindKPI, hubPageQuery(cmd))
		if err != nil {
			return err
		}
		return output.Success(page, []string{"ID", "NAME", "MODE", "TABLES", "DESCRIPTION"}, func() [][]string {
			rows := make([][]string, 0, len(page.Items))
			for _, item := range page.Items {
				rows = append(rows, []string{
					item.ID, item.Name, item.CreationMode,
					strconv.Itoa(len(item.Tables)),
					firstLine(item.Description),
				})
			}
			return rows
		})
	},
}

var hubKpiAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Create a KPI",
	Long: `  infini-cli hub kpi add 月度销售额 --mode sql_playground \
      --table chinook.invoices --sql @monthly_sales.sql
  infini-cli hub kpi add 客单价 --mode business_logic --logic "销售额 / 订单数"

` + draftNotice,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := kpiBody(cmd, args[0], nil)
		if err != nil {
			return err
		}

		api, err := hubAPI()
		if err != nil {
			return err
		}
		result, err := api.Add(hub.KindKPI, body)
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

var hubKpiUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Change a KPI",
	Long:  draftNotice,
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		current, err := findHubEntity(hub.KindKPI, args[0], "KPI",
			func(item *hub.KPI) string { return item.ID })
		if err != nil {
			return err
		}
		api, err := hubAPI()
		if err != nil {
			return err
		}

		name := current.Name
		if cmd.Flags().Changed("name") {
			name, _ = cmd.Flags().GetString("name")
		}
		body, err := kpiBody(cmd, name, current)
		if err != nil {
			return err
		}
		body["id"] = current.ID

		result, err := api.Update(hub.KindKPI, body)
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

func kpiBody(cmd *cobra.Command, name string, current *hub.KPI) (map[string]any, error) {
	flags := cmd.Flags()
	mode, _ := flags.GetString("mode")
	if current != nil && !flags.Changed("mode") {
		mode = current.CreationMode
	}
	if mode == "" {
		return nil, cliexit.Usage("--mode is required, one of: %s", strings.Join(hub.KpiCreationModes, ", "))
	}
	if !slices.Contains(hub.KpiCreationModes, mode) {
		return nil, cliexit.Usage("unknown mode %q, expected one of: %s", mode, strings.Join(hub.KpiCreationModes, ", "))
	}

	body := map[string]any{"kpi_name": name, "creation_mode": mode}
	if current != nil {
		body["kpi_alias"] = current.Alias
		body["relevant_for_designations"] = current.Designations
		body["kpi_description"] = current.Description
		body["calculation_logic"] = current.CalculationLogic
		body["sql_query"] = current.SQLQuery
		body["set_values"] = current.SetValues
		if len(current.Tables) > 0 {
			body["tables"] = current.Tables
		}
	}

	if flags.Changed("table") {
		selections, _ := flags.GetStringSlice("table")
		resolver, err := newTableRefResolver()
		if err != nil {
			return nil, err
		}
		refs, err := resolver.resolve(selections)
		if err != nil {
			return nil, err
		}
		body["tables"] = refs
	}
	for flag, field := range map[string]string{
		"alias":        "kpi_alias",
		"designations": "relevant_for_designations",
		"description":  "kpi_description",
		"logic":        "calculation_logic",
		"set":          "set_values",
	} {
		if flags.Changed(flag) {
			value, _ := flags.GetString(flag)
			body[field] = value
		}
	}
	if flags.Changed("sql") {
		sql, _ := flags.GetString("sql")
		if strings.HasPrefix(sql, "@") {
			resolved, err := readTextFile(strings.TrimPrefix(sql, "@"))
			if err != nil {
				return nil, err
			}
			sql = resolved
		}
		body["sql_query"] = sql
	}

	// The mode decides which field the server reads, so a definition that is
	// empty for the chosen mode would save an unusable KPI.
	switch mode {
	case "sql_playground":
		if asString(body["sql_query"]) == "" {
			return nil, cliexit.Usage("--sql is required in sql_playground mode")
		}
	case "business_logic":
		if asString(body["calculation_logic"]) == "" {
			return nil, cliexit.Usage("--logic is required in business_logic mode")
		}
	}
	return body, nil
}

var hubKpiRmCmd = &cobra.Command{
	Use:   "rm <id> [id...]",
	Short: "Delete KPIs",
	Args:  minArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return deleteHubEntities(hub.KindKPI, args, "Delete %d KPI definition(s)?")
	},
}

// ---------- preferences ----------

var hubPrefCmd = &cobra.Command{
	Use:     "pref",
	Aliases: []string{"preference"},
	Short:   "Manage presentation preferences",
}

var hubPrefLsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List preferences",
	Args:    exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := hubAPI()
		if err != nil {
			return err
		}
		page, err := hub.List[hub.Preference](api, hub.KindPreference, hubPageQuery(cmd))
		if err != nil {
			return err
		}
		return output.Success(page, []string{"ID", "NAME", "TYPE", "VALUE"}, func() [][]string {
			rows := make([][]string, 0, len(page.Items))
			for _, item := range page.Items {
				rows = append(rows, []string{item.ID, item.Name, item.Type, firstLine(item.Value)})
			}
			return rows
		})
	},
}

var hubPrefAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Create a preference",
	Long: `  infini-cli hub pref add "金额显示" --value "金额保留两位小数，单位万元" \
      --table chinook.invoices

` + draftNotice,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := preferenceBody(cmd, args[0], nil)
		if err != nil {
			return err
		}

		api, err := hubAPI()
		if err != nil {
			return err
		}
		result, err := api.Add(hub.KindPreference, body)
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

var hubPrefUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Change a preference",
	Long:  draftNotice,
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		current, err := findHubEntity(hub.KindPreference, args[0], "preference",
			func(item *hub.Preference) string { return item.ID })
		if err != nil {
			return err
		}
		api, err := hubAPI()
		if err != nil {
			return err
		}

		name := current.Name
		if cmd.Flags().Changed("name") {
			name, _ = cmd.Flags().GetString("name")
		}
		body, err := preferenceBody(cmd, name, current)
		if err != nil {
			return err
		}
		body["id"] = current.ID

		result, err := api.Update(hub.KindPreference, body)
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

func preferenceBody(cmd *cobra.Command, name string, current *hub.Preference) (map[string]any, error) {
	flags := cmd.Flags()
	prefType, _ := flags.GetString("type")
	if prefType == "" {
		if current != nil {
			prefType = current.Type
		} else {
			prefType = hub.PreferenceTypes[0]
		}
	}
	if !slices.Contains(hub.PreferenceTypes, prefType) {
		return nil, cliexit.Usage("unknown type %q, expected one of: %s", prefType, strings.Join(hub.PreferenceTypes, ", "))
	}

	body := map[string]any{"preference_name": name, "preference_type": prefType}
	if current != nil {
		body["preference_value"] = current.Value
		if len(current.Tables) > 0 {
			body["tables"] = current.Tables
		}
	}
	if flags.Changed("value") {
		value, _ := flags.GetString("value")
		body["preference_value"] = value
	}
	if flags.Changed("table") {
		selections, _ := flags.GetStringSlice("table")
		resolver, err := newTableRefResolver()
		if err != nil {
			return nil, err
		}
		refs, err := resolver.resolve(selections)
		if err != nil {
			return nil, err
		}
		body["tables"] = refs
	}

	if asString(body["preference_value"]) == "" {
		return nil, cliexit.Usage("--value is required; a preference with no content changes nothing")
	}
	return body, nil
}

var hubPrefRmCmd = &cobra.Command{
	Use:   "rm <id> [id...]",
	Short: "Delete preferences",
	Args:  minArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return deleteHubEntities(hub.KindPreference, args, "Delete %d preference(s)?")
	},
}

func deleteHubEntities(kind hub.Kind, ids []string, prompt string) error {
	if err := confirm(fmt.Sprintf(prompt, len(ids))); err != nil {
		return err
	}
	api, err := hubAPI()
	if err != nil {
		return err
	}
	result, err := api.Delete(kind, ids)
	if err != nil {
		return err
	}
	return output.Success(map[string]any{
		"entityType": string(kind),
		"deleted":    ids,
		"result":     normalizeRaw(result),
	}, nil, nil)
}

func requireDescription(cmd *cobra.Command) (string, error) {
	description, _ := cmd.Flags().GetString("description")
	if description == "" {
		return "", cliexit.Usage("--description is required; an entry with no description tells the agent nothing")
	}
	if strings.HasPrefix(description, "@") {
		return readTextFile(strings.TrimPrefix(description, "@"))
	}
	return description, nil
}

func asString(value any) string {
	text, _ := value.(string)
	return text
}

func init() {
	for _, cmd := range []*cobra.Command{
		hubTableLsCmd, hubColumnLsCmd, hubPlaybookLsCmd, hubKpiLsCmd, hubPrefLsCmd,
	} {
		addHubPageFlags(cmd)
	}

	hubTableAddCmd.Flags().String("description", "", "What the table holds; @file to read from a file")
	hubTableUpdateCmd.Flags().String("description", "", "New description; @file to read from a file")
	hubTableUpdateCmd.Flags().String("table-name", "", "New table name")

	for _, cmd := range []*cobra.Command{hubColumnAddCmd, hubColumnUpdateCmd} {
		cmd.Flags().String("definition", "", "Technical definition, e.g. its type and units")
		cmd.Flags().String("meaning", "", "Business meaning the agent should rely on")
	}
	hubColumnUpdateCmd.Flags().String("column-name", "", "New column name")

	for _, cmd := range []*cobra.Command{hubPlaybookAddCmd, hubPlaybookUpdateCmd} {
		cmd.Flags().StringSlice("database", nil, "Data source id or name it applies to (repeatable)")
		cmd.Flags().String("content", "", "Procedure body; @file to read from a file")
		cmd.Flags().String("description", "", "When to use this playbook")
	}
	hubPlaybookUpdateCmd.Flags().String("name", "", "New name")

	for _, cmd := range []*cobra.Command{hubKpiAddCmd, hubKpiUpdateCmd} {
		cmd.Flags().String("mode", "", "Definition mode: "+strings.Join(hub.KpiCreationModes, ", "))
		cmd.Flags().StringSlice("table", nil, "Source table as <data source>.<table> (repeatable)")
		cmd.Flags().String("alias", "", "Alternate names, comma separated")
		cmd.Flags().String("designations", "", "Roles this KPI is relevant for, comma separated")
		cmd.Flags().String("description", "", "What the metric means")
		cmd.Flags().String("logic", "", "Calculation in words (business_logic mode)")
		cmd.Flags().String("sql", "", "SQL statement (sql_playground mode); @file to read from a file")
		cmd.Flags().String("set", "", "SET statements to run before the SQL, joined with ';'")
	}
	hubKpiUpdateCmd.Flags().String("name", "", "New name")

	for _, cmd := range []*cobra.Command{hubPrefAddCmd, hubPrefUpdateCmd} {
		cmd.Flags().String("type", "", "Preference type: "+strings.Join(hub.PreferenceTypes, ", "))
		cmd.Flags().String("value", "", "The preference itself, in plain language")
		cmd.Flags().StringSlice("table", nil, "Table it applies to as <data source>.<table> (repeatable)")
	}
	hubPrefUpdateCmd.Flags().String("name", "", "New name")

	hubTableCmd.AddCommand(hubTableLsCmd, hubTableDbsCmd, hubTableAddCmd, hubTableUpdateCmd, hubTableRmCmd)
	hubColumnCmd.AddCommand(hubColumnLsCmd, hubColumnAddCmd, hubColumnUpdateCmd, hubColumnRmCmd)
	hubPlaybookCmd.AddCommand(hubPlaybookLsCmd, hubPlaybookAddCmd, hubPlaybookUpdateCmd, hubPlaybookRmCmd)
	hubKpiCmd.AddCommand(hubKpiLsCmd, hubKpiAddCmd, hubKpiUpdateCmd, hubKpiRmCmd)
	hubPrefCmd.AddCommand(hubPrefLsCmd, hubPrefAddCmd, hubPrefUpdateCmd, hubPrefRmCmd)

	hubCmd.AddCommand(hubTableCmd, hubColumnCmd, hubPlaybookCmd, hubKpiCmd, hubPrefCmd)
}
