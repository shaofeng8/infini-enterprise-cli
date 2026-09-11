// Command release cross-compiles infini-cli and writes the channel manifest.
//
// The output directory is the channel: copy it to a web server, or to the
// customer's internal mirror, and `infini-cli update` works against it. The
// manifest's urls are relative for that reason — a mirror only has to copy
// the tree, not rewrite anything inside it.
//
// This is a Go program rather than a Makefile target because releases get cut
// from Windows as often as from CI here, and the Go toolchain is the one
// dependency both already have.
//
//	go run ./scripts/release --version 1.4.0 --notes "..."
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/chaozwn/infini-enterprise-cli/internal/selfupdate"
)

const (
	appName = "infini-cli"
	module  = "github.com/chaozwn/infini-enterprise-cli"
)

var platforms = []struct{ OS, Arch string }{
	{"linux", "amd64"}, {"linux", "arm64"},
	{"darwin", "amd64"}, {"darwin", "arm64"},
	{"windows", "amd64"}, {"windows", "arm64"},
}

func main() {
	version := flag.String("version", "", "release version, e.g. 1.4.0 (required)")
	notes := flag.String("notes", "", "release notes shown by `infini-cli update --check`")
	outDir := flag.String("out", "build", "directory to write the channel into")
	flag.Parse()

	if *version == "" {
		fail("--version is required")
	}
	if err := run(*version, *notes, *outDir); err != nil {
		fail("%v", err)
	}
}

func run(version, notes, outDir string) error {
	if err := os.RemoveAll(outDir); err != nil {
		return err
	}
	commit := gitCommit()
	built := time.Now().UTC().Format(time.RFC3339)
	ldflags := strings.Join([]string{
		"-s", "-w",
		fmt.Sprintf("-X %s/cmd.Version=%s", module, version),
		fmt.Sprintf("-X %s/cmd.Commit=%s", module, commit),
		fmt.Sprintf("-X %s/cmd.BuildDate=%s", module, built),
	}, " ")

	manifest := selfupdate.Manifest{
		Version:    version,
		ReleasedAt: built,
		Notes:      notes,
	}

	fmt.Printf("==> building %s %s (%s)\n", appName, version, commit)
	for _, platform := range platforms {
		name := appName
		if platform.OS == "windows" {
			name += ".exe"
		}
		// The version sits in the path so a channel can hold several releases
		// and a rollback is a one-line manifest edit rather than a rebuild.
		rel := filepath.ToSlash(filepath.Join(version, platform.OS+"-"+platform.Arch, name))
		abs := filepath.Join(outDir, filepath.FromSlash(rel))

		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		build := exec.Command("go", "build", "-trimpath", "-ldflags", ldflags, "-o", abs, ".")
		build.Env = append(os.Environ(), "GOOS="+platform.OS, "GOARCH="+platform.Arch, "CGO_ENABLED=0")
		build.Stderr = os.Stderr
		if err := build.Run(); err != nil {
			return fmt.Errorf("%s/%s: %w", platform.OS, platform.Arch, err)
		}

		digest, size, err := checksum(abs)
		if err != nil {
			return err
		}
		fmt.Printf("  %-14s %6.1f MB  %s\n", platform.OS+"/"+platform.Arch, float64(size)/(1<<20), digest[:12])

		manifest.Artifacts = append(manifest.Artifacts, selfupdate.Artifact{
			OS: platform.OS, Arch: platform.Arch, URL: rel, SHA256: digest, Size: size,
		})
	}

	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(outDir, selfupdate.ManifestName)
	if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("==> wrote %s\n", path)
	fmt.Printf("    serve %s/ and point clients at it:\n", outDir)
	fmt.Printf("    infini-cli config set update-channel <url>\n")
	return nil
}

func checksum(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()

	digest := sha256.New()
	size, err := io.Copy(digest, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(digest.Sum(nil)), size, nil
}

func gitCommit() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "none"
	}
	return strings.TrimSpace(string(out))
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "release: "+format+"\n", args...)
	os.Exit(1)
}
