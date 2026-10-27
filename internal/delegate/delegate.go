// Package delegate is `panal delegate`: it hands a task to a coding agent CLI
// (codex, agy, opencode or cursor), falls back to the next one in a chain when an
// agent is out of quota, and records each attempt as a run file
// (internal/runs) with its log, so the dashboard shows it.
//
// Files: chain.go (the -c chain), agents.go (each CLI's command line),
// classify.go (quota and permission patterns per CLI), run.go (running the
// chain and writing run files), codexquota.go (not spending codex credits),
// output.go (logs and terminal), proc_*.go (killing a process tree per OS).
package delegate

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/AlbertoVasquezR/panal/internal/config"
	"github.com/AlbertoVasquezR/panal/internal/router"
	"github.com/AlbertoVasquezR/panal/internal/runs"
)

const usageHead = `Usage: panal delegate -d DIR [flags] "task"
       panal delegate -d DIR [flags] -f task.md

Hands a task to a coding agent CLI (codex, agy, opencode or cursor) and records the
run so the Panal dashboard shows it. If an agent is out of quota, the next
link of the chain gets the task. Each attempt is limited to -t.

Give the agent its own directory, ideally a dedicated git worktree
(git worktree add ../wt-task -b task), never the tree you are working in:
it edits files there without asking.

Flags:
`

const usageTail = `
Chain (-c, else PANAL_CHAIN, else "chain = ..." in %s):
  space-separated links "cli:model[:effort]"; model and effort are optional
  and default to the CLI's own. Example:
    -c "codex:<model>:low agy opencode:<provider>/<model>"
  Default: every installed CLI of %s, in that order.

  -c auto: the router picks the order from how past runs went (see
  "panal route -h"). It picks among the pool: PANAL_POOL, else "pool = ..."
  in the config (links listed cheapest first), else the chain. With a pool
  configured and no chain, auto is the default. "pool = auto" is every
  discovered model the models / exclude keys allow (see "panal models").
  Codex with its weekly quota used up bills paid credits, so it is skipped
  as out of quota then, unless PANAL_CODEX_CREDITS=1.

Exit codes:
  0    done: the agent finished its turn. That does NOT mean the work is
       correct: review the diff (git -C DIR diff) before you use it.
  1    failed, or the agent's own exit code: a real error, no fallback
  75   every agent tried was out of quota
  124  timeout: the agent and its processes were killed
  126  no permission: the CLI refused (sandbox, approvals, untrusted dir)
  130  interrupted (ctrl+c)

Files:
  run files  %s (PANAL_RUNS)
  logs       %s (PANAL_LOGS)
`

