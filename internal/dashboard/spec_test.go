package dashboard

import (
	"encoding/json"
	"testing"
)

func TestReadBundleAcceptsBundle(t *testing.T) {
	raw := json.RawMessage(`{
	  "id": "dash-1",
	  "revision": 7,
	  "specHash": "abc123",
	  "spec": {"version": 1, "title": "Sales", "filters": [], "queries": [], "widgets": []}
	}`)

	bundle, err := ReadBundle(raw)
	if err != nil {
		t.Fatalf("ReadBundle: %v", err)
	}
	if bundle.SpecHash != "abc123" || bundle.Revision != 7 {
		t.Fatalf("got %+v", bundle)
	}

	spec, err := ParseSpec(bundle.Spec)
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}
	if spec.Title != "Sales" {
		t.Fatalf("got %q", spec.Title)
	}
}

// A hand-written or cross-deployment spec has no bundle wrapper; it must still
// import, just without the conflict-check hash.
func TestReadBundleAcceptsBareSpec(t *testing.T) {
	raw := json.RawMessage(`{"version": 1, "title": "Sales", "filters": [], "queries": [], "widgets": []}`)

	bundle, err := ReadBundle(raw)
	if err != nil {
		t.Fatalf("ReadBundle: %v", err)
	}
	if bundle.SpecHash != "" {
		t.Fatalf("a bare spec must not carry a hash, got %q", bundle.SpecHash)
	}
	if string(bundle.Spec) != string(raw) {
		t.Fatalf("spec was altered: %s", bundle.Spec)
	}
}

func TestReadBundleRejectsUnrecognizedPayload(t *testing.T) {
	for name, raw := range map[string]string{
		"neither spec nor bundle": `{"title": "Sales"}`,
		"not an object":           `[1,2,3]`,
		"empty bundle spec":       `{"spec": null}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ReadBundle(json.RawMessage(raw)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestLayoutsRoundTripToPatchShape(t *testing.T) {
	spec := &Spec{Widgets: []Widget{
		{ID: "w_kpi", Layout: Layout{X: 0, Y: 0, W: 3, H: 2}},
		{ID: "w_trend", Layout: Layout{X: 3, Y: 0, W: 9, H: 4}},
	}}

	layouts := spec.Layouts()
	if len(layouts) != 2 {
		t.Fatalf("got %d layouts", len(layouts))
	}
	encoded, err := json.Marshal(layouts[1])
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := `{"widget_id":"w_trend","x":3,"y":0,"w":9,"h":4}`
	if string(encoded) != want {
		t.Fatalf("got %s, want %s", encoded, want)
	}
}

func TestRefreshJobTerminal(t *testing.T) {
	for status, terminal := range map[string]bool{
		"pending":               false,
		"running":               false,
		"completed":             true,
		"completed_with_errors": true,
		"failed":                true,
		"canceled":              true,
	} {
		job := RefreshJob{Status: status}
		if job.Terminal() != terminal {
			t.Fatalf("%s: got %v", status, job.Terminal())
		}
	}
}
