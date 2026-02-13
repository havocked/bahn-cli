# bahn-cli — System Software Design

## Philosophy

**bahn-cli is a tool built for an AI agent.** Not for a human at a terminal.

Nate already has the DB Navigator app for notifications, booking, and trip tracking. What he doesn't have is a way for **Ori** (his AI assistant) to programmatically access Deutsche Bahn data — to reason about trips, cross-reference with calendars, detect refund opportunities, and weave transit awareness into daily life.

The core idea: **give the agent eyes into the rail system.** Everything else follows from that.

### Design Principles

1. **JSON-first, always.** The primary consumer is an LLM parsing stdout. Human-readable output is a nice-to-have, not the goal. Every command defaults to `--json`.

2. **Structured over pretty.** Rich error messages in stderr, clean data in stdout. No spinners, no progress bars, no interactive prompts. The agent can't click things.

3. **Composable.** Each command does one thing and outputs data that can be piped or reasoned about. `bahn trips --json | bahn disruptions --for-trip` is better than one mega-command.

4. **Fail loudly, fail structured.** Errors are JSON too: `{"error": "token_expired", "message": "...", "action": "run bahn auth login"}`. The agent needs to know what went wrong and what to do about it.

5. **Auth is a solved problem, not a feature.** Auth exists to unlock data. It should work reliably and get out of the way. The interesting part is what we do with the data.

## What Ori Gets From This

### Contextual awareness
- "Nate has an ICE to Berlin at 14:23. It's currently +12min. He can leave 10 min later."
- "There are disruptions on the S1 — Nate usually takes that to Hbf. He should take the tram."

### Proactive intelligence
- Cross-reference trips with calendar events: "Your train arrives at 15:47 but your meeting is at 16:00 — tight."
- Morning briefing: real-time disruption data for home station, not RSS feed parsing
- Fahrgastrechte detection: "Your train was 67 min late. You're owed €11.48."

### Trip planning assistance
- When Nate mentions travel, Ori can search connections and present options with prices
- Monitoring booked trips for delays, platform changes, connection risks

### The agent doesn't need:
- Pretty terminal tables (JSON is the table)
- Interactive menus
- Color output
- Browser-opening (except for initial auth setup)
- Notification sending (Ori handles that via WhatsApp/etc.)

## Architecture

```
bahn-cli
├── cmd/bahn/           # Entry point
├── internal/
│   ├── cmd/            # Cobra command definitions
│   │   ├── root.go
│   │   ├── auth.go     # OIDC login + token management
│   │   ├── trips.go    # Personal bookings
│   │   ├── board.go    # Departure/arrival boards
│   │   ├── journey.go  # Connection search
│   │   ├── disruptions.go
│   │   ├── station.go  # Station info + elevators
│   │   ├── lookup.go   # Ticket lookup by code
│   │   └── onboard.go  # ICE portal (when on train)
│   ├── auth/
│   │   ├── oidc.go     # PKCE flow + local callback server
│   │   ├── tokens.go   # JWT parsing, storage, refresh
│   │   └── session.go  # "Give me an authenticated HTTP client"
│   ├── api/
│   │   ├── personal.go # bahn.de internal web API (authenticated)
│   │   ├── ris.go      # Official RIS API (public, API key)
│   │   ├── vendo.go    # Vendo/movas API (public, no key)
│   │   └── iceportal.go# Onboard train API
│   ├── model/          # Shared types (all JSON-tagged)
│   │   ├── trip.go
│   │   ├── station.go
│   │   ├── board.go
│   │   └── disruption.go
│   ├── output/
│   │   └── output.go   # JSON to stdout, diagnostics to stderr
│   └── config/
│       └── config.go   # Config file + env vars
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

### Language: Go

- Single static binary — `brew install` or download, no runtime
- Cobra for CLI
- Cross-compiles for macOS/Linux
- goroutines for concurrent API calls

## API Layers

### Layer 1: Public APIs (no auth)

**Vendo/Movas API** (bahn.de backend)
- No key needed, aggressive rate limiting
- Used by db-vendo-client community
- Journeys, departures, arrivals

**Official RIS API** (`developers.deutschebahn.com`)
- Free API key registration
- Rate limited but stable
- Boards, journeys, disruptions, stations

### Layer 2: Personal API (Keycloak OIDC auth)

**bahn.de Internal Web API**
- Auth: JWT via Keycloak OIDC flow (see `docs/auth-foundation.md`)
- 5-minute token lifetime, silent refresh via Keycloak session
- Bookings, trips, travel chains, coach sequences, profile

### Layer 3: Onboard API (WiFi only)

**ICE Portal** (`iceportal.de`)
- Only works on WIFIonICE
- Speed, GPS, next stop, full trip with real-time delays
- No auth needed

### Layer 4: Ticket Lookup (no auth)

**Booking code + last name** → ticket details, journey legs, price

## Commands

All commands output JSON to stdout by default. Diagnostics/errors go to stderr.

### Public (no auth)

```bash
bahn board "Leipzig Hbf"                    # Departures (JSON)
bahn board "Leipzig Hbf" --arrivals
bahn board "Leipzig Hbf" --duration 4h
bahn board "Leipzig Hbf" --filter ICE

bahn journey "Leipzig" "Berlin"
bahn journey "Leipzig" "Berlin" --time 14:00 --date 2026-02-20

bahn disruptions
bahn disruptions --station "Leipzig Hbf"
bahn disruptions --line "ICE 1600"

bahn station "Leipzig Hbf"
bahn station "Leipzig Hbf" --elevators

