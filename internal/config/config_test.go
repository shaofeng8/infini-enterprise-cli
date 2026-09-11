package config

import (
	"path/filepath"
	"testing"
)

// isolateEnv clears every setting's environment variable.
//
// An operator of this CLI plausibly has INFINI_SERVER or INFINI_API_KEY
// exported in the shell they run the tests from, and env beats the profile by
// design, so without this the suite fails on their machine and nowhere else.
func isolateEnv(t *testing.T) {
	t.Helper()
	for _, key := range Keys {
		if name := envNames[key]; name != "" {
			t.Setenv(name, "")
		}
	}
	t.Setenv("INFINI_PROFILE", "")
}

// useTempConfig points the package at an isolated config file.
func useTempConfig(t *testing.T) {
	t.Helper()
	isolateEnv(t)
	t.Setenv("INFINI_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	if err := Init(""); err != nil {
		t.Fatalf("Init: %v", err)
	}
}

func TestGetPrecedence(t *testing.T) {
	useTempConfig(t)

	if got := Get(KeyDefaultOutput); got != "json" {
		t.Fatalf("default: got %q, want json", got)
	}

	if err := Save(map[string]string{KeyServer: "https://file.example.com"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := Get(KeyServer); got != "https://file.example.com" {
		t.Fatalf("profile value: got %q", got)
	}

	t.Setenv("INFINI_SERVER", "https://env.example.com")
	if got := Get(KeyServer); got != "https://env.example.com" {
		t.Fatalf("env should beat the profile: got %q", got)
	}

	Set(KeyServer, "https://flag.example.com")
	if got := Get(KeyServer); got != "https://flag.example.com" {
		t.Fatalf("flag should beat env: got %q", got)
	}
}

// Creating a profile must not change which profile commands resolve against;
// switching is `config profile use`.
func TestAddProfileDoesNotSwitchActive(t *testing.T) {
	useTempConfig(t)

	if err := Save(map[string]string{KeyAPIKey: "sk-default"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := AddProfile("staging", map[string]string{KeyServer: "https://staging.example.com"}); err != nil {
		t.Fatalf("AddProfile: %v", err)
	}

	if ActiveProfile() != DefaultProfile {
		t.Fatalf("active profile changed to %q", ActiveProfile())
	}
	if got := Get(KeyAPIKey); got != "sk-default" {
		t.Fatalf("credential lost after adding a profile: got %q", got)
	}

	if err := Init(""); err != nil {
		t.Fatalf("re-Init: %v", err)
	}
	if ActiveProfile() != DefaultProfile {
		t.Fatalf("active profile not persisted: got %q", ActiveProfile())
	}
}

func TestUseProfileSwitchesAndPersists(t *testing.T) {
	useTempConfig(t)

	if err := AddProfile("staging", map[string]string{KeyServer: "https://staging.example.com"}); err != nil {
		t.Fatalf("AddProfile: %v", err)
	}
	if err := UseProfile("staging"); err != nil {
		t.Fatalf("UseProfile: %v", err)
	}
	if err := Init(""); err != nil {
		t.Fatalf("re-Init: %v", err)
	}
	if ActiveProfile() != "staging" {
		t.Fatalf("got %q, want staging", ActiveProfile())
	}
	if got := Get(KeyServer); got != "https://staging.example.com" {
		t.Fatalf("got %q", got)
	}
	if err := UseProfile("ghost"); err == nil {
		t.Fatal("expected an error for an unknown profile")
	}
}

// A config file written by agent_infini uses a flat `global` block; it must stay
// readable so operators can migrate without hand-editing YAML.
func TestLegacyGlobalBlockMigrates(t *testing.T) {
	isolateEnv(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("INFINI_CONFIG", path)

	legacy := "global:\n  server: https://legacy.example.com\n  api-key: sk-legacy\n"
	if err := writeFile(path, legacy); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}
	if err := Init(""); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if got := Get(KeyServer); got != "https://legacy.example.com" {
		t.Fatalf("got %q", got)
	}
	if value, kind := Credential(); value != "sk-legacy" || kind != KeyAPIKey {
		t.Fatalf("got %q/%q", value, kind)
	}
}

// Credential prefers a JWT from `auth login` over a stored api-key, and an
// explicit --api-key over both.
func TestCredentialPreference(t *testing.T) {
	useTempConfig(t)

	if err := Save(map[string]string{KeyAPIKey: "sk-stored", KeyToken: "jwt-stored"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if value, kind := Credential(); value != "jwt-stored" || kind != KeyToken {
		t.Fatalf("got %q/%q, want the token", value, kind)
	}

	Set(KeyAPIKey, "sk-flag")
	if value, kind := Credential(); value != "sk-flag" || kind != KeyAPIKey {
		t.Fatalf("got %q/%q, want the flag api-key", value, kind)
	}
}

func TestMask(t *testing.T) {
	if got := Mask("short"); got != "****" {
		t.Fatalf("got %q", got)
	}
	if got := Mask("sk-abcdef1234567890"); got != "sk-a****7890" {
		t.Fatalf("got %q", got)
	}
}
