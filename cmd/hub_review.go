package cmd

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/hub"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/spf13/cobra"
)

var hubDraftCmd = &cobra.Command{
	Use:   "draft",
	Short: "Inspect and submit pending changes",
	Long: `A draft is a proposed change to the semantic layer: either a memory build's
suggestion or an edit by someone who does not own the target data source.

Drafts of different kinds have different payload shapes, so listing them
requires an entity type.`,
}

var hubDraftLsCmd = &cobra.Command{
	Use:     "ls <entity-type>",
	Aliases: []string{"list"},
	Short:   "List drafts of one entity type",
	Long: `Entity types: ` + strings.Join(hub.EntityTypes, ", ") + `

  infini-cli hub draft ls table_data --table
  infini-cli hub draft ls kpi --status approved --table`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		kind, err := requireEntityKind(args[0])
		if err != nil {
			return err
		}
		query, err := hubReviewQuery(cmd)
		if err != nil {
			return err
		}

		api, err := hubAPI()
		if err != nil {
			return err
		}
		page, err := hub.ListDrafts(api, kind, query)
		if err != nil {
			return err
		}
		return output.Success(page, []string{"ID", "OP", "STATUS", "SOURCE", "TARGET", "UPDATED"}, func() [][]string {
			rows := make([][]string, 0, len(page.Items))
			for _, item := range page.Items {
				rows = append(rows, []string{
					item.ID, item.OperationType, item.Status,
					item.SourceType, item.TargetID, item.UpdatedAt,
				})
			}
			return rows
		})
	},
}

var hubDraftTableUpdatesCmd = &cobra.Command{
	Use:   "table-updates",
	Short: "List table-level candidates a memory build produced",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		query, err := hubReviewQuery(cmd)
		if err != nil {
			return err
		}
		api, err := hubAPI()
		if err != nil {
			return err
		}
		data, err := hub.TableUpdateDrafts(api, query)
		if err != nil {
			return err
		}
		return output.Success(data, nil, nil)
	},
}

var hubDraftAddCmd = &cobra.Command{
	Use:   "add <entity-type>",
	Short: "Submit a draft directly",
	Long: `Most edits go through the entity commands, which raise a draft on their own
when needed. Use this when you need to submit a change for review explicitly,
for example when importing definitions from elsewhere.

The payload's shape follows the entity type, matching what that kind's add or
update endpoint would take.

  infini-cli hub draft add table_data --operation update --target t_1 \
      --payload '{"table_description":"订单事实表"}' --source imported`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		kind, err := requireEntityKind(args[0])
		if err != nil {
			return err
		}

		flags := cmd.Flags()
		operation, _ := flags.GetString("operation")
		if !slices.Contains(hub.DraftOperations, operation) {
			return cliexit.Usage("--operation must be one of: %s", strings.Join(hub.DraftOperations, ", "))
		}
		source, _ := flags.GetString("source")
		if source != "" && !slices.Contains(hub.DraftSources, source) {
			return cliexit.Usage("--source must be one of: %s", strings.Join(hub.DraftSources, ", "))
		}
		target, _ := flags.GetString("target")
		if operation != "create" && target == "" {
			return cliexit.Usage("--target is required for an %s draft", operation)
		}

		payloadArg, _ := flags.GetString("payload")
		payload, err := readJSONArg(payloadArg)
		if err != nil {
			return err
		}
		if len(payload) == 0 {
			return cliexit.Usage("--payload is required")
		}
		comment, _ := flags.GetString("comment")

		api, err := hubAPI()
		if err != nil {
			return err
		}
		result, err := api.AddDraft(hub.DraftRequest{
			EntityType:    string(kind),
			OperationType: operation,
			TargetID:      target,
			SourceType:    source,
			ReviewComment: comment,
			Payload:       payload,
		})
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

var hubReviewCmd = &cobra.Command{
	Use:   "review",
	Short: "Approve, reject or revisit drafts",
	Long: `Approving writes the draft back into the semantic layer. An approval can be
narrowed to some of the draft's fields with --field, and values can be
corrected on the way in with --set, which is how an AI suggestion is accepted
with edits rather than wholesale.`,
}

var hubReviewPendingCmd = &cobra.Command{
	Use:   "pending",
	Short: "Count drafts waiting for review",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := hubAPI()
		if err != nil {
			return err
		}
		counts, err := api.PendingCounts()
		if err != nil {
			return err
		}
		return output.Success(counts, []string{"ENTITY TYPE", "PENDING"}, func() [][]string {
			rows := make([][]string, 0, len(hub.EntityTypes)+1)
			for _, entityType := range hub.EntityTypes {
				rows = append(rows, []string{entityType, strconv.Itoa(counts.ContextHubUpdates[entityType])})
			}
			return append(rows, []string{"total", strconv.Itoa(counts.TotalContextHubUpdates)})
		})
	},
}

