# Delegating tasks: `panal delegate`

`panal delegate` hands a task to a coding agent CLI (codex, agy,
opencode or cursor), falls back to the next one in a chain when an agent is out of
quota, and records every attempt so the dashboard shows it while it runs.
With `-c auto` the [router](router.md) picks the chain.

See also: [Router](router.md) · [Models](models.md) ·
[Reference: run files](reference.md#run-files) · back to the
[README](../README.md).

## Usage

```sh
panal delegate -d ../wt-fix "Fix the off-by-one in the pager; add a test"
panal delegate -d ../wt-fix -f task.md                  # multi-line task from a file
panal delegate -d ../wt-fix -r "Review internal/ui and list the bugs"
panal delegate -d ../wt-fix -c "codex::low opencode:<provider>/<model>" -t 30m "..."
panal delegate -d ../wt-fix -c auto "..."                # let the router pick
panal delegate -h                                       # full usage
```

Flags may also come after the task (`panal delegate "task" -d DIR`).

**Give the agent its own directory**, ideally a dedicated git worktree
(`git worktree add ../wt-fix -b fix`), never the tree you are working in:
the agent edits files there without asking, and a worktree makes its diff
easy to review and to throw away.

| flag | meaning |
|---|---|
| `-d DIR` | required: the directory the agent works in |
| `-r`, `-read-only` | the agent may read but not edit files or run commands that write |
| `-t 15m` | maximum time per attempt (default 15m); then the agent and every process it started are killed |
| `-c "cli:model[:effort] ..."` | the fallback chain (below), or `auto` for the router |
| `-f FILE` | read the task from a file (`-` = stdin); not together with a task argument |

## The chain

Links are separated by spaces (or commas): `cli:model[:effort]`. Model and
effort are optional and default to the CLI's own. `codex::low` is codex's
default model at low effort; opencode models are `provider/model`. The
last `:word` is taken as the effort only when it is one of `minimal`,
`low`, `medium`, `high`, `xhigh`, `max`, so a model name with a colon still
works.

Where the chain comes from, first match wins:

1. `-c`
2. `PANAL_CHAIN`
3. `chain = ...` in `panal.conf`
4. the default: every installed one of `codex`, `agy`, `opencode`, `cursor`, in that order

`-c auto`, `PANAL_CHAIN=auto` or `chain = auto` hand the order to the
router. **When a pool is configured and no chain is, `auto` is the
default.** See [Router](router.md#arms-and-the-pool).

How the chain moves on:

- a link whose CLI is not installed is recorded as `skipped` and the next
  one is tried;
- a link that is out of quota is recorded as `out_of_quota` and the next
  one is tried;
- any other ending stops the chain: a real error would only repeat.

Quota and permission refusals are recognized from each CLI's exit code and
error output, with one pattern table per CLI plus generic ones (HTTP 429,
rate limit, quota exceeded, usage limit, out of credits, resource
exhausted) in `internal/delegate/classify.go`. agy in print mode can exit
0 after auto-denying a tool it cannot prompt for; that is `no_permission`,
not `done`.

## How each agent is run

| CLI | may write | read-only (`-r`) |
|---|---|---|
| codex | `codex exec --json -o <log>.last -C DIR -s workspace-write [-m model] [-c model_reasoning_effort="…"] -` (task on stdin) | same with `-s read-only` |
| agy | `agy -p <task> --log-file <log>.log [--model …] [--effort …] --dangerously-skip-permissions` | `--mode plan --sandbox` instead |
| opencode | `opencode run [--model provider/model[#effort]] --auto <task>` | `--agent plan` instead of `--auto` |
| cursor | `cursor-agent -p --output-format stream-json --trust --workspace DIR [--model …] --force <task>` (binary: `cursor-agent` or `agent`) | `--auto-review` instead of `--force`; `--sandbox enabled` only on macOS/Linux |

> **agy runs with `--dangerously-skip-permissions` when it may write.** It
> never asks before acting and is **not confined to `-d`**: it can edit
> files or run commands anywhere your user account can. That is what lets
> it work unattended in print mode. If that is too much for a task, use
> `-r` (plan mode plus agy's sandbox) or leave agy out of the chain
> (`-c "codex opencode"`).

> **cursor-agent runs with `--force` when it may write.** It does not
> ask before acting and is **not confined to `-d`**: `--workspace` only
> names the project and skips the trust prompt. If that is too much for
> a task, use `-r` (`--auto-review`, plus `--sandbox enabled` on macOS
> and Linux) or leave cursor out of the chain. On Windows the CLI has
> no sandbox, so `-r` is `--auto-review` alone. `--mode plan` is not
> used for `-r`: it rejects even a harmless read-only shell command.

codex's writes stay inside `-d` (its own `workspace-write` sandbox).
opencode's `--auto` approves whatever is not explicitly denied. Long tasks
(or, for opencode, tasks with characters `cmd.exe` would mangle) are
passed as a file the agent is told to read.

## Codex and paid credits

When codex's weekly quota is used up, codex keeps working on **paid
credits**. Before running codex, Panal checks its quota the way the
dashboard does: the `rate_limits` in its newest session in
`~/.codex/sessions`, and only if that says used up, `codex app-server`
(in case the quota reset early). Neither spends anything. If it is used
up, the codex link is recorded as `out_of_quota` and the chain moves on.
`PANAL_CODEX_CREDITS=1` lets codex spend credits.

## Exit codes and statuses

| exit | status | meaning |
|---|---|---|
| 0 | `done` | the agent finished its turn |
| 1, or the agent's own code | `failed` | a real error; no fallback. Also when every link was skipped |
| 75 | `out_of_quota` | every agent tried was out of quota |
| 124 | `timeout` | `-t` passed; the agent and its processes were killed |
| 126 | `no_permission` | the CLI refused (sandbox, approvals, untrusted directory) |
| 130 | `interrupted` | ctrl+c, passed on to the agent (killed after 5 s), then the run is recorded |
| | `skipped` | that link's CLI is not installed |
| 2 | | bad usage (no task, no `-d`, a bad chain) |

> **`done` does not mean the work is correct.** It only means the agent
> finished its turn. Review the diff (`git -C DIR diff`) and run the tests
> before you use it, and tell the router with `panal feedback` when it was
> wrong.

At the end `panal delegate` prints a summary: each link with its status
and time, the log and run file of the last one and, with `-c auto`, the
router's reason and the `panal feedback` command to rate it.

## Files it writes

| file | where | what |
|---|---|---|
| run file | `~/.panal/runs/<id>-<agent>.json` (`PANAL_RUNS`) | one per attempt: written with status `running` before the agent starts, rewritten with its end, exit code and status. [Schema](reference.md#run-files) |
| log | `~/.panal/logs/<id>-<agent>-<model>.txt` (`PANAL_LOGS`) | what the agent printed |
| codex events | `<log>.jsonl`, `<log>.last` | codex's JSON events and final message (appended to the log) |
| agy log | `<log>.log` | agy's own log (where its quota summary is read from) |
| cursor events | `<log>.jsonl` | cursor-agent's `--output-format stream-json` events (last action) |
| full task | `~/.panal/runs/<id>.task.md` | the whole task when it has more than one line; the run file keeps the first line and points to it in `task_file` |
| router checks | `~/.panal/router-checks.json` | the finished run's changes and tests, checked right away for the router |

`<id>` is `YYYYMMDD-HHMMSS.mmm-xxxxxxxx` (milliseconds plus eight random hex
digits) so two `panal delegate` in the same second cannot share a run or
task file. Older files keep `YYYYMMDD-HHMMSS` (`-2`, `-3`… if taken).
The first write of a run file and of `<id>.task.md` uses `O_EXCL` and
retries on collision. A run left `running` whose process is gone is
rewritten as `failed` the next time the dashboard reads it.
`PANAL_DATA` moves `~/.panal` as a whole.
