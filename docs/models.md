# Discovered models

Panal asks each installed agent CLI which models it can use, caches the
answer, and with `pool = auto` turns it into the router's pool, filtered
by your `models` / `exclude` rules and ordered by an estimated cost.

See also: [Router](router.md) · [Configuration](configuration.md) · back
to the [README](../README.md).

## Discovery

Every command only lists: none starts a turn, spends quota or changes the
CLI's files. They run without a shell, in your home directory (so no
project config changes the answer), with no stdin and a timeout.

| CLI | command | what it gives |
|---|---|---|
| agy | `agy models` | `id<TAB>name` per line; the effort is part of the id (`gemini-3.8-flash-medium`) |
| codex | `codex debug models` | the model catalog as JSON: slugs, efforts per model, default effort, hidden and retiring models, its own priority |
| opencode | `opencode api model.list` | `provider/model`, variants (efforts), real prices, deprecated models |
| opencode (fallback) | `opencode models` | `provider/model` per line (opencode 2.0 prints nothing here, hence the API call first) |
| cursor | `cursor-agent models` | `id - Display name` per line (Windows listing; header, blank line, `(current, default)` annotations, ANSI, CRLF); also id per line with optional tab / two spaces + name; JSON arrays |
| cursor (fallback) | `cursor-agent --list-models` | the same listing |

## The cache

The catalog lives in `~/.panal/models.json` (`PANAL_DATA` moves it), per
CLI, with the command that was run, when it last found models, and the
last error.

| who | refreshes |
|---|---|
| `panal models -refresh` | every installed CLI, now |
| the dashboard | in the background on start, each CLI whose entry is older than 24 h; it never blocks the screen |
| `panal models`, `panal route`, `panal -doctor` | only the CLIs missing from the cache |
| `panal delegate -c auto` | with `pool = auto`, only missing CLIs; an explicit pool is checked against the cache as it is |

`panal -doctor` prints one line per CLI, e.g.
`✔ codex: 10 models (cached 3 h ago, codex debug models)`, or why it
listed nothing.

## `pool = auto` and the rules

```ini
pool             = auto
models           = codex:gpt-6-luna* agy:gemini-3.8-flash-* agy:gemini-3.1-pro-*
exclude          = agy:claude-*
pool_max_per_cli = 4
```

`PANAL_POOL=auto` and `panal route -p auto` do the same.

