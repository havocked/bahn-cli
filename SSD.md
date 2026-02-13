# bahn-cli — System Software Design

## Overview

A Go CLI for Deutsche Bahn that combines public APIs (timetables, disruptions, stations) with authenticated personal endpoints (bookings, trips, delays). Designed as an agent-first tool for OpenClaw integration — not just a query tool, but a proactive travel companion.

## Why This Exists

DB Navigator is a black box. You book a trip, then you're on your own until you're standing on a cold platform wondering why your ICE is 47 minutes late. There's no way to:
- Get notified *before* you leave home that your train is delayed
- Automatically track delays for Fahrgastrechte refund eligibility
- See your upcoming trips in a format an AI assistant can reason about
- Cross-reference disruptions with your actual bookings

bahn-cli fixes this by making DB data scriptable and agent-accessible.

## Architecture

```
bahn-cli
├── cmd/bahn/           # Entry point
├── internal/
│   ├── cmd/            # Cobra command definitions
│   │   ├── root.go
│   │   ├── auth.go     # Cookie import + status
│   │   ├── trips.go    # Personal bookings
│   │   ├── board.go    # Departure/arrival boards
│   │   ├── journey.go  # Connection search
│   │   ├── disruptions.go
│   │   ├── station.go  # Station info
│   │   ├── lookup.go   # Ticket lookup by code
│   │   ├── onboard.go  # ICE portal (when on train)
│   │   └── watch.go    # Delay monitoring daemon
│   ├── auth/
│   │   ├── cookies.go  # Browser cookie import (via sweetcookie pattern)
│   │   ├── jwt.go      # JWT token management + refresh
│   │   └── store.go    # Credential storage (OS keyring)
│   ├── api/
│   │   ├── personal.go # bahn.de internal web API (authenticated)
│   │   ├── ris.go      # Official RIS API (public, API key)
│   │   ├── vendo.go    # Vendo/movas API (public, no key)
│   │   └── iceportal.go# Onboard train API
│   ├── model/          # Shared types
│   │   ├── trip.go
│   │   ├── station.go
│   │   ├── board.go
│   │   └── disruption.go
│   ├── output/         # Formatters
│   │   ├── json.go
│   │   ├── plain.go    # Tab-separated
│   │   └── human.go    # Colorized terminal
│   └── config/
│       └── config.go   # Config file + env vars
├── go.mod
├── go.sum
├── Makefile
├── .goreleaser.yaml     # Cross-platform release
└── README.md
```

### Language: Go

- Single static binary — `brew install` or download, no runtime
- Cobra for CLI (same as gogcli/spogo)
- Cross-compiles for Linux/macOS/Windows
- goroutines for concurrent API calls (check multiple trips in parallel)

## API Layers

### Layer 1: Public APIs (no auth)

**Official RIS API** (`developers.deutschebahn.com`)
- Requires free registration + API key
- Rate limited but stable
- Endpoints: boards, journeys, disruptions, stations

**Vendo/Movas API** (bahn.de backend)
- No key needed, but aggressive rate limiting
- Used by db-vendo-client community
- Journeys, departures, arrivals, tickets/pricing

**Use for:** `board`, `journey`, `disruptions`, `station` commands

### Layer 2: Personal API (cookie auth)

**bahn.de Internal Web API**
- Auth: JWT from browser session cookies
- Cookie import via sweetcookie (Chrome/Firefox/Safari)
- Auto-refresh tokens where possible

**Known endpoints:**
```
GET /web/api/buchung/auftrag/v2
    ?startIndex=0
    &auftraegeReturnSize=10
    &auftragSortOrder=DESCENDING
    &letzterGeltungszeitpunktVor=<ISO-8601>
    &kundenprofilId=<profile-id>
→ List of bookings (Aufträge)

GET /web/api/reisebegleitung/reiseketten
    ?pagesize=100
    &types[]=AUFTRAG
    &types[]=WIEDERHOLEND
→ Travel chains (trips with legs, times, stations)

GET /web/api/reisebegleitung/reiseketten/<uuid>
→ Single trip detail (all stops, delays, platforms)
```

**Use for:** `trips`, `watch` commands

### Layer 3: Onboard API (WiFi only)

**ICE Portal** (`iceportal.de` — only works on WIFIonICE)
```
GET /api1/rs/status      → speed, GPS, next stop, wifi status
GET /api1/rs/tripInfo    → full trip with all stops + real-time delays
```

**Use for:** `onboard` command — fun but niche

### Layer 4: Ticket Lookup (no auth)

**db-tickets pattern:** Query any ticket with booking code + last name.
```
POST https://fahrkarten.bahn.de/mobile/dbc/xs.go
→ Ticket details, journey legs, price
```

**Use for:** `lookup` command

## Commands

### Public (no auth required)

