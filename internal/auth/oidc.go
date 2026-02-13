package auth

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/pkg/browser"
)

const (
	keycloakBaseURL = "https://accounts.bahn.de/auth/realms/db/protocol/openid-connect"
	clientID        = "kf_web"
	scopes          = "openid vendo"
	realRedirectURI = "https://www.bahn.de/.resources/bahn-common-light/webresources/assets/html/auth.v2.html"
	pollInterval    = 500 * time.Millisecond
	pollTimeout     = 120 * time.Second
)

// Login performs the OIDC browser login flow.
// On macOS, it auto-detects the auth code from the browser tab URL.
// On other platforms, falls back to paste-URL mode.
func Login(onStatus func(string)) (*TokenSet, error) {
	verifier, challenge, err := generatePKCE()
	if err != nil {
		return nil, fmt.Errorf("PKCE generation failed: %w", err)
	}
	state := randomString(32)

	if runtime.GOOS == "darwin" {
		tokens, err := loginWithBrowserPoll(verifier, challenge, state, onStatus)
		if err == nil {
			return tokens, nil
		}
		// If AppleScript polling fails, fall back to paste
		if onStatus != nil {
			onStatus(fmt.Sprintf("Auto-detect failed (%v), falling back to manual mode...", err))
		}
	}

	return loginWithPaste(verifier, challenge, state, onStatus)
}

// loginWithBrowserPoll opens the auth URL and polls the browser tab URL
// via AppleScript until it contains the auth code.
func loginWithBrowserPoll(verifier, challenge, state string, onStatus func(string)) (*TokenSet, error) {
	authURL := buildAuthURL(realRedirectURI, state, challenge)

	// Detect which browser to use
	browserName := detectBrowser()
	if browserName == "" {
		return nil, fmt.Errorf("no supported browser found")
	}

	if onStatus != nil {
		onStatus(fmt.Sprintf("Opening %s for login...", browserName))
	}
	if err := browser.OpenURL(authURL); err != nil {
		return nil, fmt.Errorf("failed to open browser: %w", err)
	}

	if onStatus != nil {
		onStatus("Log in to your DB account. Waiting for authentication...")
	}

	// Poll browser tab URL for the redirect with our state
	deadline := time.Now().Add(pollTimeout)
	for time.Now().Before(deadline) {
		time.Sleep(pollInterval)

		tabURL, err := getBrowserTabURL(browserName)
		if err != nil {
			continue // Browser might not be ready yet
		}

		// Check if the URL contains our state parameter
		if !strings.Contains(tabURL, state) {
			continue
		}

		// Extract code from fragment
		code, returnedState, err := extractFragmentParams(tabURL)
		if err != nil {
			continue
		}
		if returnedState != state {
			continue
		}

		if onStatus != nil {
			onStatus("Authentication detected! Exchanging code for tokens...")
		}
		return exchangeCode(code, verifier, realRedirectURI)
	}

	return nil, fmt.Errorf("timed out waiting for authentication")
}

// detectBrowser returns the name of a running/available browser on macOS.
func detectBrowser() string {
	browsers := []string{"Google Chrome", "Brave Browser", "Chromium"}
	for _, b := range browsers {
		// Check if app exists
		cmd := exec.Command("osascript", "-e",
			fmt.Sprintf(`tell application "System Events" to (name of processes) contains "%s"`, b))
		out, err := cmd.Output()
		if err == nil && strings.TrimSpace(string(out)) == "true" {
			return b
		}
	}
	// Default to Google Chrome
	return "Google Chrome"
}

// getBrowserTabURL reads the current tab's full URL (including fragment)
// from a Chromium-based browser via AppleScript.
func getBrowserTabURL(browserName string) (string, error) {
	script := fmt.Sprintf(`tell application "%s"
	if (count of windows) > 0 then
		execute active tab of front window javascript "document.location.href"
	end if
end tell`, browserName)

	cmd := exec.Command("osascript", "-e", script)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// loginWithPaste is the fallback: user pastes the callback URL.
func loginWithPaste(verifier, challenge, state string, onStatus func(string)) (*TokenSet, error) {
	authURL := buildAuthURL(realRedirectURI, state, challenge)

	if onStatus != nil {
		onStatus("Opening browser for login...")
	}
	_ = browser.OpenURL(authURL)

	if onStatus != nil {
		onStatus("")
		onStatus("After logging in, you'll be redirected to bahn.de.")
		onStatus("Copy the FULL URL from your browser's address bar and paste it here:")
		onStatus("")
	}

	fmt.Fprint(os.Stderr, "> ")
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return nil, fmt.Errorf("no input received")
	}
	pastedURL := strings.TrimSpace(scanner.Text())
	if pastedURL == "" {
		return nil, fmt.Errorf("empty URL")
	}

	code, returnedState, err := extractFragmentParams(pastedURL)
	if err != nil {
		return nil, err
	}
	if returnedState != state {
		return nil, fmt.Errorf("state mismatch: possible CSRF attack")
	}

	if onStatus != nil {
		onStatus("Exchanging auth code for tokens...")
	}
	return exchangeCode(code, verifier, realRedirectURI)
}

// extractFragmentParams pulls code and state from a URL fragment.
func extractFragmentParams(rawURL string) (code, state string, err error) {
	var fragment string
	if idx := strings.Index(rawURL, "#"); idx >= 0 {
		fragment = rawURL[idx+1:]
	} else {
		fragment = rawURL
	}

	params, err := url.ParseQuery(fragment)
	if err != nil {
		return "", "", fmt.Errorf("invalid URL fragment: %w", err)
	}

	if errParam := params.Get("error"); errParam != "" {
		desc := params.Get("error_description")
		return "", "", fmt.Errorf("auth error: %s (%s)", errParam, desc)
	}

	code = params.Get("code")
	if code == "" {
		return "", "", fmt.Errorf("no auth code found in URL")
	}
	state = params.Get("state")
	return code, state, nil
}

// --- PKCE ---

func generatePKCE() (verifier, challenge string, err error) {
	buf := make([]byte, 96)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(buf)
	hash := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(hash[:])
	return verifier, challenge, nil
}

func randomString(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)[:n]
}

// --- Auth URL ---

func buildAuthURL(redirectURI, state, challenge string) string {
	params := url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"response_mode":         {"fragment"},
		"scope":                 {scopes},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	return keycloakBaseURL + "/auth?" + params.Encode()
}

// --- Token exchange ---

func exchangeCode(code, verifier, redirectURI string) (*TokenSet, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"redirect_uri":  {redirectURI},
		"code":          {code},
		"code_verifier": {verifier},
	}

	resp, err := http.Post(
		keycloakBaseURL+"/token",
		"application/x-www-form-urlencoded",
		strings.NewReader(data.Encode()),
	)
	if err != nil {
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			return nil, fmt.Errorf("token exchange failed: %s — %s", errResp.Error, errResp.Description)
		}
		return nil, fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
		Scope       string `json:"scope"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parsing token response: %w", err)
	}

	tokens, err := TokenSetFromJWT(tokenResp.AccessToken)
	if err != nil {
		return nil, err
	}
	tokens.IDToken = tokenResp.IDToken
	return tokens, nil
}