- **Patterns** are `cli[:model[:effort]]`, separated by spaces or commas,
  case-insensitive, with `*` and `?` (`*` also matches the `/` of
  opencode's `provider/model`).
- **`models`** (allow): only arms matching one of these. Unset means every
  discovered model.
- **`exclude`** (deny): never an arm matching one of these. Without an
  effort part the whole model goes (`codex:gpt-6.1-*`); with one, only
  those efforts (`codex:*:high`). `exclude = opencode` drops a CLI.
- **Efforts.** A codex or opencode model counts as one arm per effort, but
  only **low, medium and high** unless the pattern says otherwise:
  `codex:gpt-6-luna:*` allows all its efforts, `codex:gpt-6-luna:xhigh` just
  that one. agy's effort is part of its ids, so its patterns match the id
  (`agy:*-high`).
- **Hidden or retiring** models (codex's `visibility: hide` and upgrade
  notices, opencode's disabled or `deprecated` ones) stay out unless an
  allow pattern names them exactly, with no wildcard in the model part.
- **Older versions** of the same line at the same effort are dropped when
  a newer one is allowed (`gemini-3.6-flash-low` when `gemini-3.8-flash-low`
  is there; `codex:gpt-5.6-luna:low` when `codex:gpt-6-luna:low` is).
- **The cap.** At most `pool_max_per_cli` arms per CLI (default 4): its
  cheapest, its strongest and evenly spaced ones in between, so exploring
  doesn't spend on dozens of models.
- On the dashboard, with `pool = auto`, the agents your `models` patterns
  don't name (or that `exclude` drops whole) count as off.

An **explicit pool stays exactly as you wrote it**, order included. `panal
route` then warns about arms the catalog doesn't have (a typo, a retired
model, an effort the model doesn't take, or one your own `models` /
`exclude` leave out) and names discovered arms it is missing.

## Cost heuristic

agy's and codex's catalogs publish no prices, so an auto pool is ordered by
an estimate (`internal/models/cost.go`); lower is cheaper and, the router
assumes, weaker:

> cost = class + effort + tiebreak

| class | words in the id or display name |
|---|---|
| 0 | `free` (or a price of 0) |
| 1 | `nano`, `micro` |
| 2 | `mini`, `lite`, `tiny`, `small`, `flash`, `haiku`, `luna`, `lightning`, `spark`, `air`, `oss` |
| 3 | no hint; `sonnet`, `terra`, `sol`, `plus` |
| 4 | `pro`, `max`, `ultra`, `opus`, `large`, `astra` |

- **Precedence** when several words appear: free, then nano / micro, then
  the strong words, then the cheap ones.
- **No word at all:** the description decides ("affordable", "efficient",
  "easier", "lightweight" → 2; "frontier", "most demanding", "hardest",
  "most capable" → 4), else 3.
- **Real price** (opencode, $ per million output tokens) sets the class
  instead: 0 → 0, up to 0.30 → 1, up to 1.50 → 2, up to 5 → 3, above → 4.
- **Effort** adds: `none` 0 · `minimal` 0.1 · `low` 0.2 · `medium` 0.4 ·
  `high` 0.6 · `xhigh` 0.7 · `max` 0.8 · `ultra` 0.9; a model with
  "thinking" in its name 0.05 more. A model that takes efforts but names
  none counts as medium.
- **Tiebreak** (under 0.1): the price / 1000 when known, else the version
  number / 1000, so a newer version ranks a little stronger. Exact ties
  keep the catalog's own priority (codex), then the id.

Because the class comes first, a cheap model at high effort still ranks
below a strong one at low effort. The router's priors then treat the first
arm of an auto pool as the cheap one and the last as the strong one, as
with a hand-written pool.

## `panal models`

Shows the rules, the pool with each arm's cost and where it came from,
then every discovered model per CLI with its efforts and its state (in the
pool, allowed, over the cap, a newer version is allowed, excluded and by
which pattern, not in `models`, hidden, retiring). Excerpt, with the
catalogs from `internal/models/testdata`:

```text
$ panal models
cache  ~/.panal/models.json
rules  models = codex:gpt-6-luna* agy:gemini-3.8-flash-* agy:gemini-3.1-pro-*
       exclude = agy:claude-*
       pool_max_per_cli = 4
pool   3 arms from pool in ~/.panal/panal.conf (auto: 3 of 3 allowed arms, at most 4 per CLI), cheapest first

   #  arm                       cost  estimated from
   1  codex:gpt-6-luna:low      2.21  luna → class 2 · low +0.2 · v6
   2  codex:gpt-6-luna:medium   2.41  luna → class 2 · medium +0.4 · v6
   3  codex:gpt-6-luna:high     2.61  luna → class 2 · high +0.6 · v6

codex — 10 models, cached 0 s ago (codex debug models)
   cost  model              efforts                          state
   2.40  gpt-reserve        low medium high xhigh max        not in models
   2.41  gpt-5.6-luna       low medium high xhigh max        not in models
   2.41  gpt-6-luna         low medium high xhigh max        in pool #1 low, #2 medium, #3 high
   3.40  codex-auto-review  low medium high xhigh max        not in models
   3.41  gpt-5.5            low medium high xhigh            not in models
   3.41  gpt-5.6-sol        low medium high xhigh max ultra  not in models
   3.41  gpt-5.6-terra      low medium high xhigh max ultra  not in models
   3.41  gpt-6-sol          low medium high xhigh max ultra  not in models
   3.41  gpt-6.1-sol        low medium high xhigh max ultra  not in models
   4.41  gpt-6-astra        low medium high xhigh max ultra  not in models

opencode — 39 models, cached 0 s ago (opencode api model.list)
   ...
```

(The cache and config paths are shortened to `~`; agy's section is left
out.)

In the model list, costs are compared at medium effort.

## Limits

- Discovery only knows what each CLI's listing says: a model your account
  can't use may still be listed (its runs then fail and the router learns
  to avoid it).
- opencode's list depends on its server answering.
- The cost is a name heuristic. When you know the real order, write the
  pool by hand: its order always wins.
