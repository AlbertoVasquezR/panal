# Work journal

What was done, why and what was left pending, so the project can be picked up
again without having to reconstruct it. Newest first. Each feature is described
in detail in the guides in `docs/` (linked from the README); this file holds
the context.

Project status: **public** at https://github.com/AlbertoVasquezR/panal. Local
commits are fine; push, pull requests, tags and releases only when the user
asks (see `AGENTS.md`).

Entries before 2026-10-01 describe the project when it was still called
`agentes` and its flags, config keys and package names were in Spanish
(`-servir`, `-informe`, `agentes.conf`, `internal/envivo`…). They are kept as
written; the current names are in the README and `docs/`.

---

## 2026-10-07: cursor-agent as a first-class agent

- New reader `internal/readers/cursor.go`: status, model, task and last
  action from `panal delegate` run files; stream-json activity from
  `<log>.jsonl`. No live quota: cursor-agent has no usage query that
  spends nothing (`/usage` is interactive only). `-doctor` lists the
  resolved CLI path (`cursor-agent`, then `agent`, then the Windows
  installer dir `%LOCALAPPDATA%\cursor-agent`).
- `panal delegate` runs
  `cursor-agent -p --output-format stream-json --trust --workspace DIR`
  with `--force` (writes; not confined to `-d`) or
  `--auto-review` (`-r`; `--sandbox enabled` only on macOS/Linux — the
  CLI refuses sandbox on Windows). `--mode plan` is too restrictive
  (it rejects `hostname`). Effort becomes `--model id[effort=…]`.
  Chain name is `cursor`.
- Models: `cursor-agent models`, fallback `--list-models`. Parser
  accepts the live Windows listing (`id - Display name`, header,
  annotations, ANSI, CRLF), id-per-line (optional tab + name), and JSON.
  On Windows the `.cmd` shim is run through `cmd.exe /c`.
- Dashboard: cyan `#38BDF8` diamond mascot; fifth card after opencode.
  Five cards use 3-across before 2-across so they don't stack 2+2+1 at
  100/132 columns. Four-card layout is unchanged.
- `-off` / `off =` / `config.Known` include cursor. Default chain appends
  it when the CLI is installed.

- agy `delegate -r` that auto-denies a tool in print mode (exit 0,
  "headless mode cannot prompt … auto-denied") is `no_permission`, not
  `done`. Separate commit.

Verified on a Windows box with cursor-agent logged in: `-p` /
`--print`, `--output-format`, `--auto-review`, `--force`/`--yolo`,
`--sandbox`, `--trust`, `--workspace`, `--model`, `--list-models`,
`status`. `--mode plan` rejected a harmless `hostname`. `--sandbox
enabled` on Windows exits 1 (`Sandbox mode is enabled but not
available`). Live `cursor-agent models` is `id - Display name`.
Not verified here: quota-refusal wording.

- Run ids are `YYYYMMDD-HHMMSS.mmm-xxxxxxxx` (milliseconds + random). The
  first write of a run file and of `<id>.task.md` uses `O_EXCL` and
  retries, so two `delegate` in the same second cannot share an id or
  task file. A `running` record whose pid is gone is rewritten `failed`
  on the next dashboard read. Old second-resolution ids still parse.

## 2026-10-03: pets inside Claude Code

- `panal pet`: one mascot for a split pane next to any CLI, plus `-json` /
  `-stream` frames (contract in `docs/reference.md#pet-frames`); `-all` adds
  every agent's own mascot with its model, task and last action.
- `plugins/panal-pet` (the repo is now a Claude Code plugin marketplace): a
  honeycomb at the right of the prompt that opens into every agent's mascot on
  hover or click, or a pane with one card per agent. `/panal band|pane|off`.
- The claude reader no longer flashes "no data" when it reads the status line
  while Claude Code is rewriting it: one retry, then the last good read.

## 2026-10-03: `panal pet`

- New subcommand `panal pet` (`internal/pet`): one mascot for a small
  split pane next to an agent CLI (`wt -w 0 sp -V -s 0.25 panal pet`,
  `tmux split-window -h -l 24 panal pet`), plus `-json` (one frame) and
  `-stream` (NDJSON while it changes, heartbeat every 2 s, exits when the
  pipe closes) for a Claude Code plugin being written separately against
  the contract in `docs/reference.md#pet-frames`. Field names are fixed;
  bump `v` if they ever change.
