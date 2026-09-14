package cmd

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/chaozwn/infini-enterprise-cli/internal/auth"
	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/config"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage settings and connection profiles",
	Long: `Settings live in ` + "`~/.infini-cli/config.yaml`" + ` as named profiles, so one
binary can target several deployments.

Precedence: flag > environment > active profile > built-in default.`,
}

var configLsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "Show the effective settings of the active profile",
	Args:    exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		showSecrets, _ := cmd.Flags().GetBool("show-secrets")
		values := config.Snapshot(showSecrets)

		return output.Success(map[string]any{
			"profile":  config.ActiveProfile(),
			"path":     config.Path(),
			"settings": values,
		}, []string{"KEY", "VALUE"}, func() [][]string {
			keys := make([]string, 0, len(values))
			for k := range values {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			rows := make([][]string, 0, len(keys))
			for _, k := range keys {
				rows = append(rows, []string{k, values[k]})
			}
			return rows
		})
	},
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Read one setting",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateKey(args[0]); err != nil {
			return err
		}
		return output.Success(map[string]string{args[0]: config.Get(args[0])}, nil, nil)
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Write one setting into the active profile",
	Args:  exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		key, value := args[0], args[1]
		if err := validateKey(key); err != nil {
			return err
		}
		if key == config.KeyPreferLanguage && !slices.Contains(config.SupportedLanguages, value) {
			return cliexit.Usage("unsupported language %q, expected one of: %s",
				value, strings.Join(config.SupportedLanguages, ", "))
		}
		if key == config.KeyDefaultOutput && value != "json" && value != "table" {
			return cliexit.Usage("unsupported output format %q, expected json or table", value)
		}
		if err := config.Save(map[string]string{key: value}); err != nil {
			return cliexit.Wrap(cliexit.CodeBusiness, err)
		}
		return output.Success(map[string]string{
			"profile": config.ActiveProfile(),
			"key":     key,
		}, nil, nil)
	},
}

var configUnsetCmd = &cobra.Command{
	Use:   "unset <key> [key...]",
	Short: "Remove settings from the active profile",
	Args:  minArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		for _, key := range args {
			if err := validateKey(key); err != nil {
				return err
			}
		}
		if err := config.Unset(args...); err != nil {
			return cliexit.Wrap(cliexit.CodeBusiness, err)
		}
		return output.Success(map[string]any{
			"profile": config.ActiveProfile(),
			"removed": args,
		}, nil, nil)
	},
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the config file location",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		return output.Success(map[string]string{"path": config.Path()}, nil, nil)
	},
}

var configProfileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Manage connection profiles",
}

var configProfileLsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List profiles",
	Args:    exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		names := config.ProfileNames()
		return output.Success(map[string]any{
			"active":   config.ActiveProfile(),
			"profiles": names,
		}, []string{"ACTIVE", "PROFILE"}, func() [][]string {
			rows := make([][]string, 0, len(names))
			for _, name := range names {
				marker := ""
				if name == config.ActiveProfile() {
					marker = "*"
				}
				rows = append(rows, []string{marker, name})
			}
			return rows
		})
	},
}

var configProfileUseCmd = &cobra.Command{
	Use:   "use <name>",
	Short: "Set the default profile for subsequent commands",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.UseProfile(args[0]); err != nil {
			return cliexit.Hint(cliexit.Wrap(cliexit.CodeUsage, err),
				"run `%s config profile ls` to see available profiles", config.AppName)
		}
		return output.Success(map[string]string{"active": args[0]}, nil, nil)
	},
}

var configProfileAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Create a profile",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		server, _ := cmd.Flags().GetString("with-server")
		console, _ := cmd.Flags().GetString("with-console")
		if err := config.AddProfile(args[0], map[string]string{
			config.KeyServer:  server,
			config.KeyConsole: console,
		}); err != nil {
			return cliexit.Wrap(cliexit.CodeUsage, err)
		}
		return output.Success(map[string]string{"profile": args[0]}, nil, nil)
	},
}

var configProfileRmCmd = &cobra.Command{
	Use:   "rm <name>",
	Short: "Delete a profile",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := confirm(fmt.Sprintf("Delete profile %q and its stored credential?", args[0])); err != nil {
			return err
		}
		if err := config.RemoveProfile(args[0]); err != nil {
			return cliexit.Wrap(cliexit.CodeUsage, err)
		}
		return output.Success(map[string]string{"removed": args[0]}, nil, nil)
	},
}

// check is one line of `config doctor` output.
type check struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok | warn | fail | skip
	Detail string `json:"detail"`
}

var configDoctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose configuration, connectivity, credential and license state",
	Long: `Runs the checks that explain almost every "it does not work" report:
config presence, server reachability, auth/proxy discovery, credential validity
and license status.`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		checks := runDoctor()

		failed := 0
		for _, c := range checks {
			if c.Status == "fail" {
				failed++
			}
		}

		if output.CurrentFormat() == output.FormatTable {
			output.Table([]string{"CHECK", "STATUS", "DETAIL"}, func() [][]string {
				rows := make([][]string, 0, len(checks))
				for _, c := range checks {
					rows = append(rows, []string{c.Name, strings.ToUpper(c.Status), c.Detail})
				}
				return rows
			}())
		} else if err := output.JSON(output.Envelope{
			Success: failed == 0,
			Data:    map[string]any{"profile": config.ActiveProfile(), "checks": checks},
		}); err != nil {
			return err
		}

		if failed > 0 {
			return cliexit.New(cliexit.CodeBusiness, "%d check(s) failed", failed)
		}
		return nil
	},
}

func runDoctor() []check {
	checks := []check{{
		Name:   "config file",
		Status: "ok",
		Detail: config.Path() + " (profile: " + config.ActiveProfile() + ")",
	}}

	server := config.Server()
	if server == "" {
		checks = append(checks, check{"server", "fail", "not configured; run `" + config.AppName + " config set server <url>`"})
		return checks
	}
	checks = append(checks, check{"server", "ok", server})

	// License status is public, which makes it the cheapest reachability probe:
	// it works before login and on a blocked deployment.
	if _, err := client.NewAnonymous(server).Get("/api/license/status", nil); err != nil {
		checks = append(checks, check{"server reachable", "fail", err.Error()})
		return checks
	}
	checks = append(checks, check{"server reachable", "ok", "GET /api/license/status responded"})

	console := config.Console()
	if console == "" {
		discovered, err := auth.DiscoverConsole(server)
		if err != nil {
			checks = append(checks, check{"auth/proxy", "warn", "not configured and discovery failed: " + err.Error()})
		} else {
			checks = append(checks, check{"auth/proxy", "warn",
				"not configured; server reports " + discovered + " (run `" + config.AppName + " auth login` to store it)"})
		}
	} else {
		checks = append(checks, check{"auth/proxy", "ok", console})
	}

	credential, kind := config.Credential()
	if credential == "" {
		checks = append(checks, check{"credential", "fail", "none stored; run `" + config.AppName + " auth login`, pass --api-key, or export INFINI_API_KEY / BUILTIN_SYSTEM_ACCESS_KEY"})
		return checks
	}
	checks = append(checks, check{"credential", "ok", kind + " " + config.Mask(credential)})

	if config.Console() != "" {
		if profile, err := auth.FetchProfile(); err != nil {
			checks = append(checks, check{"credential valid", "fail", err.Error()})
		} else {
			checks = append(checks, check{"credential valid", "ok", "user " + profile.Username + " (" + profile.ID + ")"})
		}
	} else {
		checks = append(checks, check{"credential valid", "skip", "needs the auth/proxy URL"})
	}

	c, err := client.New()
	if err != nil {
		checks = append(checks, check{"api authorized", "fail", err.Error()})
		return checks
	}
	if _, err := c.Get("/api/ai/ping", nil); err != nil {
		checks = append(checks, check{"api authorized", "fail", err.Error()})
	} else {
		checks = append(checks, check{"api authorized", "ok", "GET /api/ai/ping responded"})
	}

	if raw, err := c.Get("/api/license/limits", nil); err != nil {
		checks = append(checks, check{"license limits", "warn", err.Error()})
	} else {
		checks = append(checks, check{"license limits", "ok", summarizeLimits(raw)})
	}
	return checks
}

// summarizeLimits keeps doctor output to one line per check.
func summarizeLimits(raw json.RawMessage) string {
	var verdicts []struct {
		Kind  string `json:"kind"`
		Used  any    `json:"used"`
		Limit any    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &verdicts); err != nil || len(verdicts) == 0 {
		return "no limits reported"
	}
	parts := make([]string, 0, len(verdicts))
	for _, v := range verdicts {
		parts = append(parts, fmt.Sprintf("%s %v/%v", v.Kind, v.Used, v.Limit))
	}
	return strings.Join(parts, ", ")
}

func validateKey(key string) error {
	if slices.Contains(config.Keys, key) {
		return nil
	}
	return cliexit.Usage("unknown setting %q, expected one of: %s", key, strings.Join(config.Keys, ", "))
}

func init() {
	configLsCmd.Flags().Bool("show-secrets", false, "Print credentials in plaintext instead of masked")
	configProfileAddCmd.Flags().String("with-server", "", "Infini app backend base URL for the new profile")
	configProfileAddCmd.Flags().String("with-console", "", "Auth/proxy base URL for the new profile")

	configProfileCmd.AddCommand(configProfileLsCmd, configProfileUseCmd, configProfileAddCmd, configProfileRmCmd)
	configCmd.AddCommand(configLsCmd, configGetCmd, configSetCmd, configUnsetCmd, configPathCmd, configProfileCmd, configDoctorCmd)
	rootCmd.AddCommand(configCmd)
}
