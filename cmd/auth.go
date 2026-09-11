package cmd

import (
	"github.com/chaozwn/infini-enterprise-cli/internal/auth"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/config"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Log in, inspect and clear credentials",
	Long: `Infini does not issue credentials itself. The app backend only reports where
the auth/proxy service lives; that service signs the JWT used by every other
command. ` + "`auth login`" + ` performs both steps and stores the result in the
active profile.`,
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Exchange username and password for a JWT",
	Long: `Discovers the auth/proxy URL from --server when it is not configured, logs in,
verifies the token, and stores it in the active profile.

The password is prompted without echo unless --password-stdin is used. Passing a
password as a flag is intentionally unsupported: it would land in shell history
and in the process list.`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		username, _ := cmd.Flags().GetString("username")
		passwordStdin, _ := cmd.Flags().GetBool("password-stdin")

		if config.Server() == "" {
			return cliexit.Hint(
				cliexit.New(cliexit.CodeUsage, "server is not configured"),
				"run `%s config set server <url>` or pass --server", config.AppName,
			)
		}

		// The proxy URL is usually unknown on a fresh machine; ask the backend
		// rather than making the operator hunt for it.
		console := config.Console()
		if console == "" {
			discovered, err := auth.DiscoverConsole(config.Server())
			if err != nil {
				return cliexit.Hint(err,
					"pass --console explicitly if the auth/proxy URL cannot be discovered")
			}
			output.Note("Discovered auth/proxy service: %s", discovered)
			console = discovered
		}

		if username == "" {
			prompted, err := promptLine("Username or email")
			if err != nil {
				return cliexit.Hint(cliexit.Wrap(cliexit.CodeUsage, err), "pass --username")
			}
			username = prompted
		}

		var password string
		if passwordStdin {
			read, err := readAllStdin()
			if err != nil {
				return cliexit.Wrap(cliexit.CodeUsage, err)
			}
			password = read
		} else {
			prompted, err := promptSecret("Password")
			if err != nil {
				return cliexit.Hint(cliexit.Wrap(cliexit.CodeUsage, err),
					"pipe the password and pass --password-stdin in non-interactive environments")
			}
			password = prompted
		}
		if password == "" {
			return cliexit.Usage("password must not be empty")
		}

		session, err := auth.Login(console, username, password, config.Get(config.KeyTenantCode))
		if err != nil {
			return err
		}

		// Persist before verifying: FetchProfile reads the credential through
		// the same resolution chain every other command uses.
		if err := auth.Persist(session); err != nil {
			return cliexit.Wrap(cliexit.CodeBusiness, err)
		}
		config.Set(config.KeyToken, session.Token)
		config.Set(config.KeyConsole, console)

		result := map[string]any{
			"profile":   config.ActiveProfile(),
			"server":    config.Server(),
			"console":   console,
			"username":  session.Username,
			"expiresAt": session.ExpiresAt,
		}
		if profile, err := auth.FetchProfile(); err == nil {
			result["userId"] = profile.ID
			result["super"] = profile.Super
			session.UserID = profile.ID
			if err := auth.Persist(session); err != nil {
				return cliexit.Wrap(cliexit.CodeBusiness, err)
			}
		} else {
			output.Note("Logged in, but the profile lookup failed: %v", err)
		}
		return output.Success(result, nil, nil)
	},
}

var authWhoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show the user behind the stored credential",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		profile, err := auth.FetchProfile()
		if err != nil {
			return err
		}
		return output.Success(profile, nil, nil)
	},
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show local credential state without calling the server",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		credential, kind := config.Credential()
		status := map[string]any{
			"profile":       config.ActiveProfile(),
			"server":        config.Server(),
			"console":       config.Console(),
			"authenticated": credential != "",
		}
		if credential != "" {
			status["credentialKind"] = kind
			status["credential"] = config.Mask(credential)
		}
		if expires := config.Get(config.KeyTokenExpiresAt); expires != "" {
			status["tokenExpiresAt"] = expires
		}
		if username := config.Get(config.KeyUsername); username != "" {
			status["username"] = username
		}
		return output.Success(status, nil, nil)
	},
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Invalidate the session and clear the stored credential",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := auth.Logout(); err != nil {
			// A server-side failure must not leave the credential on disk.
			output.Note("Server-side logout failed (%v); clearing the local credential anyway", err)
		}
		if err := config.Unset(config.KeyToken, config.KeyTokenExpiresAt); err != nil {
			return cliexit.Wrap(cliexit.CodeBusiness, err)
		}
		return output.Success(map[string]string{"profile": config.ActiveProfile()}, nil, nil)
	},
}

var authModeCmd = &cobra.Command{
	Use:   "mode",
	Short: "Show the deployment's auth/proxy URL and brand code",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		server := config.Server()
		if server == "" {
			return cliexit.Hint(
				cliexit.New(cliexit.CodeUsage, "server is not configured"),
				"run `%s config set server <url>` or pass --server", config.AppName,
			)
		}
		result := map[string]any{"server": server}

		console, err := auth.DiscoverConsole(server)
		if err != nil {
			return err
		}
		result["console"] = console

		if brand, err := auth.Brand(server); err == nil {
			result["brand"] = normalizeRaw(brand)
		}
		return output.Success(result, nil, nil)
	},
}

func init() {
	authLoginCmd.Flags().String("username", "", "Username or email to log in as")
	authLoginCmd.Flags().Bool("password-stdin", false, "Read the password from stdin instead of prompting")

	authCmd.AddCommand(authLoginCmd, authWhoamiCmd, authStatusCmd, authLogoutCmd, authModeCmd)
	rootCmd.AddCommand(authCmd)
}
