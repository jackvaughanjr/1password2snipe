# 1password2snipe — Integration Context

Sync active 1Password Business members into Snipe-IT as license seat assignments.

---

## Purpose

Reads all active (non-suspended) 1Password Business members via the SCIM 2.0
interface exposed by the 1Password SCIM Bridge, and maps them to seats on a
named Snipe-IT software license. Seats are checked out when a user becomes
active and checked in when they are suspended or removed.

The seat `notes` field records the member's current 1Password role(s) (e.g.
`roles: Administrator`) so Snipe-IT reflects access level alongside the seat.

---

## Authentication

**Method:** HTTP Bearer token  
**Issued from:** 1Password Business Admin Console → Integrations → SCIM Bridge

The SCIM bridge is a separate service that must be deployed and configured by
the 1Password administrator. It exposes a SCIM 2.0 API at a customer-controlled
URL. The bearer token is generated once in the admin console and does not expire
unless revoked.

Set the token in `settings.yaml` (`onepassword.api_token`) or the `OP_SCIM_TOKEN`
environment variable.

---

## API

**Protocol:** SCIM 2.0 (RFC 7644)  
**Base URL:** Configured as `onepassword.url` — the root of the SCIM bridge  
**Prefix:** `/scim/v2` (appended by the client)

### Endpoints used

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/scim/v2/ServiceProviderConfig` | Connectivity probe |
| GET | `/scim/v2/Users?startIndex=N&count=100` | Paginated member list (filtered client-side on `active`) |

### Pagination

SCIM uses 1-based `startIndex` / `count` / `totalResults`. The client loops
until `startIndex + len(page) - 1 >= totalResults`.

### Response shape (Users list)

```json
{
  "totalResults": 42,
  "startIndex": 1,
  "itemsPerPage": 100,
  "Resources": [
    {
      "id": "AAAA-BBBB-CCCC",
      "userName": "user@example.com",
      "name": { "givenName": "Jane", "familyName": "Doe" },
      "active": true,
      "roles": [{ "value": "MEMBER", "display": "Member" }]
    }
  ]
}
```

---

## Config schema (`settings.yaml` / env overrides)

```yaml
onepassword:
  url: "https://your-scim-bridge.example.com"      # OP_SCIM_URL
  api_token: ""                                     # OP_SCIM_TOKEN
  include_guests: false                             # include Guest-role members in sync

snipe_it:
  url: "https://your-snipe-it-instance.example.com" # SNIPE_URL
  api_key: ""                                       # SNIPE_TOKEN
  license_name: "1Password Business"
  license_category_id: 0                            # required; find at Admin → Categories
  license_seats: 0                                  # optional; 0 = use active member count as floor
  license_manufacturer_id: 0                        # optional; 0 = auto find/create "1Password"
  license_supplier_id: 0                            # optional; 0 = omit

sync:
  dry_run: false
  force: false
  rate_limit_ms: 500

slack:
  webhook_url: ""                                   # SLACK_WEBHOOK
```

### Full env var override table

| Env var | Config key |
|---------|-----------|
| `OP_SCIM_URL` | `onepassword.url` |
| `OP_SCIM_TOKEN` | `onepassword.api_token` |
| `SNIPE_URL` | `snipe_it.url` |
| `SNIPE_TOKEN` | `snipe_it.api_key` |
| `SLACK_WEBHOOK` | `slack.webhook_url` |

---

## File structure

```
main.go
cmd/
  root.go          # cobra root; viper init; env bindings; logging init (PersistentPreRunE)
  sync.go          # sync command; --include-guests flag; Slack notifications
  test.go          # test command; Ping probe; active user count; license state
internal/
  onepassword/
    client.go      # SCIM 2.0 client: Ping, ListActiveUsers, pagination
  slack/
    client.go      # verbatim from docs/source-files.md
  snipeit/
    client.go      # verbatim from docs/source-files.md
  sync/
    syncer.go      # core sync logic; guest filtering; role-based notes
    result.go      # Result struct (verbatim)
.github/
  workflows/
    release.yml    # cross-platform binary release on v* tag
go.mod
settings.example.yaml
README.md
CONTEXT.md
.gitignore
```

---

## 1Password-specific gotchas

### Member roles

1Password Business has five member types. Their SCIM `roles[].value` strings are:

| SCIM value | Meaning |
|---|---|
| `OWNER` | Account owner — full control |
| `ADMIN` | Administrator — can manage members and vaults |
| `MEMBER` | Standard member |
| `RECOVERY` | Recovery team member — emergency account recovery only |
| `GUEST` | Guest — access only to vaults explicitly shared with them |

Guests do **not** consume a full Business license seat; they are billed separately
at a lower rate. The `include_guests` config key (default `false`) controls whether
guests are included in the Snipe-IT sync. When `false` (default), any guest who
currently holds a seat will be checked in on the next run.

### Suspended / inactive users

Suspended members have `active: false` in the SCIM response. The client paginates
through all users and filters locally on `u.Active == true`. Server-side filtering
via the SCIM `filter` query parameter is not used — the 1Password SCIM bridge does
not reliably support it and returns 400 for filter queries.

### SCIM bridge URL

The client accepts either the server root (`https://your-bridge.example.com`)
or the full SCIM base path (`https://provisioning.1password.com/scim/v2`).
Any trailing `/scim/v2` suffix is stripped on construction so the client always
works from the server root and appends `/scim/v2` itself. Trailing slashes are
also stripped. The 1Password cloud SCIM bridge URL is
`https://provisioning.1password.com/scim/v2`.

### Purchased seat count not available via API

1Password does not expose the purchased/licensed seat count through any API —
not SCIM, Events API, Connect API, or the Partnership API. The web UI shows it
at 1Password.com → Billing and Seats → Usage, but there is no programmatic
equivalent. Set `snipe_it.license_seats` manually to reflect your purchased seat
count. If unset, the Snipe-IT license tracks active member count as a floor.

### First sync — no license yet

On the first run the license does not exist in Snipe-IT and will be created
automatically with `license_category_id` as required. Dry-run synthesizes a
placeholder license (id=0) so the rest of the run completes meaningfully.

### Ghost checkouts

If seats were previously managed via the Snipe-IT UI and the user was later
removed without checking the seat in, the seat appears free in the seat listing
but Snipe-IT's internal counter still shows it as used. The syncer detects and
cleans up ghost checkouts before the checkout pass.

### Rate limiting

The SCIM bridge does not publish a formal rate limit, but the client paginates
with a 100-item page size and does not need a rate limiter on the 1Password side.
The Snipe-IT client applies its standard 2 req/s limiter for all Snipe-IT calls.