- Which agent: forced, else claude orchestrating, else the working one
  that started last, else the latest finished. Mood comes from the
  dashboard's mode and reaction rules; the pet also reacts to the other
  agents' runs finishing or failing, and a run that ended < 5 s ago counts
  as "just finished" so a single `-json` call can celebrate it.
- Refactors, no visible change (golden screens untouched):
  `mascots.Sprite.Cells` holds the half-block encoding and `RenderWith`
  draws from it; `ui.StatusMode` (status mode + nap) and
  `ui.ReactionForChange` are exported; `internal/ui/shared.go` exposes the
  glyph, status/agent/dim colors, NO_COLOR and the disabled agents.
- It reads only the cheap readers (`readers.All()`, no live quota, no
  agent process). One `-json` call is ~0.3 s on Windows.
- Pending: the pet does not read the run history, so "just finished" for
  a status change relies on the row's `End` (the dashboard also checks the
  last run); a `-scale` for bigger rasters if a consumer asks for one.

## 2026-10-02: Apache 2.0

Relicensed from MIT to the Apache License 2.0 (explicit patent grant, no
trademark rights to the Panal name, contributions under the same terms).
Added `NOTICE`. The copy of tui-design-skill keeps its own MIT license.

## 2026-10-02: Panal brand and the docs split

- The product is **Panal** in prose and on screen (title badge, window
  title, alert prefix, `-doctor` header, first-run text); the command,
  module, flags, files and config keys stay `panal`. Golden screens
  regenerated; only the badge changed.
- The 871-line README became a short landing page (screenshot from the
  golden cards screen, router diagram, real `panal route` output from a
  synthetic history) and the detail moved to focused guides:
  `docs/getting-started.md`, `dashboard.md`, `delegate.md`, `router.md`,
  `models.md`, `configuration.md`, `reference.md`, plus
  `docs/ARCHITECTURE.md` and `llms.txt`. AGENTS.md gained a repo map,
  invariants, "where to change X" and a definition of done.
- Claims dropped while checking the old README against the code: agy
  launched with `--v=3` (panal never passes it), read-only runs as `-l`
  (the flag is `-r`), the report period key `3` (it switches to the Report
  view; `7` and `tab` / arrows set the period).
- `docs/GLOSSARY-rename.tsv` (the Spanish → English rename table) removed.
- Pending: the weekly and monthly quota bars still use the internal keys
  `sem` and `mes` across packages (state, live, ui, forecast); they are
  persisted in `samples.json` and travel in `-serve`'s JSON as
  `bars[].name`. Renaming them needs a migration of the samples file.

## 2026-10-02: discovered models (`panal models`, `pool = auto`)

The router only knew the arms written by hand in `pool`. Now panal asks
each CLI which models it can use, so `pool = auto` can offer every option
while the user's `models` / `exclude` globs decide what is actually used.

