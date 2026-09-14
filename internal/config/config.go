// Package config resolves CLI settings from flags, environment and the profile file.
//
// A single binary has to talk to several Infini deployments (SaaS, staging, a
// customer's private cluster), so settings live in named profiles rather than a
// single flat block:
//
//	current-profile: prod
//	profiles:
//	  prod:
//	    server: https://app.infinisynapse.cn
//	    console: https://api.infinisynapse.cn/api
//	    token: <jwt from `infini-cli auth login`>
//
// Value precedence is flag > environment > active profile > built-in default.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	AppName        = "infini-cli"
	DefaultProfile = "default"
)

const (
	KeyServer         = "server"
	KeyConsole        = "console"
	KeyAPIKey         = "api-key"
	KeyToken          = "token"
	KeyTokenExpiresAt = "token-expires-at"
	KeyTenantCode     = "tenant-code"
	KeyUserID         = "user-id"
	KeyUsername       = "username"
	KeyPreferLanguage = "prefer-language"
	KeyDefaultOutput  = "default-output"
	KeyUpdateChannel  = "update-channel"
)

// Keys is the full set of persistable settings, in display order.
var Keys = []string{
	KeyServer,
	KeyConsole,
	KeyAPIKey,
	KeyToken,
	KeyTokenExpiresAt,
	KeyTenantCode,
	KeyUserID,
	KeyUsername,
	KeyPreferLanguage,
	KeyDefaultOutput,
	KeyUpdateChannel,
}

// SecretKeys are masked unless the caller explicitly asks for plaintext.
var SecretKeys = map[string]bool{
	KeyAPIKey: true,
	KeyToken:  true,
}

var defaults = map[string]string{
	KeyPreferLanguage: "zh_CN",
	KeyDefaultOutput:  "json",
}

var envNames = map[string]string{
	KeyServer:         "INFINI_SERVER",
	KeyConsole:        "INFINI_CONSOLE",
	KeyAPIKey:         "INFINI_API_KEY",
	KeyToken:          "INFINI_TOKEN",
	KeyTenantCode:     "INFINI_TENANT_CODE",
	KeyPreferLanguage: "INFINI_PREFER_LANGUAGE",
	KeyDefaultOutput:  "INFINI_DEFAULT_OUTPUT",
	KeyUpdateChannel:  "INFINI_UPDATE_CHANNEL",
}

const (
	// EnvAppBaseURL is Infini's public base URL. When server is not configured,
	// the CLI uses this as-is (scheme, host and port included).
	EnvAppBaseURL = "APP_BASE_URL"
	// DefaultServer is used when APP_BASE_URL is also unset.
	DefaultServer = "http://127.0.0.1:8088"
)

// EnvBuiltinSystemAccessKey is Infini's name for a process-level system
// api-key. Worker and local-dev shells already export it; treating it as a
// last-resort credential lets the CLI talk to a running instance without
// `auth login`.
const EnvBuiltinSystemAccessKey = "BUILTIN_SYSTEM_ACCESS_KEY"

// BakedAPIKey is empty in normal builds. A throwaway test binary may inject one
// with -ldflags so testers can skip login. It is last resort after the
// environment and the profile, and must never be committed as a real value.
var BakedAPIKey = ""

// SupportedLanguages mirrors the server's i18n directories.
var SupportedLanguages = []string{"en", "zh_CN", "ar", "ja", "ko", "ru"}

type store struct {
	CurrentProfile string                       `yaml:"current-profile,omitempty"`
	Profiles       map[string]map[string]string `yaml:"profiles,omitempty"`
	// Global keeps agent_infini-shaped files readable; it is migrated into the
	// default profile on load and never written back.
	Global map[string]string `yaml:"global,omitempty"`
}

var (
	loaded    store
	active    = DefaultProfile
	overrides = map[string]string{}
)

