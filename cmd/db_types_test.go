package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/chaozwn/infini-enterprise-cli/internal/output"
)

func TestDbTypesCommandRegistered(t *testing.T) {
	for _, c := range dbCmd.Commands() {
		if c.Name() == "types" {
			return
		}
	}
	t.Fatal("db types is not registered")
}

func TestDbTypesDamengJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	output.SetWriters(&stdout, &stderr)
	t.Cleanup(func() { output.SetWriters(os.Stdout, os.Stderr) })

	if err := dbTypesCmd.RunE(dbTypesCmd, []string{"dm"}); err != nil {
		t.Fatal(err)
	}
	var env output.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("json: %v\n%s", err, stdout.String())
	}
	if !env.Success {
		t.Fatalf("not success: %s", stdout.String())
	}
	raw, err := json.Marshal(env.Data)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, key := range []string{"dm_host", "dm_port", "dm_username", "dm_password", "dm_database"} {
		if !strings.Contains(body, key) {
			t.Errorf("missing %s in %s", key, body)
		}
	}
}
