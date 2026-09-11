package cmd

import (
	"runtime"

	"github.com/chaozwn/infini-enterprise-cli/internal/config"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/spf13/cobra"
)

// Injected at build time via -ldflags; see Makefile.
var (
	Version   = "0.1.0"
	Commit    = "none"
	BuildDate = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version and build information",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		return output.Success(map[string]string{
			"name":      config.AppName,
			"version":   Version,
			"commit":    Commit,
			"built":     BuildDate,
			"goVersion": runtime.Version(),
			"platform":  runtime.GOOS + "/" + runtime.GOARCH,
		}, nil, nil)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
	rootCmd.Version = Version
}