- `internal/models`. Queries (no shell, hidden window, timeouts, cwd = the
  user's home): `agy models` (14 models here; "Fetching available
  models..." goes to stderr), `codex debug models` (10; ~600 KB of JSON,
  most of it each model's prompts), `opencode api model.list` (39).
  Without `--bundled`, codex refreshes its catalog from the network first;
  still read-only.
- opencode 2.0.21: `opencode models` exits 0 and prints nothing, with or
  without `--standalone`, with a pipe or a real pseudo console (tried
  ConPTY), with `CI`/`TERM`/`NO_COLOR`. Its server's `GET /api/provider`
  returns `[]` while `GET /api/model` lists the models, which is likely why.
  `opencode api model.list` goes through the CLI (it brings the server's
  auth); with `--standalone` the private server answers `data: []` too, so
  panal uses the background service. The answer carries real prices and
  variants; base URLs and keys in `settings` are never read (the zen ones
  are the literal "public"). The 60 s silence seen before may have been the
  background service starting (not reproduced). Git Bash turns `/api/model` into a Windows
  path: test such commands with `MSYS_NO_PATHCONV=1`.
- Cache `~/.panal/models.json`, per CLI `at` (last success), `tried`,
  `error`. A failed refresh keeps the old models; a failed CLI is not
  retried for 10 minutes. Dashboard: background refresh when older than
  24 h (`RefreshInBackground`, its own goroutine, merges with what another
  process may have written). route / delegate -c auto / -doctor: only fill
  a missing cache (`Ensure`).
- Cost heuristic (`cost.go`): class 0–4 from name words (codex's made-up
  names: luna 2, sol and terra 3, astra 4), description words when the
  name has none, opencode's real price when there is one; + effort 0–0.9;
  + version/1000 as a tiebreak (a size like 120b is not a version). Class
  first, so `luna:high` < `sol:low`. codex's `priority` is its picker's
  order, not a price: only for exact ties.
- Rules (`pool.go`): allow/deny globs `cli[:model[:effort]]`; no effort part
  = low/medium/high; hidden and retiring models only when named exactly;
  older versions of a line dropped (`Newest`), then at most
  `pool_max_per_cli` (4) per CLI via `Spread` (cheapest, strongest, evenly
  spaced). Without `Newest`, the cap picked gemini 3.6 over 3.8.
- The router itself is unchanged: an auto pool is sorted by the heuristic,
  so its position is the cost rank and the priors work as before. Test:
  with the cheap arms failing complex tasks, an untried pro arm is the
  favorite (it is the cheaper pro arm, within the 8-point margin of the
  strongest).
- Explicit `pool` unchanged; `panal route` warns (unknown model, effort,
  retiring, outside the user's rules) and names discovered arms it lacks.
  With `pool = auto`, "off" agents come from the CLIs `models` names.
- Chain efforts (`minimal … max`) now live in `models.ChainEfforts`; codex's
  `ultra` is not one, so `codex:x:ultra` stays a model name.

Pending: show the explicit pool's order against the estimate (the user's
pool lists flash-medium before luna-low, which the heuristic ranks
cheaper); per-arm cost in the dashboard's run detail.

## 2026-10-02: the router (`panal delegate -c auto`, `panal route`, `panal feedback`)

The headline feature for the launch: panal learns which agent, model and
effort to send each task to, from the user's own runs.

- `internal/router`, pure Go, no new dependencies. Arms are chain links from
  a `pool` (config key, `PANAL_POOL` wins; default: the chain, else the
  installed CLIs), listed cheapest first: the position is the cost rank.
- Tier (simple/medium/complex) from one table of scored rules in `tier.go`
  (length, steps, read-only, allowed files and dirs from `changes.Allowed`,
  type, and size words in English and Spanish). Type is `report.TaskType`.
- Outcome of a run: success = done, no rule broken, last tests (if seen)
  passing; failure = failed/timeout/no permission/broken rule/failing tests;
  no signal = out of quota, skipped, interrupted, running. A rating
  (`feedback.json`) overrides both (decided: the user looked; a violation
  check by file dates can be a false positive).
- Posterior per (arm, type, tier): Beta, half-life 30 days. Fallback chain:
  cell ← (arm, tier), worth up to 5 runs ← prior(tier, rank), worth 3 runs,
  with its odds multiplied by the arm's overall lift (successes+1)/(expected
  +1), clamped to [0.5, 2]. Deviation from the first idea ((arm) as a plain
  level above (arm, tier)): with that, 20 simple successes made a cheap arm
  look great at complex tasks; the lift keeps the tier's prior shape.
  Priors: simple 0.80→0.85, medium 0.60→0.80, complex 0.35→0.75 from the
  cheapest to the strongest arm.
- Choice: Thompson sampling, cheapest arm within 0.08 of the best draw;
  200 extra draws estimate how often it explores ("exploring 1 in 10").
  Arms whose CLI the dashboard's readers see as out of quota go last.
- A run with a model the pool doesn't name counts for the CLI's bare arm
  (`codex`) when the pool has one; otherwise it only shows in `-stats`.
- Checks (git violation check + tests in the log) are cached in
  `~/.panal/router-checks.json`; the first `panal route` over ~40 legacy
  runs took ~25 s, then ~3 s. The run that ends a delegation is checked
  right away.
- Run files gain `task_type`, `tier` (every run), `auto`, `route`,
  `choice` and `explored` (auto only). `runs/legacy.go` untouched.
- `auto` is the default only when a pool is configured and no chain is;
  `chain = auto` works too, and the "off" agents then come from the pool.
- Optional external router: `router = <command>` (no shell), JSON in/out,
  5 s, falls back to the built-in one and says so.
- UI: `+`/`=` good and `-` bad in History and run detail (again clears);
  ▲/▼ after the result in the list; run detail shows rating, type · tier
  and a "why this agent (router)" section. `-report` has a Router line.

