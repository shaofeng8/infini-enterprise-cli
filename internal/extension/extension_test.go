package extension

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

type capture struct {
	method string
	path   string
	query  url.Values
	body   map[string]any
	// contentType shows whether a request went out as JSON or multipart.
	contentType string
	raw         string
}

func newAPI(t *testing.T, response string) (*API, *capture) {
	t.Helper()
	recorded := &capture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorded.method = r.Method
		recorded.path = r.URL.Path
		recorded.query = r.URL.Query()
		recorded.contentType = r.Header.Get("Content-Type")
		payload, _ := io.ReadAll(r.Body)
		recorded.raw = string(payload)
		_ = json.Unmarshal(payload, &recorded.body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(server.Close)
	return NewAPI(client.NewAnonymous(server.URL)), recorded
}

// The skill and tool listings page with pageNum/pageSize, unlike every other
// module's page/pageSize, so the names are pinned rather than assumed.
func TestKeywordQueryUsesPageNum(t *testing.T) {
	api, recorded := newAPI(t, `{"code":200,"data":{"list":[],"total":0}}`)
	if _, err := api.InstalledSkills(KeywordQuery{PageNum: 2, PageSize: 30, Keyword: "sql"}); err != nil {
		t.Fatalf("InstalledSkills: %v", err)
	}
	if recorded.path != "/api/ai_skill/list" {
		t.Fatalf("path: got %s", recorded.path)
	}
	for key, want := range map[string]string{"pageNum": "2", "pageSize": "30", "keyword": "sql"} {
		if got := recorded.query.Get(key); got != want {
			t.Fatalf("%s: got %q, want %q", key, got, want)
		}
	}
	if _, present := recorded.query["page"]; present {
		t.Fatal("page must not be sent; this endpoint reads pageNum")
	}
}

func TestAvailableSkillsOmitsBrowserWhenOff(t *testing.T) {
	api, recorded := newAPI(t, `{"code":200,"data":[]}`)
	if _, err := api.AvailableSkills("t_1", false); err != nil {
		t.Fatalf("AvailableSkills: %v", err)
	}
	if recorded.query.Get("taskId") != "t_1" {
		t.Fatalf("taskId: got %q", recorded.query.Get("taskId"))
	}
	if _, present := recorded.query["supportsBrowser"]; present {
		t.Fatal("supportsBrowser should be omitted when false")
	}
}

// Uninstalling addresses the catalog entry while deleting a local upload
// addresses the installation row, and the two use different routes.
func TestSkillUninstallAndDeleteUseDifferentIdentifiers(t *testing.T) {
	api, recorded := newAPI(t, `{"code":200,"data":{"success":true}}`)
	if _, err := api.UninstallSkill("507f1f77bcf86cd799439011"); err != nil {
		t.Fatalf("UninstallSkill: %v", err)
	}
	if recorded.path != "/api/ai_skill/uninstall" || recorded.body["skillId"] != "507f1f77bcf86cd799439011" {
		t.Fatalf("uninstall: got %s %v", recorded.path, recorded.body)
	}

	if _, err := api.DeleteSkill("local-row-1"); err != nil {
		t.Fatalf("DeleteSkill: %v", err)
	}
	if recorded.path != "/api/ai_skill/deleteLocal/local-row-1" {
		t.Fatalf("delete: got %s", recorded.path)
	}
}

func TestDeleteSkillRequiresAnID(t *testing.T) {
	api, _ := newAPI(t, `{}`)
	if _, err := api.DeleteSkill(""); cliexit.CodeOf(err) != cliexit.CodeUsage {
		t.Fatalf("got %v", err)
	}
}

// Editing a local skill without a new archive still has to go out as
// multipart, because the endpoint reads its fields from the form.
func TestEditSkillSendsMultipartWithoutAFile(t *testing.T) {
	api, recorded := newAPI(t, `{"code":200,"data":{"id":"sk_1"}}`)
	if _, err := api.EditSkill("", map[string]string{"id": "sk_1", "status": "inactive"}); err != nil {
		t.Fatalf("EditSkill: %v", err)
	}
	if !strings.HasPrefix(recorded.contentType, "multipart/form-data") {
		t.Fatalf("content type: got %q", recorded.contentType)
	}
	if !strings.Contains(recorded.raw, `name="status"`) || !strings.Contains(recorded.raw, "inactive") {
		t.Fatalf("body should carry the fields, got %q", recorded.raw)
	}
	if strings.Contains(recorded.raw, "filename=") {
		t.Fatal("no file part should be present")
	}
}

func TestIsToolInstalledEscapesThePluginID(t *testing.T) {
	api, recorded := newAPI(t, `{"code":200,"data":{"installed":true}}`)
	installed, err := api.IsToolInstalled("group/plugin 1")
	if err != nil {
		t.Fatalf("IsToolInstalled: %v", err)
	}
	if !installed {
		t.Fatal("expected installed")
	}
	if recorded.path != "/api/ai_tool/isInstalled/group%2Fplugin 1" && recorded.path != "/api/ai_tool/isInstalled/group/plugin 1" {
		t.Fatalf("path: got %q", recorded.path)
	}
}

// enabled=0 is a real filter, so it has to survive as a query parameter rather
// than being dropped the way an unset field is.
func TestRuleQuerySendsEnabledZero(t *testing.T) {
	api, recorded := newAPI(t, `{"code":200,"data":{"items":[],"meta":{}}}`)
	off := 0
	if _, err := api.Rules(RuleQuery{Enabled: &off, RuleType: "database"}); err != nil {
		t.Fatalf("Rules: %v", err)
	}
	if got := recorded.query.Get("enabled"); got != "0" {
		t.Fatalf("enabled: got %q, want 0", got)
	}
	if got := recorded.query.Get("rule_type"); got != "database" {
		t.Fatalf("rule_type: got %q", got)
	}
}

func TestRuleQueryOmitsUnsetEnabled(t *testing.T) {
	api, recorded := newAPI(t, `{"code":200,"data":{"items":[],"meta":{}}}`)
	if _, err := api.Rules(RuleQuery{}); err != nil {
		t.Fatalf("Rules: %v", err)
	}
	if _, present := recorded.query["enabled"]; present {
		t.Fatal("enabled should be omitted when unset")
	}
}

// The toggle endpoint takes numeric ids while delete takes strings; sending
// the wrong kind fails validation server-side.
func TestSetRulesEnabledSendsNumericIDs(t *testing.T) {
	api, recorded := newAPI(t, `{"code":200,"data":{}}`)
	if _, err := api.SetRulesEnabled([]int{1, 2}, 1); err != nil {
		t.Fatalf("SetRulesEnabled: %v", err)
	}
	ids, ok := recorded.body["ids"].([]any)
	if !ok || len(ids) != 2 {
		t.Fatalf("ids: got %v", recorded.body["ids"])
	}
	if _, isNumber := ids[0].(float64); !isNumber {
		t.Fatalf("ids must be numbers, got %T", ids[0])
	}
}

func TestDeleteRulesRejectsAnEmptyList(t *testing.T) {
	api, _ := newAPI(t, `{}`)
	if _, err := api.DeleteRules(nil); cliexit.CodeOf(err) != cliexit.CodeUsage {
		t.Fatalf("got %v", err)
	}
	if _, err := api.DeleteTemplates(nil); cliexit.CodeOf(err) != cliexit.CodeUsage {
		t.Fatalf("got %v", err)
	}
}

// Templates list under the bare controller path, with no trailing segment.
func TestTemplatesUseTheBarePath(t *testing.T) {
	api, recorded := newAPI(t, `{"code":200,"data":{"items":[],"meta":{}}}`)
	if _, err := api.Templates(TemplateQuery{Page: 3, Keyword: "报表"}); err != nil {
		t.Fatalf("Templates: %v", err)
	}
	if recorded.path != "/api/ai_template" {
		t.Fatalf("path: got %q", recorded.path)
	}
	if recorded.query.Get("page") != "3" || recorded.query.Get("keyword") != "报表" {
		t.Fatalf("query: got %v", recorded.query)
	}
}

func TestUpdateTemplatePutsTheIDInThePath(t *testing.T) {
	api, recorded := newAPI(t, `{"code":200,"data":{"id":"tpl_1"}}`)
	if _, err := api.UpdateTemplate("tpl_1", map[string]string{"name": "n", "text": "t"}); err != nil {
		t.Fatalf("UpdateTemplate: %v", err)
	}
	if recorded.method != http.MethodPost || recorded.path != "/api/ai_template/update/tpl_1" {
		t.Fatalf("got %s %s", recorded.method, recorded.path)
	}
}
