package hub

import "encoding/json"

// Draft is one pending change to the semantic layer. Writes by someone who
// does not own the target data source land here instead of taking effect.
type Draft struct {
	ID            string          `json:"id"`
	EntityType    string          `json:"entity_type"`
	OperationType string          `json:"operation_type"`
	SourceType    string          `json:"source_type,omitempty"`
	Status        string          `json:"status"`
	TargetID      string          `json:"target_id,omitempty"`
	TaskID        string          `json:"task_id,omitempty"`
	ReviewerID    string          `json:"reviewer_id,omitempty"`
	ReviewComment string          `json:"review_comment,omitempty"`
	Payload       json.RawMessage `json:"payload_json,omitempty"`
	Before        json.RawMessage `json:"before_json,omitempty"`
	UpdatedAt     string          `json:"updatedAt,omitempty"`
}

type PendingCounts struct {
	ValidationRequests     int            `json:"validationRequests"`
	UserRatings            int            `json:"userRatings"`
	ContextHubUpdates      map[string]int `json:"contextHubUpdates"`
	TotalContextHubUpdates int            `json:"totalContextHubUpdates"`
}

// DraftRequest mirrors ContextHubDraftCreateDto. Payload carries the candidate
// change in the shape of the entity kind it targets.
type DraftRequest struct {
	EntityType    string          `json:"entity_type"`
	OperationType string          `json:"operation_type"`
	TargetID      string          `json:"target_id,omitempty"`
	SourceType    string          `json:"source_type,omitempty"`
	ReviewComment string          `json:"review_comment,omitempty"`
	Payload       json.RawMessage `json:"payload"`
}

// ReviewRequest mirrors ContextHubReviewDto.
//
// ApprovedFields narrows an approval to a subset of the draft's fields, and
// FieldValues overrides values before they are written back, which is how a
// reviewer accepts an AI suggestion with corrections.
type ReviewRequest struct {
	EntityType     string          `json:"entity_type"`
	DraftID        string          `json:"draft_id"`
	ReviewComment  string          `json:"review_comment,omitempty"`
	ApprovedFields []string        `json:"approved_fields,omitempty"`
	FieldValues    json.RawMessage `json:"field_values,omitempty"`
}

type TranslateField struct {
	Key   string `json:"key"`
	Label string `json:"label,omitempty"`
	Text  string `json:"text"`
}

func (a *API) AddDraft(request DraftRequest) (json.RawMessage, error) {
	return a.c.Post(BasePath+"/draft/add", request)
}

// ListDrafts requires an entity type: drafts of different kinds have different
// payload shapes and the endpoint does not mix them.
func ListDrafts(a *API, kind Kind, query PageQuery) (*Page[Draft], error) {
	params := query.params()
	params["entity_type"] = string(kind)
	raw, err := a.c.Get(BasePath+"/draft/list", params)
	page, err := decode[Page[Draft]](raw, err, "draft list")
	if err != nil {
		return nil, err
	}
	return &page, nil
}

// TableUpdateDrafts lists the table-level candidates a memory build produced.
func TableUpdateDrafts(a *API, query PageQuery) (json.RawMessage, error) {
	return a.c.Get(BasePath+"/draft/table-updates/list", query.params())
}

func (a *API) PendingCounts() (*PendingCounts, error) {
	raw, err := a.c.Get(BasePath+"/review/pending-counts", nil)
	counts, err := decode[PendingCounts](raw, err, "pending counts")
	if err != nil {
		return nil, err
	}
	return &counts, nil
}

func (a *API) Approve(request ReviewRequest) (json.RawMessage, error) {
	return a.c.Post(BasePath+"/review/approve", request)
}

func (a *API) Reject(request ReviewRequest) (json.RawMessage, error) {
	return a.c.Post(BasePath+"/review/reject", request)
}

// Restore puts an already-reviewed draft back into the pending queue.
func (a *API) Restore(request ReviewRequest) (json.RawMessage, error) {
	return a.c.Post(BasePath+"/review/restore", request)
}

// Translate renders review fields in another language without changing them,
// so a reviewer can read a description written by someone else.
func (a *API) Translate(targetLanguage string, fields []TranslateField) (json.RawMessage, error) {
	return a.c.Post(BasePath+"/review/translate", map[string]any{
		"target_language": targetLanguage,
		"fields":          fields,
	})
}
