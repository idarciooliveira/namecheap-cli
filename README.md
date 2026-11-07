# ncli

A read-only command-line tool for your Namecheap account. See your domains, when they expire, what's available and what it costs, and whether your email is set up right, without opening the Namecheap dashboard.

Built for people and for AI agents: tables in the terminal, stable JSON when piped.

## Goals

- **Skip the dashboard** for everyday questions: how many domains, what expires soon, is this name free, what does it cost.
- **Agent-friendly**: every command has JSON output, clear exit codes and documented shapes ([AGENTS.md](AGENTS.md)).
- **Safe by design**: read-only. It can't buy, renew or change anything, so it can't spend money.
- **Small and fast**: one Go binary, no runtime.

## Install

macOS / Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/idarciooliveira/namecheap-cli/main/install.sh | sh
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/idarciooliveira/namecheap-cli/main/install.ps1 | iex
```

The scripts download the latest [release](https://github.com/idarciooliveira/namecheap-cli/releases), verify its checksum and install to `~/.local/bin` (Windows: `%LOCALAPPDATA%\ncli\bin`, added to PATH). Pin a version with `NCLI_VERSION=v0.1.0`, or change the folder with `NCLI_INSTALL_DIR`.

With Go: `go install github.com/idarciooliveira/namecheap-cli/cmd/ncli@latest`. From source: `make install`.

## Setup

1. In Namecheap: **Profile > Tools > Namecheap API Access**. Turn it on and copy the API key.
2. In the same page, **whitelist your public IPv4** (`curl https://api.ipify.org`). Add every network you work from.
3. Log in:

```sh
ncli auth login     # asks for username and API key (hidden), saves and verifies
ncli auth status
```

Credentials are stored in `~/.config/ncli/config.json` (mode 0600). Env vars override it: `NAMECHEAP_API_USER`, `NAMECHEAP_API_KEY`, `NAMECHEAP_USERNAME`, `NAMECHEAP_CLIENT_IP`, `NAMECHEAP_SANDBOX=1`.

API access is free, but Namecheap only enables it on accounts with 20+ domains, $50+ balance, or $50+ spent in the last 2 years.

## Commands

```sh
ncli domains list [--search x] [--sort name|expire|created] [--desc]
ncli domains count
ncli domains expiring [--within 60d] [--include-expired]
ncli domains info <domain>

ncli check <name|domain>... [--tlds com,io,dev]   # availability + yearly prices
ncli price <tld>... [--action register|renew|transfer|reactivate]

ncli mail status [domain...] [--private-only]     # Private Email, MX, SPF, DKIM, DMARC
ncli mail check <domain>                          # + IMAP/SMTP TLS checks
```

Add `--json` to force JSON. Exit codes: `0` ok, `1` API error, `2` usage, `3` auth/IP.

## How it works

ncli calls the [Namecheap XML API](https://www.namecheap.com/support/api/methods/) using only read methods:

| Method | Used by |
|---|---|
| `namecheap.domains.getList` | `domains list/count/expiring`, `mail status` |
| `namecheap.domains.getInfo` | `domains info` |
| `namecheap.domains.check` | `check` (50 domains per call) |
| `namecheap.users.getPricing` | `check`, `price` (cached 24h) |
| `namecheap.users.getBalances` | `auth status` |
| `namecheap.domains.dns.getHosts` | `mail status/check` (Namecheap mail setting) |

Mail checks also read public DNS (MX, SPF, DKIM, DMARC) and open TLS connections to `mail.privateemail.com`. Namecheap's API has no Private Email methods, so subscription expiry and mailboxes are not available.

Other details:
- Requests go out over IPv4 only, and your public IP is detected on each run, so switching networks works.
- Calls are throttled under Namecheap's 50/min limit.
- The API key never appears in errors or output.

## Development

```sh
make test      # unit tests, no network
make build     # bin/ncli
make windows   # bin/ncli.exe
```

## License

MIT
