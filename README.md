# sshm

A CLI tool to connect to EC2 instances via AWS SSM Session Manager with a human-friendly interface. Instead of memorizing instance IDs, browse and filter instances by name across multiple AWS accounts using IAM Identity Center (SSO).

## Features

- **Interactive instance selection** with real-time filtering (type to search)
- **Multi-account support** — discovers instances across all SSO-configured AWS accounts
- **Two modes of operation:**
  - **Profile mode** (default): uses `~/.aws/config` SSO profiles
  - **Discover mode** (`-d`): dynamically lists accounts/roles via SSO API — no profiles needed
- **Local caching** — instance lists are cached for 30 days for instant startup
- **Name filtering** — pass a name pattern as an argument to skip the menu

## Prerequisites

- [AWS CLI v2](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html)
- [Session Manager Plugin](https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html)
- AWS IAM Identity Center (SSO) configured and logged in (`aws sso login`)

## Installation

```bash
go install github.com/takah/sshm@latest
```

Or build from source:

```bash
git clone https://github.com/takah/sshm.git
cd sshm
go build -o sshm .
```

## Usage

```bash
# Interactive mode — list all instances, pick one
sshm

# Filter by name — connect directly if single match
sshm web-server

# Discover mode — pick account/role/region via SSO API (no profiles needed)
sshm -d

# Discover mode with name filter
sshm -d api

# Refresh cached instance list
sshm --update-cache

# Clear all cached data
sshm --clear-cache
```

## How it works

### Profile mode (default)

1. Reads SSO profiles from `~/.aws/config`
2. Queries each account in parallel for SSM-managed EC2 instances
3. Presents an interactive list with filtering
4. Connects via `aws ssm start-session`

### Discover mode (`-d`)

1. Reads `[sso-session]` from `~/.aws/config`
2. Uses the cached SSO token from `aws sso login`
3. Lists available accounts → select one
4. Lists available roles → select one
5. Select a region
6. Discovers instances and connects

## AWS config example

```ini
[sso-session my-org]
sso_start_url = https://my-org.awsapps.com/start
sso_region = ap-northeast-1
sso_registration_scopes = sso:account:access

[profile dev-admin]
sso_session = my-org
sso_account_id = 111111111111
sso_role_name = AdministratorAccess
region = ap-northeast-1

[profile prod-readonly]
sso_session = my-org
sso_account_id = 222222222222
sso_role_name = ReadOnlyAccess
region = ap-northeast-1
```

## Cache

Instance lists are cached in `~/.sshm/cache/` with a 30-day TTL.

- `sshm --update-cache` — fetch fresh data and update the cache
- `sshm --clear-cache` — delete all cached data

## License

MIT