Pending: validate with real runs (are the tiers right for the orchestrator's
long tasks, which have a lot of boilerplate? is the exploration rate at cold
start acceptable?); maybe use tokens/credits as a real cost instead of the
pool order; the dashboard could show the route of a running run on its card.

---

## 2026-10-01: renamed to panal, translated to English, published

The project is opened up to contributors worldwide.

- New name: **panal** (Spanish for *honeycomb*: each agent is a cell of the
  honeycomb). Module `github.com/AlbertoVasquezR/panal`, binary `cmd/panal`,
  repo https://github.com/AlbertoVasquezR/panal, published publicly.
- Everything translated to English: package and directory names
  (`internal/state`, `readers`, `ui`, `mascots`, `history`, `report`,
  `forecast`, `remote`, `live`, `changes`, `alerts`, `credits`, `activity`,
  `config`; `cmd/vista` became `cmd/preview`), exported identifiers, comments,
  UI text, flags and docs.
- Flags renamed with no Spanish aliases (`-every`, `-once`, `-history`,
  `-report`, `-format`, `-summary`, `-mascots`, `-preview`, `-screen`, `-off`,
  `-alerts`, `-statusline`, `-theme`, `-serve`, `-doctor`, `-no-animation`).
- Compatibility kept for existing users: the config file is now
  `~/.ct-delegar/panal.conf` but `agentes.conf` is still read if it's missing,
  and the old Spanish keys and values are accepted as aliases; `AGENTES_*`
  environment variables are still read as a fallback for `PANAL_*`; `-serve`
  answers on `/status` and still on `/estado`; panal's own data is read from
  its old location if the new one doesn't exist yet.
- Not renamed, because other tools own them: `DELEGAR_*`, `CODEX_*`,
  `OPENCODE_*`, `~/.ct-delegar/estado`, `~/.ct-delegar/registros` and every
  JSON key written by external tools.
- 2026-10-02: those aliases are gone (no `agentes.conf`, `AGENTES_*`, Spanish
  keys/values or `/estado`; `-serve` JSON keys are English); config and status
  line moved to `~/.panal`, and every reader goes through `internal/runs`, whose
  `legacy.go` is the only code that still reads delegar.sh's Spanish run files.
- Docs: `docs/BITACORA.md` became `docs/JOURNAL.md`, `docs/auditoria-ux.md`
  became `docs/ux-audit.md`, the `agentes-ux` skill became `panal-ux`,
  `docs/mascotas.png` and `docs/animaciones.png` became `docs/mascots.png` and
  `docs/animations.png`; new `CONTRIBUTING.md`.
- Screen golden tests: `go test ./internal/ui -run TestScreens -update`.

---

## 2026-10-01: getting ready for GitHub

The user declared it finished and is publishing it as a personal, public
project, at https://github.com/AlbertoVasquezR/agentes-tui.

- Module `github.com/AlbertoVasquezR/agentes-tui`; `main.go` moved to
  `cmd/agentes/` so the binary keeps the name `agentes`.
- MIT license.
- The screen tests had this machine's temp directory baked in and only passed
  here; now a helper replaces it with a fixed marker of the same width.
- Removed from tests and docs: the Windows user name and the names of work
  projects.
- History rewritten with the author `AlbertoVasquezR`; the original stayed on
  the `respaldo-antes-de-publicar` branch. The `mejora/*` branches (worktrees)
  were not rewritten and are not pushed.

---

## 2026-09-29: live quota, more data per run, report and multiple machines

### Quotas that don't get stuck in the past

**Problem:** each agent reports its quota only when it runs, so the dashboard
showed data from days ago. Codex showed "week 100 %" even though it had reset
on Sep 26 at 12:32; resets done by the platform (OpenAI, Anthropic and Google
did several in 2026) and usage from elsewhere (IDE, web, another machine) were
not visible either.