```bash
# Departure board
bahn board "Leipzig Hbf"                    # Next departures
bahn board "Leipzig Hbf" --arrivals         # Arrivals
bahn board "Leipzig Hbf" --duration 4h      # Extended window
bahn board "Leipzig Hbf" --filter ICE       # Only ICE trains

# Search connections
bahn journey "Leipzig Hbf" "Berlin Hbf"
bahn journey "Leipzig" "Berlin" --time 14:00 --date 2026-02-20
bahn journey "Leipzig" "Berlin" --json      # For piping

# Current disruptions
bahn disruptions                             # All current
bahn disruptions --station "Leipzig Hbf"     # For specific station
bahn disruptions --line "ICE 1600"           # For specific line
bahn disruptions --region sachsen            # Regional filter

# Station info
bahn station "Leipzig Hbf"                   # Platforms, services
bahn station "Leipzig Hbf" --elevators       # Elevator/escalator status

# Ticket lookup (no account needed)
bahn lookup W7KHTA Mustermann               # By booking code + name
```

### Personal (cookie auth required)

```bash
# Auth
bahn auth import --browser chrome            # Import cookies
bahn auth import --browser firefox
bahn auth status                             # Check auth + profile info
bahn auth clear                              # Remove stored credentials

# Your trips
bahn trips                                   # Upcoming booked trips
bahn trips --past                            # Past trips
bahn trips --past --days 90                  # Last 90 days
bahn trips <uuid>                            # Detailed trip view

# Delay monitoring
bahn watch                                   # One-shot: check all upcoming trips for delays
bahn watch --daemon                          # Run continuously, output events
bahn watch --threshold 5                     # Only report delays > 5 min
```

### Global Flags

```
--json          Structured JSON output
--plain         Tab-separated, grep-friendly
--no-color      Disable color output
--config        Config file path
--api-key       RIS API key (or BAHN_API_KEY env)
-q, --quiet     Suppress info messages
-v, --verbose   Extra detail
```

## OpenClaw Integration — The Proactive Layer

This is where bahn-cli becomes more than just a query tool. The real value is **OpenClaw turning raw data into timely, contextual actions.**

### 1. Pre-Departure Alerts (Cron)

**Trigger:** Cron job checks your upcoming trips N hours before departure.

```
Schedule: Every 30 min (or dynamic based on next departure)
Payload: agentTurn
Task: "Run bahn watch --json, check for delays/disruptions on upcoming trips.
       If any trip in the next 4h has a delay >5min or platform change,
       alert Nate on WhatsApp with actionable info."
```

**What Ori does with the data:**
- "Your 14:23 ICE to Berlin is running 12 min late — now departing 14:35 from platform 11 (changed from 9). You can leave 10 min later."
- "S-Bahn S1 to the Hauptbahnhof has disruptions — consider taking the tram instead."
- Cross-references with calendar: "You have a meeting at 16:00 in Berlin. Even with the delay, you'd arrive at 15:47. Tight but doable."

### 2. Morning Briefing Enhancement

**Currently:** The morning briefing checks RSS feeds for transit news.
**With bahn-cli:** Direct, structured data.

```bash
# In HEARTBEAT.md morning briefing section:
bahn disruptions --station "Leipzig Hbf" --json
bahn trips --json  # Any travel today?
```

**What changes:**
- Instead of parsing news articles hoping to find "S-Bahn disruption", we get **real-time structured disruption data**
- If you have a trip booked today, it shows up with current delay status
- No more "I think there might be a strike" — it's "S1/S2 suspended until 10:00, replacement bus from Markkleeberg"

### 3. Delay Tracking for Fahrgastrechte

**The money feature.** DB owes you 25% refund for 60min+ delays, 50% for 120min+.

```
Schedule: After each trip's arrival time + 30min
Payload: agentTurn
Task: "Check bahn trips <uuid> --json for final delay data.
       If arrival delay >= 60min, log to ~/clawd/memory/fahrgastrechte.json
       and notify Nate: 'Your ICE 1234 arrived 67 min late. You're owed 25%
       refund (€X.XX). Want me to prepare the claim?'"
```

**Claim workflow:**
1. bahn-cli detects delay ≥ 60min on a completed trip
2. Ori notifies with amount owed
3. On approval, Ori pre-fills the Fahrgastrechte PDF form with trip data
4. Sends it to Nate for signature/submission

**State file:** `~/clawd/memory/fahrgastrechte.json`
```json
{
  "claims": [
    {
      "tripId": "5633beeb-374b-400d-83fd-df46a1020a66",
      "date": "2026-02-15",
      "route": "Leipzig Hbf → Berlin Hbf",
      "train": "ICE 1556",
      "scheduledArrival": "16:12",
      "actualArrival": "17:25",
      "delayMinutes": 73,
      "ticketPrice": 45.90,
      "refundPercent": 25,
      "refundAmount": 11.48,
      "status": "pending",
      "claimedAt": null
    }
  ]
}
```

### 4. Smart Journey Suggestions

When Nate mentions traveling somewhere:

```
Nate: "I need to go to Berlin next Tuesday"
Ori: *runs bahn journey "Leipzig" "Berlin" --date 2026-02-17 --json*
Ori: "Direct ICE options: 8:23 (arrive 9:35), 10:23 (arrive 11:35), 12:23 (arrive 13:35).
      The 10:23 has the cheapest Sparpreis at €17.90. Want me to open the booking page?"
```

