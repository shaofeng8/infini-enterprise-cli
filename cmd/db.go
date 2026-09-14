package cmd

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/database"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/spf13/cobra"
)

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Manage data sources",
	Long: `Data sources are the databases and file stores the agent queries.

Connection details live in a JSON config whose shape depends on --type. Field
names are driver-prefixed (dm_host, mysql_host, sqlite_path) — not a generic
host/port/path. The CLI passes --config through verbatim; the server stores it
as a string and the inspector reads those exact keys.

Run infini-cli db types (or db types dm) for the per-type --config catalog.
db ls only lists saved sources; it is not how you discover field names.
db test checks a config before you save it.`,
}

func dbAPI() (*database.API, error) {
	c, err := client.New()
	if err != nil {
		return nil, err
	}
	return database.NewAPI(c), nil
}

var dbLsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List data sources",
	Long: `  infini-cli db ls --table
  infini-cli db ls --type mysql --enabled --table
  infini-cli db ls --context-hub not_in --table   # sources with no semantic layer yet

With --type, the JSON also includes typeGuide (required keys and an example).
That is so an agent that probes with db ls --type dm still sees dm_host and
friends, even when no Dameng source has been saved yet. For the full catalog
use db types.`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()

		dbType, _ := flags.GetString("type")
		if dbType != "" && !slices.Contains(database.Types, dbType) {
			return unknownDatabaseType(dbType)
		}
		source, _ := flags.GetString("source")
		if source != "" && !slices.Contains(database.Sources, source) {
			return cliexit.Usage("--source must be one of: %s", strings.Join(database.Sources, ", "))
		}
		contextHub, _ := flags.GetString("context-hub")
		if contextHub != "" && contextHub != "in" && contextHub != "not_in" {
			return cliexit.Usage("--context-hub must be in or not_in, received %q", contextHub)
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
		name, _ := flags.GetString("name")

		api, err := dbAPI()
		if err != nil {
			return err
		}
		result, err := api.List(database.ListQuery{
			Page:            page,
			PageSize:        pageSize,
			Name:            name,
			Type:            dbType,
			Enabled:         enabled,
			ContextHubState: contextHub,
			Source:          source,
		})
		if err != nil {
			return err
		}

		payload := any(result)
		hint := ""
		if dbType != "" {
			if guide, ok := database.GuideFor(dbType); ok {
				payload = map[string]any{
					"items":     result.Items,
					"meta":      result.Meta,
					"typeGuide": guide,
				}
				if len(result.Items) == 0 {
					hint = "no saved " + guide.Type + " sources; --config keys are in data.typeGuide (required: " +
						strings.Join(guide.Required, ", ") + "). Do not infer host/port from another driver. infini-cli db types " +
						guide.Type + " prints the same catalog."
				}
			}
		}

		return output.SuccessHint(payload, hint, []string{"ID", "NAME", "NICKNAME", "TYPE", "ENABLED", "SOURCE"}, func() [][]string {
			rows := make([][]string, 0, len(result.Items))
			for _, item := range result.Items {
				rows = append(rows, []string{
					item.ID, item.Name, item.Nickname, item.Type,
					intOrDash(item.Enabled), item.Source,
				})
			}
			return rows
		})
	},
}

