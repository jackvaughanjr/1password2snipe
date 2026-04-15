# 1password2snipe

[![Latest Release](https://img.shields.io/github/v/release/jackvaughanjr/1password2snipe)](https://github.com/jackvaughanjr/1password2snipe/releases/latest) [![Go Version](https://img.shields.io/github/go-mod/go-version/jackvaughanjr/1password2snipe)](go.mod) [![License](https://img.shields.io/github/license/jackvaughanjr/1password2snipe)](LICENSE) [![Build](https://github.com/jackvaughanjr/1password2snipe/actions/workflows/release.yml/badge.svg)](https://github.com/jackvaughanjr/1password2snipe/actions/workflows/release.yml) [![Go Report Card](https://goreportcard.com/badge/github.com/jackvaughanjr/1password2snipe)](https://goreportcard.com/report/github.com/jackvaughanjr/1password2snipe) [![Downloads](https://img.shields.io/github/downloads/jackvaughanjr/1password2snipe/total)](https://github.com/jackvaughanjr/1password2snipe/releases)

Syncs active 1Password Business members into Snipe-IT as license seat assignments.

Reads all active (non-suspended) members from your 1Password SCIM Bridge and maps
each one to a seat on a named Snipe-IT software license. Seats are checked out when
a user becomes active and checked in when they are suspended or removed. The seat
`notes` field records the member's current 1Password role(s) (e.g. `roles: Administrator`).

## Requirements

- 1Password Business account with SCIM Bridge deployed
- SCIM Bearer token from Admin Console → Integrations → SCIM Bridge
- Snipe-IT instance with API key (license management permissions)

## Installation

Download a pre-built binary from the [latest release](https://github.com/jackvaughanjr/1password2snipe/releases/latest):

    # macOS (Apple Silicon)
    curl -L https://github.com/jackvaughanjr/1password2snipe/releases/latest/download/1password2snipe-darwin-arm64 -o 1password2snipe
    chmod +x 1password2snipe

    # Linux (amd64)
    curl -L https://github.com/jackvaughanjr/1password2snipe/releases/latest/download/1password2snipe-linux-amd64 -o 1password2snipe
    chmod +x 1password2snipe

    # Linux (arm64)
    curl -L https://github.com/jackvaughanjr/1password2snipe/releases/latest/download/1password2snipe-linux-arm64 -o 1password2snipe
    chmod +x 1password2snipe

Or build from source:

    git clone https://github.com/jackvaughanjr/1password2snipe
    cd 1password2snipe
    go build -o 1password2snipe .

## Configuration

Copy `settings.example.yaml` to `settings.yaml` and fill in your values:

```yaml
onepassword:
  url: "https://your-scim-bridge.example.com"
  api_token: "your-scim-bearer-token"
  include_guests: false   # set true to also sync Guest-role members

snipe_it:
  url: "https://your-snipe-it-instance.example.com"
  api_key: "your-snipe-it-api-key"
  license_name: "1Password Business"
  license_category_id: 5   # required — find at Admin → Categories
  license_seats: 50         # optional — your purchased seat count (1Password has no API for this)
```

All values can be set via environment variables instead:

| Variable | Config key |
|---|---|
| `OP_SCIM_URL` | `onepassword.url` |
| `OP_SCIM_TOKEN` | `onepassword.api_token` |
| `SNIPE_URL` | `snipe_it.url` |
| `SNIPE_TOKEN` | `snipe_it.api_key` |
| `SLACK_WEBHOOK` | `slack.webhook_url` |

## Usage

**Validate connections and report current state:**

    ./1password2snipe test

**Dry-run (simulate without making changes):**

    ./1password2snipe sync --dry-run

**Run a full sync:**

    ./1password2snipe sync

**Sync a single user:**

    ./1password2snipe sync --email user@example.com

**Create Snipe-IT accounts for unmatched users:**

    ./1password2snipe sync --create-users

**Include Guest-role members:**

    ./1password2snipe sync --include-guests

## Global flags

| Flag | Description |
|---|---|
| `--config` | Path to config file (default: `settings.yaml`) |
| `-v, --verbose` | INFO-level logging |
| `-d, --debug` | DEBUG-level logging |
| `--log-file` | Append logs to a file |
| `--log-format` | `text` (default) or `json` |

## How it works

1. Fetches all active members from the 1Password SCIM Bridge (`active eq true`)
2. Optionally filters out Guest-role members (default: excluded)
3. Finds or creates the target license in Snipe-IT
4. Expands the seat count if more active members than current seats
5. Checks out a seat for each active member not yet assigned one
6. Updates seat notes if the member's role has changed
7. Checks in seats for members who are no longer active

## Member roles

1Password Business has five member types recorded in seat notes:

| Role | Description |
|---|---|
| `OWNER` | Account owner — full control |
| `ADMIN` | Administrator — can manage members and vaults |
| `MEMBER` | Standard member |
| `RECOVERY` | Recovery team member |
| `GUEST` | Guest — limited vault access (excluded by default) |

## Slack notifications

Set `slack.webhook_url` (or `SLACK_WEBHOOK`) to receive notifications on sync
completion, failures, and unmatched users. Suppressed in dry-run and with `--no-slack`.

## Version History

| Version | Key changes |
|---------|-------------|
| v1.0.0 | Initial scaffold — 1Password SCIM → Snipe-IT license seat sync |
| v1.0.1 | Fixed SCIM URL normalization; dropped unreliable server-side filter |
| v1.0.2 | Fixed ghost cleanup consuming all free seats on a newly created license |
| v1.1.0 | Added `snipe_it.license_seats` override for purchased seat count |
