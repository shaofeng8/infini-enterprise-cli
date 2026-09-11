package cmd

import (
	"slices"
	"strconv"
	"strings"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/extension"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/spf13/cobra"
)

var ruleCmd = &cobra.Command{
	Use:   "rule",
	Short: "Manage the agent's standing instructions",
	Long: `A rule is text prepended to the agent's context on every run — a house style,
a naming convention, a constraint on how to query something.

Scope is either global or tied to specific data sources, so a rule about one
warehouse's quirks does not follow the agent everywhere.

` + "`rule enabled`" + ` shows what is actually in effect, which is the answer worth
trusting: it is what the agent itself receives.`,
}

var ruleListCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List rules",
	Long: `  infini-cli rule ls --table
  infini-cli rule ls --type database --on --table`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		query, err := ruleQuery(cmd)
		if err != nil {
			return err
		}
		api, err := extensionAPI()
		if err != nil {
			return err
		}
		page, err := api.Rules(*query)
		if err != nil {
			return err
		}
		return output.Success(page, []string{"ID", "NAME", "TYPE", "ENABLED", "VALUE"}, func() [][]string {
			return ruleRows(page.Items)
		})
	},
}

func ruleQuery(cmd *cobra.Command) (*extension.RuleQuery, error) {
	flags := cmd.Flags()
	page, _ := flags.GetInt("page")
	pageSize, _ := flags.GetInt("page-size")
	name, _ := flags.GetString("search")
	ruleType, _ := flags.GetString("type")

	if ruleType != "" && !slices.Contains(extension.RuleTypes, ruleType) {
		return nil, cliexit.Usage("unknown rule type %q, expected one of: %s",
			ruleType, strings.Join(extension.RuleTypes, ", "))
	}

	query := &extension.RuleQuery{Page: page, PageSize: pageSize, Name: name, RuleType: ruleType}
	on, _ := flags.GetBool("on")
	off, _ := flags.GetBool("off")
	switch {
	case on && off:
		return nil, cliexit.Usage("--on and --off are mutually exclusive")
	case on:
		enabled := 1
		query.Enabled = &enabled
	case off:
		enabled := 0
		query.Enabled = &enabled
	}
	return query, nil
}

func ruleRows(rules []extension.Rule) [][]string {
	rows := make([][]string, 0, len(rules))
	for _, rule := range rules {
		scope := rule.RuleType
		if rule.RuleType == "database" && len(rule.DatabaseIDs) > 0 {
			scope += " (" + strconv.Itoa(len(rule.DatabaseIDs)) + ")"
		}
		rows = append(rows, []string{
			rule.ID, rule.Name, scope, strconv.Itoa(rule.Enabled), firstLine(rule.Value),
		})
	}
	return rows
}

var ruleEnabledCmd = &cobra.Command{
	Use:   "enabled",
	Short: "List the rules currently in effect",
	Long:  `This is what the agent receives, rather than what the listing implies.`,
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := extensionAPI()
		if err != nil {
			return err
		}
		rules, err := api.EnabledRules()
		if err != nil {
			return err
		}
		return output.Success(rules, []string{"ID", "NAME", "TYPE", "ENABLED", "VALUE"}, func() [][]string {
			return ruleRows(rules)
		})
	},
}

var ruleAllCmd = &cobra.Command{
	Use:   "all",
	Short: "List every rule, unpaginated",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := extensionAPI()
		if err != nil {
			return err
		}
		rules, err := api.AllRules()
		if err != nil {
			return err
		}
		return output.Success(rules, []string{"ID", "NAME", "TYPE", "ENABLED", "VALUE"}, func() [][]string {
			return ruleRows(rules)
		})
	},
}

