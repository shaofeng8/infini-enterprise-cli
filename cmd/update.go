package cmd

import (
	"path/filepath"
	"runtime"

	"github.com/chaozwn/infini-enterprise-cli/internal/config"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/chaozwn/infini-enterprise-cli/internal/selfupdate"
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update this binary from a release channel",
	Long: `Reads ` + selfupdate.ManifestName + ` from the configured channel, downloads the
build for this platform, verifies its checksum, and replaces this binary.

There is no built-in channel. A private deployment mirrors releases on its own
network — often with no route out at all — so a baked-in public URL would be
wrong more often than right. Point it somewhere first:

  infini-cli config set update-channel https://releases.example.com/infini-cli
  infini-cli update --check
  infini-cli update

The channel also accepts INFINI_UPDATE_CHANNEL and --channel, which is what a
one-off or a CI job usually uses.

The manifest looks like this, and the checksum is mandatory: an artifact
without one is refused rather than installed unverified.

  {
    "version": "1.4.0",
    "releasedAt": "2026-09-11T00:00:00Z",
    "notes": "...",
    "artifacts": [
      {"os": "linux", "arch": "amd64", "url": "1.4.0/linux-amd64/infini-cli",
       "sha256": "...", "size": 18874368}
    ]
  }

A relative url is resolved against the channel, so a mirror only has to copy
the directory tree.`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		channel, _ := flags.GetString("channel")
		if channel == "" {
			channel = config.UpdateChannel()
		}
		checkOnly, _ := flags.GetBool("check")

		client, err := selfupdate.New(channel)
		if err != nil {
			return err
		}
		manifest, err := client.Manifest()
		if err != nil {
			return err
		}

		current := Version
		uptodate := manifest.Version == current
		report := map[string]any{
			"current":    current,
			"latest":     manifest.Version,
			"upToDate":   uptodate,
			"platform":   selfupdate.Platform(),
			"releasedAt": manifest.ReleasedAt,
			"notes":      manifest.Notes,
		}

		if checkOnly {
			return output.Success(report, []string{"FIELD", "VALUE"}, func() [][]string {
				return [][]string{
					{"current", current},
					{"latest", manifest.Version},
					{"upToDate", boolText(uptodate)},
					{"releasedAt", manifest.ReleasedAt},
				}
			})
		}

		// Version strings are compared for equality, not ordered: a private
		// channel may pin an older build deliberately, and refusing to install
		// it because it looks like a downgrade would be wrong.
		force, _ := flags.GetBool("force")
		if uptodate && !force {
			output.Note("already on %s", current)
			return output.Success(report, nil, nil)
		}

		artifact, err := manifest.For(runtime.GOOS, runtime.GOARCH)
		if err != nil {
			return err
		}

		target, err := selfupdate.Self()
		if err != nil {
			return err
		}
		if err := confirm("Replace " + target + " with " + manifest.Version + "?"); err != nil {
			return err
		}

		output.Note("downloading %s for %s ...", manifest.Version, selfupdate.Platform())
		staged, err := client.Download(*artifact, filepath.Dir(target))
		if err != nil {
			return err
		}
		if err := selfupdate.Replace(target, staged); err != nil {
			return err
		}

		report["installed"] = manifest.Version
		report["path"] = target
		return output.Success(report, nil, nil)
	},
}

func boolText(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func init() {
	updateCmd.Flags().Bool("check", false, "Report the latest version without installing it")
	updateCmd.Flags().Bool("force", false, "Reinstall even when the versions match")
	updateCmd.Flags().String("channel", "", "Release channel base URL, overriding the configured one")
	rootCmd.AddCommand(updateCmd)
}
