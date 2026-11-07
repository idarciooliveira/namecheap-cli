# Using ncli (for agents)

`ncli` reads the user's Namecheap account. It is read-only: nothing it does costs money or changes the account.

Output is JSON when stdout is not a terminal. Pass `--json` to be explicit. Errors go to stderr as `{"error":{"code","message","hint"}}`.

## Which command

| Question | Command |
|---|---|
| How many domains? | `ncli domains count --json` |
| What expires soon? | `ncli domains expiring --within 60d --json` |
| Full list | `ncli domains list --json` |
| One domain's details | `ncli domains info example.com --json` |
| Is a name available? | `ncli check myidea --tlds com,io,dev --json` |
| Is this exact domain free? | `ncli check example.dev --json` |
| What does a TLD cost? | `ncli price io dev --json` |
| Which domains use Private Email? | `ncli mail status --private-only --json` |
| Is mail DNS correct (MX/SPF/DKIM/DMARC)? | `ncli mail status --json` |
| Full mail check for one domain (DNS + IMAP/SMTP TLS) | `ncli mail check example.com --json` |
| Does the setup work? | `ncli auth status --json` |

## Output shapes

- `domains list|expiring`: `{"total", "domains":[{"domain","created","expires","daysLeft","expired","autoRenew","locked","privacy","premium","namecheapDns"}]}`. Dates are `YYYY-MM-DD`. `daysLeft` is negative for expired domains.
- `domains count`: `{"total","active","expired","expiringIn30Days","expiringIn60Days","expiringIn90Days","autoRenewOn","autoRenewOff"}`
- `check`: `{"total","available","results":[{"domain","available","premium","registerPrice","renewPrice","currency","minYears","icannFee","error"}]}`. Prices are per-year USD numbers or `null`, for 1 year or the TLD's minimum term (`minYears`, e.g. 2 for .ai). `icannFee` is extra per year and not included in the price. Premium domains use Namecheap's premium price.
- `price`: `{"prices":[{"tld","register","renew","currency","minYears","icannFee","error"}]}`

- `mail status`: `{"total","privateEmail","domains":[{"domain","service","privateEmail","namecheapEmailType","dns","records":{"mx","spf","dkim","dmarc"},"issues","ok","note"}]}`. `service` is `private-email`, `forwarding`, `google-workspace`, `microsoft-365`, `custom-mx` or `none`. Each record is `{"status","records","detail"}` with status `ok`, `missing`, `wrong`, `unknown` or `error`.
- `mail check`: same as one `mail status` entry, plus `server` and `endpoints:[{"service","port","mode","ok","tlsVersion","certSubject","certExpires","certDaysLeft","latencyMs","error"}]`.

Private Email subscription expiry, plan and mailboxes are not in Namecheap's API. Tell the user to check the dashboard for those.

## Exit codes

`0` ok, `1` API error, `2` bad usage, `3` not configured / bad key / IP not whitelisted. On exit 3, tell the user to run `ncli auth status` and follow the hint. Don't try to fix credentials yourself.

## Limits

50 API calls/min per key. `check` sends up to 50 domains per call, so prefer one `check` with many names over many separate calls.
