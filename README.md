# cf — Cloudflare CLI

A fast, beautiful Cloudflare command-line tool for human operators and AI assistants.

## Features

- **DNS management** — create A, AAAA, CNAME, MX, TXT, and NS records
- **Ray ID lookup** — investigate blocked/challenged requests by Ray ID via Cloudflare GraphQL Analytics
- **Beautiful human output** — styled tables and colored output powered by [Charm](https://charm.sh)
- **AI-friendly mode** — `--json` flag emits pure structured JSON; no ANSI, no spinners, fully pipeable

## Installation

```bash
go install github.com/ssccio/cloudflare-go@latest
```

Or build from source:

```bash
git clone https://github.com/ssccio/cloudflare-go.git
cd cloudflare-go
go build -o cf .
```

## Authentication

Set your Cloudflare API token in the environment:

```bash
export CLOUDFLARE_API_TOKEN=your_token_here
```

Or pass it per-command:

```bash
cf --token your_token_here dns list --zone ZONE_ID
```

## Usage

### Global Flags

| Flag | Description |
|------|-------------|
| `--json` | Machine-readable JSON output (ideal for AI assistants and scripts) |
| `--no-color` | Disable ANSI color output |
| `-q, --quiet` | Suppress progress and informational lines |
| `--token` | Cloudflare API token (overrides `CLOUDFLARE_API_TOKEN`) |

---

### DNS Records

#### Create a record

```bash
# A record
cf dns create --zone ZONE_ID --name example.com --type A --content 1.2.3.4

# AAAA record
cf dns create --zone ZONE_ID --name example.com --type AAAA --content 2001:db8::1

# CNAME with proxy enabled
cf dns create --zone ZONE_ID --name www --type CNAME --content example.com --proxied

# MX record
cf dns create --zone ZONE_ID --name example.com --type MX --content mail.example.com --ttl 3600

# TXT record
cf dns create --zone ZONE_ID --name example.com --type TXT --content "v=spf1 include:example.com ~all"

# JSON output for AI assistants
cf dns create --zone ZONE_ID --name api --type A --content 1.2.3.4 --json
```

#### List records

```bash
# All records in a zone
cf dns list --zone ZONE_ID

# Filter by type
cf dns list --zone ZONE_ID --type A

# Filter by exact name
cf dns list --zone ZONE_ID --name example.com

# JSON output
cf dns list --zone ZONE_ID --json
```

---

### Ray ID Lookup

Look up why a request was blocked, challenged, or allowed by Cloudflare:

```bash
cf rayid lookup 7f9b3c1a4e5d6f8a --zone ZONE_ID

# JSON output
cf rayid lookup 7f9b3c1a4e5d6f8a --zone ZONE_ID --json
```

**Sample output:**

```
Ray ID          7f9b3c1a4e5d6f8a
Datetime        2026-02-24T10:30:00Z
Action          BLOCK
Source          firewallRules
Rule ID         abc123def456
Client IP       1.2.3.4
Country         US
Method          GET
Host            example.com
Path            /admin
User Agent      curl/7.68.0
```

> **Note:** Ray ID lookup queries the Cloudflare GraphQL Analytics API. It returns firewall events from
> the Cloudflare security event log for the given zone. Results are typically available within seconds
> and retained for the duration configured in your Cloudflare plan.

---

## AI Assistant Usage

When using `cf` from an AI assistant (Claude, GPT, etc.), always pass `--json`:

```bash
# Create record and capture result
RESULT=$(cf dns create --zone $ZONE_ID --name api --type A --content 1.2.3.4 --json)
RECORD_ID=$(echo "$RESULT" | jq -r .id)

# List all A records as JSON
cf dns list --zone $ZONE_ID --type A --json | jq '.[].content'

# Investigate a Ray ID
cf rayid lookup $RAY_ID --zone $ZONE_ID --json | jq '{action: .events[0].action, rule: .events[0].ruleId}'
```

The `--json` flag:
- Writes pure JSON to stdout (no ANSI codes, no spinners, no progress lines)
- Implies `--quiet` automatically
- Returns exit code `0` on success, non-zero on error
- Progress/errors always go to stderr; data always goes to stdout

## Development

```bash
# Install pre-commit hooks
pip install pre-commit
pre-commit install

# Run tests
go test ./...

# Lint
golangci-lint run
```

## License

MIT
