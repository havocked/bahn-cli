# Auth Foundation — SSD

## Goal

Get authenticated access to bahn.de's personal APIs so we can fetch:
- **Booked trips** (upcoming + past)
- **Boarding passes / ticket data**
- **Trip detail** (all stops, real-time delays, platforms)
- **Account/profile info** (BahnCard, kundenprofilId)

## How bahn.de Auth Works

### The Token Chain

When you log into bahn.de in a browser, the flow is:

1. **Login** → bahn.de sets session cookies
2. **Cookies contain (or lead to) a JWT** access token
3. **JWT** is sent as `Authorization: Bearer <token>` on API calls
4. **JWT contains** your `kundenprofilId` (or a profile endpoint reveals it)
5. **Tokens expire** — need refresh or re-import

### Key Cookies / Tokens to Extract

From browser session:
- **Session cookies** on `.bahn.de` domain
- **JWT access token** (may be in cookies, localStorage, or fetched via a token endpoint)
- **Refresh token** (if available — allows silent re-auth)

### Discovery Tasks (Phase 0)

Before building, we need to reverse-engineer the exact auth flow. **You're logged into bahn.de right now on the train — perfect time to sniff.**

#### What to capture (DevTools → Network tab):

1. **Go to bahn.de/buchung/auftraege** (your bookings page)
   - Note the `Authorization` header on XHR requests
   - Note the request URL pattern and query params
   - Look for `kundenprofilId` in request/response

2. **Check cookies** (DevTools → Application → Cookies → bahn.de)
   - List all cookie names, note which look session-related
   - Check for JWTs (long base64 strings with two dots)

3. **Check localStorage/sessionStorage** for tokens
   - `localStorage.getItem('token')` etc.

4. **Hit the profile/account endpoint**
   - Usually something like `/web/api/profil` or `/web/api/account`
   - This reveals `kundenprofilId` and BahnCard info

5. **Test token refresh**
   - Is there a `/web/api/auth/refresh` or similar?
   - What happens when the access token expires?

## Architecture

```
internal/auth/
├── cookies.go      # Import cookies from browser stores
├── tokens.go       # JWT parsing, validation, refresh
├── store.go        # Secure credential storage
├── session.go      # High-level: "give me an authenticated HTTP client"
└── discovery.go    # Profile/account info fetching
```

### Cookie Import (`cookies.go`)

**Pattern:** Same as steipete/spogo (Spotify CLI) — read browser cookie databases directly.

```
bahn auth import --browser chrome
bahn auth import --browser firefox
bahn auth import --browser safari
```

**How it works:**
1. Find browser's cookie database file
   - Chrome: `~/Library/Application Support/Google/Chrome/Default/Cookies`
   - Firefox: `~/Library/Application Support/Firefox/Profiles/*/cookies.sqlite`
   - Safari: `~/Library/Cookies/Cookies.binarycookies`
2. Read cookies for `.bahn.de` domain
3. Extract session cookies + any JWT tokens
4. If JWT not in cookies, use session cookies to hit a token endpoint

**Go libraries:**
- `github.com/AgileBits/go-chromium` or `github.com/nicholasgasior/gochrome` for Chrome cookie decryption
- `github.com/nicholasgasior/goffcookies` for Firefox
- Or: use `kooky` (`github.com/nicholasgasior/kooky`) — unified multi-browser cookie reader

