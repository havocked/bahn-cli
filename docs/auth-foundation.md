# Auth Foundation — SSD

## Goal

Unlock bahn.de's personal APIs for Ori. Auth is plumbing — it should work reliably and get out of the way so we can get to the real value: trip data, delay tracking, Fahrgastrechte detection.

## How bahn.de Auth Works (Reverse-Engineered Feb 13, 2026)

### Architecture: Keycloak + PKCE + Silent Iframe Refresh

bahn.de uses **Keycloak** as its identity provider with a standard **OAuth2 Authorization Code + PKCE** flow. Key discovery: **there are no refresh tokens** — the system uses silent iframe-based re-authentication instead.

### The Token Chain

1. **User logs in** → Keycloak sets session cookies on `accounts.bahn.de`
2. **Auth code** returned via redirect to `auth.v2.html`
3. **Code exchanged** for `access_token` + `id_token` (PKCE S256 verification)
4. **No refresh_token** — Keycloak explicitly does not issue one for this client
5. **Tokens stored** in `sessionStorage["token"]` as `{accessToken, idToken}`
6. **Silent refresh** via hidden iframe: loads Keycloak auth endpoint with `prompt=none`, Keycloak session cookie authenticates silently, new tokens returned
7. **Session monitoring** via Keycloak's `check_session_iframe` (polls every ~1s)

### Keycloak Endpoints

| Endpoint | URL |
|----------|-----|
| **Authorization** | `https://accounts.bahn.de/auth/realms/db/protocol/openid-connect/auth` |
| **Token** | `https://accounts.bahn.de/auth/realms/db/protocol/openid-connect/token` |
| **Logout** | `https://accounts.bahn.de/auth/realms/db/protocol/openid-connect/logout` |
| **Check Session** | `https://accounts.bahn.de/auth/realms/db/protocol/openid-connect/login-status-iframe.html` |
| **Revocation** | `https://accounts.bahn.de/auth/realms/db/protocol/openid-connect/revoke` |
| **UserInfo** | `https://accounts.bahn.de/auth/realms/db/protocol/openid-connect/userinfo` |

### OAuth2 Client Parameters

| Parameter | Value |
|-----------|-------|
| **Client ID** | `kf_web` |
| **Redirect URI** | `https://www.bahn.de/.resources/bahn-common-light/webresources/assets/html/auth.v2.html` |
| **Scopes** | `openid vendo` |
| **Response type** | `code` |
| **Response mode** | `fragment` (tokens in URL hash) |
| **PKCE method** | `S256` |
| **Code verifier length** | 128 chars |

### JWT Token Details

- **Lifetime:** 300 seconds (5 minutes)
- **Min validity before refresh:** 30 seconds
- **Algorithm:** RS256
- **Issuer:** `https://accounts.bahn.de/auth/realms/db`
- **Audience:** `kf_web`
- **Auth methods (`amr`):** `["otp", "pwd"]` (password + 2FA OTP)

### Key JWT Claims

```json
{
  "sub": "9dc3b522-947a-4d0f-89e1-9808541d7850",       // User ID
  "kundenkontoid": "f3f18f96-a823-4cf5-83fb-f043a9b52c25", // Account ID (needed for bookings API)
  "preferred_username": "natmartin31@gmail.com",
  "scope": "openid vendo",
  "groups": ["EndKd"],                                    // End customer
  "realm_access": {
    "roles": [
      "bue_auf_lesen_own",          // Read own bookings
      "bue_buchen",                  // Make bookings
      "tck_m_own",                   // Ticket access (mobile)
      "tck_lb_c_own",               // Ticket access (create)
      "tck_lb_r_own",               // Ticket access (read)
      "rbl_rk_own",                  // Travel chains (Reiseketten)
      "rbl_dt_own",                  // Trip details
      "rbl_fav_own",                 // Favorites
      "res_chk",                     // Reservation check
      "bue_buchungsbestaetigung",    // Booking confirmation
      "kto_endkd_selfsvc",          // Account self-service
      "..."
    ]
  }
}
```

