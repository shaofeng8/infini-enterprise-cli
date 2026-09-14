package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/config"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/spf13/cobra"
)

var globals struct {
	profile     string
	server      string
	console     string
	apiKey      string
	token       string
	tenantCode  string
	lang        string
	jsonOutput  bool
	tableOutput bool
	assumeYes   bool
	timeout     time.Duration
	verbose     bool
	trace       bool
	dryRun      bool
	auditLog    string
}

var rootCmd = &cobra.Command{
	Use:   config.AppName,
	Short: "Enterprise CLI for InfiniSynapse",
	Long: `infini-cli drives an InfiniSynapse deployment from the terminal: dashboards,
tasks, data sources, knowledge bases, projects, the context hub, and platform
operations.

Quick start:
  infini-cli config set server https://infini.example.com
  infini-cli auth login --username alice@example.com
  infini-cli config doctor
  infini-cli dash ls

Locally, if server is unset, it is $APP_BASE_URL, or http://127.0.0.1:8088
when that is also unset. A process with BUILTIN_SYSTEM_ACCESS_KEY can skip login.

Every command prints a JSON envelope by default ({success, data, message}) and
accepts --table for human-readable list output.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if err := config.Init(globals.profile); err != nil {
			return cliexit.Wrap(cliexit.CodeUsage, err)
		}
		applyGlobals()
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

// applyGlobals turns flags into config overrides so every package resolves
// settings through one precedence chain.
func applyGlobals() {
	config.Set(config.KeyServer, globals.server)
	config.Set(config.KeyConsole, globals.console)
	config.Set(config.KeyAPIKey, globals.apiKey)
	config.Set(config.KeyToken, globals.token)
	config.Set(config.KeyTenantCode, globals.tenantCode)
	config.Set(config.KeyPreferLanguage, globals.lang)

	output.SetFormat(resolveFormat())
	client.Timeout = globals.timeout
	client.Verbose = globals.verbose
	client.Trace = globals.trace
	client.DryRun = globals.dryRun
	output.DryRun = globals.dryRun
	client.AuditLog = globals.auditLog
}

// resolveFormat honors --table > --json > configured default > json.
func resolveFormat() output.Format {
	switch {
	case globals.tableOutput:
		return output.FormatTable
	case globals.jsonOutput:
		return output.FormatJSON
	case config.DefaultOutput() == string(output.FormatTable):
		return output.FormatTable
	default:
		return output.FormatJSON
	}
}

func init() {
	pf := rootCmd.PersistentFlags()
	pf.StringVar(&globals.profile, "profile", "", "Config profile to use (env INFINI_PROFILE)")
	pf.StringVar(&globals.server, "server", "", "Infini app backend base URL, e.g. https://infini.example.com")
	pf.StringVar(&globals.console, "console", "", "Auth/proxy base URL, e.g. https://api.example.com/api")
	pf.StringVar(&globals.apiKey, "api-key", "", "API key to use instead of the stored credential")
	pf.StringVar(&globals.token, "token", "", "JWT to use instead of the stored credential")
	pf.StringVar(&globals.tenantCode, "tenant-code", "", "Tenant code for tenant-scoped deployments")
	pf.StringVar(&globals.lang, "lang", "", "Preferred language sent as x-lang (en|zh_CN|ar|ja|ko|ru)")
	pf.BoolVar(&globals.jsonOutput, "json", false, "Force JSON output (default)")
	pf.BoolVar(&globals.tableOutput, "table", false, "Force table output for list commands")
	pf.BoolVarP(&globals.assumeYes, "yes", "y", false, "Skip confirmation prompts for destructive operations")
	pf.DurationVar(&globals.timeout, "timeout", 100*time.Second, "HTTP request timeout")
	pf.BoolVar(&globals.verbose, "verbose", false, "Print request summaries to stderr")
	pf.BoolVar(&globals.trace, "trace", false, "Print full requests and responses to stderr (secrets redacted)")
	pf.BoolVar(&globals.dryRun, "dry-run", false, "Describe writes instead of sending them; reads still happen")
	pf.StringVar(&globals.auditLog, "audit-log", "", "Append one JSON line per request to this file")

	rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return cliexit.Wrap(cliexit.CodeUsage, err)
	})
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	err := rootCmd.Execute()
	if err == nil {
		return cliexit.CodeOK
	}
	output.Failure(err, cliexit.HintOf(err))
	return cliexit.CodeOf(err)
}

// exactArgs reports argument-count problems as usage errors (exit 2) rather
// than generic failures.
func exactArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != n {
			return cliexit.Usage("%q accepts exactly %d argument(s), received %d\n\nUsage:\n  %s",
				cmd.CommandPath(), n, len(args), cmd.UseLine())
		}
		return nil
	}
}

func minArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < n {
			return cliexit.Usage("%q requires at least %d argument(s), received %d\n\nUsage:\n  %s",
				cmd.CommandPath(), n, len(args), cmd.UseLine())
		}
		return nil
	}
}

// confirm gates destructive operations. A non-interactive run must pass --yes
// explicitly, so a piped script can never delete something by accident.
func confirm(prompt string) error {
	if globals.assumeYes {
		return nil
	}
	if !isTerminal(os.Stdin) {
		return cliexit.Hint(
			cliexit.New(cliexit.CodeUsage, "%s requires confirmation", prompt),
			"pass --yes to confirm in a non-interactive environment",
		)
	}
	fmt.Fprintf(os.Stderr, "%s [y/N]: ", prompt)
	var answer string
	if _, err := fmt.Fscanln(os.Stdin, &answer); err != nil {
		return cliexit.New(cliexit.CodeUsage, "aborted")
	}
	if answer != "y" && answer != "Y" && answer != "yes" {
		return cliexit.New(cliexit.CodeUsage, "aborted")
	}
	return nil
}