**Preferred:** [`github.com/nicholasgasior/kooky`](https://github.com/nicholasgasior/kooky) — supports Chrome, Firefox, Safari, Edge on macOS/Linux/Windows. Single dependency.

**Alternative approach:** `github.com/nicholasgasior/kooky` may not be maintained. Consider:
- [`github.com/nicholasgasior/gochrome`](https://github.com/nicholasgasior/gochrome) for Chrome-only (most common)
- Or [`browsercookie`](https://github.com/nicholasgasior/browsercookie) pattern
- Or use Go's `crypto` + SQLite to read Chrome cookies directly (Chrome encrypts with Keychain on macOS)

### Token Management (`tokens.go`)

```go
type TokenSet struct {
    AccessToken  string    // JWT
    RefreshToken string    // If available
    ExpiresAt    time.Time // Parsed from JWT `exp` claim
    ProfileID    string    // kundenprofilId (extracted from JWT or profile endpoint)
}

// ParseJWT extracts claims without validation (we trust the source)
func ParseJWT(token string) (*Claims, error)

// IsExpired checks if token needs refresh
func (t *TokenSet) IsExpired() bool

// Refresh attempts to get a new access token using refresh token
func (t *TokenSet) Refresh(client *http.Client) error
```

**JWT decoding:** Use `github.com/golang-jwt/jwt/v5` for parsing claims. We don't validate signatures (we're the consumer, not the issuer).

### Secure Storage (`store.go`)

```go
// Store credentials in OS keyring
func SaveTokens(tokens *TokenSet) error
func LoadTokens() (*TokenSet, error)
func ClearTokens() error
```

**Go library:** `github.com/zalando/go-keyring` — uses macOS Keychain, Linux Secret Service, Windows Credential Manager.

**Fallback:** Encrypted file at `~/.config/bahn-cli/credentials.enc` (for headless environments without keyring).

### Session (`session.go`)

The main consumer interface:

```go
// AuthenticatedClient returns an http.Client with auth headers set.
// Handles token refresh transparently.
func AuthenticatedClient() (*http.Client, error)

// Or simpler:
func AuthHeaders() (http.Header, error)
```

This is what every other command calls. They don't care about cookies vs JWT vs refresh — they just get an authenticated client.

### Profile Discovery (`discovery.go`)

After auth, discover account details:

```go
type Profile struct {
    KundenprofilID string
    FirstName      string
    LastName       string
    BahnCard       *BahnCard // nil if none
}

type BahnCard struct {
    Type    string // "BahnCard 25", "BahnCard 50", etc.
    Number  string
    ValidTo time.Time
}

func FetchProfile(client *http.Client) (*Profile, error)
```

## Commands

### `bahn auth import`

```bash
bahn auth import --browser chrome

# Flow:
# 1. Read Chrome cookies for .bahn.de
# 2. Extract session/JWT tokens
# 3. Test by hitting profile endpoint
# 4. Store in keyring
# 5. Print: "✓ Authenticated as Nataniel M. (BahnCard 25, valid until 2027-03)"
```

### `bahn auth status`

```bash
bahn auth status

# Output:
# Status: authenticated
# Name: Nataniel M.
# Profile ID: abc-123-def
# BahnCard: BahnCard 25 (valid until 2027-03-15)
# Token expires: 2026-02-13T18:30:00Z (2h 15m remaining)
# Token source: Chrome cookie import
```

With `--json`:
```json
{
  "authenticated": true,
  "name": "Nataniel M.",
  "profileId": "abc-123-def",
  "bahnCard": { "type": "BahnCard 25", "validTo": "2027-03-15" },
  "tokenExpires": "2026-02-13T18:30:00Z",
  "tokenSource": "chrome"
}
```

### `bahn auth clear`

```bash
bahn auth clear
# Removes all stored credentials from keyring
```

### `bahn auth token` (manual fallback)

```bash
# For headless setups or when cookie import fails
bahn auth token <paste-jwt-here>

# Tests the token, fetches profile, stores it
```

## Trips Command (First Consumer)

Once auth works, `trips` is the first payoff:

```bash
bahn trips                    # Upcoming booked trips
bahn trips --past             # Past trips  
bahn trips --past --days 90   # Last 90 days
bahn trips <uuid>             # Single trip detail
```

### API Endpoints

**List bookings:**
```
GET https://www.bahn.de/web/api/buchung/auftrag/v2
    ?startIndex=0
    &auftraegeReturnSize=10
    &auftragSortOrder=DESCENDING
    &kundenprofilId=<profile-id>
Authorization: Bearer <jwt>
```

**Travel chains (trips with legs):**
```
GET https://www.bahn.de/web/api/reisebegleitung/reiseketten
    ?pagesize=100
    &types[]=AUFTRAG
    &types[]=WIEDERHOLEND
Authorization: Bearer <jwt>
```

**Single trip detail:**
```
GET https://www.bahn.de/web/api/reisebegleitung/reiseketten/<uuid>
Authorization: Bearer <jwt>
```

### Output

```bash
bahn trips

# ┌─────────┬──────────────────────────────┬──────────┬────────┐
# │ Date    │ Route                        │ Train    │ Status │
# ├─────────┼──────────────────────────────┼──────────┼────────┤
# │ Feb 13  │ Leipzig Hbf → Berlin Hbf     │ ICE 1556 │ +7 min │
# │ Feb 15  │ Berlin Hbf → Leipzig Hbf     │ ICE 1559 │ on time│
# └─────────┴──────────────────────────────┴──────────┴────────┘
```

```bash
bahn trips <uuid>

# ICE 1556 — Leipzig Hbf → Berlin Hbf
# Date: 2026-02-13
# 
# Leipzig Hbf          dep 14:23  (+7)  Pl. 11 (was 9)
# Halle(Saale)Hbf      dep 14:51  (+5)  Pl. 4
# Berlin Südkreuz      arr 15:58  (+8)  Pl. 3
# Berlin Hbf           arr 16:12  (+8)  Pl. 14
#
# Booking: W7KHTA
# Seat: Wagen 24, Platz 61 (Fenster, Ruhebereich)
# BahnCard 25 discount applied
```

With `--json`: full structured data including all stops, delays, platform changes, booking reference, seat info.

## Implementation Plan

### Step 1: Project Scaffold
- [ ] `go mod init github.com/havocked/bahn-cli`
- [ ] Cobra root + `auth` command group
- [ ] Config loading (`~/.config/bahn-cli/config.toml`)
- [ ] Output framework (json/plain/human)

### Step 2: Cookie Import (Chrome first)
- [ ] Read Chrome cookie DB on macOS
- [ ] Decrypt cookies (macOS Keychain)
- [ ] Extract `.bahn.de` cookies
- [ ] Find JWT in cookies or use cookies to fetch JWT
- [ ] `bahn auth import --browser chrome` working

### Step 3: Token Storage & Management
- [ ] OS keyring integration
- [ ] JWT parsing (extract expiry, profile ID)
- [ ] `bahn auth status` working
- [ ] `bahn auth clear` working
- [ ] `bahn auth token` (manual) working

### Step 4: Profile Discovery
- [ ] Hit profile endpoint after auth
- [ ] Extract kundenprofilId, name, BahnCard
- [ ] Store profile with tokens

### Step 5: Trips (First Real Feature)
- [ ] `bahn trips` — list upcoming
- [ ] `bahn trips --past` — list past
- [ ] `bahn trips <uuid>` — detail view
- [ ] All three output modes

### Step 6: Boarding Pass / Ticket Data
- [ ] Find the boarding pass endpoint (likely in trip detail or a separate endpoint)
- [ ] Extract QR code data / PDF link
- [ ] `bahn trips <uuid> --ticket` or `bahn ticket <booking-code>`

## Open Questions (To Research on the Train)

1. **Where is the JWT?** Is it in cookies, localStorage, or fetched from a token endpoint using session cookies?

2. **What's the token lifetime?** Minutes? Hours? Days? This determines how aggressive refresh needs to be.

3. **Is there a refresh endpoint?** Or does re-auth require full cookie re-import?

4. **Boarding pass endpoint?** The DB app shows a QR code boarding pass. The web version likely has an endpoint for this too.

5. **Cookie encryption on macOS:** Chrome encrypts cookies with macOS Keychain. Need to handle the `v10` encryption prefix. Libraries exist but need to verify they work on current Chrome versions.

6. **Rate limits on personal API:** How aggressive can we poll for delay updates?

## Dependencies (Go modules)

```
github.com/spf13/cobra          # CLI framework
github.com/spf13/viper          # Config
github.com/golang-jwt/jwt/v5    # JWT parsing
github.com/zalando/go-keyring   # OS keyring
github.com/mattn/go-sqlite3     # Chrome cookie DB (CGO)
github.com/fatih/color          # Terminal colors
github.com/olekukonez/tablewriter # Table output
```

For cookie decryption, evaluate:
- `kooky` (multi-browser, may be stale)
- Direct Chrome decryption (AES-CBC with Keychain-stored key)
- Or: skip cookie import initially, start with manual `bahn auth token` for faster iteration

## Recommended Build Order

**Fast path to value:**
1. Scaffold + `bahn auth token <jwt>` (manual paste — 30 min)
2. JWT parsing + profile fetch (test on the train — 30 min)
3. `bahn trips` list + detail (the payoff — 1-2h)
4. Cookie import (Chrome on macOS — 2-3h, can do later)

This way you have a working `trips` command TODAY with manual token paste, and cookie import comes later as a UX improvement.

---

*This is the auth layer that unlocks everything personal — trips, delays, Fahrgastrechte, boarding passes. Once this works, every other personal command just needs its API endpoint.*