var hubReviewApproveCmd = &cobra.Command{
	Use:   "approve <entity-type> <draft-id>",
	Short: "Approve a draft",
	Long: `  infini-cli hub review approve table_data d_1
  infini-cli hub review approve table_data d_1 --field table_description
  infini-cli hub review approve kpi d_2 --set kpi_description="季度口径已修正"`,
	Args: exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		request, err := reviewRequest(cmd, args)
		if err != nil {
			return err
		}
		api, err := hubAPI()
		if err != nil {
			return err
		}
		result, err := api.Approve(request)
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

var hubReviewRejectCmd = &cobra.Command{
	Use:   "reject <entity-type> <draft-id>",
	Short: "Reject a draft",
	Long:  `A comment is worth leaving: the submitter sees it, and a memory build's suggestion is otherwise unexplained.`,
	Args:  exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		request, err := reviewRequest(cmd, args)
		if err != nil {
			return err
		}
		api, err := hubAPI()
		if err != nil {
			return err
		}
		result, err := api.Reject(request)
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

var hubReviewRestoreCmd = &cobra.Command{
	Use:   "restore <entity-type> <draft-id>",
	Short: "Put an already-reviewed draft back in the pending queue",
	Args:  exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		request, err := reviewRequest(cmd, args)
		if err != nil {
			return err
		}
		api, err := hubAPI()
		if err != nil {
			return err
		}
		result, err := api.Restore(request)
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

var hubReviewTranslateCmd = &cobra.Command{
	Use:   "translate",
	Short: "Translate review fields without changing them",
	Long: `Renders a draft's text in another language so it can be reviewed by someone
who does not read the original. Nothing is written back.

  infini-cli hub review translate --to zh-CN \
      --field table_description="Fact table of orders, one row per order line"`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		target, _ := flags.GetString("to")
		if target == "" {
			return cliexit.Usage("--to is required, e.g. zh-CN, en, ja, ko, ru or ar")
		}

		pairs, _ := flags.GetStringSlice("field")
		if len(pairs) == 0 {
			return cliexit.Usage("--field is required at least once, as key=text")
		}
		fields := make([]hub.TranslateField, 0, len(pairs))
		for _, pair := range pairs {
			key, text, found := strings.Cut(pair, "=")
			if !found || key == "" || text == "" {
				return cliexit.Usage("--field expects key=text, received %q", pair)
			}
			fields = append(fields, hub.TranslateField{Key: key, Text: text})
		}

		api, err := hubAPI()
		if err != nil {
			return err
		}
		result, err := api.Translate(target, fields)
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(result), nil, nil)
	},
}

func reviewRequest(cmd *cobra.Command, args []string) (hub.ReviewRequest, error) {
	kind, err := requireEntityKind(args[0])
	if err != nil {
		return hub.ReviewRequest{}, err
	}

	flags := cmd.Flags()
	comment, _ := flags.GetString("comment")
	fields, _ := flags.GetStringSlice("field")
	overrides, _ := flags.GetStringSlice("set")

	request := hub.ReviewRequest{
		EntityType:     string(kind),
		DraftID:        args[1],
		ReviewComment:  comment,
		ApprovedFields: fields,
	}

	if len(overrides) > 0 {
		values, err := parseKeyValues(overrides, "set")
		if err != nil {
			return hub.ReviewRequest{}, err
		}
		encoded, err := json.Marshal(values)
		if err != nil {
			return hub.ReviewRequest{}, cliexit.Usage("cannot encode --set values: %v", err)
		}
		request.FieldValues = encoded
	}
	return request, nil
}

func hubReviewQuery(cmd *cobra.Command) (hub.PageQuery, error) {
	query := hubPageQuery(cmd)
	status, _ := cmd.Flags().GetString("status")
	if status != "" && !slices.Contains(hub.ReviewStatuses, status) {
		return query, cliexit.Usage("--status must be one of: %s", strings.Join(hub.ReviewStatuses, ", "))
	}
	query.Status = status
	return query, nil
}

func init() {
	for _, cmd := range []*cobra.Command{hubDraftLsCmd, hubDraftTableUpdatesCmd} {
		addHubPageFlags(cmd)
		cmd.Flags().String("status", "", "Review status: "+strings.Join(hub.ReviewStatuses, ", "))
	}

	draftFlags := hubDraftAddCmd.Flags()
	draftFlags.String("operation", "", "Operation: "+strings.Join(hub.DraftOperations, ", "))
	draftFlags.String("target", "", "Id of the record being changed (required unless creating)")
	draftFlags.String("source", "", "Where the change came from: "+strings.Join(hub.DraftSources, ", "))
	draftFlags.String("payload", "", "Candidate change as JSON, @file or @-")
	draftFlags.String("comment", "", "Note for the reviewer")

	for _, cmd := range []*cobra.Command{hubReviewApproveCmd, hubReviewRejectCmd, hubReviewRestoreCmd} {
		cmd.Flags().String("comment", "", "Review note")
		cmd.Flags().StringSlice("field", nil, "Limit the decision to this field (repeatable)")
		cmd.Flags().StringSlice("set", nil, "Override a field value as key=value (repeatable)")
	}

	hubReviewTranslateCmd.Flags().String("to", "", "Target language, e.g. zh-CN")
	hubReviewTranslateCmd.Flags().StringSlice("field", nil, "Text to translate as key=text (repeatable)")

	hubDraftCmd.AddCommand(hubDraftLsCmd, hubDraftTableUpdatesCmd, hubDraftAddCmd)
	hubReviewCmd.AddCommand(
		hubReviewPendingCmd, hubReviewApproveCmd, hubReviewRejectCmd,
		hubReviewRestoreCmd, hubReviewTranslateCmd,
	)
	hubCmd.AddCommand(hubDraftCmd, hubReviewCmd)
}
