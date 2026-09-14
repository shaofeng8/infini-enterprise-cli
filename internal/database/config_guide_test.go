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
