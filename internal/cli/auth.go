package cli

import (
	"fmt"
	"time"

	"github.com/havocked/bahn-cli/internal/app"
	"github.com/havocked/bahn-cli/internal/auth"
)

type AuthCmd struct {
	Login   AuthLoginCmd   `kong:"cmd,help='Authenticate via browser (OIDC flow).'"`
	Status  AuthStatusCmd  `kong:"cmd,help='Show current auth state.'"`
	Token   AuthTokenCmd   `kong:"cmd,help='Manually set a JWT token.'"`
	Refresh AuthRefreshCmd `kong:"cmd,help='Silently refresh the access token.'"`
	Clear   AuthClearCmd   `kong:"cmd,help='Remove stored credentials.'"`
}

// --- auth status ---

type AuthStatusCmd struct{}

type authStatusPayload struct {
	Authenticated bool    `json:"authenticated"`
	Username      string  `json:"username,omitempty"`
	Kundenkontoid string  `json:"kundenkontoid,omitempty"`
	Sub           string  `json:"sub,omitempty"`
	ExpiresAt     string  `json:"expiresAt,omitempty"`
	Expired       bool    `json:"expired,omitempty"`
	Remaining     string  `json:"remaining,omitempty"`
}

func (cmd *AuthStatusCmd) Run(ctx *app.Context) error {
	tokens, err := auth.LoadTokens()
	if err != nil {
		return err
	}
	if tokens == nil {
		return ctx.Output.Emit(
			authStatusPayload{Authenticated: false},
			[]string{"Not authenticated. Run `bahn auth login` or `bahn auth token <jwt>`."},
		)
	}

	remaining := tokens.TimeRemaining()
	expired := tokens.IsExpired()
	remainingStr := ""
	if !expired {
		remainingStr = remaining.Round(time.Second).String()
	}

	payload := authStatusPayload{
		Authenticated: !expired,
		Username:      tokens.Username,
		Kundenkontoid: tokens.Kundenkontoid,
		Sub:           tokens.Sub,
		ExpiresAt:     tokens.ExpiresAt.Format(time.RFC3339),
		Expired:       expired,
		Remaining:     remainingStr,
	}

	human := []string{
		fmt.Sprintf("User: %s", tokens.Username),
		fmt.Sprintf("Account: %s", tokens.Kundenkontoid),
	}
	if expired {
		human = append(human, "Token: expired")
	} else {
		human = append(human, fmt.Sprintf("Token: valid (%s remaining)", remainingStr))
	}

	return ctx.Output.Emit(payload, human)
}

// --- auth token (manual) ---

type AuthTokenCmd struct {
	JWT string `arg:"" help:"JWT access token to store."`
}

func (cmd *AuthTokenCmd) Run(ctx *app.Context) error {
	tokens, err := auth.TokenSetFromJWT(cmd.JWT)
	if err != nil {
		return fmt.Errorf("invalid JWT: %w", err)
	}
	if err := auth.SaveTokens(tokens); err != nil {
		return err
	}

	remaining := tokens.TimeRemaining().Round(time.Second)
	payload := map[string]any{
		"status":        "ok",
		"username":      tokens.Username,
		"kundenkontoid": tokens.Kundenkontoid,
		"expiresAt":     tokens.ExpiresAt.Format(time.RFC3339),
		"remaining":     remaining.String(),
	}
	human := []string{
		fmt.Sprintf("✓ Authenticated as %s", tokens.Username),
		fmt.Sprintf("  Token expires in %s", remaining),
		"  Note: token has 5 min lifetime. Use `bahn auth login` for persistent auth.",
	}
	return ctx.Output.Emit(payload, human)
}

// --- auth login (OIDC) ---

type AuthLoginCmd struct{}

func (cmd *AuthLoginCmd) Run(ctx *app.Context) error {
	onStatus := func(msg string) {
		ctx.Output.Infof("%s", msg)
	}
	tokens, err := auth.Login(onStatus)
	if err != nil {
		return err
	}
	if err := auth.SaveTokens(tokens); err != nil {
		return err
	}

	remaining := tokens.TimeRemaining().Round(time.Second)
	payload := map[string]any{
		"status":        "ok",
		"username":      tokens.Username,
		"kundenkontoid": tokens.Kundenkontoid,
		"expiresAt":     tokens.ExpiresAt.Format(time.RFC3339),
		"remaining":     remaining.String(),
	}
	human := []string{
		fmt.Sprintf("✓ Authenticated as %s", tokens.Username),
		fmt.Sprintf("  Account: %s", tokens.Kundenkontoid),
		fmt.Sprintf("  Token valid for %s", remaining),
	}
	return ctx.Output.Emit(payload, human)
}

// --- auth refresh ---

type AuthRefreshCmd struct{}

func (cmd *AuthRefreshCmd) Run(ctx *app.Context) error {
	return app.WrapExit(1, fmt.Errorf("not implemented yet — silent refresh coming in step 4"))
}

// --- auth clear ---

type AuthClearCmd struct{}

func (cmd *AuthClearCmd) Run(ctx *app.Context) error {
	if err := auth.ClearTokens(); err != nil {
		return err
	}
	return ctx.Output.Emit(
		map[string]string{"status": "ok"},
		[]string{"Credentials cleared."},
	)
}