// Main runs `panal delegate` with args (without "delegate") and returns the
// exit code.
func Main(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("panal delegate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("d", "", "directory the agent works in (required; a dedicated git worktree)")
	readOnly := fs.Bool("r", false, "read-only: the agent may read but not edit files or run commands that write")
	fs.BoolVar(readOnly, "read-only", false, "same as -r")
	timeout := fs.Duration("t", 15*time.Minute, "maximum time per attempt; then the agent's process tree is killed")
	chainFlag := fs.String("c", "", `fallback chain "cli:model[:effort] ..." (default: PANAL_CHAIN, the config key chain, or the installed CLIs)`)
	file := fs.String("f", "", `read the task from this file ("-" = stdin)`)
	fs.Usage = func() {
		fmt.Fprint(stderr, usageHead)
		fs.PrintDefaults()
		fmt.Fprintf(stderr, usageTail, config.Path(), strings.Join(DefaultCLIs, ", "), runs.Dir(), runs.LogDir())
	}

	// Flags may come after the task too: panal delegate "task" -d DIR.
	var words []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			return 2
		}
		rest := fs.Args()
		if len(rest) == 0 {
			break
		}
		words, args = append(words, rest[0]), rest[1:]
	}

	bad := func(msg string) int {
		fmt.Fprintln(stderr, "panal delegate:", msg)
		fmt.Fprintln(stderr, `run "panal delegate -h" for help`)
		return 2
	}
	text := strings.Join(words, " ")
	switch {
	case *file != "" && text != "":
		return bad("give the task as an argument or with -f, not both")
	case *file == "-":
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return bad(err.Error())
		}
		text = string(b)
	case *file != "":
		b, err := os.ReadFile(*file)
		if err != nil {
			return bad(err.Error())
		}
		text = string(b)
	}
	if strings.TrimSpace(text) == "" {
		return bad("no task given")
	}
	if *dir == "" {
		return bad("-d DIR is required: the directory the agent works in")
	}
	abs, err := filepath.Abs(*dir)
	if err != nil {
		return bad(err.Error())
	}
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		return bad(abs + " is not a directory")
	}
	if *timeout <= 0 {
		return bad("-t must be positive")
	}

	conf := config.Read(config.Path())
	spec, from := chainSpec(*chainFlag, conf)
	configured, _ := chainSpec("", conf)
	pSpec, pFrom := poolSpec(configured, conf)
	// The router picks the chain with -c auto (or PANAL_CHAIN / chain =
	// auto), and by default when a pool is configured and no chain is.
	auto := config.IsAuto(spec) || (spec == "" && strings.TrimSpace(pSpec) != "")
	task := Task{Text: text, Dir: abs, ReadOnly: *readOnly, Timeout: *timeout}
	var rt routing
	if auto {
		rt = systemRouting(conf)
		// An explicit pool is checked against the cached catalog only; pool =
		// auto fills a missing cache first.
		pool, info, err := rt.resolvePool(pSpec, pFrom, conf, config.LookPath, false)
		if err != nil {
			return bad(fmt.Sprintf("%v (pool from %s)", err, pFrom))
		}
		for _, n := range info.Notes {
			fmt.Fprintln(stderr, "panal: warning:", n)
		}
		d, err := autoTask(rt, &task, pool)
		if err != nil {
			return bad(err.Error())
		}
		fmt.Fprintln(stderr, "panal:", d.Why)
	} else {
		if spec != "" {
			if task.Chain, err = ParseChain(spec); err != nil {
				return bad(fmt.Sprintf("%v (from %s)", err, from))
			}
		} else if task.Chain = defaultChain(config.LookPath); len(task.Chain) == 0 {
			fmt.Fprintf(stderr, "panal delegate: none of %s is installed (or in PATH)\n", strings.Join(DefaultCLIs, ", "))
			return runs.ExitFailed
		}
		task.TaskType, task.Tier = classifyTask(text, *readOnly)
		rt.Checks = router.LoadChecks(filepath.Join(runs.Home(), router.ChecksFile))
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)

	r := &Runner{
		Stdout: stdout, Stderr: stderr,
		RunsDir: runs.Dir(), LogDir: runs.LogDir(),
		LookPath:    config.LookPath,
		Now:         time.Now,
		Interrupt:   sig,
		Grace:       5 * time.Second,
		CodexUsedUp: codexWeeklyUsedUp,
	}
	code, results := r.Run(task)
	recordCheck(rt.Checks, results, text, time.Now())
	fmt.Fprint(stdout, Summary(results, abs))
	if task.Auto {
		fmt.Fprintf(stdout, "%s\nRate it so the router learns: panal feedback %s good|bad\n", task.Route, runID(results))
	}
	return code
}

// Summary is the closing report: who did it, how it ended, how long it took,
// where the log is, and a reminder to review.
func Summary(results []Result, dir string) string {
	var b strings.Builder
	b.WriteString("\n── panal delegate ──\n")
	if len(results) == 0 {
		b.WriteString("nothing was run\n")
		return b.String()
	}
	last := results[len(results)-1]
	for _, r := range results {
		line := fmt.Sprintf("  %-34s %s", r.Link.String(), r.Status)
		if r.Duration > 0 {
			line += " in " + r.Duration.Round(time.Second).String()
		}
		if r.Note != "" {
			line += " (" + r.Note + ")"
		}
		b.WriteString(line + "\n")
	}
	switch last.Status {
	case runs.Done:
		fmt.Fprintf(&b, "Done by %s in %s.\n", last.Link, last.Duration.Round(time.Second))
	case runs.OutOfQuota, runs.Skipped:
		b.WriteString("No agent in the chain could take the task.\n")
	default:
		fmt.Fprintf(&b, "%s ended with status %s (exit %d).\n", last.Link, last.Status, last.RC)
	}
	fmt.Fprintf(&b, "log: %s\nrun: %s\n", last.Log, last.RunFile)
	if last.Status == runs.Done {
		fmt.Fprintf(&b, "\"done\" only means the agent finished its turn, not that the work is right.\nReview the diff before using it: git -C \"%s\" diff\n", dir)
	}
	return b.String()
}