var ruleShowCmd = &cobra.Command{
	Use:   "show <id|name>",
	Short: "Show one rule",
	Long: `Looks the argument up as an id first, then as a name, so either works
without a flag.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := extensionAPI()
		if err != nil {
			return err
		}
		rule, err := api.Rule(args[0])
		if err != nil || rule == nil || rule.ID == "" {
			byName, nameErr := api.RuleByName(args[0])
			if nameErr == nil && byName != nil && byName.ID != "" {
				return output.Success(byName, nil, nil)
			}
			if err != nil {
				return err
			}
			return cliexit.New(cliexit.CodeBusiness, "no rule with id or name %q", args[0])
		}
		return output.Success(rule, nil, nil)
	},
}

var ruleDatabasesCmd = &cobra.Command{
	Use:   "databases",
	Short: "List the data sources a database-scoped rule can bind to",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := extensionAPI()
		if err != nil {
			return err
		}
		raw, err := api.RuleDatabases()
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

var ruleAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Add a rule",
	Long: `  infini-cli rule add "时间范围" --value "查询必须带时间范围限制"
  infini-cli rule add "chinook 口径" --value @rule.md --type database --database db_1

--value accepts @file, since a rule worth writing is usually longer than a
shell argument.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := ruleBody(cmd)
		if err != nil {
			return err
		}
		body["name"] = args[0]
		if _, present := body["value"]; !present {
			return cliexit.Usage("--value is required")
		}

		api, err := extensionAPI()
		if err != nil {
			return err
		}
		raw, err := api.AddRule(body)
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

var ruleUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a rule",
	Long: `The server's update DTO requires the name whether or not it changes, so the
current rule is read first and its name carried over unless --name is given.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := ruleBody(cmd)
		if err != nil {
			return err
		}
		if len(body) == 0 {
			return cliexit.Usage("nothing to change; pass --name, --value, --type, --database, --on or --off")
		}

		api, err := extensionAPI()
		if err != nil {
			return err
		}
		current, err := api.Rule(args[0])
		if err != nil {
			return err
		}
		if current == nil || current.ID == "" {
			return cliexit.New(cliexit.CodeBusiness, "no rule with id %q", args[0])
		}

		body["id"] = args[0]
		if _, present := body["name"]; !present {
			body["name"] = current.Name
		}

		raw, err := api.UpdateRule(body)
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

func ruleBody(cmd *cobra.Command) (map[string]any, error) {
	flags := cmd.Flags()
	body := map[string]any{}

	if flags.Changed("name") {
		name, _ := flags.GetString("name")
		body["name"] = name
	}
	if flags.Changed("value") {
		value, _ := flags.GetString("value")
		if strings.HasPrefix(value, "@") {
			resolved, err := readTextFile(strings.TrimPrefix(value, "@"))
			if err != nil {
				return nil, err
			}
			value = resolved
		}
		body["value"] = value
	}
	if flags.Changed("type") {
		ruleType, _ := flags.GetString("type")
		if !slices.Contains(extension.RuleTypes, ruleType) {
			return nil, cliexit.Usage("unknown rule type %q, expected one of: %s",
				ruleType, strings.Join(extension.RuleTypes, ", "))
		}
		body["rule_type"] = ruleType
	}
	if flags.Changed("database") {
		databases, _ := flags.GetStringSlice("database")
		body["databaseIds"] = databases
		// A database-scoped rule is the only kind that reads databaseIds, so
		// binding data sources without saying so would silently do nothing.
		if _, present := body["rule_type"]; !present {
			body["rule_type"] = "database"
		}
	}

	on, _ := flags.GetBool("on")
	off, _ := flags.GetBool("off")
	switch {
	case on && off:
		return nil, cliexit.Usage("--on and --off are mutually exclusive")
	case on:
		body["enabled"] = 1
	case off:
		body["enabled"] = 0
	}
	return body, nil
}

var ruleToggleCmd = &cobra.Command{
	Use:   "toggle <id>...",
	Short: "Enable or disable rules in bulk",
	Long: `  infini-cli rule toggle 1 2 3 --on
  infini-cli rule toggle 4 --off

This endpoint takes numeric ids, unlike ` + "`rule rm`" + ` which takes strings, so an
id that is not a number is rejected here.`,
	Args: minArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		on, _ := flags.GetBool("on")
		off, _ := flags.GetBool("off")
		if on == off {
			return cliexit.Usage("pass either --on or --off")
		}

		ids := make([]int, 0, len(args))
		for _, arg := range args {
			id, err := strconv.Atoi(arg)
			if err != nil {
				return cliexit.Usage("this endpoint takes numeric rule ids, received %q", arg)
			}
			ids = append(ids, id)
		}

		enabled := 0
		if on {
			enabled = 1
		}

		api, err := extensionAPI()
		if err != nil {
			return err
		}
		raw, err := api.SetRulesEnabled(ids, enabled)
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

var ruleRemoveCmd = &cobra.Command{
	Use:     "rm <id>...",
	Aliases: []string{"delete"},
	Short:   "Delete rules",
	Args:    minArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := confirm("Delete " + strconv.Itoa(len(args)) + " rule(s)?"); err != nil {
			return err
		}
		api, err := extensionAPI()
		if err != nil {
			return err
		}
		raw, err := api.DeleteRules(args)
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

func addRuleWriteFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.String("name", "", "Rule name")
	flags.String("value", "", "Rule text; @file to read from a file")
	flags.String("type", "", "Scope: "+strings.Join(extension.RuleTypes, " or "))
	flags.StringSlice("database", nil, "Data source ids to bind (implies --type database)")
	flags.Bool("on", false, "Enable the rule")
	flags.Bool("off", false, "Disable the rule")
}

func init() {
	ruleListCmd.Flags().Int("page", 1, "Page number")
	ruleListCmd.Flags().Int("page-size", 10, "Items per page")
	ruleListCmd.Flags().String("search", "", "Filter by name")
	ruleListCmd.Flags().String("type", "", "Filter by scope: "+strings.Join(extension.RuleTypes, " or "))
	ruleListCmd.Flags().Bool("on", false, "Only enabled rules")
	ruleListCmd.Flags().Bool("off", false, "Only disabled rules")

	addRuleWriteFlags(ruleAddCmd)
	addRuleWriteFlags(ruleUpdateCmd)

	ruleToggleCmd.Flags().Bool("on", false, "Enable")
	ruleToggleCmd.Flags().Bool("off", false, "Disable")

	ruleCmd.AddCommand(
		ruleListCmd, ruleEnabledCmd, ruleAllCmd, ruleShowCmd, ruleDatabasesCmd,
		ruleAddCmd, ruleUpdateCmd, ruleToggleCmd, ruleRemoveCmd,
	)
	rootCmd.AddCommand(ruleCmd)
}
