# Configuration

Every config key and environment variable Panal reads, in one place.
Nothing is required: without a config file Panal uses the defaults below.

See also: [Getting started](getting-started.md) · [Router](router.md) ·
[Models](models.md) · back to the [README](../README.md).

## The file

`~/.panal/panal.conf` (on Windows `%USERPROFILE%\.panal\panal.conf`), or
the path in `PANAL_CONF`; `PANAL_DATA` moves the whole `~/.panal`
directory. One `key = value` per line; `#` starts a comment, also at the
end of a line (after a space). Keys are case-insensitive. Unknown keys are
ignored.

The dashboard re-reads the file when it changes (the `off` agents and
anything derived from `chain` / `pool`); the other keys are read when a
command starts.

**Precedence:** command-line flag > environment variable > `panal.conf` >
default.

```ini
# ~/.panal/panal.conf
chain        = codex agy                 # panal delegate's default chain; off = the rest
theme        = contrast
alerts       = bell
every        = 5s
claude_price = 15
pool         = auto
models       = codex:gpt-6-luna* agy:gemini-3.8-flash-*
```

## Keys

### Dashboard

| key | default | meaning | flag / variable that wins |
|---|---|---|---|
| `off` | derived from the chain or pool | agents you don't use, comma-separated: while idle they get one grey `⏻ off` line instead of a card. When unset, the agents not in `panal delegate`'s chain (or the router's pool) are off. `off =` (empty) shows them all | `-off a,b` (`-off ""` shows all) |
| `theme` | `auto` | `auto`, `dark`, `light` or `contrast` | `-theme`, `PANAL_THEME` |
| `animation` | on | `no` / `off` / `false` / `0`: still mascots, no reactions | `-no-animation`, `PANAL_NO_ANIMATION` |
| `alerts` | `all` | `all` (Windows notification and bell), `bell` or `none` | `-alerts` |
| `every` | `2s` | refresh interval (a Go duration) | `-every` |
| `live_quota` | yes | `no`: don't ask the CLIs for their quota | `PANAL_LIVE_QUOTA` |
| `ntfy` | unset | an ntfy topic URL: alerts also go there | |
| `webhook` | unset | a URL that takes a JSON POST (Slack, Discord…): alerts also go there | |
| `claude_price` | unset | $ per million tokens, to estimate what delegating saved in the report | |

### Delegating and the router

| key | default | meaning | wins over it |
|---|---|---|---|
| `chain` | every installed one of codex, agy, opencode, cursor | `panal delegate`'s fallback chain, `cli:model[:effort] ...`, or `auto` | `-c`, `PANAL_CHAIN` |
| `pool` | the chain, else every installed CLI | the arms the router picks among, cheapest first, or `auto` (the discovered models). With a pool and no chain, `auto` is the default | `-p` (route), `PANAL_POOL` |
| `router` | unset | an external classifier command (no shell): see [Router](router.md#plug-in-your-own-classifier) | `PANAL_ROUTER` |
| `models` | every discovered model | allow patterns for `pool = auto`: see [Models](models.md#pool--auto-and-the-rules) | |
| `exclude` | unset | deny patterns for `pool = auto` | |
| `pool_max_per_cli` | `4` | at most this many arms per CLI in an auto pool | |

### Multiple machines

| key | default | meaning | wins over it |
|---|---|---|---|
| `serve` | unset | an address (`:8765`): the dashboard also serves this machine's status there | |
| `serve_token` | unset | the token callers must bring; nothing is served without it | `PANAL_TOKEN` |
| `machine` | none | `name URL token`, one line per machine to watch | |

See [Reference: multiple machines](reference.md#multiple-machines).

## Environment variables

| variable | default | meaning |
|---|---|---|
| `PANAL_CONF` | `~/.panal/panal.conf` | the config file |
| `PANAL_DATA` | `~/.panal` | Panal's own directory (runs, logs, caches, ratings, samples, Claude's status line copy) |
| `PANAL_RUNS` | `$PANAL_DATA/runs` | where run files are written and read |
| `PANAL_LOGS` | `$PANAL_DATA/logs` | where attempt logs are written and read |
| `PANAL_CLAUDE` | `$PANAL_DATA/claude/statusline.json` | the copy of Claude Code's status line JSON (read by the dashboard, written by `scripts/statusline.sh`) |
| `PANAL_STATUSLINE_NEXT` | unset | `scripts/statusline.sh` only: a status line command to show instead of the agents line |
| `PANAL_CHAIN` | unset | `panal delegate`'s chain (wins over `chain`) |
| `PANAL_POOL` | unset | the router's pool (wins over `pool`) |
| `PANAL_ROUTER` | unset | the external router command (wins over `router`) |
| `PANAL_CODEX_CREDITS` | unset | `1`: let `panal delegate` run codex when it would spend paid credits |
| `PANAL_THEME` | unset | the theme (`-theme` wins) |
| `PANAL_NO_ANIMATION` | unset | any value: still mascots |
| `PANAL_LIVE_QUOTA` | unset | `no`: don't ask the CLIs for their quota (wins over `live_quota`) |
| `PANAL_TOKEN` | unset | the `-serve` token (wins over `serve_token`) |
| `NO_COLOR` | unset | any value: no colors and no mascots |
| `CODEX_HOME` | `~/.codex` | the codex card reads `$CODEX_HOME/sessions` |
| `CODEX_SESSIONS` | `~/.codex/sessions` | the codex sessions history, tokens and credits are read from |
| `OPENCODE_LOG` | `~/.local/share/opencode/log/opencode.log` | opencode's log |
| `OPENCODE_DB` | `~/.local/share/opencode/opencode.db` | opencode's database (opened read-only) |
| `OPENCODE_API_KEY` | from `~/.local/share/opencode/auth.json` | the key for opencode's Go plan usage endpoint |
| `DELEGAR_ESTADOS`, `DELEGAR_REGISTROS` | `~/.ct-delegar/estado`, `~/.ct-delegar/registros` | run and log directories of the older delegar.sh helper, read when they exist |

`~` is `%USERPROFILE%` on Windows (Panal checks `USERPROFILE` first, then
the OS home directory).

## Files in `~/.panal`

Panal writes only here (or wherever `PANAL_DATA` and friends point):

| file | written by | what |
|---|---|---|
| `panal.conf` | you | this configuration |
| `runs/*.json`, `runs/*.task.md` | `panal delegate` | [run files](reference.md#run-files) and full multi-line tasks |
| `logs/*.txt` (+ `.jsonl`, `.last`, `.log`) | `panal delegate` | each attempt's output |
| `claude/statusline.json` | `scripts/statusline.sh` | Claude Code's status line JSON |
| `samples.json` | the dashboard | quota samples for the forecast (at most once a minute and on exit; only from the interactive UI) |
| `feedback.json` | `panal feedback`, `+` / `-` in History | your ratings |
| `router-checks.json` | `panal delegate`, `panal route` | cached checks of finished runs (rules broken, tests) |
| `models.json` | `panal models`, the dashboard, `route`, `delegate`, `-doctor` | the discovered models |
