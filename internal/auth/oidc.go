package auth

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/pkg/browser"
	"github.com/steipete/sweetcookie"
)

const (
	keycloakBaseURL = "https://accounts.bahn.de/auth/realms/db/protocol/openid-connect"
	clientID        = "kf_web"
	scopes          = "openid vendo"
	realRedirectURI = "https://www.bahn.de/.resources/bahn-common-light/webresources/assets/html/auth.v2.html"
)

// Login performs the OIDC browser login flow.
// Opens browser, user logs in, pastes callback URL.
func Login(onStatus func(string)) (*TokenSet, error) {
	verifier, challenge, err := generatePKCE()
	if err != nil {
		return nil, fmt.Errorf("PKCE generation failed: %w", err)
	}
	state := randomString(32)

	authURL := buildAuthURL(realRedirectURI, state, challenge)

	if onStatus != nil {
		onStatus("Opening browser for login...")
	}
	_ = browser.OpenURL(authURL)

	if onStatus != nil {
		onStatus("")
		onStatus("After logging in, copy the FULL URL from your browser's address bar and paste it here:")
		onStatus("")
	}

	fmt.Fprint(os.Stderr, "> ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 4096), 16384) // URLs can be long
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

// Refresh attempts to get new tokens by reading Keycloak session cookies
// from the browser and replaying them with a prompt=none auth request.
// No user interaction needed if the browser session is still alive.
func Refresh(onStatus func(string)) (*TokenSet, error) {
	verifier, challenge, err := generatePKCE()
	if err != nil {
		return nil, fmt.Errorf("PKCE generation failed: %w", err)
	}
	state := randomString(32)

	authURL := buildAuthURL(realRedirectURI, state, challenge)
	authURL += "&prompt=none"

	if onStatus != nil {
		onStatus("Reading browser cookies for accounts.bahn.de...")
	}

	// Read Keycloak session cookies from the browser
	cookieResult, err := sweetcookie.Get(context.Background(), sweetcookie.Options{
		URL:  "https://accounts.bahn.de/",
		Mode: sweetcookie.ModeMerge,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to read browser cookies: %w (make sure you've logged into bahn.de recently)", err)
	}

	if len(cookieResult.Cookies) == 0 {
		return nil, fmt.Errorf("no cookies found for accounts.bahn.de — log into bahn.de in your browser first, then retry")
	}

	if onStatus != nil {
		onStatus(fmt.Sprintf("Found %d cookies, attempting silent refresh...", len(cookieResult.Cookies)))
	}

	// Build HTTP request with cookies
	req, err := http.NewRequest("GET", authURL, nil)
	if err != nil {
		return nil, err
	}
	for _, c := range cookieResult.Cookies {
		req.AddCookie(&http.Cookie{
			Name:  c.Name,
			Value: c.Value,
		})
	}

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 10 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusSeeOther {
		return nil, fmt.Errorf("refresh failed: expected redirect, got status %d (session likely expired — run `bahn auth login`)", resp.StatusCode)
	}

	location := resp.Header.Get("Location")
	if location == "" {
		return nil, fmt.Errorf("refresh failed: no redirect location")
	}

	code, returnedState, err := extractFragmentParams(location)
	if err != nil {
		if strings.Contains(location, "error=login_required") || strings.Contains(location, "error=interaction_required") {
			return nil, fmt.Errorf("session expired — run `bahn auth login` to re-authenticate")
		}
		return nil, fmt.Errorf("refresh failed: %w", err)
	}
	if returnedState != state {
		return nil, fmt.Errorf("state mismatch during refresh")
	}

	if onStatus != nil {
		onStatus("Exchanging code for tokens...")
	}
	return exchangeCode(code, verifier, realRedirectURI)
}

// --- Fragment parsing ---

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
		AccessToken  string `json:"access_token"`
		IDToken      string `json:"id_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parsing token response: %w", err)
	}

	tokens, err := TokenSetFromJWT(tokenResp.AccessToken)
	if err != nil {
		return nil, err
	}
	tokens.IDToken = tokenResp.IDToken
	tokens.RefreshToken = tokenResp.RefreshToken
	return tokens, nil
}