No new code needed in Ori — just `bahn journey` + natural language.

### 5. Connection Watch

For multi-leg trips, the scary part is connections.

```
Trigger: 1h before each connection point
Task: "Check if the arriving train is delayed enough to miss the connection.
       If connection is at risk, search alternatives and alert."
```

"Your RE from Leipzig is 8 min late. Your ICE connection in Halle has 12 min buffer — you'll make it, but hustle."

vs.

"Your RE is 25 min late. You'll miss the 14:45 ICE in Halle. Next option: 15:15 ICE (arrives Berlin 16:47 instead of 16:12). Platform 3."

## Data Flow

```
                    ┌──────────────┐
                    │   bahn-cli   │
                    └──────┬───────┘
                           │ JSON stdout
                    ┌──────▼───────┐
                    │   OpenClaw   │
                    │  (cron/hb)   │
                    └──────┬───────┘
                           │ reasoning
                    ┌──────▼───────┐
                    │     Ori      │
                    │  (context +  │
                    │   judgment)  │
                    └──────┬───────┘
                           │ WhatsApp/notification
                    ┌──────▼───────┐
                    │     Nate     │
                    └──────────────┘
```

bahn-cli is the **data source**. Ori is the **brain**. The CLI never sends notifications directly — it outputs structured data that Ori interprets with context (calendar, location, preferences, time of day).

## Auth Strategy

### Cookie Import (spogo-style)
```bash
bahn auth import --browser chrome
# Reads bahn.de cookies from Chrome's cookie store
# Extracts session/JWT tokens
# Stores in OS keyring (or encrypted file)
```

**Cookie targets:**
- `bahn.de` session cookies
- JWT access token (if available in cookies/localStorage)
- `kundenprofilId` (extracted from profile API call after auth)

**Token refresh:**
- JWT tokens from bahn.de are short-lived
- Auto-refresh using refresh token if available
- Fall back to re-importing cookies if refresh fails
- `bahn auth status` shows expiry time

### Fallback: Manual Token
```bash
# For headless setups (Mac Mini)
bahn auth token <jwt-token>
# Paste from browser DevTools Network tab
```

## Config

`~/.config/bahn-cli/config.toml`
```toml
[auth]
# Managed automatically by `bahn auth import`

[api]
ris_key = ""           # Optional: RIS API key for official endpoints
default_station = "Leipzig Hbf"

[output]
format = "human"       # human | json | plain
color = true

[watch]
threshold_minutes = 5  # Min delay to report
check_before_hours = 4 # How early to start monitoring a trip
stations = ["Leipzig Hbf", "Leipzig/Halle Flughafen"]  # Home stations for board checks
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

**goreleaser** for:
- macOS (arm64, amd64)
- Linux (arm64, amd64)
- Homebrew tap: `brew install havocked/tap/bahn-cli`

## Project Phases

### Phase 1: Foundation
- [ ] Project scaffold (Go modules, Cobra, config)
- [ ] `board` command (via db-vendo-client API, no auth)
- [ ] `journey` command (connection search)
- [ ] `disruptions` command
- [ ] `lookup` command (ticket by code)
- [ ] Output modes: `--json`, `--plain`, human
- [ ] Tests

### Phase 2: Personal Data
- [ ] Cookie import from Chrome/Firefox
- [ ] JWT extraction + storage
- [ ] `auth status/import/clear`
- [ ] `trips` command (list + detail)
- [ ] Token refresh logic

### Phase 3: OpenClaw Integration
- [ ] `watch` command (one-shot delay check)
- [ ] OpenClaw skill (SKILL.md + cron templates)
- [ ] Fahrgastrechte tracking (`~/clawd/memory/fahrgastrechte.json`)
- [ ] Morning briefing integration
- [ ] Pre-departure alert cron job

### Phase 4: Polish
- [ ] `watch --daemon` mode
- [ ] `onboard` command (ICE portal)
- [ ] Connection risk detection
- [ ] Homebrew tap
- [ ] `station` with elevator status
- [ ] Fahrgastrechte PDF pre-fill

## Open Questions

1. **Cookie longevity:** How long do bahn.de session tokens last? Need to test. If short-lived, may need periodic re-import or a headless browser refresh approach.

2. **Rate limits on internal API:** Unknown. Need to be conservative and cache aggressively.

3. **kundenprofilId discovery:** The bookings endpoint needs this ID. Need to find which endpoint exposes it after auth (likely a profile/account endpoint).

4. **BahnCard integration:** Can we read BahnCard status/number from the account API? Useful for price calculations.

5. **DB Navigator app API vs web API:** The mobile app may use different (better?) endpoints. Worth investigating if web API proves flaky.

---

*Inspired by steipete/spogo (Spotify) and steipete/gogcli (Google). Same philosophy: CLI-first, JSON-native, agent-friendly. Cookie auth where official APIs fall short.*
