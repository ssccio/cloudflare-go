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
| `--toon` | Token-Oriented Object Notation output — 30-60% fewer tokens than JSON, ideal for LLMs |
| `--query` | JMESPath expression to filter `--json` or `--toon` output (e.g. `'[].id'`) |
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

# NS record — delegate a subdomain to another provider (e.g. Route 53)
cf dns create --zone ZONE_ID --name aws.example.com --type NS --content ns-123.awsdns-45.com --ttl 172800
cf dns create --zone ZONE_ID --name aws.example.com --type NS --content ns-456.awsdns-67.net --ttl 172800
cf dns create --zone ZONE_ID --name aws.example.com --type NS --content ns-789.awsdns-01.org --ttl 172800
cf dns create --zone ZONE_ID --name aws.example.com --type NS --content ns-012.awsdns-23.co.uk --ttl 172800

# JSON output for AI assistants
cf dns create --zone ZONE_ID --name api --type A --content 1.2.3.4 --json
```

#### Delegating a subdomain to Route 53

To hand off `aws.example.com` (and everything beneath it) to AWS Route 53, add the four NS records
that Route 53 assigns to your hosted zone. Cloudflare will stop answering DNS for that subtree and
forward resolution to Route 53 instead.

```bash
# 1. Look up your zone ID (or use --domain to skip this step)
cf zones lookup example.com

# 2. Add each Route 53 nameserver as an NS record
#    Replace ns-*.awsdns-*.* with the values from your Route 53 hosted zone
ZONE_ID=your_zone_id
for ns in \
  ns-123.awsdns-45.com \
  ns-456.awsdns-67.net \
  ns-789.awsdns-01.org \
  ns-012.awsdns-23.co.uk; do
  cf dns create --zone $ZONE_ID \
    --name aws.example.com \
    --type NS \
    --content "$ns" \
    --ttl 172800
done

# 3. Verify the NS records were created
cf dns list --zone $ZONE_ID --name aws.example.com --type NS
```

> **TTL:** 172800 (48 hours) is the standard delegation TTL — it controls how long resolvers cache
> the delegation before re-checking. Use a lower value (e.g. `3600`) while testing so changes
> propagate faster.

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

When using `cf` from an AI assistant (Claude, GPT, etc.), use `--json` for full output or `--toon`
for token-efficient output. Both support `--query` for JMESPath filtering.

```bash
# Create record and capture result
RESULT=$(cf dns create --zone $ZONE_ID --name api --type A --content 1.2.3.4 --json)
RECORD_ID=$(echo "$RESULT" | jq -r .id)

# List all A records, return only names
cf dns list --zone $ZONE_ID --type A --json --query '[].name'

# Investigate a Ray ID — full event
cf rayid lookup $RAY_ID --zone $ZONE_ID --json

# Extract just action and ray_id using --query (no jq needed)
cf rayid lookup $RAY_ID --zone $ZONE_ID --json --query 'events[*].{ray_id: ray_id, action: action}'

# Token-efficient output for LLM context windows
cf dns list --domain example.com --toon
cf rayid lookup $RAY_ID --domain example.com --toon --query 'events[*].{ray_id: ray_id, action: action}'
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
