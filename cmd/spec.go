package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/chaozwn/infini-enterprise-cli/internal/config"
	"github.com/chaozwn/infini-enterprise-cli/internal/database"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// The command inventory is walked out of the cobra tree rather than written
// by hand. A hand-maintained list of 200-odd commands drifts from the binary
// within a release or two, and a spec that lies is worse than no spec.
//
// The prose around it is written by hand, because the parts an agent gets
// wrong are not the flag names — they are the conventions: that agent work is
// queued rather than called, that an omitted resource list means something
// different from an empty one, and that secrets go through the environment.

const specConventions = `================================================================================
Conventions that are not obvious from --help
================================================================================

Output. Every command prints one JSON envelope on stdout:

  {"success": true,  "data": ...,  "message": ""}
  {"success": false, "data": null, "message": "...", "hint": "..."}

Diagnostics, progress and warnings go to stderr, never stdout, so stdout is
always parseable. List commands accept --table for human output; do not use it
when parsing.

Exit codes carry the outcome, so branch on them rather than on the text:

  0  success
  1  business error (the server refused, or the answer was empty)
  2  usage error (bad arguments; nothing was sent)
  3  authentication failed
  4  network or service unreachable
  5  blocked by a license limit

Agent work is queued, not called. "agent new", "agent reply", "dash new" and
friends hand a command to a worker and then follow the result over SSE. They
do not return the agent's answer synchronously. Two consequences:

  - a command can succeed while the run later fails; check the streamed
    result, not just the exit code
  - only killActiveJobs runs synchronously, because it needs the caller's own
    credentials

Resource lists have three states, not two. For --database, --rag and --project
on agent and task commands:

  - flag omitted     leave the group as it is
  - --database ""    clear the group
  - --database x,y   replace the group with x and y

So "agent reply" without --database does not detach the task's data sources.

Secrets never travel as flags, because a flag lands in shell history and in
the process list. These come from the environment instead:

  INFINI_MODEL_API_KEY    model provider key for agent settings
  INFINI_STORAGE_SECRET   object storage secret for data source uploads
  INFINI_INTERNAL_TOKEN   worker drain token
  INFINI_TOKEN            JWT, as an alternative to a stored credential
  INFINI_API_KEY          API key, same
  BUILTIN_SYSTEM_ACCESS_KEY
                        Infini's process-level system api-key. Last resort
                        after INFINI_API_KEY, so a local CLI can skip login.

Destructive commands confirm first. In a non-interactive run they refuse
unless --yes is passed, so a script cannot delete something by accident.

--dry-run suppresses writes but not reads. Use it to find out what a command
would send. The envelope carries "dryRun": true, and data is the empty result
type rather than a real record, so do not read fields out of it.

--audit-log <path> appends one JSON line per request: {ts, method, path,
status}. If the file cannot be written the command fails instead of sending
the request.

Long text takes @file. --prompt, --value, --text and similar flags read a
file when the value starts with @, and stdin when it is @-.

Anything not wrapped as a command is still reachable:

  infini-cli api GET /api/some/new/endpoint
  infini-cli api endpoints

================================================================================
Setup
================================================================================

  infini-cli config set server https://infini.example.com
  infini-cli config set console https://api.example.com/api
  infini-cli auth login --username you@example.com        # prompts, no echo
  infini-cli config doctor                                # verify the chain

If server is not set, it defaults to $APP_BASE_URL, or
http://127.0.0.1:8088 when APP_BASE_URL is also unset.

Credentials can also come from the environment (INFINI_TOKEN, INFINI_API_KEY,
BUILTIN_SYSTEM_ACCESS_KEY) or from --token / --api-key. A password is
never accepted as a flag; use --password-stdin.

Several deployments at once are handled with profiles:

  infini-cli config profile add staging
  infini-cli --profile staging dash ls

================================================================================
Typical sequences
================================================================================

Ask a question and follow the answer:

  infini-cli db ls --table                       # find the data source
  infini-cli agent new "上季度各区域 GMV 对比" --database db_sales
  # the command streams the run and prints the task id
  infini-cli agent reply <taskId> "再按月拆开"

When the agent asks for approval:

  infini-cli agent reply <taskId> --approve
  infini-cli agent reply <taskId> --deny
  infini-cli agent options <taskId> 2             # pick an offered option

Build a dashboard, then keep it running:

  infini-cli dash new "销售看板" --database db_sales
  infini-cli dash export <dashId> --out ./dash.yaml
  # edit, then
  infini-cli dash apply <dashId> --file ./dash.yaml
  infini-cli dash refresh <dashId> --wait

Run something every morning:

  infini-cli schedule create "每日简报" --prompt "汇总昨日销售" \
      --cron "0 30 9 * * ?" --database db_sales
  infini-cli schedule runs <scheduleId> --table

Load a large file into a data source, resumably:

  infini-cli fs session push ./dump.csv \
      --target-type database --target-id db_1 --post-action import_database
  infini-cli fs session push ./dump.csv --resume <uploadId>   # after a break

Data source --config keys are driver-prefixed. infini-cli db add --help is
the catalog; do not invent host/port/path. sqlite uses sqlite_path, Dameng
(dm) uses dm_host/dm_port/dm_username/dm_password/dm_database (default port
5236). Always db test before db add.

================================================================================
Things that will otherwise waste your time
================================================================================

Check someone else's shared result:

  infini-cli task public show <taskId>
  infini-cli task public evidence <taskId> <messageTs>...

================================================================================
Things that will otherwise waste your time
================================================================================

  - Schedule cron is six-field Quartz (second first): "0 30 9 * * ?" is 09:30
    daily. The five-field Unix form is rejected.
  - "agent mode" cannot switch a task into or out of graph or fast mode; those
    are chosen at creation.
  - "agent auto-approve" overwrites the whole settings object server-side, so
    the CLI reads before writing. Do not hand-roll it through "api".
  - Rollback points are derived from the conversation, not listed by an
    endpoint; "agent snapshots" is where they come from.
  - "browser" commands report a disconnected browser as a business error even
    though the server answers HTTP 200. Check "browser session" first.
  - "engine stop" stops the engine for everyone on the deployment.
  - An empty model list from "agent models" is ambiguous: that endpoint
    swallows its own failures.
`

