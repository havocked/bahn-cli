package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pkg/browser"
)

const (
	keycloakBaseURL = "https://accounts.bahn.de/auth/realms/db/protocol/openid-connect"
	clientID        = "kf_web"
	scopes          = "openid vendo"
	callbackTimeout = 120 * time.Second
)

// Login performs the full OIDC browser login flow:
// 1. Generate PKCE challenge
// 2. Start local callback server
// 3. Open browser to Keycloak auth URL
// 4. Wait for auth code via callback
// 5. Exchange code for tokens
func Login(onStatus func(string)) (*TokenSet, error) {
	// Step 1: PKCE
	verifier, challenge, err := generatePKCE()
	if err != nil {
		return nil, fmt.Errorf("PKCE generation failed: %w", err)
	}

	// Step 2: Start local server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to start callback server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://localhost:%d/callback", port)

	codeChan := make(chan callbackResult, 1)
	srv := &http.Server{Handler: callbackHandler(codeChan)}
	go func() { _ = srv.Serve(listener) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	// Step 3: Build auth URL and open browser
	state := randomString(32)
	authURL := buildAuthURL(redirectURI, state, challenge)

	if onStatus != nil {
		onStatus(fmt.Sprintf("Opening browser for login (port %d)...", port))
	}
	if err := browser.OpenURL(authURL); err != nil {
		return nil, fmt.Errorf("failed to open browser: %w\n\nManually open: %s", err, authURL)
	}

	// Step 4: Wait for callback
	if onStatus != nil {
		onStatus("Waiting for authentication...")
	}
	var result callbackResult
	select {
	case result = <-codeChan:
	case <-time.After(callbackTimeout):
		return nil, fmt.Errorf("login timed out after %s", callbackTimeout)
	}
	if result.err != nil {
		return nil, result.err
	}
	if result.state != state {
		return nil, fmt.Errorf("state mismatch: possible CSRF attack")
	}

	// Step 5: Exchange code for tokens
	if onStatus != nil {
		onStatus("Exchanging auth code for tokens...")
	}
	return exchangeCode(result.code, verifier, redirectURI)
}

// --- PKCE ---

func generatePKCE() (verifier, challenge string, err error) {
	// 128 random bytes → base64url → 43-128 char verifier
	buf := make([]byte, 96)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(buf)

	// S256: base64url(sha256(verifier))
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

// --- Callback server ---

type callbackResult struct {
	code  string
	state string
	err   error
}

// callbackHandler serves two purposes:
// 1. /callback — serves an HTML page that reads the fragment and sends it to /exchange
// 2. /exchange — receives the code+state from the HTML page
func callbackHandler(codeChan chan<- callbackResult) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		// The auth code is in the URL fragment (#code=...&state=...).
		// Fragments aren't sent to the server, so we serve a page that extracts them.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, callbackHTML)
	})

	mux.HandleFunc("/exchange", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")
		errParam := r.URL.Query().Get("error")

		if errParam != "" {
			desc := r.URL.Query().Get("error_description")
			codeChan <- callbackResult{err: fmt.Errorf("auth error: %s (%s)", errParam, desc)}
		} else if code == "" {
			codeChan <- callbackResult{err: fmt.Errorf("no auth code received")}
		} else {
			codeChan <- callbackResult{code: code, state: state}
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, successHTML)
	})

	return mux
}

const callbackHTML = `<!DOCTYPE html>
<html><head><title>bahn-cli</title></head>
<body>
<p>Authenticating...</p>
<script>
const params = new URLSearchParams(location.hash.slice(1));
const code = params.get('code');
const state = params.get('state');
const error = params.get('error');
const errorDesc = params.get('error_description');
let url = '/exchange?';
if (error) {
  url += 'error=' + encodeURIComponent(error) + '&error_description=' + encodeURIComponent(errorDesc || '');
} else if (code) {
  url += 'code=' + encodeURIComponent(code) + '&state=' + encodeURIComponent(state || '');
} else {
  url += 'error=no_code&error_description=No+authorization+code+in+response';
}
fetch(url).then(() => {}).catch(() => {});
</script>
</body></html>`

const successHTML = `<!DOCTYPE html>
<html><head><title>bahn-cli</title></head>
<body>
<p>✓ Authentication successful. You can close this tab.</p>
</body></html>`

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