### Personal API Endpoints

**User context (profile, BahnCard):**
```
GET https://www.bahn.de/web/api/kundenkonto/user-context-data
Authorization: Bearer <access_token>
```

**List bookings:**
```
GET https://www.bahn.de/web/api/buchung/auftrag/v2
    ?startIndex=0
    &auftraegeReturnSize=10
    &auftragSortOrder=DESCENDING
    &kundenprofilId=<kundenkontoid>
Authorization: Bearer <access_token>
```

**Travel chains (trips with legs):**
```
GET https://www.bahn.de/web/api/reisebegleitung/reiseketten
    ?pagesize=100
    &types[]=AUFTRAG
    &types[]=WIEDERHOLEND
Authorization: Bearer <access_token>
```

**Single trip detail:**
```
GET https://www.bahn.de/web/api/reisebegleitung/reiseketten/<uuid>
Authorization: Bearer <access_token>
```

**Wagenreihung (coach sequence):**
```
GET https://www.bahn.de/web/api/reisebegleitung/wagenreihung/administrations
Authorization: Bearer <access_token>
```

---

## Auth Strategy: Local OIDC Flow (Option 3)

### How It Works

Same pattern as `gh auth login` — open a real browser, capture the callback:

```
┌─────────┐     1. Open browser      ┌──────────────┐
│ bahn-cli │ ──────────────────────→  │   Browser    │
│          │                          │ (bahn.de     │
│          │     4. Auth code         │  login page) │
│  :18923  │ ←──────────────────────  │              │
│ callback │     (redirect to         └──────┬───────┘
│ server   │      localhost)                 │
└────┬─────┘                                 │
     │          2. User logs in              │
     │          3. Keycloak redirects ───────┘
     │
     │  5. Exchange code for tokens
     │     POST /protocol/openid-connect/token
     │     (with PKCE code_verifier)
     │
     ▼
  Store tokens in ~/.config/bahn-cli/
  (encrypted or OS keyring)
```

### Detailed Flow

#### Step 1: Generate PKCE Challenge

```
code_verifier  = random(128 chars, [A-Za-z0-9])
code_challenge = base64url(sha256(code_verifier))
```

#### Step 2: Start Local HTTP Server

Listen on `http://localhost:<random-port>/callback`

#### Step 3: Open Browser to Auth URL

```
https://accounts.bahn.de/auth/realms/db/protocol/openid-connect/auth
  ?client_id=kf_web
  &redirect_uri=http://localhost:<port>/callback
  &response_type=code
  &response_mode=fragment
  &scope=openid+vendo
  &state=<random>
  &code_challenge=<S256-challenge>
  &code_challenge_method=S256
```

**Note:** `redirect_uri` must be in Keycloak's allowed origins list for `kf_web`. If `localhost` isn't allowed, we'll need to use the real redirect URI and intercept differently. **This is the main risk — needs testing.**

**Fallback if localhost not allowed:** Use the real redirect URI (`https://www.bahn.de/.resources/.../auth.v2.html`) and extract the code from the URL fragment via:
- A local proxy/interceptor
- Or ask user to paste the callback URL

#### Step 4: Receive Auth Code

Browser redirects to `http://localhost:<port>/callback#code=<code>&state=<state>`

Since `response_mode=fragment`, the code is in the URL **hash** (not query params). This means the server won't see it directly — we need a small HTML page that extracts the hash and sends it to the server:

```html
<script>
  const params = new URLSearchParams(location.hash.slice(1));
  fetch('/exchange?' + params.toString()).then(() => window.close());
</script>
```

#### Step 5: Exchange Code for Tokens

