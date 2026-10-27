# Getting started

Install Panal, connect each agent, and check what it can see with
`panal -doctor`. Nothing here is required up front: the dashboard opens
with whatever it finds and tells you what is missing.

See also: [Dashboard](dashboard.md) · [Delegating](delegate.md) ·
[Configuration](configuration.md) · back to the [README](../README.md).

## Install

You need **Go 1.27 or newer** (`go version`).

```sh
go install github.com/AlbertoVasquezR/panal/cmd/panal@latest
```

`go install` puts the binary in `$GOBIN`, or `$(go env GOPATH)/bin` when
that is unset. Make sure that directory is on your `PATH`.

| OS | binary | notes |
|---|---|---|
| Windows | `%USERPROFILE%\go\bin\panal.exe` | The platform Panal is developed and used on. Windows Terminal is recommended. Alerts include a Windows notification. |
| macOS | `~/go/bin/panal` | Builds and runs (process trees are handled with process groups). Less tested: no desktop notification (the bell, ntfy and webhooks still work), and the `w` key opens a Windows Terminal tab, so it does nothing useful there. |
| Linux | `~/go/bin/panal` | Same as macOS. |

From a clone of the repository:

```sh
git clone https://github.com/AlbertoVasquezR/panal
cd panal
go install ./cmd/panal
```

Panal's own data lives in `~/.panal` (on Windows `%USERPROFILE%\.panal`).
`PANAL_DATA` moves it; see [Configuration](configuration.md).

## Connect each agent

Panal only **reads**. For each agent it looks in a few places; `panal
-doctor` lists them with ✔ (found) or ○ (missing) and the variable that
moves each one.

### Claude Code (the orchestrator)

The claude card comes from the JSON Claude Code sends to its status line.
Point the status line at [`scripts/statusline.sh`](../scripts/statusline.sh),
which saves that JSON to `~/.panal/claude/statusline.json` (or
`PANAL_CLAUDE`) and prints your agents on one line. In
`~/.claude/settings.json`:

```json
"statusLine": { "type": "command", "command": "sh /path/to/panal/scripts/statusline.sh" }
```

The script needs `sh` (Git Bash on Windows) and, for the agents line,
`panal` on the `PATH`. If you already have a status line, put its command
in `PANAL_STATUSLINE_NEXT`: it gets the same JSON and is shown instead of
the agents line, while the JSON is still saved for Panal.

From that file the card shows the model, context use, session cost and the
5-hour and weekly rate limits; the last action comes from the session
transcript the JSON points to. While the dashboard is open, Panal also asks
Claude Code for its usage every 5 minutes (see [Live quota](dashboard.md#live-quota)).

### codex

Nothing to set up. Panal reads `~/.codex/sessions/**/*.jsonl` (or
`$CODEX_HOME/sessions`) for rate limits, tokens and credits, and runs
`codex app-server` to ask for live quota without starting a turn. Runs
you launch with `panal delegate` add their logs, activity and tests.

### agy

agy writes nothing Panal can find on its own, so its card comes from runs
launched with `panal delegate` (each one leaves agy's own log next to the
run's log, with its quota summary) and from `agy -p /usage --output-format
json`, which answers without starting a turn.

> Trying `/usage` by hand from Git Bash: Git Bash turns `/usage` into a
> Windows path and agy takes it as a prompt. Use `MSYS_NO_PATHCONV=1`.

### opencode

Panal reads opencode's log (`~/.local/share/opencode/log/opencode.log`, or
`OPENCODE_LOG`) for quota errors and its database
(`~/.local/share/opencode/opencode.db`, or `OPENCODE_DB`) read-only, only
the `message` table. For the Go plan's live usage it reads the key opencode
keeps in `~/.local/share/opencode/auth.json` (or `OPENCODE_API_KEY`) and
sends it only to opencode.ai.

### cursor (`cursor-agent`, also `agent`)

The cursor card comes from runs launched with `panal delegate`. Panal
looks up the CLI as `cursor-agent`, then `agent`, then (on Windows) the
installer shims under `%LOCALAPPDATA%\cursor-agent`. It never writes to
cursor-agent's own files.

There is no live quota. cursor-agent can list models (`cursor-agent
models` / `--list-models`) and show the logged-in account
(`cursor-agent status`) without starting a turn, but account usage is
only in the interactive `/usage` view. The card shows "no live quota"
unless a delegated run ended `out_of_quota`.

`panal delegate` runs it non-interactively (`--trust` and `--workspace`
so headless does not stall on the trust prompt):

```text
cursor-agent -p --output-format stream-json --trust --workspace DIR
             [--model MODEL] --force   # may write; not confined to DIR
cursor-agent -p --output-format stream-json --trust --workspace DIR
             [--model MODEL] --auto-review [--sandbox enabled]   # -r
```

`--mode plan` is not used for `-r`: it rejects even a harmless
read-only command. `--auto-review` lets the server classifier run safe
reads and commands and deny the rest. `--sandbox enabled` is passed only
on macOS and Linux. On Windows the CLI has no sandbox (`Sandbox mode is
enabled but not available on this system`); `-r` is `--auto-review`
alone, which still reads files and runs safe commands.

A chain link is `cursor:<model>[:effort]`. Effort is not a separate flag:
it is passed as `--model model[effort=…]` when the model id has no
brackets. Long tasks go in a file the CLI is told to read.

### Runs from `panal delegate`

Every attempt leaves a run file in `~/.panal/runs` (`PANAL_RUNS`) and its
log in `~/.panal/logs` (`PANAL_LOGS`). Runs recorded by the older
delegar.sh helper (`~/.ct-delegar/estado` and `~/.ct-delegar/registros`)
are read too. See [Delegating](delegate.md).

## First run

```sh
panal -doctor    # sources found, what was read from each agent, models, what to configure
panal            # the dashboard
```

`-doctor` also tries each live quota query once and lists the models each
installed CLI reports (listing only; nothing is spent). Live quota can be
turned off with `live_quota = no` or `PANAL_LIVE_QUOTA=no`.

If no reader finds anything and there are no runs, the dashboard opens
with a welcome screen instead of empty cards: which sources it looks
for, at which path, which exist and what to do next.

To look at the screens without opening the interactive UI:

```sh
panal -once                         # the status as plain text
panal -preview 100                  # the dashboard at 100 columns
panal -preview 132 -screen history  # or table, detail, history-detail, report, timeline, help
```

## Next steps

- Hand a first task to an agent: [Delegating](delegate.md).
- Let the router choose: `panal route "task"`, then
  `panal delegate -c auto -d DIR "task"`: [Router](router.md).
- Turn off agents you don't use, pick a theme, set alerts:
  [Configuration](configuration.md).