| commit | what |
|---|---|
| `cae4f8a` | `Cuota.Vencer` + `lectores.Vigente`: a window whose reset has passed goes back to 0 % ("already reset"); if it was the one keeping the agent out of quota, the agent becomes available. |
| `a50773a` | **codex live**: a long-lived `codex app-server` and `account/rateLimits/read` every minute (30/15/5 s when usage is high). It's the official API of its own TUI; it spends no quota. It also gives credits and saved resets. |
| `1f2ae1a` | **agy live**: `agy -p /usage --output-format json` every 5 min (since agy 1.1.11 it doesn't start a turn). |
| `b28f4a5` | **claude live**: `claude -p` in stream-json with control requests only (`initialize` + `get_usage`), every 5 min. |
| `ea879fa` | **opencode live**: `GET https://opencode.ai/zen/go/v1/usage` with the Go API key from `auth.json`; three windows (5 h, week, month). |

Common to all of them (`internal/envivo`):
- The `r` key asks all four right away; `cuota_en_vivo = no` in `agentes.conf` turns it off.
- It detects **early resets** (usage drops before its reset time) and, for codex, redeemed saved resets and new credits: an alert is shown.
- A quota not confirmed within 15 min is marked "≈ quota seen … ago".
- Safeguards: if an agy answer contains a turn/tokens, or a claude answer a cost, it stops asking for that session and alerts.
- `-diagnostico` tries the four queries.

What was learned (important when touching this):
- **Git Bash turns `/usage` into a Windows path** (`C:/…/Git/usage`) and agy takes it as a prompt: one manual test spent ~13,800 agy tokens. It doesn't happen from Go (exec doesn't go through the shell). By hand: `MSYS_NO_PATHCONV=1`.
- `claude -p` runs the user's `SessionStart` hooks (there was one from a third-party tool): that's why it runs with `--settings '{"disableAllHooks":true}'`, `--no-session-persistence` and from the temp directory. `--bare` doesn't work: it only accepts an API key, not the subscription.
- Anthropic's terms forbid third parties from using Claude's credentials: that's why the CLI itself is asked and `.credentials.json` is **not** read.
- opencode requires `User-Agent: opencode` (without it, Cloudflare answers 403). The cost in `opencode.db` is useless for estimating quota (in v2 it is sometimes 0).
- No provider announces a reset: you only notice by comparing readings.
- Real data on Sep 29: codex week 32 % and 979 credits (not 0 % and 997), 1 saved reset; agy week 17 %; opencode **month at 100 % until Oct 17** (the "Go exhausted").

### What each agent is doing and how it's going