var specCmd = &cobra.Command{
	Use:     "spec",
	Aliases: []string{"skill-spec"},
	Short:   "Print the full command specification, for AI agents",
	Long: `Prints everything an agent needs to drive this CLI: the whole command
inventory, the output and exit-code contract, and the conventions that are not
visible in --help.

The inventory is generated from the binary's own command tree, so it cannot
drift from what the binary actually accepts.

Note that ` + "`skill`" + ` is a different thing here than in agent_infini: it
manages the agent's skills. This command is the specification.`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		var out strings.Builder
		fmt.Fprintf(&out, "# %s — InfiniSynapse Enterprise CLI Specification\n\n", config.AppName)
		fmt.Fprintf(&out, "Version %s (commit %s)\n\n", Version, Commit)
		out.WriteString(`Drives an InfiniSynapse deployment from the terminal: dashboards, tasks, data
sources, knowledge bases, projects, the semantic layer, the agent itself, and
platform operations.

`)
		out.WriteString(specConventions)
		out.WriteString("\n")
		out.WriteString("================================================================================\n")
		out.WriteString("Command inventory\n")
		out.WriteString("================================================================================\n\n")
		writeCommands(&out, rootCmd)

		out.WriteString("================================================================================\n")
		out.WriteString("Data source --config catalog\n")
		out.WriteString("================================================================================\n\n")
		out.WriteString(database.ConfigGuide)
		out.WriteString("\n")

		_, err := os.Stdout.WriteString(out.String())
		return err
	},
}

// writeCommands prints every runnable command, grouped under its top-level
// command, with the flags each one takes.
func writeCommands(out *strings.Builder, root *cobra.Command) {
	for _, group := range root.Commands() {
		if group.Hidden || group.Name() == "help" || group.Name() == "completion" {
			continue
		}
		fmt.Fprintf(out, "%s — %s\n", group.Name(), group.Short)
		leaves := collectLeaves(group)
		for _, leaf := range leaves {
			fmt.Fprintf(out, "  %-52s %s\n", leafUsage(leaf), leaf.Short)
			if flags := leafFlags(leaf); flags != "" {
				fmt.Fprintf(out, "  %-52s   flags: %s\n", "", flags)
			}
		}
		out.WriteString("\n")
	}
}

// collectLeaves walks a command subtree down to the commands that actually
// run, since the intermediate groups carry no usage of their own.
func collectLeaves(cmd *cobra.Command) []*cobra.Command {
	var leaves []*cobra.Command
	if cmd.Runnable() && !cmd.HasSubCommands() {
		return []*cobra.Command{cmd}
	}
	if cmd.Runnable() && cmd.RunE != nil && cmd.Name() != cmd.Root().Name() {
		// A command that both runs and has children, such as `hub memory`.
		leaves = append(leaves, cmd)
	}
	for _, child := range cmd.Commands() {
		if child.Hidden || child.Name() == "help" {
			continue
		}
		leaves = append(leaves, collectLeaves(child)...)
	}
	return leaves
}

func leafUsage(cmd *cobra.Command) string {
	path := strings.Fields(cmd.CommandPath())
	// Drop the binary name; the reader knows what they typed.
	if len(path) > 1 {
		path = path[1:]
	}
	usage := strings.Join(path, " ")
	// Use carries the argument placeholders after the command's own name.
	if fields := strings.Fields(cmd.Use); len(fields) > 1 {
		usage += " " + strings.Join(fields[1:], " ")
	}
	return usage
}

func leafFlags(cmd *cobra.Command) string {
	var names []string
	cmd.LocalFlags().VisitAll(func(flag *pflag.Flag) {
		if flag.Hidden {
			return
		}
		names = append(names, "--"+flag.Name)
	})
	return strings.Join(names, " ")
}

func init() {
	rootCmd.AddCommand(specCmd)
}
