package database

import (
	"strings"
	"testing"
)

func TestConfigExampleCoversEveryAddType(t *testing.T) {
	for _, dbType := range Types {
		if ConfigExample(dbType) == "" {
			t.Errorf("no example for %s", dbType)
		}
	}
}

func TestConfigGuideNamesRequiredKeys(t *testing.T) {
	// Agents copy keys from this catalog. If a required field is missing from
	// the prose, they will invent host/port instead of dm_host/dm_port.
	for _, key := range []string{
		"dm_host", "dm_port", "dm_username", "dm_password", "dm_database",
		"mysql_host", "sqlite_path", "pg_host", "kingbase_port",
		"file_system", "snowflake_host",
	} {
		if !strings.Contains(ConfigGuide, key) {
			t.Errorf("ConfigGuide is missing %s", key)
		}
	}
	if strings.Contains(ConfigGuide, `"path"`) && !strings.Contains(ConfigGuide, "sqlite_path") {
		t.Fatal("sqlite still documented as path")
	}
}

func TestGuideForDameng(t *testing.T) {
	guide, ok := GuideFor("dm")
	if !ok {
		t.Fatal("dm missing from the catalog")
	}
	want := []string{"dm_host", "dm_port", "dm_username", "dm_password", "dm_database"}
	if strings.Join(guide.Required, ",") != strings.Join(want, ",") {
		t.Fatalf("required: got %v", guide.Required)
	}
	if guide.DefaultPort != 5236 {
		t.Fatalf("port: got %d", guide.DefaultPort)
	}
	if !strings.Contains(guide.Example, "dm_host") {
		t.Fatalf("example: got %s", guide.Example)
	}
}

func TestGuideForEveryAddType(t *testing.T) {
	for _, dbType := range Types {
		guide, ok := GuideFor(dbType)
		if !ok {
			t.Errorf("no TypeGuide for add type %s", dbType)
			continue
		}
		if !guide.CanAdd {
			t.Errorf("%s should be addable", dbType)
		}
		if len(guide.Required) == 0 {
			t.Errorf("%s has no required keys", dbType)
		}
	}
}

func TestGbaseAlias(t *testing.T) {
	guide, ok := GuideFor("gbase")
	if !ok || guide.Type != "gbase8a" {
		t.Fatalf("gbase should resolve to gbase8a, got %+v ok=%v", guide, ok)
	}
}

func TestGuideForEveryTestType(t *testing.T) {
	for _, dbType := range TestTypes {
		if _, ok := GuideFor(dbType); !ok {
			t.Errorf("no TypeGuide for test type %s", dbType)
		}
	}
}
