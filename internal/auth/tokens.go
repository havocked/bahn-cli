package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/havocked/bahn-cli/internal/config"
)

// TokenSet holds the current authentication tokens.
type TokenSet struct {
	AccessToken   string    `json:"accessToken"`
	IDToken       string    `json:"idToken"`
	ExpiresAt     time.Time `json:"expiresAt"`
	Kundenkontoid string    `json:"kundenkontoid"`
	Sub           string    `json:"sub"`
	Username      string    `json:"username"`
}

// Claims represents parsed JWT claims we care about.
type Claims struct {
	Exp               int64    `json:"exp"`
	Iat               int64    `json:"iat"`
	Sub               string   `json:"sub"`
	Kundenkontoid     string   `json:"kundenkontoid"`
	PreferredUsername string   `json:"preferred_username"`
	Scope             string   `json:"scope"`
	Groups            []string `json:"groups"`
}

// IsExpired returns true if the token is past its expiry.
func (t *TokenSet) IsExpired() bool {
	return time.Now().After(t.ExpiresAt)
}

// NeedsRefresh returns true if the token expires within 30 seconds.
func (t *TokenSet) NeedsRefresh() bool {
	return time.Now().After(t.ExpiresAt.Add(-30 * time.Second))
}

// TimeRemaining returns how long until the token expires.
func (t *TokenSet) TimeRemaining() time.Duration {
	return time.Until(t.ExpiresAt)
}

// ParseJWT decodes a JWT payload without signature validation.
// We trust the source (Keycloak) so we only need to read claims.
func ParseJWT(token string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid JWT: expected 3 parts")
	}
	payload := parts[1]
	// Add padding
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}
	data, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("invalid JWT payload: %w", err)
	}
	var claims Claims
	if err := json.Unmarshal(data, &claims); err != nil {
		return nil, fmt.Errorf("invalid JWT claims: %w", err)
	}
	return &claims, nil
}

// TokenSetFromJWT creates a TokenSet from a raw JWT access token.
func TokenSetFromJWT(accessToken string) (*TokenSet, error) {
	claims, err := ParseJWT(accessToken)
	if err != nil {
		return nil, err
	}
	return &TokenSet{
		AccessToken:   accessToken,
		ExpiresAt:     time.Unix(claims.Exp, 0),
		Kundenkontoid: claims.Kundenkontoid,
		Sub:           claims.Sub,
		Username:      claims.PreferredUsername,
	}, nil
}

// tokensPath returns ~/.config/bahn-cli/tokens.json
func tokensPath() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "tokens.json"), nil
}

// SaveTokens stores the token set to disk.
func SaveTokens(tokens *TokenSet) error {
	path, err := tokensPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// LoadTokens reads the stored token set from disk.
func LoadTokens() (*TokenSet, error) {
	path, err := tokensPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var tokens TokenSet
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, err
	}
	return &tokens, nil
}

// ClearTokens removes stored tokens.
func ClearTokens() error {
	path, err := tokensPath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
