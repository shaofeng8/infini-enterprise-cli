// Package selfupdate replaces the running binary with a newer one.
//
// The channel is a base URL holding a manifest and the artifacts it names.
// Nothing is baked in: a private deployment mirrors releases on its own
// network, often with no route to the public internet at all, so a
// hard-coded prefix would be wrong more often than right. The channel comes
// from the update-channel setting, INFINI_UPDATE_CHANNEL, or --channel.
//
// The channel is deliberately not the one agent_infini uses. Sharing it would
// let a consumer-CLI release land on an enterprise deployment.
package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

// ManifestName is the file the channel serves its release description as.
const ManifestName = "latest.json"

// Artifact is one platform's build.
type Artifact struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
	// URL may be absolute or relative to the channel.
	URL string `json:"url"`
	// SHA256 is required. An unverified binary is not worth installing, so a
	// manifest without it is rejected rather than trusted.
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size,omitempty"`
}

type Manifest struct {
	Version    string     `json:"version"`
	ReleasedAt string     `json:"releasedAt,omitempty"`
	Notes      string     `json:"notes,omitempty"`
	Artifacts  []Artifact `json:"artifacts"`
}

// For finds the artifact matching a platform.
func (m Manifest) For(goos, goarch string) (*Artifact, error) {
	for _, artifact := range m.Artifacts {
		if artifact.OS == goos && artifact.Arch == goarch {
			return &artifact, nil
		}
	}
	available := make([]string, 0, len(m.Artifacts))
	for _, artifact := range m.Artifacts {
		available = append(available, artifact.OS+"/"+artifact.Arch)
	}
	return nil, cliexit.Hint(
		cliexit.New(cliexit.CodeBusiness, "release %s has no build for %s/%s", m.Version, goos, goarch),
		"the manifest offers: %s", strings.Join(available, ", "),
	)
}

type Client struct {
	channel string
	http    *http.Client
}

func New(channel string) (*Client, error) {
	if channel == "" {
		return nil, cliexit.Hint(
			cliexit.New(cliexit.CodeUsage, "no update channel is configured"),
			"set one with `config set update-channel <base-url>`, export "+
				"INFINI_UPDATE_CHANNEL, or pass --channel; a private deployment "+
				"usually points this at its own mirror",
		)
	}
	return &Client{
		channel: strings.TrimRight(channel, "/"),
		http:    &http.Client{Timeout: 10 * time.Minute},
	}, nil
}

func (c *Client) Manifest() (*Manifest, error) {
	url := c.channel + "/" + ManifestName
	resp, err := c.http.Get(url)
	if err != nil {
		return nil, cliexit.New(cliexit.CodeNetwork, "cannot reach the update channel at %s: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, cliexit.New(cliexit.CodeNetwork,
			"the update channel answered HTTP %d for %s", resp.StatusCode, url)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, cliexit.New(cliexit.CodeNetwork, "cannot read the manifest: %v", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, cliexit.New(cliexit.CodeBusiness, "the manifest at %s is not valid JSON: %v", url, err)
	}
	if manifest.Version == "" {
		return nil, cliexit.New(cliexit.CodeBusiness, "the manifest at %s names no version", url)
	}
	return &manifest, nil
}

func (c *Client) artifactURL(artifact Artifact) string {
	if strings.HasPrefix(artifact.URL, "http://") || strings.HasPrefix(artifact.URL, "https://") {
		return artifact.URL
	}
	return c.channel + "/" + strings.TrimPrefix(artifact.URL, "/")
}

// Download fetches an artifact into dir and verifies its checksum.
//
// The download lands next to the binary it will replace rather than in the
// system temp directory, because a rename across filesystems is not atomic
// and the replacement has to be.
func (c *Client) Download(artifact Artifact, dir string) (string, error) {
	if artifact.SHA256 == "" {
		return "", cliexit.New(cliexit.CodeBusiness,
			"the manifest gives no checksum for %s/%s; refusing to install an unverified binary",
			artifact.OS, artifact.Arch)
	}

	url := c.artifactURL(artifact)
	resp, err := c.http.Get(url)
	if err != nil {
		return "", cliexit.New(cliexit.CodeNetwork, "cannot download %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", cliexit.New(cliexit.CodeNetwork, "downloading %s answered HTTP %d", url, resp.StatusCode)
	}

	file, err := os.CreateTemp(dir, ".infini-cli-update-*")
	if err != nil {
		return "", cliexit.Usage("cannot stage the download in %s: %v", dir, err)
	}
	staged := file.Name()

	digest := sha256.New()
	written, err := io.Copy(io.MultiWriter(file, digest), resp.Body)
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(staged)
		return "", cliexit.New(cliexit.CodeNetwork, "cannot write the download: %v", err)
	}

	if artifact.Size > 0 && written != artifact.Size {
		os.Remove(staged)
		return "", cliexit.New(cliexit.CodeBusiness,
			"the download is %d bytes but the manifest says %d", written, artifact.Size)
	}

	sum := hex.EncodeToString(digest.Sum(nil))
	if !strings.EqualFold(sum, artifact.SHA256) {
		os.Remove(staged)
		return "", cliexit.New(cliexit.CodeBusiness,
			"checksum mismatch: the download is %s but the manifest says %s", sum, artifact.SHA256)
	}
	return staged, nil
}

// rename is a seam so the restore path below can be tested. That path is the
// highest-stakes code in this package — a failed update must not leave the
// machine without a CLI — and there is no portable way to make the second
// rename fail on demand from outside.
var rename = os.Rename

// Replace swaps the running binary for the staged one.
//
// The current binary is moved aside rather than deleted first. A running
// executable cannot be overwritten on Windows, but it can be renamed, and
// keeping it means a failure halfway through leaves a working binary to fall
// back to instead of nothing.
func Replace(target, staged string) error {
	if err := os.Chmod(staged, 0o755); err != nil {
		return cliexit.Usage("cannot make the new binary executable: %v", err)
	}

	backup := target + ".old"
	os.Remove(backup)
	if err := rename(target, backup); err != nil {
		os.Remove(staged)
		return cliexit.Hint(
			cliexit.Usage("cannot move the current binary aside: %v", err),
			"the install directory may need elevated permissions",
		)
	}

	if err := rename(staged, target); err != nil {
		// Put the old binary back; a failed update must not uninstall the CLI.
		if restoreErr := rename(backup, target); restoreErr != nil {
			return cliexit.New(cliexit.CodeUsage,
				"cannot install the new binary (%v) and cannot restore the old one (%v); "+
					"the previous binary is at %s", err, restoreErr, backup)
		}
		os.Remove(staged)
		return cliexit.Usage("cannot install the new binary: %v", err)
	}

	// Best effort: Windows will refuse while the old image is still mapped,
	// and the next update cleans it up.
	os.Remove(backup)
	return nil
}

// Self reports the path of the running binary, resolved through any symlink so
// an update replaces the real file rather than a link to it.
func Self() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", cliexit.Usage("cannot locate the running binary: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path, nil
	}
	return resolved, nil
}

// Platform names the build this binary would need.
func Platform() string {
	return fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
}
