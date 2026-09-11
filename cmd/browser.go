package cmd

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/chaozwn/infini-enterprise-cli/internal/browser"
	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/spf13/cobra"
)

func browserAPI() (*browser.API, error) {
	c, err := client.New()
	if err != nil {
		return nil, err
	}
	return browser.NewAPI(c), nil
}

var browserCmd = &cobra.Command{
	Use:   "browser",
	Short: "Drive the browser the agent uses",
	Long: `The browser is not part of this deployment. It is a Chrome extension attached
over a websocket, so these commands are relayed to whichever browser your
account currently has connected — and do nothing at all when none is.

Check first:

  infini-cli browser session

Actions sharing a session id share a browser tab, so a navigate followed by a
click acts on the same page only if both use the same --session.

This is the operator's door into the browser. To hand an in-flight agent run
its browser back or take it away, use ` + "`agent browser`" + ` instead.`,
}

var browserSessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "List every attached browser across the fleet",
	Long: `Includes sessions held by other API instances, which is why an instance can
list a session it has no socket for.`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := browserAPI()
		if err != nil {
			return err
		}
		sessions, err := api.Sessions()
		if err != nil {
			return err
		}
		return output.Success(sessions, []string{"UID", "SESSION", "INSTANCE", "CONNECTED", "USER AGENT"}, func() [][]string {
			rows := make([][]string, 0, len(sessions))
			for _, session := range sessions {
				rows = append(rows, []string{
					session.UID, session.SessionID, session.InstanceID,
					strconv.FormatBool(session.Connected), firstLine(session.UserAgent),
				})
			}
			return rows
		})
	},
}

var browserSessionCmd = &cobra.Command{
	Use:   "session [uid]",
	Short: "Show one account's browser session",
	Long: `Without an argument this is your own. The answer is null rather than an error
when no browser is attached, so null means "nothing connected", not
"lookup failed".`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		uid := ""
		if len(args) == 1 {
			uid = args[0]
		}
		api, err := browserAPI()
		if err != nil {
			return err
		}
		raw, err := api.Session(uid)
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

var browserGoCmd = &cobra.Command{
	Use:     "go <url>",
	Aliases: []string{"navigate"},
	Short:   "Navigate to a URL",
	Args:    exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendAction(cmd, "navigate", map[string]any{"url": args[0]})
	},
}

var browserClickCmd = &cobra.Command{
	Use:   "click",
	Short: "Click an element or a coordinate",
	Long: `Three ways to say where, in the order the extension resolves them:

  infini-cli browser click --selector "button.submit"
  infini-cli browser click --index 4
  infini-cli browser click --x 320 --y 180

--index refers to the numbering in ` + "`browser view`" + `, so take a view first.`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		payload := map[string]any{}
		if selector, _ := flags.GetString("selector"); selector != "" {
			payload["selector"] = selector
		}
		if flags.Changed("index") {
			index, _ := flags.GetInt("index")
			payload["index"] = index
		}
		if err := addCoordinates(cmd, payload, false); err != nil {
			return err
		}
		if clickType, _ := flags.GetString("type"); clickType != "" {
			payload["click_type"] = clickType
		}
		if len(payload) == 0 {
			return cliexit.Usage("say where to click: --selector, --index, or --x and --y")
		}
		return sendAction(cmd, "click", payload)
	},
}

var browserInputCmd = &cobra.Command{
	Use:   "input <text>",
	Short: "Type text into a field",
	Long: `  infini-cli browser input "hello" --selector "input[name=q]" --enter

The field is cleared first unless you pass --append.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		payload := map[string]any{"text": args[0]}
		if selector, _ := flags.GetString("selector"); selector != "" {
			payload["selector"] = selector
		}
		if flags.Changed("index") {
			index, _ := flags.GetInt("index")
			payload["index"] = index
		}
		if err := addCoordinates(cmd, payload, false); err != nil {
			return err
		}
		if enter, _ := flags.GetBool("enter"); enter {
			payload["press_enter"] = true
		}
		if appendText, _ := flags.GetBool("append"); appendText {
			payload["is_clear"] = false
		}
		return sendAction(cmd, "input", payload)
	},
}

var browserScrollCmd = &cobra.Command{
	Use:   "scroll <up|down|left|right>",
	Short: "Scroll the page or a container",
	Long: `  infini-cli browser scroll down
  infini-cli browser scroll down --to-end
  infini-cli browser scroll down --target container --x 400 --y 300

Scrolling a container needs coordinates inside it, since that is how the
extension finds which container you mean.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireChoice("the direction", args[0], browser.ScrollDirections); err != nil {
			return err
		}
		flags := cmd.Flags()
		payload := map[string]any{"direction": args[0]}
		if target, _ := flags.GetString("target"); target != "" {
			if err := requireChoice("--target", target, browser.ScrollTargets); err != nil {
				return err
			}
			payload["target"] = target
		}
		if toEnd, _ := flags.GetBool("to-end"); toEnd {
			payload["to_end"] = true
		}
		if err := addCoordinates(cmd, payload, false); err != nil {
			return err
		}
		return sendAction(cmd, "scroll", payload)
	},
}