var dbTypesCmd = &cobra.Command{
	Use:   "types [type]",
	Short: "Show --config fields for each driver",
	Long: `Prints the connection-config catalog as JSON. This is the command to run
when adding a data source — not db ls, which only lists sources that already
exist.

  infini-cli db types
  infini-cli db types dm
  infini-cli db types --table

Keys are driver-prefixed: Dameng is dm_host/dm_port/dm_username/dm_password/
dm_database, not host/port/username.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			guide, ok := database.GuideFor(args[0])
			if !ok {
				return unknownDatabaseType(args[0])
			}
			return output.Success(guide, []string{"FIELD", "VALUE"}, func() [][]string {
				port := ""
				if guide.DefaultPort != 0 {
					port = fmt.Sprintf("%d", guide.DefaultPort)
				}
				return [][]string{
					{"type", guide.Type},
					{"title", guide.Title},
					{"defaultPort", port},
					{"required", strings.Join(guide.Required, ", ")},
					{"optional", strings.Join(guide.Optional, ", ")},
					{"example", guide.Example},
					{"notes", guide.Notes},
				}
			})
		}
		guides := database.TypeGuides()
		return output.Success(map[string]any{"types": guides}, []string{"TYPE", "TITLE", "PORT", "REQUIRED"}, func() [][]string {
			rows := make([][]string, 0, len(guides))
			for _, guide := range guides {
				port := ""
				if guide.DefaultPort != 0 {
					port = fmt.Sprintf("%d", guide.DefaultPort)
				}
				rows = append(rows, []string{guide.Type, guide.Title, port, strings.Join(guide.Required, ", ")})
			}
			return rows
		})
	},
}

var dbShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show a data source",
	Long: `Looks the data source up by id, or by name with --by-name.

  infini-cli db show db_1
  infini-cli db show chinook --by-name`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		byName, _ := cmd.Flags().GetBool("by-name")

		api, err := dbAPI()
		if err != nil {
			return err
		}

		var item *database.Item
		if byName {
			item, err = api.GetByName(args[0])
		} else {
			item, err = api.Get(args[0])
		}
		if err != nil {
			return err
		}
		return output.Success(item, []string{"FIELD", "VALUE"}, func() [][]string {
			return [][]string{
				{"id", item.ID},
				{"name", item.Name},
				{"nickname", item.Nickname},
				{"type", item.Type},
				{"enabled", intOrDash(item.Enabled)},
				{"description", firstLine(item.Description)},
				{"config", firstLine(item.Config)},
			}
		})
	},
}

var dbAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a data source",
	Long: `Creates a data source. --name, --type and --config are required.

--config is a JSON object whose keys depend on --type (see the catalog below).
Inline JSON, @file or @- are all accepted. Test first:

  infini-cli db test --type dm --config @dm.json
  infini-cli db add --name dameng_prod --type dm --config @dm.json --nickname 达梦生产
  infini-cli db add --name chinook --type sqlite --config '{"sqlite_path":"/data/chinook.sqlite"}'
  infini-cli db add --name warehouse --type mysql --config @mysql.json --nickname 数仓

` + database.ConfigGuide,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		name, _ := flags.GetString("name")
		dbType, _ := flags.GetString("type")
		configArg, _ := flags.GetString("config")

		if name == "" || dbType == "" || configArg == "" {
			if dbType != "" {
				if guide, ok := database.GuideFor(dbType); ok {
					return cliexit.Hint(
						cliexit.Usage("--name, --type and --config are all required"),
						"%s --config example: %s; required keys: %s",
						guide.Type, guide.Example, strings.Join(guide.Required, ", "),
					)
				}
			}
			return cliexit.Hint(
				cliexit.Usage("--name, --type and --config are all required"),
				"run `infini-cli db types` (or db types <type>) for the --config keys; do not infer host/port from another driver",
			)
		}
		if !slices.Contains(database.Types, dbType) {
			return unknownDatabaseType(dbType)
		}
		config, err := readConfigArg(configArg)
		if err != nil {
			return err
		}

		nickname, _ := flags.GetString("nickname")
		description, _ := flags.GetString("description")
		disabled, _ := flags.GetBool("disabled")
		enabled := 1
		if disabled {
			enabled = 0
		}

		api, err := dbAPI()
		if err != nil {
			return err
		}
		result, err := api.Add(database.Spec{
			Name:        name,
			Nickname:    nickname,
			Type:        dbType,
			Description: description,
			Config:      config,
			Enabled:     enabled,
		})
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

var dbUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a data source",
	Long: `The server's update endpoint requires the full record, so this reads the
current data source first and overlays only the flags you passed. That way
omitting --description does not blank it.

  infini-cli db update db_1 --nickname 生产库
  infini-cli db update db_1 --config @mysql.json

Use ` + "`db enable`" + ` / ` + "`db disable`" + ` to change enablement: it lives in a
per-user mapping, not on the data source itself, and update ignores it.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		if !flags.Changed("name") && !flags.Changed("nickname") &&
			!flags.Changed("type") && !flags.Changed("description") && !flags.Changed("config") {
			return cliexit.Usage("nothing to update; pass at least one of --name, --nickname, --type, --description, --config")
		}

		api, err := dbAPI()
		if err != nil {
			return err
		}
		current, err := api.Get(args[0])
		if err != nil {
			return err
		}
		if current.ID == "" {
			return cliexit.New(cliexit.CodeBusiness, "data source %s was not found", args[0])
		}

		spec := database.Spec{
			ID:          current.ID,
			Name:        current.Name,
			Nickname:    current.Nickname,
			Type:        current.Type,
			Description: current.Description,
			Config:      current.Config,
		}
		if current.Enabled != nil {
			spec.Enabled = *current.Enabled
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
		if flags.Changed("type") {
			dbType, _ := flags.GetString("type")
			if !slices.Contains(database.Types, dbType) {
				return unknownDatabaseType(dbType)
			}
			spec.Type = dbType
		}
		if flags.Changed("config") {
			configArg, _ := flags.GetString("config")
			config, err := readConfigArg(configArg)
			if err != nil {
				return err
			}
			spec.Config = config
		}

		result, err := api.Update(spec)
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

var dbRmCmd = &cobra.Command{
	Use:   "rm <id> [id...]",
	Short: "Delete data sources",
	Args:  minArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := confirm(fmt.Sprintf("Delete %d data source(s)? Dashboards and KPIs that read from them will break.", len(args))); err != nil {
			return err
		}
		api, err := dbAPI()
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

var dbEnableCmd = &cobra.Command{
	Use:   "enable <id> [id...]",
	Short: "Enable data sources for the current user",
	Args:  minArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return setDatabaseEnabled(args, true) },
}

var dbDisableCmd = &cobra.Command{
	Use:   "disable <id> [id...]",
	Short: "Disable data sources for the current user",
	Args:  minArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return setDatabaseEnabled(args, false) },
}

func setDatabaseEnabled(ids []string, enabled bool) error {
	api, err := dbAPI()
	if err != nil {
		return err
	}
	if _, err := api.SetEnabled(ids, enabled); err != nil {
		return err
	}
	return output.Success(map[string]any{"ids": ids, "enabled": enabled}, nil, nil)
}

var dbTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Test a connection config without saving it",
	Long: `Validates a config against the driver. Test a stored data source with --id,
or an unsaved config with --type and --config.

  infini-cli db test --id db_1
  infini-cli db test --type dm --config @dm.json
  infini-cli db test --type mysql --config '{"mysql_host":"127.0.0.1","mysql_port":3306,"mysql_username":"root","mysql_password":"...","mysql_database":"sales"}'

Field names are driver-prefixed. infini-cli db types lists every type.

` + database.ConfigGuide,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		id, _ := flags.GetString("id")
		dbType, _ := flags.GetString("type")
		configArg, _ := flags.GetString("config")

		if id != "" && (dbType != "" || configArg != "") {
			return cliexit.Usage("pass either --id, or --type with --config, not both")
		}
		if id == "" && (dbType == "" || configArg == "") {
			return cliexit.Usage("pass either --id, or --type with --config")
		}

		api, err := dbAPI()
		if err != nil {
			return err
		}

		config := ""
		if id != "" {
			current, err := api.Get(id)
			if err != nil {
				return err
			}
			dbType, config = current.Type, current.Config
		} else {
			if !slices.Contains(database.TestTypes, dbType) {
				return unknownDatabaseType(dbType)
			}
			if config, err = readConfigArg(configArg); err != nil {
				return err
			}
		}

		result, err := api.TestConnection(dbType, config)
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

var dbSchemaCmd = &cobra.Command{
	Use:   "schema <id>",
	Short: "Show a data source's tables and columns",
	Long: `Returns the live schema together with whatever the semantic layer knows about
it, which is what tells you if a source is ready for the agent to query.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := dbAPI()
		if err != nil {
			return err
		}
		data, err := api.Schema(args[0])
		if err != nil {
			return err
		}
		return output.Success(data, nil, nil)
	},
}

var dbUploadCmd = &cobra.Command{
	Use:   "upload <id> <file>",
	Short: "Upload a file into a file-type data source",
	Long: `Only data sources of --type file accept uploads. The body is streamed, so a
large file is not bounded by memory.

  infini-cli db upload db_1 ./sales-2026.csv`,
	Args: exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := dbAPI()
		if err != nil {
			return err
		}
		output.Note("uploading %s...", args[1])
		result, err := api.Upload(args[0], args[1])
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

var dbBindsCmd = &cobra.Command{
	Use:   "binds <id>",
	Short: "List the knowledge bases bound to a data source",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := dbAPI()
		if err != nil {
			return err
		}
		data, err := api.BoundRags(args[0])
		if err != nil {
			return err
		}
		return output.Success(data, nil, nil)
	},
}

var dbBindRagCmd = &cobra.Command{
	Use:   "bind-rag <id>",
	Short: "Replace the knowledge bases bound to a data source",
	Long: `The list is replaced, not merged, so pass every knowledge base you want bound.
Passing no --rag unbinds all of them.

  infini-cli db bind-rag db_1 --rag rag_1 --rag rag_2
  infini-cli db bind-rag db_1            # unbind everything`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ragIDs, _ := cmd.Flags().GetStringSlice("rag")
		if len(ragIDs) == 0 {
			if err := confirm(fmt.Sprintf("Unbind every knowledge base from data source %s?", args[0])); err != nil {
				return err
			}
		}

		api, err := dbAPI()
		if err != nil {
			return err
		}
		result, err := api.BindRags(args[0], ragIDs)
		if err != nil {
			return err
		}
		return output.Success(map[string]any{
			"databaseId": args[0],
			"ragIds":     ragIDs,
			"result":     normalizeRaw(result),
		}, nil, nil)
	},
}