```
POST https://accounts.bahn.de/auth/realms/db/protocol/openid-connect/token
Content-Type: application/x-www-form-urlencoded

grant_type=authorization_code
&client_id=kf_web
&redirect_uri=http://localhost:<port>/callback
&code=<auth_code>
&code_verifier=<code_verifier>
```

Response:
```json
{
  "access_token": "<jwt>",
  "id_token": "<jwt>",
  "token_type": "bearer",
  "expires_in": 300,
  "scope": "openid vendo"
}
```

#### Step 6: Store Tokens + Session Cookie

Store in `~/.config/bahn-cli/tokens.json` (encrypted) or OS keyring:
```json
{
  "accessToken": "<jwt>",
  "idToken": "<jwt>",
  "expiresAt": "2026-02-13T16:30:38Z",
  "kundenkontoid": "f3f18f96-a823-4cf5-83fb-f043a9b52c25"
}
```

### Token Refresh Strategy

Since there's no refresh token, we have two options:

**Option A: Re-run PKCE flow with `prompt=none`**
If the Keycloak session is still alive (browser cookies), we can silently get new tokens:
```
GET .../auth?client_id=kf_web&...&prompt=none
```
This is what bahn.de's iframe does. For CLI, we'd need to:
1. Store the Keycloak session cookies from the initial login
2. Send them with the silent auth request
3. This works without user interaction as long as the Keycloak session is alive

**Option B: Re-authenticate**
If the Keycloak session expired, re-open the browser for login.

**Option C: Manual token paste (fallback)**
For headless environments or when browser auth is impractical:
```bash
bahn auth token <jwt>  # Paste from browser DevTools
```
Works for 5 min — enough for quick queries.

### Keycloak Session Lifetime

Unknown — this is the critical open question. Keycloak sessions can last hours to days depending on server config. The `sid` claim in the JWT tracks the session. We need to test how long the session cookies remain valid.

---

## Architecture

```
internal/auth/
├── oidc.go         # PKCE + local callback server + token exchange
├── tokens.go       # JWT parsing, validation, expiry check
├── store.go        # Token storage (keyring or encrypted file)
├── session.go      # High-level: "give me an authenticated HTTP client"
├── refresh.go      # Silent re-auth via Keycloak session
└── profile.go      # User context / profile fetching
```

### OIDC Flow (`oidc.go`)

```go
// Login opens a browser for the full OIDC login flow.
// Returns tokens on success.
func Login() (*TokenSet, error)

// Internal:
// - generatePKCE() → (verifier, challenge)
// - startCallbackServer() → (port, codeChan)
// - openBrowser(authURL)
// - exchangeCode(code, verifier) → TokenSet
```

### Token Management (`tokens.go`)

```go
type TokenSet struct {
    AccessToken  string    `json:"accessToken"`
    IDToken      string    `json:"idToken"`
    ExpiresAt    time.Time `json:"expiresAt"`
    Kundenkontoid string   `json:"kundenkontoid"`
    Sub          string    `json:"sub"`
    Username     string    `json:"username"`
}

func ParseJWT(token string) (*Claims, error)
func (t *TokenSet) IsExpired() bool
func (t *TokenSet) NeedsRefresh() bool  // true if <30s remaining
```

### Storage (`store.go`)

```go
func SaveTokens(tokens *TokenSet) error
func LoadTokens() (*TokenSet, error)
func ClearTokens() error
```

Primary: `github.com/zalando/go-keyring`
Fallback: `~/.config/bahn-cli/tokens.json` (chmod 600)

### Session (`session.go`)

```go
// Client returns an authenticated http.Client.
// Handles transparent token refresh.
func Client() (*http.Client, error)

// EnsureAuth checks for valid tokens, triggers login if needed.
func EnsureAuth() (*TokenSet, error)
```

### Profile (`profile.go`)

```go
type Profile struct {
    Kundenkontoid string
    Vorname       string
    Nachname      string
    ProfilArt     string  // "PR" (Privat) or "GE" (Geschäftlich)
    BahnCard      *BahnCard
}

func FetchProfile(client *http.Client) (*Profile, error)
```