var browserKeyCmd = &cobra.Command{
	Use:   "key <key>",
	Short: "Press a key",
	Long:  `  infini-cli browser key Enter`,
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendAction(cmd, "press_key", map[string]any{"key": args[0]})
	},
}

var browserFindCmd = &cobra.Command{
	Use:   "find <keyword>",
	Short: "Find a keyword on the page",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendAction(cmd, "find_keyword", map[string]any{"keyword": args[0]})
	},
}

var browserViewCmd = &cobra.Command{
	Use:   "view",
	Short: "Read the current page",
	Long: `Returns the page as the agent sees it, including the element indexes that
` + "`browser click --index`" + ` and ` + "`browser input --index`" + ` refer to.`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendAction(cmd, "view", nil)
	},
}

var browserMoveCmd = &cobra.Command{
	Use:   "move",
	Short: "Move the mouse to a coordinate",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		payload := map[string]any{}
		if err := addCoordinates(cmd, payload, true); err != nil {
			return err
		}
		return sendAction(cmd, "move_mouse", payload)
	},
}

var browserExecCmd = &cobra.Command{
	Use:   "exec <javascript>",
	Short: "Run JavaScript in the page",
	Long: `Runs in the page's own context, so it can read and change anything on it.
Accepts @file.

  infini-cli browser exec "document.title"
  infini-cli browser exec @script.js

There is no dedicated route for this action, so it only works through the
generic endpoint — which is what every command here uses.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		script := args[0]
		if strings.HasPrefix(script, "@") {
			resolved, err := readTextFile(strings.TrimPrefix(script, "@"))
			if err != nil {
				return err
			}
			script = resolved
		}
		return sendAction(cmd, "browser_console_exec", map[string]any{"javascript": script})
	},
}

var browserConsoleCmd = &cobra.Command{
	Use:   "console",
	Short: "Read the page's console output",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		payload := map[string]any{}
		if lines, _ := cmd.Flags().GetInt("lines"); lines > 0 {
			payload["max_lines"] = lines
		}
		return sendAction(cmd, "browser_console_view", payload)
	},
}

var browserRawCmd = &cobra.Command{
	Use:   "raw <action-type> [payload]",
	Short: "Send any action, with the payload as JSON",
	Long: `The escape hatch for an action this CLI has no command for yet. The payload
is inline JSON, @file, or @- for stdin.

  infini-cli browser raw click '{"selector":"button"}'
  infini-cli browser raw navigate @payload.json`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := browser.CheckType(args[0]); err != nil {
			return err
		}
		var payload json.RawMessage
		if len(args) == 2 {
			parsed, err := readJSONArg(args[1])
			if err != nil {
				return err
			}
			payload = parsed
		}

		api, err := browserAPI()
		if err != nil {
			return err
		}
		action, err := browserAction(cmd, args[0])
		if err != nil {
			return err
		}
		action.Payload = payload
		result, err := api.Send(*action)
		return reportBrowserResult(result, err)
	},
}

var browserDirectCmd = &cobra.Command{
	Use:   "direct <action> [payload]",
	Short: "Call a dedicated per-action route instead of the generic one",
	Long: `The server also exposes a route per action — /action/navigate, /action/click
and so on. They predate the generic endpoint, always act on the default
session, and ignore any timeout, so they are diagnostic rather than useful.
This command exists so those routes can be exercised.

  infini-cli browser direct navigate '{"url":"https://example.com"}'

For real work use the ordinary commands, which honour --session and
--timeout.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		var payload any
		if len(args) == 2 {
			parsed, err := readJSONArg(args[1])
			if err != nil {
				return err
			}
			payload = parsed
		}
		api, err := browserAPI()
		if err != nil {
			return err
		}
		result, err := api.Direct(args[0], payload)
		return reportBrowserResult(result, err)
	},
}

