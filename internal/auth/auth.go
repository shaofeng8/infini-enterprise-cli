// Package auth implements the login chain against the auth/proxy service.
//
// Infini itself does not issue credentials. The app backend only reports where
// the auth/proxy service lives (GET /api/auth/getAuthingPath); the proxy signs
// the JWT that every other API call carries.
package auth

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/chaozwn/infini-enterprise-cli/internal/client"
	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/config"
)

// Session is the result of a successful login.
type Session struct {
	Token     string `json:"token"`
	TokenType string `json:"token_type,omitempty"`
	ExpiresAt int64  `json:"expires_at,omitempty"`
	UserID    string `json:"user_id,omitempty"`
	Username  string `json:"username,omitempty"`
	Console   string `json:"console"`
}

// Profile is the proxy's view of the logged-in user.
type Profile struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	Super      bool   `json:"super"`
	CreateTime int64  `json:"createTime"`
}

type loginResponse struct {
	AccessToken   string `json:"access_token"`
	PasswordToken string `json:"passwordToken"`
	TokenType     string `json:"token_type"`
	ExpiresIn     int64  `json:"expires_in"`
	IssuedAt      int64  `json:"issued_at"`
	ExpiresAt     int64  `json:"expires_at"`
}

// HashPassword applies the same transformation the web client uses: trim, then
// md5 hex. The proxy compares this value verbatim against the stored password,
// so sending the plaintext can never authenticate.
func HashPassword(password string) string {
	sum := md5.Sum([]byte(strings.TrimSpace(password)))
	return hex.EncodeToString(sum[:])
}

// DiscoverConsole asks the app backend where the auth/proxy service lives.
func DiscoverConsole(server string) (string, error) {
	if server == "" {
		return "", cliexit.New(cliexit.CodeUsage, "server is required to discover the auth/proxy URL")
	}
	raw, err := client.NewAnonymous(server).Get("/api/auth/getAuthingPath", nil)
	if err != nil {
		return "", err
	}
	var payload struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", cliexit.New(cliexit.CodeBusiness, "cannot parse getAuthingPath response: %v", err)
	}
	if payload.Path == "" {
		return "", cliexit.New(cliexit.CodeBusiness, "server did not report an auth/proxy URL")
	}
	return strings.TrimRight(payload.Path, "/"), nil
}

// Brand returns the server's branding code, used by tenant-scoped deployments.
func Brand(server string) (json.RawMessage, error) {
	return client.NewAnonymous(server).Get("/api/auth/getBrand", nil)
}

// Login exchanges username + password for a JWT.
func Login(console, username, password, tenantCode string) (*Session, error) {
	body := map[string]string{
		"username": strings.TrimSpace(username),
		"password": HashPassword(password),
	}
	if tenantCode != "" {
		body["tenantCode"] = tenantCode
	}

	raw, err := client.NewAnonymous(console).Post("/auth/loginByUsername", body)
	if err != nil {
		return nil, err
	}

	var resp loginResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, cliexit.New(cliexit.CodeBusiness, "cannot parse login response: %v", err)
	}
	token := resp.AccessToken
	if token == "" {
		token = resp.PasswordToken
	}
	if token == "" {
		return nil, cliexit.New(cliexit.CodeAuth, "login response did not contain an access token")
	}

	expiresAt := resp.ExpiresAt
	if expiresAt == 0 && resp.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second).UnixMilli()
	}

	return &Session{
		Token:     token,
		TokenType: resp.TokenType,
		ExpiresAt: expiresAt,
		Username:  strings.TrimSpace(username),
		Console:   console,
	}, nil
}

// FetchProfile reads the logged-in user from the proxy.
func FetchProfile() (*Profile, error) {
	c, err := client.NewConsole()
	if err != nil {
		return nil, err
	}
	raw, err := c.Get("/user/getJwtProfile", nil)
	if err != nil {
		return nil, err
	}
	var profile Profile
	if err := json.Unmarshal(raw, &profile); err != nil {
		return nil, cliexit.New(cliexit.CodeBusiness, "cannot parse profile response: %v", err)
	}
	return &profile, nil
}

// Logout invalidates the session server-side, best effort.
func Logout() error {
	c, err := client.NewConsole()
	if err != nil {
		return err
	}
	_, err = c.Get("/auth/logout", nil)
	return err
}

// Persist stores the session in the active profile.
func Persist(session *Session) error {
	values := map[string]string{
		config.KeyToken:    session.Token,
		config.KeyConsole:  session.Console,
		config.KeyUsername: session.Username,
	}
	if session.ExpiresAt > 0 {
		values[config.KeyTokenExpiresAt] = time.UnixMilli(session.ExpiresAt).UTC().Format(time.RFC3339)
	}
	if session.UserID != "" {
		values[config.KeyUserID] = session.UserID
	}
	return config.Save(values)
}