var dbReviewListCmd = &cobra.Command{
	Use:   "review-list",
	Short: "List data sources whose semantic layer you can review",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := dbAPI()
		if err != nil {
			return err
		}
		data, err := api.ReviewDatabases()
		if err != nil {
			return err
		}
		return output.Success(data, nil, nil)
	},
}

// readConfigArg resolves a connection config into the JSON text the server
// stores, accepting an inline document or @file / @- like the other commands.
func readConfigArg(value string) (string, error) {
	raw, err := readJSONArg(value)
	if err != nil {
		return "", err
	}
	if len(raw) == 0 {
		return "", cliexit.Usage("--config is empty")
	}
	// Re-encode so a formatted file lands as compact JSON.
	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", cliexit.Usage("--config is not valid JSON: %v", err)
	}
	compact, err := json.Marshal(parsed)
	if err != nil {
		return "", cliexit.Usage("cannot re-encode --config: %v", err)
	}
	return string(compact), nil
}

func unknownDatabaseType(dbType string) error {
	hint := "known types: " + strings.Join(database.Types, ", ") + "; infini-cli db types lists the --config keys for each"
	if example := database.ConfigExample(dbType); example != "" {
		hint += "; example: " + example
	}
	return cliexit.Hint(
		cliexit.Usage("unknown type %q", dbType),
		"%s", hint,
	)
}

func addDatabaseSpecFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.String("name", "", "Unique data source name used in SQL")
	flags.String("nickname", "", "Display name")
	flags.String("type", "", "Driver type: "+strings.Join(database.Types, ", "))
	flags.String("description", "", "Description")
	flags.String("config", "", "Connection config as JSON, @file or @-; keys depend on --type, see db types")
}

func init() {
	lsFlags := dbLsCmd.Flags()
	lsFlags.Int("page", 1, "Page number")
	lsFlags.Int("page-size", 20, "Items per page")
	lsFlags.String("name", "", "Filter by name")
	lsFlags.String("type", "", "Filter by driver type")
	lsFlags.Bool("enabled", false, "Filter by enablement; --enabled=false lists disabled ones")
	lsFlags.String("context-hub", "", "Semantic layer coverage: in or not_in")
	lsFlags.String("source", "", "Scope: "+strings.Join(database.Sources, ", "))

	dbShowCmd.Flags().Bool("by-name", false, "Treat the argument as a name instead of an id")

	addDatabaseSpecFlags(dbAddCmd)
	dbAddCmd.Flags().Bool("disabled", false, "Create the data source disabled")
	addDatabaseSpecFlags(dbUpdateCmd)

	testFlags := dbTestCmd.Flags()
	testFlags.String("id", "", "Test a stored data source's config")
	testFlags.String("type", "", "Driver type of an unsaved config")
	testFlags.String("config", "", "Unsaved config as JSON, @file or @-")

	dbBindRagCmd.Flags().StringSlice("rag", nil, "Knowledge base id (repeatable)")

	dbCmd.AddCommand(
		dbLsCmd, dbTypesCmd, dbShowCmd, dbAddCmd, dbUpdateCmd, dbRmCmd,
		dbEnableCmd, dbDisableCmd, dbTestCmd, dbSchemaCmd, dbUploadCmd,
		dbBindsCmd, dbBindRagCmd, dbReviewListCmd,
	)
	rootCmd.AddCommand(dbCmd)
}