| commit | what |
|---|---|
| `87545dc`, `fb01529`, `887d73c` | **"What it's doing now"** (`internal/actividad`): under the mascot, the last action read from the tail of the log. agy: `toolAction` of its last `functionCall`; codex: the command (without the powershell wrapping it), its last message or its plan (`plan 1/3 · …`); claude: the last tool in the transcript (the command's `description`, reads/edits a file). |
| `fa23f83` | **Stuck?**: the same command 3+ times in a row without editing anything → "⟳ ×4 $ go test" in amber and an alert on the fourth. For agy only the "Cortex API Chunk" lines count (requests copy the whole conversation). |
| `bbe5c3e` | **Tests**: if codex ran tests, "✓ go test ./..." or "✗ 2 failures · …" (from the `exit_code` and the output in its log). agy doesn't leave the output in a reliable format. |
| `4f8e27b`, `7674b73` | **Context**: claude (from the status line, plus the session cost) and codex while it works (`last_token_usage.input_tokens / model_context_window`). |
| `c343146` | **Streaks**: "★ N in a row without failure" (it says "without failure" and not "good": "done" doesn't guarantee it went well). |

### Report

| commit | what |
|---|---|
| `90e5596` | **Agent × task type** matrix in the Report tab ("no failure / runs", colored) and what was **delegated** (runs and tokens Claude didn't spend; in dollars with `precio_claude`). The type is inferred from the task without its rules: read-only or a first sentence asking for a review → review; otherwise, the type with the most mentions. |
| `1fc4c2f` | `agentes -informe N -formato texto|md|csv|json`. |

### Daily use and other devs

| commit | what |
|---|---|
| `4404282` | Keys `c` (VS Code) and `w` (Windows Terminal) open the run's directory. |
| `4694491` | Alerts also to **ntfy** or a **webhook** (Slack/Discord), only if configured; at most 6 per minute. It's the only thing that leaves the machine. |
| `9039921` | **Multiple machines** (`internal/remoto`): `agentes -servir :8765` with `servir_token` (it won't start without a token) and `maquina = pc2 http://ip:8765 token` on the dashboard → one line per machine. Plain HTTP: local network or VPN only. |
| `3fb466e` | Window title with what needs attention. |
| `66d0c73` | Preferences in `agentes.conf`: `tema`, `animacion`, `avisos`, `cada`. |
| `0977a2c`, `d11d2c7` | `agentes -diagnostico` and the README's **Install** section + `scripts/statusline.sh` (generic, with no paths from this machine). The user's `settings.json` was not touched. |

---

## 2026-09-28: UX: skills, mascots, first run

Request: a simple, useful and pretty UX/UI so other devs want to use it; look
for a skill and similar projects; more mascot animations.

| commit | what |
|---|---|
| `3053151` | `AGENTS.md` and `CLAUDE.md` with the repo rules for any agent. |
| `5f1952b` | **Phase 1**: the `tui-design` skill (a copy of gfargo/tui-design-skill, MIT) and our own `agentes-ux` skill in `.claude/skills/`; audit in `docs/auditoria-ux.md`; `Barra` uses the model's `ahora` (the golden screens failed depending on the day). |
| `ac37abc` | **Phase 2**: mascots that blink out of sync, look around, nap, react (celebrate, scared, wake up, greet, pet with `p`), sweat at high quota, and speech bubbles. |
| `efcfbd9` | **Phase 3**: the ✦ "unseen" mark, tab 4 **timeline**, the **first run** screen. |
| `a85298f` | **Phase 4**: `-linea` (status on one line), `-tema`, `-sin-animacion`, no mascots with `NO_COLOR`. |
| `dbe4a4c` | A space after "↻": Cascadia doesn't have that glyph and the fallback font draws it wide, overlapping the date. |
| `322755a` | Mascots that see each other (claude conducts "your turn, codex!", the others turn to look). |
| `a1215a7`, `ebc6c30` | Compact header at 80 columns; help explaining what each animation means. |
| `336bf6c` | Fixes from a code review: orphaned "running" runs, timeline with more than 200 runs, pwsh with spaces, colors split when trimming, esc in the help, false "unseen" when re-reading the quota, 23-hour days. |

Research that guided this: agent-deck, claude-squad, agenmux,
tmux-agent-sidebar, agtop, CodexBar, Claude-Code-Usage-Monitor, ccusage,
lazygit, k9s, btop; mascots: Clawd, /buddy, codachi, claude-code-tamagotchi.

## 2026-10-02: `panal delegate`

Request: anyone who installs panal can hand a task to a cheap agent, with
fallback when one runs out of quota, without an external script.

- New `internal/delegate` (written from scratch from each CLI's `--help`):
  chain parsing, one command builder per CLI, one quota/permission pattern
  table per CLI, run files (`internal/runs`) written before and after each
  attempt, process-tree kill per OS, codex skipped when it would bill credits.
- Config key `chain`; `PANAL_CHAIN`, `PANAL_CODEX_CREDITS`.
- Tests use the test binary as a fake codex/agy/opencode; no real agent runs.
- Not verified against real agents yet: the exact quota messages, agy's
  print-mode permission flags and whether opencode reads `--file` as the task.

---

## Pending and ideas

- Animate claude's "note" flying to the agent's card when delegating; mascot accessories for streaks.
- Command palette (`:` or `ctrl+p`).
- Test results and context for agy too (we need to find where it leaves its commands' output and its tokens).
- "Waiting for your answer" (pending tool vs. yielded turn) for claude.
- Classify the task type from a field passed by `delegar.sh` instead of guessing it from the text.
- Audit: the "model / time / task" labels repeat on every card (finding 5 in `docs/ux-audit.md`).
- Try a clean install on another PC (`go install github.com/AlbertoVasquezR/panal/cmd/panal@latest` and `panal -doctor`) and decide whether live quota should be on by default.

## For whoever picks this up

- Read `AGENTS.md` and `.claude/skills/panal-ux/SKILL.md` before touching the UI.
- Tests: `go vet ./...` and `go test ./...`. Golden screens: `go test ./internal/ui -run TestScreens -update` and **review the diff**.
- To see it installed: `go install ./cmd/panal` (binary in `%USERPROFILE%\go\bin\panal.exe`); close and reopen `panal`.
- When trying CLIs by hand from Git Bash, watch out for arguments that start with `/` (see above). Never kill `panal.exe` processes by name: it may be the user's open dashboard.
- Never: redeem resets (codex's `account/rateLimitResetCredit/consume`), read Claude's credentials, or send a message in the `get_usage` query.
