package cmd

import (
	"net/http"
	"slices"
	"strings"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/spf13/cobra"
)

var methods = []string{
	http.MethodGet, http.MethodPost, http.MethodPut,
	http.MethodPatch, http.MethodDelete, http.MethodHead,
}

// apiCmd is the escape hatch that makes "the CLI can do everything" true even
// before an endpoint gets its own subcommand.
var apiCmd = &cobra.Command{
	Use:   "api <METHOD> <path>",
	Short: "Call any Infini endpoint directly with the active credential",
	Long: `Sends an authenticated request to an arbitrary path. Useful for endpoints that
do not have a dedicated subcommand yet, and for debugging.

Examples:
  infini-cli api GET /api/ai/dashboards
  infini-cli api GET /api/ai_task/list --query page=1 --query pageSize=20
  infini-cli api POST /api/ai/dashboards/abc/query --data @payload.json
  infini-cli api GET /user/getJwtProfile --proxy

Payloads accept inline JSON, @file, or @- for stdin.`,
	Args: exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		method := strings.ToUpper(args[0])
		if !slices.Contains(methods, method) {
			return cliexit.Usage("unsupported method %q, expected one of: %s", args[0], strings.Join(methods, ", "))
		}

		path := args[1]
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}

		data, _ := cmd.Flags().GetString("data")
		queryPairs, _ := cmd.Flags().GetStringArray("query")
		useConsole, _ := cmd.Flags().GetBool("proxy")
		raw, _ := cmd.Flags().GetBool("raw")

		body, err := readJSONArg(data)
		if err != nil {
			return err
		}
		params, err := parseKeyValues(queryPairs, "query")
		if err != nil {
			return err
		}

		c, err := targetClient(useConsole)
		if err != nil {
			return err
		}
		path = client.WithQuery(path, params)

		// --raw skips envelope unwrapping and error mapping, which is what you
		// want when inspecting a non-conforming or @Bypass route.
		if raw {
			status, payload, err := c.RawResponse(method, path, body)
			if err != nil {
				return err
			}
			output.Note("HTTP %d", status)
			return output.Raw(payload)
		}

		payload, err := c.Do(method, path, body)
		if err != nil {
			return err
		}
		return output.Success(payload, nil, nil)
	},
}

func targetClient(useConsole bool) (*client.Client, error) {
	if useConsole {
		return client.NewConsole()
	}
	return client.New()
}

var apiEndpointsCmd = &cobra.Command{
	Use:   "endpoints",
	Short: "List the known Infini endpoint groups",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		groups := []map[string]string{
			{"group": "dashboards", "prefix": "/api/ai/dashboards", "notes": "list, spec, revisions, layout, query, refresh, filters, ask"},
			{"group": "tasks", "prefix": "/api/ai_task", "notes": "list, detail, cancel, workspace, share, notebook graph"},
			{"group": "agent", "prefix": "/api/ai", "notes": "message queue, state, settings, models, SSE events"},
			{"group": "databases", "prefix": "/api/ai_database", "notes": "CRUD, test connection, schema, rag binding"},
			{"group": "rag", "prefix": "/api/ai_rag_sdk", "notes": "CRUD, files, database binding"},
			{"group": "projects", "prefix": "/api/ai_project", "notes": "CRUD, members, file tree"},
			{"group": "context hub", "prefix": "/api/ai/context-hub", "notes": "memory build, tables, columns, playbooks, KPIs, review"},
			{"group": "skills", "prefix": "/api/ai_skill", "notes": "market, install, toggle, local edit"},
			{"group": "tools", "prefix": "/api/ai_tool", "notes": "market, install, toggle, local edit"},
			{"group": "rules", "prefix": "/api/ai_rule", "notes": "CRUD, enable, lookup"},
			{"group": "templates", "prefix": "/api/ai_template", "notes": "CRUD"},
			{"group": "settings", "prefix": "/api/ai_setting", "notes": "language, keys, engine config, model info"},
			{"group": "scheduler", "prefix": "/api/ai_scheduler", "notes": "CRUD, pause, resume, run now, runs"},
			{"group": "engine", "prefix": "/api/infinity-sql", "notes": "status, start, stop, logs"},
			{"group": "byzer", "prefix": "/api/ai_byzer", "notes": "engine availability"},
			{"group": "runtime", "prefix": "/api/runtime", "notes": "instances, execution, drain, autoscaling"},
			{"group": "license", "prefix": "/api/license", "notes": "status, refresh, limits"},
			{"group": "browser", "prefix": "/api/ai_browser", "notes": "sessions and actions"},
			{"group": "storage", "prefix": "/api/storage", "notes": "download, delete"},
			{"group": "upload", "prefix": "/api", "notes": "directories, fileTree, upload, taskUpload"},
			{"group": "chunked upload", "prefix": "/api/file_upload", "notes": "init, status, chunks, complete, abort"},
			{"group": "auth", "prefix": "/api/auth", "notes": "getAuthingPath, getBrand"},
		}
		return output.Success(groups, []string{"GROUP", "PREFIX", "NOTES"}, func() [][]string {
			rows := make([][]string, 0, len(groups))
			for _, g := range groups {
				rows = append(rows, []string{g["group"], g["prefix"], g["notes"]})
			}
			return rows
		})
	},
}

func init() {
	apiCmd.Flags().String("data", "", "JSON request body: inline, @file, or @- for stdin")
	apiCmd.Flags().StringArray("query", nil, "Query parameter as key=value (repeatable)")
	apiCmd.Flags().Bool("proxy", false, "Target the auth/proxy service instead of the app backend")
	apiCmd.Flags().Bool("raw", false, "Print the untouched response without unwrapping the envelope")

	apiCmd.AddCommand(apiEndpointsCmd)
	rootCmd.AddCommand(apiCmd)
}