---

## Commands

All auth commands output JSON to stdout, diagnostics to stderr.

- `bahn auth login` — Full OIDC browser flow (one-time setup). Opens browser, user logs in, tokens stored.
- `bahn auth status` — Current auth state: token validity, profile, kundenkontoid.
- `bahn auth refresh` — Silent re-auth attempt. Fails with exit code 2 if Keycloak session expired.
- `bahn auth token <jwt>` — Manual fallback: paste JWT from DevTools. 5 min lifetime.
- `bahn auth clear` — Remove all stored credentials.

---

## Implementation Plan

### Step 1: Project Scaffold (30 min)
- [x] `go mod init github.com/havocked/bahn-cli`
- [ ] Cobra root + `auth` command group
- [ ] Config loading (`~/.config/bahn-cli/config.toml`)
- [ ] Output framework (json/plain/human)

### Step 2: Manual Token Auth — Quick Win (30 min)
- [ ] `bahn auth token <jwt>` — parse, extract claims, store
- [ ] `bahn auth status` — show current auth state
- [ ] `bahn auth clear` — remove stored tokens
- [ ] Token file at `~/.config/bahn-cli/tokens.json`

### Step 3: OIDC Browser Login (1-2h)
- [ ] PKCE generation (verifier + S256 challenge)
- [ ] Local HTTP callback server
- [ ] Fragment-to-server bridge (small HTML page)
- [ ] Browser opening (`open` on macOS, `xdg-open` on Linux)
- [ ] Token exchange with Keycloak
- [ ] `bahn auth login` end-to-end

### Step 4: Token Refresh (1h)
- [ ] Silent re-auth attempt (if we can capture Keycloak session cookies)
- [ ] Auto-refresh in `session.go` before API calls
- [ ] Graceful fallback to re-login

### Step 5: Profile + First API Call (30 min)
- [ ] `GET /web/api/kundenkonto/user-context-data`
- [ ] Extract name, BahnCard, profilArt
- [ ] Display in `auth status`

### Step 6: Trips (First Real Feature) (1-2h)
- [ ] `bahn trips` — list upcoming via reiseketten API
- [ ] `bahn trips --past` — past trips
- [ ] `bahn trips <uuid>` — detail view
- [ ] All three output modes (json/plain/human)

---

## Open Questions

1. **Does Keycloak accept `localhost` as redirect_uri for `kf_web`?**
   This is the #1 risk. If not, we need to use the real redirect URI and intercept differently. Test by hitting the auth URL with `redirect_uri=http://localhost:18923/callback`.

2. **Keycloak session lifetime?**
   How long do the session cookies on `accounts.bahn.de` stay valid? This determines how long between re-logins.

3. **Can we capture Keycloak session cookies during login?**
   If we can store them, we can do silent refresh from CLI without a browser. The login happens in the user's browser though, so we'd need a different approach.

4. **Rate limits on personal API?**
   How aggressively can we poll for delay updates?

5. **`response_mode=fragment` complication:**
   The auth code is returned in the URL hash, which the local server can't see directly. We need the HTML bridge page approach (documented above). This is a known pattern.

---

## Dependencies (Go modules)

```
github.com/spf13/cobra           # CLI framework
github.com/spf13/viper           # Config
github.com/golang-jwt/jwt/v5     # JWT parsing
github.com/zalando/go-keyring    # OS keyring (optional)
github.com/pkg/browser           # Open URL in default browser
github.com/fatih/color           # Terminal colors
github.com/olekukonez/tablewriter # Table output
```

No cookie import libraries needed — we're doing proper OIDC.

---

*Reverse-engineered from bahn.de's `initUserContextService-BQ7nOpGX.js` (Feb 13, 2026). The frontend uses AppAuth-JS (OpenID Connect library) with a custom Keycloak integration. Our CLI replicates the same flow natively in Go.*
