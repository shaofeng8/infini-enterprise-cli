package cmd

import (
	"slices"
	"strings"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/config"
	"github.com/chaozwn/infini-enterprise-cli/internal/database"
	"github.com/chaozwn/infini-enterprise-cli/internal/hub"
	"github.com/spf13/cobra"
)

var hubCmd = &cobra.Command{
	Use:   "hub",
	Short: "Manage the context hub (semantic layer)",
	Long: `The context hub is what the agent reads before it writes SQL: table and column
descriptions, analysis playbooks, KPI definitions and presentation
preferences. A data source with an empty hub gets guesswork; a described one
gets answers you can defend.

Memory build is the fast way to fill it: it reads a data source's schema and
sample data and proposes descriptions for review.

Editing a data source you do not own produces a draft rather than a change, so
` + "`hub review`" + ` is part of normal editing, not an admin afterthought.`,
}

func hubAPI() (*hub.API, error) {
	c, err := client.New()
	if err != nil {
		return nil, err
	}
	return hub.NewAPI(c), nil
}

// tableRefResolver turns `<data source name>.<table>` into the three fields the
// server's DTO requires, caching each name lookup.
type tableRefResolver struct {
	api   *database.API
	cache map[string]*database.Item
}

func newTableRefResolver() (*tableRefResolver, error) {
	api, err := dbAPI()
	if err != nil {
		return nil, err
	}
	return &tableRefResolver{api: api, cache: map[string]*database.Item{}}, nil
}

func (r *tableRefResolver) resolve(values []string) ([]hub.TableRef, error) {
	refs := make([]hub.TableRef, 0, len(values))
	for _, value := range values {
		name, table, found := strings.Cut(value, ".")
		if !found || name == "" || table == "" {
			return nil, cliexit.Usage("--table expects <data source>.<table>, received %q", value)
		}

		item, cached := r.cache[name]
		if !cached {
			resolved, err := r.api.GetByName(name)
			if err != nil {
				return nil, err
			}
			if resolved == nil || resolved.ID == "" {
				return nil, cliexit.Usage("no data source is named %q", name)
			}
			item = resolved
			r.cache[name] = item
		}

		refs = append(refs, hub.TableRef{
			DatabaseID:   item.ID,
			DatabaseName: item.Name,
			TableName:    table,
		})
	}
	return refs, nil
}

// resolveDatabaseID accepts either an id or a name, so the hub commands compose
// with `db ls` output either way.
func resolveDatabaseID(value string) (*database.Item, error) {
	api, err := dbAPI()
	if err != nil {
		return nil, err
	}
	if item, err := api.Get(value); err == nil && item != nil && item.ID != "" {
		return item, nil
	}
	item, err := api.GetByName(value)
	if err != nil {
		return nil, cliexit.Hint(err, "pass a data source id or name; `%s db ls --table` lists both", config.AppName)
	}
	if item == nil || item.ID == "" {
		return nil, cliexit.Usage("no data source matches %q by id or name", value)
	}
	return item, nil
}

func addHubPageFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.Int("page", 1, "Page number")
	flags.Int("page-size", 20, "Items per page")
	flags.String("search", "", "Fuzzy search across the entity's text fields")
	flags.String("database", "", "Filter by data source id")
	flags.String("table-id", "", "Filter by table definition id")
	flags.Bool("only-mine", false, "Only entities you created")
}

func hubPageQuery(cmd *cobra.Command) hub.PageQuery {
	flags := cmd.Flags()
	page, _ := flags.GetInt("page")
	pageSize, _ := flags.GetInt("page-size")
	search, _ := flags.GetString("search")
	databaseID, _ := flags.GetString("database")
	tableID, _ := flags.GetString("table-id")
	onlyMine, _ := flags.GetBool("only-mine")

	return hub.PageQuery{
		Page:       page,
		PageSize:   pageSize,
		Search:     search,
		DatabaseID: databaseID,
		TableID:    tableID,
		OnlyMine:   onlyMine,
	}
}

func requireEntityKind(value string) (hub.Kind, error) {
	if !slices.Contains(hub.EntityTypes, value) {
		return "", cliexit.Usage("unknown entity type %q, expected one of: %s",
			value, strings.Join(hub.EntityTypes, ", "))
	}
	return hub.Kind(value), nil
}

func init() {
	rootCmd.AddCommand(hubCmd)
}
