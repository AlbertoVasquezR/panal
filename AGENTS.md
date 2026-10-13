# Rules for agents in this project

Claude Code, Codex, opencode and any other agent working here reads this
file. Read it before making changes.

## What it is

Panal (`panal`, Go, Bubble Tea) is a read-only terminal dashboard of what
opencode, codex, agy, cursor-agent and Claude Code as orchestrator are doing, plus
`panal delegate` (hands tasks to those CLIs with quota fallback) and a
router (`-c auto`) that learns which agent, model and effort fits each
task. Overview: `README.md`; guides: `docs/`; structure:
`docs/ARCHITECTURE.md`.

## Rules

1. **Work only inside this repository** (or its git worktrees).
   Don't edit or commit to any other project of the user, including the repo
   where `delegar.sh` lives. If an improvement would need a change outside this
   repo, solve it on the dashboard's side, or tell the user and stop.
2. **The repo is public** at https://github.com/AlbertoVasquezR/panal (module
   `github.com/AlbertoVasquezR/panal`). Local commits on `master` (or on the
   worktree's branch) are fine; `git push`, pull requests, tags and releases
   only when the user asks for them.
3. Because it is public, never put personal paths, email addresses, tokens or
   names of work projects in code, tests or docs.
4. **No `Co-Authored-By` or any other AI attribution** in commit messages.
5. **English everywhere**: code, identifiers, comments, UI text, commit
   messages and docs. The product name is **Panal** in prose and on screen;
   the command, binary, module, flags, file names and config keys stay `panal`.

## Commands

```sh
go build ./...
go vet ./...
go test ./...
gofmt -l .                                      # must print nothing
go test ./internal/ui -run TestScreens -update  # regenerate golden screens, then read the diff
go run ./cmd/panal -preview 100                 # one screen at 100 columns (add -screen history, …)
go run ./cmd/panal -doctor                      # sources found, what was read, models
go install ./cmd/panal                          # installs the binary to ~/go/bin
```

## Repo map

| path | owns |
|---|---|
| `cmd/panal` | flags and subcommand dispatch |
| `cmd/preview` | every mascot frame to a PNG |
| `internal/state` | the shared `Row`, `Status`, `Quota` |
| `internal/readers` | one reader per agent (claude, agy, codex, opencode, cursor); read-only; real samples in `testdata/` |
| `internal/live` | live quota queries to the CLIs (no turn, no quota spent) |
| `internal/runs` | run file format and `~/.panal` paths; `legacy.go` reads the delegar.sh format |
| `internal/history` | past runs with tokens, cost, ratings, full tasks |
| `internal/delegate` | the `delegate`, `route` and `models` commands; each CLI's command line; quota patterns |
| `internal/router` | tier rules, outcomes, posteriors, Thompson choice, external router |
| `internal/models` | model discovery, `models.json`, cost heuristic, `pool = auto` |
| `internal/feedback` | `panal feedback` and `feedback.json` |
| `internal/changes` | what a run really touched (git, read-only) |
| `internal/activity` | last action, loops and tests from a log tail |
| `internal/credits`, `internal/forecast` | codex credits; when a quota runs out |
| `internal/report` | task types, `-report`, `-summary` |
| `internal/remote` | `-serve` and other machines |
| `internal/config` | `panal.conf`; which agents are off |
| `internal/alerts` | notifications, bell, ntfy, webhooks |
| `internal/mascots`, `internal/ui` | sprites; Bubble Tea views and keys |
| `internal/pet` | `panal pet`: the mascot for a split pane and its JSON frame contract (`docs/reference.md#pet-frames`) |
| `plugins/panal-pet`, `.claude-plugin/marketplace.json` | the Claude Code plugin that draws `panal pet -stream` frames above the prompt (TypeScript hooks module; check with `claude plugin validate`) |

## Invariants

- **The dashboard is read-only.** Panal never writes to the agents' files.
  It writes only its own files under `~/.panal` (`PANAL_DATA`): run files
  and logs, `samples.json`, `feedback.json`, `router-checks.json`,
  `models.json`.
- **External formats keep their names.** JSON keys, paths and values that
  belong to codex, agy, opencode, cursor-agent or Claude Code are never renamed.
- **The legacy delegar.sh format** (Spanish keys) is read only in
  `internal/runs/legacy.go`.
- **Spanish only where allowed:** `internal/runs/legacy.go`, regex patterns
  that match tasks written in Spanish (each one says so), and real captured
  output in `testdata/`.
- **No real agents in tests.** Fakes only (the test binary as a fake CLI,
  real captured outputs); never spend quota.
- **Golden screens are reviewed.** After `-update`, read the diff: only the
  intended change may appear.
- **The router stays explainable.** Tier rules live in one table with names
  that `panal route` prints; Thompson sampling is seeded in tests.

## Where to change X

| to… | change | and also |
|---|---|---|
| read a new agent | a reader in `internal/readers` (implements `Reader`), added to `All()` and `Sources()` | real samples in `testdata/`; a color and a mascot (read the UX skill first) |
| run a new CLI from `panal delegate` | `agents` in `internal/delegate/agents.go`, its patterns in `classify.go`, `DefaultCLIs` in `chain.go` | a fake in `fake_test.go`; `config.Known`; discovery in `internal/models/discover.go` |
| add a key | the view's key switch in `internal/ui/ui.go`, the help in `help.go`, the footer | `docs/dashboard.md#keys`; golden help screens |
| add a router tier rule | `tierRules` in `internal/router/tier.go` | tests in English and Spanish in `tier_test.go`; the table in `docs/router.md` |
| change outcomes or priors | `internal/router/evidence.go`, `posterior.go` | seeded learning tests; the numbers in `docs/router.md` |
| add a config key | `Conf` and `Read` in `internal/config/config.go` | `docs/configuration.md` |
| add a run file field | `runs.Run` in `internal/runs/runs.go` (with `omitempty`) | `docs/reference.md#run-files` |

## Definition of done

- `gofmt -l .` prints nothing; `go build ./...`, `go vet ./...` and
  `go test ./...` pass.
- New behavior has tests; UI changes have reviewed golden diffs.
- The docs in `docs/` (and the README, if it is user-facing) say what the
  code does; every command, flag, key and path they mention exists.
- No personal data; no AI attribution in commits.
- An entry in `docs/JOURNAL.md` when you finish a work session.

## UI design

Before touching `internal/ui` or `internal/mascots`, read
`.claude/skills/panal-ux/SKILL.md`: palette, status glyphs, mascot and
animation rules, sizes and checklist. For general TUI principles there is
`.claude/skills/tui-design/` (a copy of gfargo/tui-design-skill). What is left
from the last UX audit is in `docs/ux-audit.md`.

## History

What was done, why, what was learned and what is pending: `docs/JOURNAL.md`.
Read it when you pick the project back up, and add an entry when you finish a
work session.