// Path returns the config file location, overridable for tests and CI.
func Path() string {
	if custom := os.Getenv("INFINI_CONFIG"); custom != "" {
		return custom
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join("."+AppName, "config.yaml")
	}
	return filepath.Join(home, "."+AppName, "config.yaml")
}

// Init loads the config file and selects the active profile.
//
// A missing file is not an error: `auth login` and `config set` must work on a
// machine that has never run the CLI before.
func Init(profileFlag string) error {
	loaded = store{Profiles: map[string]map[string]string{}}
	overrides = map[string]string{}

	if data, err := os.ReadFile(Path()); err == nil {
		var parsed store
		if err := yaml.Unmarshal(data, &parsed); err != nil {
			return fmt.Errorf("config file %s is not valid YAML: %w", Path(), err)
		}
		loaded = parsed
		if loaded.Profiles == nil {
			loaded.Profiles = map[string]map[string]string{}
		}
		if len(loaded.Global) > 0 {
			merged := map[string]string{}
			for k, v := range loaded.Global {
				merged[k] = v
			}
			for k, v := range loaded.Profiles[DefaultProfile] {
				merged[k] = v
			}
			loaded.Profiles[DefaultProfile] = merged
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("cannot read config file %s: %w", Path(), err)
	}

	switch {
	case profileFlag != "":
		active = profileFlag
	case os.Getenv("INFINI_PROFILE") != "":
		active = os.Getenv("INFINI_PROFILE")
	case loaded.CurrentProfile != "":
		active = loaded.CurrentProfile
	default:
		active = DefaultProfile
	}
	return nil
}

func ActiveProfile() string { return active }

func ProfileNames() []string {
	names := make([]string, 0, len(loaded.Profiles))
	for name := range loaded.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func HasProfile(name string) bool {
	_, ok := loaded.Profiles[name]
	return ok
}

// Get resolves a key through the full precedence chain.
func Get(key string) string {
	if v, ok := overrides[key]; ok && v != "" {
		return v
	}
	if env, ok := envNames[key]; ok {
		if v := os.Getenv(env); v != "" {
			return v
		}
	}
	if v, ok := loaded.Profiles[active][key]; ok && v != "" {
		return v
	}
	if key == KeyServer {
		return defaultServer()
	}
	// api-key only: Infini already injects this into the process. Last resort
	// after INFINI_API_KEY and a stored key, so a logged-in or configured
	// session cannot be silently replaced by the built-in one.
	if key == KeyAPIKey {
		if v := os.Getenv(EnvBuiltinSystemAccessKey); v != "" {
			return v
		}
		if BakedAPIKey != "" {
			return BakedAPIKey
		}
	}
	return defaults[key]
}

// defaultServer is the local-dev fallback: Infini's APP_BASE_URL as-is,
// or http://127.0.0.1:8088 when that is also unset. The value already carries
// the port, so APP_PORT is not consulted.
func defaultServer() string {
	if v := strings.TrimRight(strings.TrimSpace(os.Getenv(EnvAppBaseURL)), "/"); v != "" {
		return v
	}
	return DefaultServer
}

// Set records a process-lifetime override, used for global flags.
func Set(key, value string) {
	if value == "" {
		return
	}
	overrides[key] = value
}

// Save persists values into the active profile, creating it if needed.
func Save(values map[string]string) error {
	if loaded.Profiles == nil {
		loaded.Profiles = map[string]map[string]string{}
	}
	if loaded.Profiles[active] == nil {
		loaded.Profiles[active] = map[string]string{}
	}
	// Record the active profile explicitly, so a later `profile add` cannot
	// change which profile subsequent commands resolve against.
	if loaded.CurrentProfile == "" {
		loaded.CurrentProfile = active
	}
	for k, v := range values {
		if v == "" {
			delete(loaded.Profiles[active], k)
			continue
		}
		loaded.Profiles[active][k] = v
	}
	return flush()
}

// Unset removes keys from the active profile.
func Unset(keys ...string) error {
	if loaded.Profiles[active] == nil {
		return nil
	}
	for _, k := range keys {
		delete(loaded.Profiles[active], k)
	}
	return flush()
}

// UseProfile persists the default profile for subsequent invocations.
func UseProfile(name string) error {
	if !HasProfile(name) {
		return fmt.Errorf("profile %q does not exist", name)
	}
	loaded.CurrentProfile = name
	active = name
	return flush()
}

func AddProfile(name string, values map[string]string) error {
	if loaded.Profiles == nil {
		loaded.Profiles = map[string]map[string]string{}
	}
	if HasProfile(name) {
		return fmt.Errorf("profile %q already exists", name)
	}
	profile := map[string]string{}
	for k, v := range values {
		if v != "" {
			profile[k] = v
		}
	}
	loaded.Profiles[name] = profile
	// Creating a profile never switches to it; that is `profile use`.
	if loaded.CurrentProfile == "" {
		loaded.CurrentProfile = active
	}
	return flush()
}

func RemoveProfile(name string) error {
	if !HasProfile(name) {
		return fmt.Errorf("profile %q does not exist", name)
	}
	delete(loaded.Profiles, name)
	if loaded.CurrentProfile == name {
		loaded.CurrentProfile = ""
	}
	return flush()
}

func flush() error {
	// The file holds long-lived credentials; keep it owner-only and drop the
	// legacy block so we never write two sources of truth.
	loaded.Global = nil

	dir := filepath.Dir(Path())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("cannot create config directory %s: %w", dir, err)
	}
	data, err := yaml.Marshal(&loaded)
	if err != nil {
		return fmt.Errorf("cannot serialize config: %w", err)
	}
	if err := os.WriteFile(Path(), data, 0o600); err != nil {
		return fmt.Errorf("cannot write config file %s: %w", Path(), err)
	}
	return nil
}

// Snapshot returns the effective settings of the active profile for display.
func Snapshot(showSecrets bool) map[string]string {
	out := map[string]string{}
	for _, key := range Keys {
		value := Get(key)
		if value == "" {
			continue
		}
		if SecretKeys[key] && !showSecrets {
			value = Mask(value)
		}
		out[key] = value
	}
	return out
}

// Credential returns the bearer value and which kind of credential it is.
//
// Precedence, high to low:
//
//  1. --api-key
//  2. --token
//  3. a JWT from `auth login` (or INFINI_TOKEN)
//  4. a stored / INFINI_API_KEY api-key
//  5. BUILTIN_SYSTEM_ACCESS_KEY, which Infini already puts in the process
//     environment so a local CLI can skip login
func Credential() (value string, kind string) {
	if v := overrides[KeyAPIKey]; v != "" {
		return v, KeyAPIKey
	}
	if v := overrides[KeyToken]; v != "" {
		return v, KeyToken
	}
	if v := Get(KeyToken); v != "" {
		return v, KeyToken
	}
	if v := Get(KeyAPIKey); v != "" {
		return v, KeyAPIKey
	}
	return "", ""
}

func Server() string         { return strings.TrimRight(Get(KeyServer), "/") }
func Console() string        { return strings.TrimRight(Get(KeyConsole), "/") }
func PreferLanguage() string { return Get(KeyPreferLanguage) }
func DefaultOutput() string  { return Get(KeyDefaultOutput) }

// UpdateChannel is the base URL self-update reads its manifest from. There is
// no default on purpose: a private deployment mirrors releases on its own
// network, and a baked-in public URL would be wrong more often than right.
func UpdateChannel() string { return strings.TrimRight(Get(KeyUpdateChannel), "/") }

// Mask keeps enough of a secret visible to tell two credentials apart.
func Mask(secret string) string {
	if len(secret) <= 8 {
		return "****"
	}
	return secret[:4] + "****" + secret[len(secret)-4:]
}