bahn lookup W7KHTA Martin                   # Ticket by booking code
```

### Personal (auth required)

```bash
bahn auth login                              # OIDC browser flow (one-time setup)
bahn auth status                             # Token validity, profile info
bahn auth refresh                            # Silent token refresh
bahn auth token <jwt>                        # Manual fallback
bahn auth clear

bahn trips                                   # Upcoming booked trips
bahn trips --past --days 90
bahn trips <uuid>                            # Full trip detail (stops, delays, seat, booking ref)

bahn watch                                   # One-shot: all upcoming trips, delays + platform changes
bahn watch --threshold 5                     # Only delays > 5 min
```

### Onboard (ICE WiFi only)

```bash
bahn onboard                                 # Current train status (speed, GPS, next stop)
bahn onboard --trip                          # Full trip with all stops + delays
```

### Global Flags

```
--human         Human-readable output (opt-in, not default)
--quiet         Suppress stderr diagnostics
--verbose       Extra detail in stderr
--config        Config file path
--api-key       RIS API key (or BAHN_API_KEY env)
```

## Output Contract

**stdout:** Always valid JSON (or nothing on error). This is the agent's data channel.

**stderr:** Human-readable diagnostics, warnings, progress info. The agent reads this for error handling.

**Exit codes:**
- `0` — success, JSON on stdout
- `1` — general error
- `2` — auth required (token expired/missing)
- `3` — network error (API unreachable)
- `4` — not found (station/trip/ticket doesn't exist)

**Error format (stdout on non-zero exit):**
```json
{
  "error": "token_expired",
  "message": "Access token expired at 2026-02-13T16:25:38Z",
  "action": "run `bahn auth refresh` or `bahn auth login`"
}
```

The `action` field tells the agent exactly what to do to recover.

## How Ori Uses This

### In heartbeats / cron jobs
```bash
# Morning briefing: any travel today? Any disruptions at home station?
bahn trips
bahn disruptions --station "Leipzig Hbf"

# Pre-departure check (cron, 30 min before departure)
bahn watch

# Post-trip Fahrgastrechte check
bahn trips <uuid>  # Check actual arrival vs scheduled
```

### In conversation
```
Nate: "I need to go to Berlin next Tuesday"
Ori: *runs* bahn journey "Leipzig" "Berlin" --date 2026-02-18
Ori: "Direct ICE options: 8:23 (arrive 9:35), 10:23 (arrive 11:35).
      The 10:23 has Sparpreis at €17.90."
```

### Cross-referencing
Ori combines bahn-cli data with:
- **Calendar** — "Your train arrives at 15:47, meeting at 16:00"
- **Weather** — "It's -3°C at Leipzig Hbf, platform 11 is outdoors"
- **Location context** — "You're at home, 20 min to Hbf by tram"
- **History** — "This ICE 1556 was late 3 of the last 5 times"

### Fahrgastrechte tracking
Ori maintains `~/clawd/memory/fahrgastrechte.json` by checking completed trips:
```json
{
  "claims": [{
    "tripId": "...",
    "date": "2026-02-15",
    "route": "Leipzig Hbf → Berlin Hbf",
    "train": "ICE 1556",
    "scheduledArrival": "16:12",
    "actualArrival": "17:25",
    "delayMinutes": 73,
    "ticketPrice": 45.90,
    "refundPercent": 25,
    "refundAmount": 11.48,
    "status": "pending"
  }]
}
```

## Data Flow

```
  bahn-cli (data source)
      │ JSON stdout
      ▼
  Ori (reasoning + context)
      │ natural language
      ▼
  Nate (via WhatsApp/chat)
```

bahn-cli never sends notifications. It outputs structured data. Ori decides what's worth telling Nate, when, and how.

## Config

`~/.config/bahn-cli/config.toml`
```toml
[api]
ris_key = ""                    # Optional: RIS API key
default_station = "Leipzig Hbf"

[output]
format = "json"                 # json (default) | human

[watch]
threshold_minutes = 5
check_before_hours = 4
```

## Build & Distribution

```makefile
build:
	go build -o bin/bahn ./cmd/bahn

install:
	go install ./cmd/bahn

test:
	go test ./...
```

goreleaser for macOS (arm64, amd64) + Linux. Homebrew tap: `brew install havocked/tap/bahn-cli`.

## Project Phases

### Phase 1: Auth + First Data
- [ ] Project scaffold (Go modules, Cobra, config)
- [ ] OIDC browser login flow (Keycloak PKCE)
- [ ] Token storage + refresh
- [ ] `auth login/status/refresh/token/clear`
- [ ] `trips` command (list + detail)
- [ ] JSON output contract
- [ ] Tests

### Phase 2: Public Data
- [ ] `board` command (departures/arrivals)
- [ ] `journey` command (connection search)
- [ ] `disruptions` command
- [ ] `station` command
- [ ] `lookup` command (ticket by code)

### Phase 3: Integration
- [ ] `watch` command (delay monitoring)
- [ ] OpenClaw skill (SKILL.md)
- [ ] Fahrgastrechte tracking
- [ ] Morning briefing integration
- [ ] Cron job templates

### Phase 4: Extras
- [ ] `onboard` command (ICE portal)
- [ ] Connection risk detection
- [ ] Homebrew tap

## Dependencies

```
github.com/spf13/cobra           # CLI framework
github.com/spf13/viper           # Config
github.com/golang-jwt/jwt/v5     # JWT parsing
github.com/zalando/go-keyring    # OS keyring
github.com/pkg/browser           # Open URL in browser (auth only)
```

Minimal. No output formatting libraries — JSON is the format.

---

*A tool built for Ori. JSON in, reasoning out.*
