package dashboard

import (
	"encoding/json"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

// EnumAllValue selects every option of an enum filter.
const EnumAllValue = "__all__"

// Spec models only the parts of a DashboardSpec the CLI reasons about: filter
// types (to coerce --filter values correctly), query ids (to default
// --query-ids) and widget layout. Everything else round-trips as raw JSON so
// this package never has to track the full server-side schema.
type Spec struct {
	Version     int      `json:"version"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Filters     []Filter `json:"filters"`
	Queries     []Query  `json:"queries"`
	Widgets     []Widget `json:"widgets"`
}

type Filter struct {
	Name     string          `json:"name"`
	Label    string          `json:"label"`
	Type     string          `json:"type"` // daterange | enum | number | text
	Multiple bool            `json:"multiple,omitempty"`
	Default  json.RawMessage `json:"default,omitempty"`
}

type Query struct {
	ID   string `json:"id"`
	Role string `json:"role,omitempty"` // data (default) | filter_options
}

type Widget struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Type   string `json:"type"`
	Layout Layout `json:"layout"`
}

type Layout struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

func ParseSpec(raw json.RawMessage) (*Spec, error) {
	var spec Spec
	if err := json.Unmarshal(raw, &spec); err != nil {
		return nil, cliexit.New(cliexit.CodeBusiness, "cannot parse dashboard spec: %v", err)
	}
	return &spec, nil
}

// DataQueryIDs returns the queries a dashboard actually renders. filter_options
// queries exist to populate filter dropdowns and are rejected by the query
// endpoint, so they must never be requested by default.
func (s *Spec) DataQueryIDs() []string {
	ids := make([]string, 0, len(s.Queries))
	for _, query := range s.Queries {
		if query.Role == "filter_options" {
			continue
		}
		ids = append(ids, query.ID)
	}
	return ids
}

func (s *Spec) FilterByName(name string) *Filter {
	for i := range s.Filters {
		if s.Filters[i].Name == name {
			return &s.Filters[i]
		}
	}
	return nil
}

func (s *Spec) FilterNames() []string {
	names := make([]string, 0, len(s.Filters))
	for _, filter := range s.Filters {
		names = append(names, filter.Name)
	}
	return names
}

func (s *Spec) WidgetByID(id string) *Widget {
	for i := range s.Widgets {
		if s.Widgets[i].ID == id {
			return &s.Widgets[i]
		}
	}
	return nil
}

func (s *Spec) WidgetIDs() []string {
	ids := make([]string, 0, len(s.Widgets))
	for _, widget := range s.Widgets {
		ids = append(ids, widget.ID)
	}
	return ids
}

// Layouts converts the spec's widget layout into the shape PATCH .../layout
// expects, so `layout get` output can be edited and fed straight back.
func (s *Spec) Layouts() []WidgetLayout {
	layouts := make([]WidgetLayout, 0, len(s.Widgets))
	for _, widget := range s.Widgets {
		layouts = append(layouts, WidgetLayout{
			WidgetID: widget.ID,
			X:        widget.Layout.X,
			Y:        widget.Layout.Y,
			W:        widget.Layout.W,
			H:        widget.Layout.H,
		})
	}
	return layouts
}

// Bundle is the export format: the spec plus the identity needed to apply an
// edit back safely.
type Bundle struct {
	ID       string          `json:"id,omitempty"`
	Title    string          `json:"title,omitempty"`
	Revision int             `json:"revision,omitempty"`
	SpecHash string          `json:"specHash,omitempty"`
	Spec     json.RawMessage `json:"spec"`
}

// ReadBundle accepts either an exported bundle or a bare spec, so a
// hand-written spec file works as well as one produced by `dash export`.
func ReadBundle(raw json.RawMessage) (*Bundle, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, cliexit.Usage("payload must be a JSON object containing a dashboard spec: %v", err)
	}

	if _, isBundle := probe["spec"]; isBundle {
		var bundle Bundle
		if err := json.Unmarshal(raw, &bundle); err != nil {
			return nil, cliexit.Usage("cannot parse dashboard bundle: %v", err)
		}
		// A JSON null decodes into a 4-byte RawMessage, not an empty one.
		if len(bundle.Spec) == 0 || string(bundle.Spec) == "null" {
			return nil, cliexit.Usage("bundle has an empty spec")
		}
		return &bundle, nil
	}

	if _, hasVersion := probe["version"]; !hasVersion {
		return nil, cliexit.Usage("payload is neither a dashboard spec (missing \"version\") nor an exported bundle (missing \"spec\")")
	}
	return &Bundle{Spec: raw}, nil
}
