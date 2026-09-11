package cmd

import (
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/spf13/cobra"
)

var licenseCmd = &cobra.Command{
	Use:   "license",
	Short: "Inspect authorization and quota",
	Long: `Synapse makes no licensing decision of its own; it passes through what the
proxy concludes. These commands therefore report the proxy's verdict, and a
failure here usually means the proxy is unreachable rather than that the
license is invalid.

` + "`status`" + ` needs no login, deliberately: an expired deployment still has to be
able to say so on its sign-in page. ` + "`limits`" + ` does need one, because quota is
per account.

When ` + "`agent new`" + ` is refused for quota, ` + "`license limits`" + ` is where the
number it hit is visible.`,
}

var licenseStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the authorization verdict",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := opsAPI()
		if err != nil {
			return err
		}
		raw, err := api.LicenseStatus()
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

var licenseRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Re-query the verdict, bypassing the cache",
	Long:  `Use this right after installing a new license.key.`,
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := opsAPI()
		if err != nil {
			return err
		}
		raw, err := api.RefreshLicense()
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

var licenseLimitsCmd = &cobra.Command{
	Use:   "limits",
	Short: "Show quota watermarks",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := opsAPI()
		if err != nil {
			return err
		}
		raw, err := api.LicenseLimits()
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

func init() {
	licenseCmd.AddCommand(licenseStatusCmd, licenseRefreshCmd, licenseLimitsCmd)
	rootCmd.AddCommand(licenseCmd)
}
