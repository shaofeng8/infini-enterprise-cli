package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

func channel(t *testing.T, files map[string]string) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, present := files[strings.TrimPrefix(r.URL.Path, "/")]
		if !present {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	client, err := New(server.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

func sum(contents string) string {
	digest := sha256.Sum256([]byte(contents))
	return hex.EncodeToString(digest[:])
}

// Without a channel the command has to say what to set, not fail obscurely:
// nothing is baked in on purpose.
func TestNoChannelIsAUsageErrorWithAHint(t *testing.T) {
	_, err := New("")
	if cliexit.CodeOf(err) != cliexit.CodeUsage {
		t.Fatalf("code: got %d", cliexit.CodeOf(err))
	}
	if !strings.Contains(cliexit.HintOf(err), "update-channel") {
		t.Fatalf("hint: got %q", cliexit.HintOf(err))
	}
}

func TestManifestIsReadFromTheChannelRoot(t *testing.T) {
	client := channel(t, map[string]string{
		ManifestName: `{"version":"1.4.0","artifacts":[{"os":"linux","arch":"amd64","url":"x","sha256":"aa"}]}`,
	})
	manifest, err := client.Manifest()
	if err != nil {
		t.Fatalf("Manifest: %v", err)
	}
	if manifest.Version != "1.4.0" {
		t.Fatalf("version: got %q", manifest.Version)
	}
}

func TestAManifestWithoutAVersionIsRejected(t *testing.T) {
	client := channel(t, map[string]string{ManifestName: `{"artifacts":[]}`})
	if _, err := client.Manifest(); cliexit.CodeOf(err) != cliexit.CodeBusiness {
		t.Fatalf("got %v", err)
	}
}

func TestMissingPlatformListsWhatIsAvailable(t *testing.T) {
	manifest := Manifest{
		Version: "1.4.0",
		Artifacts: []Artifact{
			{OS: "linux", Arch: "amd64"},
			{OS: "darwin", Arch: "arm64"},
		},
	}
	_, err := manifest.For("windows", "arm64")
	if err == nil {
		t.Fatal("expected a failure")
	}
	hint := cliexit.HintOf(err)
	if !strings.Contains(hint, "linux/amd64") || !strings.Contains(hint, "darwin/arm64") {
		t.Fatalf("hint: got %q", hint)
	}
}

// An artifact with no checksum is refused rather than installed unverified.
func TestDownloadRequiresAChecksum(t *testing.T) {
	client := channel(t, map[string]string{"build": "binary"})
	_, err := client.Download(Artifact{OS: "linux", Arch: "amd64", URL: "build"}, t.TempDir())
	if cliexit.CodeOf(err) != cliexit.CodeBusiness {
		t.Fatalf("got %v", err)
	}
}

func TestDownloadRejectsAChecksumMismatchAndLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	client := channel(t, map[string]string{"build": "binary"})
	_, err := client.Download(Artifact{URL: "build", SHA256: sum("something else")}, dir)
	if cliexit.CodeOf(err) != cliexit.CodeBusiness {
		t.Fatalf("got %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("the staged file should have been removed, found %v", entries)
	}
}

func TestDownloadRejectsASizeMismatch(t *testing.T) {
	client := channel(t, map[string]string{"build": "binary"})
	_, err := client.Download(Artifact{URL: "build", SHA256: sum("binary"), Size: 999}, t.TempDir())
	if cliexit.CodeOf(err) != cliexit.CodeBusiness {
		t.Fatalf("got %v", err)
	}
}

func TestDownloadVerifiesAndStagesNextToTheTarget(t *testing.T) {
	dir := t.TempDir()
	client := channel(t, map[string]string{"1.4.0/linux-amd64/infini-cli": "binary"})
	staged, err := client.Download(Artifact{
		URL: "1.4.0/linux-amd64/infini-cli", SHA256: sum("binary"), Size: 6,
	}, dir)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	// Staging in the install directory is what makes the rename atomic.
	if filepath.Dir(staged) != dir {
		t.Fatalf("staged in %q, want %q", filepath.Dir(staged), dir)
	}
	contents, _ := os.ReadFile(staged)
	if string(contents) != "binary" {
		t.Fatalf("contents: got %q", contents)
	}
}

// An absolute url in the manifest wins, so a manifest can point at a CDN
// while still being served from the channel.
func TestAbsoluteArtifactURLsAreUsedAsGiven(t *testing.T) {
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("elsewhere"))
	}))
	t.Cleanup(elsewhere.Close)

	client := channel(t, map[string]string{})
	staged, err := client.Download(Artifact{
		URL: elsewhere.URL + "/build", SHA256: sum("elsewhere"),
	}, t.TempDir())
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	contents, _ := os.ReadFile(staged)
	if string(contents) != "elsewhere" {
		t.Fatalf("contents: got %q", contents)
	}
}

func TestReplaceSwapsTheBinary(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "infini-cli")
	staged := filepath.Join(dir, "staged")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Replace(target, staged); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	contents, _ := os.ReadFile(target)
	if string(contents) != "new" {
		t.Fatalf("contents: got %q", contents)
	}
}

// A failed install must not leave the machine without a CLI.
func TestReplaceRestoresTheOldBinaryWhenInstallFails(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "infini-cli")
	staged := filepath.Join(dir, "staged")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Fail the install rename only, leaving the binary already moved aside.
	original := rename
	calls := 0
	rename = func(from, to string) error {
		calls++
		if calls == 2 {
			return errors.New("disk full")
		}
		return original(from, to)
	}
	t.Cleanup(func() { rename = original })

	if err := Replace(target, staged); err == nil {
		t.Fatal("expected the install to fail")
	}
	contents, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("the old binary should have been restored: %v", err)
	}
	if string(contents) != "old" {
		t.Fatalf("contents: got %q", contents)
	}
}

// If even the restore fails, the message has to say where the old binary is,
// because that is the only way back.
func TestReplaceReportsWhereTheOldBinaryIsWhenRestoreAlsoFails(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "infini-cli")
	staged := filepath.Join(dir, "staged")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	original := rename
	calls := 0
	rename = func(from, to string) error {
		calls++
		if calls >= 2 {
			return errors.New("disk full")
		}
		return original(from, to)
	}
	t.Cleanup(func() { rename = original })

	err := Replace(target, staged)
	if err == nil {
		t.Fatal("expected a failure")
	}
	if !strings.Contains(err.Error(), target+".old") {
		t.Fatalf("the message must point at the backup, got %q", err)
	}
}