// addCoordinates collects --x and --y, which the extension only accepts as a
// pair: one without the other would aim at an axis rather than a point.
func addCoordinates(cmd *cobra.Command, payload map[string]any, required bool) error {
	flags := cmd.Flags()
	hasX, hasY := flags.Changed("x"), flags.Changed("y")
	if hasX != hasY {
		return cliexit.Usage("--x and --y must be given together")
	}
	if !hasX {
		if required {
			return cliexit.Usage("--x and --y are required")
		}
		return nil
	}
	x, _ := flags.GetInt("x")
	y, _ := flags.GetInt("y")
	payload["coordinate_x"] = x
	payload["coordinate_y"] = y
	return nil
}

func browserAction(cmd *cobra.Command, actionType string) (*browser.Action, error) {
	flags := cmd.Flags()
	action := &browser.Action{Type: actionType}
	if session, _ := flags.GetString("session"); session != "" {
		action.SessionID = session
	}
	if flags.Changed("timeout") {
		timeout, _ := flags.GetInt("timeout")
		if timeout < 100 || timeout > browser.MaxTimeoutMs {
			return nil, cliexit.Usage(
				"--timeout must be between 100 and %d milliseconds; the default can only be shortened",
				browser.MaxTimeoutMs)
		}
		action.TimeoutMs = timeout
	}
	return action, nil
}

func sendAction(cmd *cobra.Command, actionType string, payload map[string]any) error {
	action, err := browserAction(cmd, actionType)
	if err != nil {
		return err
	}
	if len(payload) > 0 {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return cliexit.New(cliexit.CodeUsage, "cannot serialize the action: %v", err)
		}
		action.Payload = encoded
	}

	api, err := browserAPI()
	if err != nil {
		return err
	}
	result, err := api.Send(*action)
	return reportBrowserResult(result, err)
}

// reportBrowserResult prints the result even on a refusal, because the body
// carries the reason and the exit code alone would not.
func reportBrowserResult(result *browser.Result, err error) error {
	if result != nil {
		if printErr := output.Success(result, nil, nil); printErr != nil {
			return printErr
		}
	}
	return err
}

// addActionFlags gives every action command the session and timeout controls
// the generic endpoint accepts.
func addActionFlags(cmds ...*cobra.Command) {
	for _, cmd := range cmds {
		cmd.Flags().String("session", "", "Session id; actions sharing one share a browser tab")
		cmd.Flags().Int("timeout", 0, "Timeout in milliseconds (100–60000, shorter than the default only)")
	}
}

// addPointFlags gives the pointer-driven commands their coordinates.
func addPointFlags(cmds ...*cobra.Command) {
	for _, cmd := range cmds {
		cmd.Flags().Int("x", 0, "X coordinate")
		cmd.Flags().Int("y", 0, "Y coordinate")
	}
}

func init() {
	browserClickCmd.Flags().String("selector", "", "CSS selector")
	browserClickCmd.Flags().Int("index", 0, "Element index from `browser view`")
	browserClickCmd.Flags().String("type", "", "Click type, e.g. click or dblclick")

	browserInputCmd.Flags().String("selector", "", "CSS selector")
	browserInputCmd.Flags().Int("index", 0, "Element index from `browser view`")
	browserInputCmd.Flags().Bool("enter", false, "Press Enter afterwards")
	browserInputCmd.Flags().Bool("append", false, "Keep the field's current value")

	browserScrollCmd.Flags().String("target", "", "What to scroll: "+strings.Join(browser.ScrollTargets, " or "))
	browserScrollCmd.Flags().Bool("to-end", false, "Scroll all the way")

	browserConsoleCmd.Flags().Int("lines", 0, "Cap the number of lines returned")

	addPointFlags(browserClickCmd, browserInputCmd, browserScrollCmd, browserMoveCmd)
	addActionFlags(
		browserGoCmd, browserClickCmd, browserInputCmd, browserScrollCmd,
		browserKeyCmd, browserFindCmd, browserViewCmd, browserMoveCmd,
		browserExecCmd, browserConsoleCmd, browserRawCmd,
	)

	browserCmd.AddCommand(
		browserSessionsCmd, browserSessionCmd, browserGoCmd, browserClickCmd,
		browserInputCmd, browserScrollCmd, browserKeyCmd, browserFindCmd,
		browserViewCmd, browserMoveCmd, browserExecCmd, browserConsoleCmd,
		browserRawCmd, browserDirectCmd,
	)
	rootCmd.AddCommand(browserCmd)
}
