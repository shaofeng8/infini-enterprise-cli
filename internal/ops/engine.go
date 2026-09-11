package ops

import (
	"encoding/json"
	"strconv"
)

const (
	engineBase  = "/api/infinity-sql"
	byzerBase   = "/api/ai_byzer"
	runtimeBase = "/api/runtime"
	// The worker lifecycle probes live under internal, not runtime.
	internalBase = "/api/internal"
	licenseBase  = "/api/license"
)

// EngineStatus describes the embedded Infini-SQL engine of this deployment.
// The shape varies by deployment mode, so it is kept raw.
func (a *API) EngineStatus() (json.RawMessage, error) {
	return a.c.Get(engineBase+"/status", nil)
}

// EngineRunning is the cheap liveness check.
func (a *API) EngineRunning() (bool, error) {
	response, err := fetch[struct {
		Running bool `json:"running"`
	}](a, engineBase+"/check-engine", nil, "engine status")
	return response.Running, err
}

// ConfigParam is one engine setting passed at start time.
type ConfigParam struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func (a *API) StartEngine(params []ConfigParam) (json.RawMessage, error) {
	return a.c.Post(engineBase+"/start", engineBody(params))
}

func (a *API) StopEngine() (json.RawMessage, error) {
	return a.c.Post(engineBase+"/stop", nil)
}

// EnsureEngine starts the engine only if it is not already up, which is the
// idempotent form to use from a script.
func (a *API) EnsureEngine(params []ConfigParam) (json.RawMessage, error) {
	return a.c.Post(engineBase+"/ensure-running", engineBody(params))
}

// engineBody omits configParams entirely when there is nothing to set, since
// the endpoint treats an absent body and an empty list differently only by
// accident and the absent form is what the UI sends.
func engineBody(params []ConfigParam) any {
	if len(params) == 0 {
		return nil
	}
	return map[string]any{"configParams": params}
}

func (a *API) EngineLogs(limit int) ([]string, error) {
	params := map[string]string{}
	if limit > 0 {
		params["limit"] = strconv.Itoa(limit)
	}
	response, err := fetch[struct {
		Logs []string `json:"logs"`
	}](a, engineBase+"/logs", params, "engine logs")
	return response.Logs, err
}

// AvailableEngine is an engine a task may be bound to. Unlike the embedded
// engine above, these come from the proxy and are per-user.
type AvailableEngine struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type,omitempty"`
	Status  string `json:"status,omitempty"`
	Enabled bool   `json:"enabled,omitempty"`
	URL     string `json:"url,omitempty"`
}

func (a *API) AvailableEngines() ([]AvailableEngine, error) {
	response, err := fetch[struct {
		Items []AvailableEngine `json:"items"`
	}](a, byzerBase+"/available", nil, "engine list")
	return response.Items, err
}

func (a *API) EnabledEngine() (json.RawMessage, error) {
	return a.c.Get(byzerBase+"/getEnabledInfiniSQLEngine", nil)
}

// Instance is one process in the fleet: an API node, a worker, or a browser
// gateway.
type Instance struct {
	InstanceID string `json:"instanceId"`
	Role       string `json:"role"`
	Status     string `json:"status"`
	// EffectiveStatus is offline once the heartbeat has lapsed, even while
	// Status still reads whatever the instance last reported about itself.
	EffectiveStatus string `json:"effectiveStatus"`
	Host            string `json:"host,omitempty"`
	Version         string `json:"version,omitempty"`
	StartedAt       string `json:"startedAt,omitempty"`
	HeartbeatAt     string `json:"heartbeatAt,omitempty"`
}

func (a *API) Instances() ([]Instance, error) {
	raw, err := a.c.Get(runtimeBase+"/instances", nil)
	if err != nil {
		return nil, err
	}
	// The endpoint has returned both a bare array and an {instances: []}
	// wrapper across versions, so both are accepted.
	var instances []Instance
	if err := json.Unmarshal(raw, &instances); err == nil {
		return instances, nil
	}
	wrapped, err := decode[struct {
		Instances []Instance `json:"instances"`
	}](raw, nil, "instance list")
	return wrapped.Instances, err
}

// TaskExecution is the lease record: which worker holds a task and for how
// much longer. This is the answer to "why is my task not moving".
func (a *API) TaskExecution(taskID string) (json.RawMessage, error) {
	return a.c.Get(runtimeBase+"/tasks/"+taskID+"/execution", nil)
}

// WorkerReady answers the readiness probe. It returns 503 rather than an error
// body when the worker is not ready, so the status code is the answer.
func (a *API) WorkerReady() (json.RawMessage, error) {
	return a.c.Get(internalBase+"/ready", nil)
}

// Autoscaling is the queue-depth metric an autoscaler consumes.
func (a *API) Autoscaling() (json.RawMessage, error) {
	return a.c.Get(internalBase+"/autoscaling", nil)
}

// Drain tells this worker to stop taking new work and finish what it holds.
//
// The token goes in a header rather than the body or the query string; the
// server only checks it when INTERNAL_HTTP_TOKEN is configured, so a
// deployment without one accepts an unauthenticated drain.
func (a *API) Drain(token string) (json.RawMessage, error) {
	headers := map[string]string{}
	if token != "" {
		headers["x-internal-token"] = token
	}
	return a.c.DoWithHeaders("POST", internalBase+"/drain", nil, headers)
}

// LicenseStatus is the proxy's verdict, passed through. Synapse makes no
// licensing decision of its own; this endpoint exists so the frontend does not
// have to reach the proxy directly, which private deployments often block.
func (a *API) LicenseStatus() (json.RawMessage, error) {
	return a.c.Get(licenseBase+"/status", nil)
}

// RefreshLicense re-queries past the cache, for right after a new key is
// installed.
func (a *API) RefreshLicense() (json.RawMessage, error) {
	return a.c.Post(licenseBase+"/refresh", nil)
}

// LicenseLimits is the quota watermark. Unlike status it needs a login, on
// purpose: the limits are per-account.
func (a *API) LicenseLimits() (json.RawMessage, error) {
	return a.c.Get(licenseBase+"/limits", nil)
}
