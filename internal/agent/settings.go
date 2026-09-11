package agent

import (
	"encoding/json"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

// AutoApprovalSettings is the agent's capability and budget envelope: how many
// model requests a run may spend and which tool families it may reach for.
//
// Every field is a pointer so a partial update stays partial. The endpoint
// stores whatever object it is given, so sending a zero value for a field the
// caller did not mention would quietly disable a capability or drop a budget
// to nothing.
type AutoApprovalSettings struct {
	MaxRequests            *int  `json:"maxRequests,omitempty"`
	MaxSubAgentRequests    *int  `json:"maxSubAgentRequests,omitempty"`
	DatabaseReturnLimit    *int  `json:"databaseReturnLimit,omitempty"`
	DelegateMaxConcurrency *int  `json:"delegateMaxConcurrency,omitempty"`
	EnableNotifications    *bool `json:"enableNotifications,omitempty"`
	DebugMode              *bool `json:"debugMode,omitempty"`
	EnableSubAgent         *bool `json:"enableSubAgent,omitempty"`
	EnableWebSearch        *bool `json:"enableWebSearch,omitempty"`
	EnableBrowser          *bool `json:"enableBrowser,omitempty"`
	EnableShell            *bool `json:"enableShell,omitempty"`
	EnableMap              *bool `json:"enableMap,omitempty"`
}

// Merge overlays the set fields of patch onto a copy of the current settings,
// which is how a flag-driven partial update keeps the rest intact.
func (s AutoApprovalSettings) Merge(patch AutoApprovalSettings) AutoApprovalSettings {
	merged := s
	for _, field := range []struct{ dst, src **int }{
		{&merged.MaxRequests, &patch.MaxRequests},
		{&merged.MaxSubAgentRequests, &patch.MaxSubAgentRequests},
		{&merged.DatabaseReturnLimit, &patch.DatabaseReturnLimit},
		{&merged.DelegateMaxConcurrency, &patch.DelegateMaxConcurrency},
	} {
		if *field.src != nil {
			*field.dst = *field.src
		}
	}
	for _, field := range []struct{ dst, src **bool }{
		{&merged.EnableNotifications, &patch.EnableNotifications},
		{&merged.DebugMode, &patch.DebugMode},
		{&merged.EnableSubAgent, &patch.EnableSubAgent},
		{&merged.EnableWebSearch, &patch.EnableWebSearch},
		{&merged.EnableBrowser, &patch.EnableBrowser},
		{&merged.EnableShell, &patch.EnableShell},
		{&merged.EnableMap, &patch.EnableMap},
	} {
		if *field.src != nil {
			*field.dst = *field.src
		}
	}
	return merged
}

// APIConfiguration is the account-level model configuration.
type APIConfiguration struct {
	APIProvider     string `json:"apiProvider,omitempty"`
	APIModelID      string `json:"apiModelId,omitempty"`
	OpenAiBaseURL   string `json:"openAiBaseUrl,omitempty"`
	OpenAiAPIKey    string `json:"openAiApiKey,omitempty"`
	OpenAiModelID   string `json:"openAiModelId,omitempty"`
	AnthropicBase   string `json:"anthropicBaseUrl,omitempty"`
	APIKey          string `json:"apiKey,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
}

// APIProviders the server recognises.
var APIProviders = []string{"anthropic", "openai", "infinisynapse"}

// SettingsRequest is the body of POST /api/ai/settings.
//
// With TaskID set, a model change is applied to that task through the command
// queue; without it, it becomes the account default.
type SettingsRequest struct {
	APIConfiguration    *APIConfiguration     `json:"apiConfiguration,omitempty"`
	CustomInstructions  *string               `json:"customInstructionsSetting,omitempty"`
	AutoApproval        *AutoApprovalSettings `json:"autoApprovalSettings,omitempty"`
	ChatSettings        *ChatSettings         `json:"chatSettings,omitempty"`
	TaskID              string                `json:"taskId,omitempty"`
	SubAgentInherit     *bool                 `json:"subAgentModelInheritMain,omitempty"`
	SubAgentAPIProvider string                `json:"subAgentApiProvider,omitempty"`
	SubAgentAPIModelID  string                `json:"subAgentApiModelId,omitempty"`
}

// Notification is the human-facing message some endpoints attach.
type Notification struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Message  string `json:"message"`
	Duration int    `json:"duration,omitempty"`
}

type SettingsResponse struct {
	Success      bool          `json:"success"`
	Notification *Notification `json:"notification,omitempty"`
}

// FullState is the whole ExtensionState, kept as raw JSON alongside the parts
// the CLI reads, because the server's shape is much wider than what any one
// command needs.
type FullState struct {
	AutoApproval *AutoApprovalSettings `json:"autoApprovalSettings,omitempty"`
	ChatSettings *ChatSettings         `json:"chatSettings,omitempty"`
	Raw          json.RawMessage       `json:"-"`
}

func (r *Runner) UpdateSettings(request SettingsRequest) (*SettingsResponse, error) {
	raw, err := r.c.Post("/api/ai/settings", request)
	if err != nil {
		return nil, err
	}
	var response SettingsResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, cliexit.New(cliexit.CodeBusiness, "cannot parse settings response: %v", err)
	}
	return &response, nil
}

// RawState returns the full ExtensionState untouched.
func (r *Runner) RawState(taskID string) (json.RawMessage, error) {
	params := map[string]string{}
	if taskID != "" {
		params["taskId"] = taskID
	}
	return r.c.Get("/api/ai/state", params)
}

// Settings reads the parts of the state that the settings commands edit.
func (r *Runner) Settings(taskID string) (*FullState, error) {
	raw, err := r.RawState(taskID)
	if err != nil {
		return nil, err
	}
	var state FullState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, cliexit.New(cliexit.CodeBusiness, "cannot parse state: %v", err)
	}
	state.Raw = raw
	return &state, nil
}

// Configuration returns the account's saved model configuration.
func (r *Runner) Configuration() (json.RawMessage, error) {
	return r.c.Get("/api/ai/configuration", nil)
}

// Models lists the ids the configured OpenAI-compatible endpoint reports.
//
// The server swallows every failure here and answers with an empty list, so an
// empty result means "no models, or the lookup failed" and the CLI says so
// rather than presenting it as a definitive answer.
func (r *Runner) Models() ([]string, error) {
	raw, err := r.c.Get("/api/ai/models", nil)
	if err != nil {
		return nil, err
	}
	var models []string
	if err := json.Unmarshal(raw, &models); err != nil {
		return nil, cliexit.New(cliexit.CodeBusiness, "cannot parse the model list: %v", err)
	}
	return models, nil
}

func (r *Runner) Ping() (json.RawMessage, error) {
	return r.c.Get("/api/ai/ping", nil)
}
