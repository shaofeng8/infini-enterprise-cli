package ops

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
)

type capture struct {
	method  string
	path    string
	query   url.Values
	body    map[string]any
	headers http.Header
	raw     string
}

func newAPI(t *testing.T, response string) (*API, *capture) {
	t.Helper()
	recorded := &capture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorded.method = r.Method
		recorded.path = r.URL.Path
		recorded.query = r.URL.Query()
		recorded.headers = r.Header.Clone()
		payload, _ := io.ReadAll(r.Body)
		recorded.raw = string(payload)
		_ = json.Unmarshal(payload, &recorded.body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(server.Close)
	return NewAPI(client.NewAnonymous(server.URL)), recorded
}

// The controller is mounted at ai_scheduler; ai_schedule is a 404.
func TestSchedulesUseTheSchedulerPath(t *testing.T) {
	api, recorded := newAPI(t, `{"code":200,"data":{"items":[],"total":0,"page":1,"pageSize":20}}`)
	if _, err := api.Schedules(ScheduleQuery{Page: 2, Status: "paused"}); err != nil {
		t.Fatalf("Schedules: %v", err)
	}
	if recorded.path != "/api/ai_scheduler" {
		t.Fatalf("path: got %q", recorded.path)
	}
	if recorded.query.Get("status") != "paused" {
		t.Fatalf("status: got %q", recorded.query.Get("status"))
	}
}

func TestPauseAndResumeAreDistinctPatchRoutes(t *testing.T) {
	api, recorded := newAPI(t, `{"code":200,"data":{"scheduleId":"s_1"}}`)
	if _, err := api.SetScheduleEnabled("s_1", false); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if recorded.method != http.MethodPatch || recorded.path != "/api/ai_scheduler/s_1/pause" {
		t.Fatalf("pause: got %s %s", recorded.method, recorded.path)
	}

	if _, err := api.SetScheduleEnabled("s_1", true); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if recorded.path != "/api/ai_scheduler/s_1/resume" {
		t.Fatalf("resume: got %s", recorded.path)
	}
}

func TestUpdateScheduleUsesPut(t *testing.T) {
	api, recorded := newAPI(t, `{"code":200,"data":{"scheduleId":"s_1"}}`)
	if _, err := api.UpdateSchedule("s_1", map[string]any{"title": "t"}); err != nil {
		t.Fatalf("UpdateSchedule: %v", err)
	}
	if recorded.method != http.MethodPut || recorded.path != "/api/ai_scheduler/s_1" {
		t.Fatalf("got %s %s", recorded.method, recorded.path)
	}
}

func TestArchiveScheduleUsesDelete(t *testing.T) {
	api, recorded := newAPI(t, `{"code":200,"data":{"success":true}}`)
	if _, err := api.ArchiveSchedule("s_1"); err != nil {
		t.Fatalf("ArchiveSchedule: %v", err)
	}
	if recorded.method != http.MethodDelete {
		t.Fatalf("method: got %s", recorded.method)
	}
}

// Starting with nothing to configure must not send an empty configParams
// array, which is not what the UI does.
func TestStartEngineOmitsEmptyConfigParams(t *testing.T) {
	api, recorded := newAPI(t, `{"code":200,"data":{}}`)
	if _, err := api.StartEngine(nil); err != nil {
		t.Fatalf("StartEngine: %v", err)
	}
	if recorded.raw != "" {
		t.Fatalf("body should be empty, got %q", recorded.raw)
	}

	if _, err := api.StartEngine([]ConfigParam{{Key: "spark.executor.cores", Value: "2"}}); err != nil {
		t.Fatalf("StartEngine: %v", err)
	}
	params, ok := recorded.body["configParams"].([]any)
	if !ok || len(params) != 1 {
		t.Fatalf("configParams: got %v", recorded.body)
	}
}

func TestEngineRunningReadsTheRunningField(t *testing.T) {
	api, recorded := newAPI(t, `{"code":200,"data":{"running":true}}`)
	running, err := api.EngineRunning()
	if err != nil {
		t.Fatalf("EngineRunning: %v", err)
	}
	if !running {
		t.Fatal("expected running")
	}
	if recorded.path != "/api/infinity-sql/check-engine" {
		t.Fatalf("path: got %q", recorded.path)
	}
}

func TestAvailableEnginesUnwrapsItems(t *testing.T) {
	api, _ := newAPI(t, `{"code":200,"data":{"items":[{"id":"e_1","name":"default"}]}}`)
	engines, err := api.AvailableEngines()
	if err != nil {
		t.Fatalf("AvailableEngines: %v", err)
	}
	if len(engines) != 1 || engines[0].ID != "e_1" {
		t.Fatalf("got %v", engines)
	}
}

// The instances endpoint has returned both a bare array and a wrapper, so
// both have to decode.
func TestInstancesAcceptBothShapes(t *testing.T) {
	api, _ := newAPI(t, `{"code":200,"data":[{"instanceId":"i_1","role":"worker"}]}`)
	instances, err := api.Instances()
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(instances) != 1 || instances[0].InstanceID != "i_1" {
		t.Fatalf("bare array: got %v", instances)
	}

	wrapped, _ := newAPI(t, `{"code":200,"data":{"instances":[{"instanceId":"i_2","role":"api"}]}}`)
	instances, err = wrapped.Instances()
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(instances) != 1 || instances[0].InstanceID != "i_2" {
		t.Fatalf("wrapper: got %v", instances)
	}
}

// The worker lifecycle probes live under internal, not runtime.
func TestLifecycleProbesUseTheInternalPrefix(t *testing.T) {
	api, recorded := newAPI(t, `{"code":200,"data":{"ready":true}}`)
	if _, err := api.WorkerReady(); err != nil {
		t.Fatalf("WorkerReady: %v", err)
	}
	if recorded.path != "/api/internal/ready" {
		t.Fatalf("ready: got %q", recorded.path)
	}
	if _, err := api.Autoscaling(); err != nil {
		t.Fatalf("Autoscaling: %v", err)
	}
	if recorded.path != "/api/internal/autoscaling" {
		t.Fatalf("autoscaling: got %q", recorded.path)
	}
}

// The drain token travels as a header, never in the body or query string.
func TestDrainSendsTheTokenAsAHeader(t *testing.T) {
	api, recorded := newAPI(t, `{"code":200,"data":{"draining":true}}`)
	if _, err := api.Drain("secret-token"); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if recorded.method != http.MethodPost || recorded.path != "/api/internal/drain" {
		t.Fatalf("got %s %s", recorded.method, recorded.path)
	}
	if got := recorded.headers.Get("x-internal-token"); got != "secret-token" {
		t.Fatalf("header: got %q", got)
	}
	if recorded.query.Get("token") != "" || recorded.raw != "" {
		t.Fatal("the token must not appear in the query string or body")
	}
}

func TestDrainOmitsTheHeaderWithoutAToken(t *testing.T) {
	api, recorded := newAPI(t, `{"code":200,"data":{}}`)
	if _, err := api.Drain(""); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if _, present := recorded.headers["X-Internal-Token"]; present {
		t.Fatal("header should be absent when no token is configured")
	}
}

func TestLicenseRoutes(t *testing.T) {
	api, recorded := newAPI(t, `{"code":200,"data":{"valid":true}}`)
	for _, probe := range []struct {
		call   func() (json.RawMessage, error)
		method string
		path   string
	}{
		{api.LicenseStatus, http.MethodGet, "/api/license/status"},
		{api.RefreshLicense, http.MethodPost, "/api/license/refresh"},
		{api.LicenseLimits, http.MethodGet, "/api/license/limits"},
	} {
		if _, err := probe.call(); err != nil {
			t.Fatalf("%s: %v", probe.path, err)
		}
		if recorded.method != probe.method || recorded.path != probe.path {
			t.Fatalf("got %s %s, want %s %s", recorded.method, recorded.path, probe.method, probe.path)
		}
	}
}
