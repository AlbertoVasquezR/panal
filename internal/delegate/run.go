package delegate

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/AlbertoVasquezR/panal/internal/runs"
)

// Runner runs a task through a chain of agents. The zero value is not
// usable; Main fills one with the real system, tests with fakes.
type Runner struct {
	Stdout, Stderr io.Writer
	RunsDir        string // run files (runs.Dir())
	LogDir         string // attempt logs (runs.LogDir())
	LookPath       func(string) (string, error)
	Env            []string // added to the agents' environment
	Now            func() time.Time
	Interrupt      <-chan os.Signal
	Grace          time.Duration // after ctrl+c, how long the agent gets to stop on its own
	// CodexUsedUp says whether running codex now would bill paid credits.
	CodexUsedUp func(time.Time) (bool, string)

	termOnce sync.Once
	termMu   *sync.Mutex
}

// Task is what to do and how.
type Task struct {
	Text     string
	Dir      string
	ReadOnly bool
	Timeout  time.Duration // per attempt
	Chain    []Link

	// Recorded in every run file (internal/router).
	TaskType, Tier string
	// Set when the router picked the chain (-c auto).
	Auto     bool
	Route    string // its one-line explanation
	Explored bool   // the first link was exploration
}

// Result of one link of the chain.
type Result struct {
	Link     Link
	Status   runs.Status
	RC       int
	RunFile  string
	Log      string
	Duration time.Duration
	Note     string // why it was skipped or bounced, when panal knows
}

// Run tries the chain until one link finishes (done), fails for real, times
// out or is interrupted. It returns the exit code and what each link did.
func (r *Runner) Run(t Task) (int, []Result) {
	start := r.Now()
	base, taskFile, err := r.allocate(start, t.Text)
	if err != nil {
		fmt.Fprintln(r.Stderr, "panal: could not allocate a run id:", err)
		return runs.ExitFailed, nil
	}
	first, _ := firstLine(t.Text)
	writeTaskFile := func() string {
		if taskFile == "" {
			p, err := r.saveTask(base, t.Text)
			if err != nil {
				fmt.Fprintln(r.Stderr, "panal: could not save the task file:", err)
			}
			taskFile = p
		}
		return taskFile
	}

	var results []Result
	used := map[string]bool{}
	for i, l := range t.Chain {
		select {
		case <-r.Interrupt:
			fmt.Fprintln(r.Stderr, "panal: interrupted")
			return runs.ExitInterrupted, results
		default:
		}
		id := r.uniqueID(base, l.CLI, used)
		run := runs.Run{
			ID: id, Agent: l.CLI, Model: l.Model, Effort: l.Effort,
			Task: first, TaskFile: taskFile, Dir: t.Dir, ReadOnly: t.ReadOnly,
			PID: os.Getpid(),
			Log: filepath.Join(r.LogDir, id+"-"+l.CLI+"-"+safeName(l.Model)+".txt"),

			TaskType: t.TaskType, Tier: t.Tier,
		}
		if t.Auto {
			run.Auto, run.Route, run.Choice = true, t.Route, i+1
			run.Explored = i == 0 && t.Explored
		}
		res := Result{Link: l, Log: run.Log}

		bounce := func(st runs.Status, rc *int, note string) {
			now := r.Now()
			run.Start, run.End, run.Status, run.RC = stamp(now), stamp(now), st, rc
			r.note(run.Log, note)
			res.Status, res.Note = st, note
			res.RunFile = r.writeNew(&run, used)
			res.Log = run.Log
			if rc != nil {
				res.RC = *rc
			}
			results = append(results, res)
			fmt.Fprintf(r.Stderr, "panal: [%d/%d] %s: %s: %s\n", i+1, len(t.Chain), l, st, note)
		}

		path, err := r.LookPath(l.CLI)
		if err != nil {
			bounce(runs.Skipped, nil, l.CLI+" not found in PATH")
			continue
		}
		if l.CLI == "codex" && r.CodexUsedUp != nil {
			if usedUp, why := r.CodexUsedUp(r.Now()); usedUp {
				bounce(runs.OutOfQuota, intp(runs.ExitOutOfQuota), why)
				continue
			}
		}

		a := attempt{Link: l, Task: t.Text, TaskFile: taskFile, Dir: t.Dir, ReadOnly: t.ReadOnly, Log: run.Log}
		inv := agents[l.CLI].build(a)
		if inv.NeedsTaskFile && a.TaskFile == "" {
			a.TaskFile = writeTaskFile()
			run.TaskFile = a.TaskFile
			inv = agents[l.CLI].build(a)
		}

		run.Start, run.Status = stamp(r.Now()), runs.Running
		res.RunFile = r.writeNew(&run, used) // before launching: the dashboard sees it start
		a.Log = run.Log
		res.Log = run.Log
		fmt.Fprintf(r.Stderr, "panal: [%d/%d] %s in %s\npanal: log %s\n", i+1, len(t.Chain), l, t.Dir, run.Log)

		began := time.Now()
		st, rc := r.execute(path, inv, a, t.Timeout, func(pid int) {
			run.PID = pid
			r.write(run)
		})
		res.Log = run.Log
		res.Duration = time.Since(began)
		run.End, run.Status, run.RC = stamp(r.Now()), st, intp(rc)
		r.write(run)
		res.Status, res.RC = st, rc
		results = append(results, res)

		if st != runs.OutOfQuota {
			return rc, results
		}
		fmt.Fprintf(r.Stderr, "panal: %s is out of quota, trying the next agent\n", l)
	}
	for _, res := range results {
		if res.Status == runs.OutOfQuota {
			return runs.ExitOutOfQuota, results
		}
	}
	return runs.ExitFailed, results
}

// execute runs one attempt and returns its status and exit code.
func (r *Runner) execute(path string, inv invocation, a attempt, timeout time.Duration, started func(pid int)) (runs.Status, int) {
	if err := os.MkdirAll(r.LogDir, 0o755); err != nil {
		fmt.Fprintln(r.Stderr, "panal:", err)
		return runs.Failed, runs.ExitFailed
	}
	logf, err := os.Create(a.Log)
	if err != nil {
		fmt.Fprintln(r.Stderr, "panal:", err)
		return runs.Failed, runs.ExitFailed
	}
	defer logf.Close()
	log := &syncWriter{w: logf, mu: new(sync.Mutex)}
	stdout, stderr := r.term(r.Stdout), r.term(r.Stderr)
	errText, outText := newTail(16<<10), newTail(4<<10)

	cmd := exec.Command(path, inv.Args...)
	cmd.Dir = a.Dir
	cmd.Env = append(os.Environ(), r.Env...)
	if inv.Stdin != "" {
		cmd.Stdin = strings.NewReader(inv.Stdin)
	}
	if inv.JSONEvents {
		jf, err := os.Create(a.Log + ".jsonl")
		if err != nil {
			fmt.Fprintln(r.Stderr, "panal:", err)
			return runs.Failed, runs.ExitFailed
		}
		defer jf.Close()
		cmd.Stdout = io.MultiWriter(jf, &codexEvents{out: stdout, errors: errText})
	} else {
		cmd.Stdout = io.MultiWriter(log, stdout, outText)
	}
	cmd.Stderr = io.MultiWriter(log, stderr, errText)
	cmd.WaitDelay = 5 * time.Second // a grandchild holding the pipes must not hang us
	setProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(log, "panal: could not start %s: %v\n", path, err)
		fmt.Fprintf(r.Stderr, "panal: could not start %s: %v\n", path, err)
		if errors.Is(err, fs.ErrPermission) {
			return runs.NoPermission, runs.ExitNoPermission
		}
		return runs.Failed, runs.ExitFailed
	}
	started(cmd.Process.Pid)

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var status runs.Status
	var rc int
	select {
	case err := <-done:
		rc = exitCode(cmd, err)
		// On Windows the console gives ctrl+c to the agent too, and it may
		// exit before panal sees the signal.
		if rc != 0 {
			select {
			case <-r.Interrupt:
				fmt.Fprintln(log, "\npanal: interrupted (ctrl+c)")
				status, rc = runs.Interrupted, runs.ExitInterrupted
			case <-time.After(200 * time.Millisecond):
			}
		}
	case <-timer.C:
		killTree(cmd)
		<-done
		fmt.Fprintf(log, "\npanal: timed out after %s; killed the agent and its processes\n", timeout)
		status, rc = runs.Timeout, runs.ExitTimeout
	case <-r.Interrupt:
		interruptTree(cmd)
		select {
		case <-done:
		case <-time.After(r.Grace):
			killTree(cmd)
			<-done
		}
		fmt.Fprintln(log, "\npanal: interrupted (ctrl+c)")
		status, rc = runs.Interrupted, runs.ExitInterrupted
	}

	if inv.LastMessage != "" {
		if b, err := os.ReadFile(inv.LastMessage); err == nil {
			if msg := strings.TrimSpace(string(b)); msg != "" {
				fmt.Fprintf(log, "\n%s\n", msg)
				fmt.Fprintf(stdout, "\n%s\n", msg)
			}
			os.Remove(inv.LastMessage)
		}
	}
	if status != "" {
		return status, rc
	}
	text := errText.String() + "\n" + outText.String()
	status = classify(a.Link.CLI, rc, text)
	switch status {
	case runs.OutOfQuota:
		rc = runs.ExitOutOfQuota
	case runs.NoPermission:
		rc = runs.ExitNoPermission
		if hint := permissionHint(a.Link.CLI, text); hint != "" {
			fmt.Fprintln(r.Stderr, "panal:", hint)
			fmt.Fprintln(log, "panal:", hint)
		}
	}
	return status, rc
}

// permissionHint is a short extra line when classify's status is not
// enough to say what to do. Empty if there is nothing useful to add.
func permissionHint(cli, text string) string {
	if cli == "cursor" && cursorSandboxUnavailable(text) {
		return "cursor-agent --sandbox is only available on macOS and Linux; on Windows, panal -r passes --auto-review alone"
	}
	return ""
}

func cursorSandboxUnavailable(text string) bool {
	low := strings.ToLower(text)
	return strings.Contains(low, "sandbox") && strings.Contains(low, "not available")
}

func exitCode(cmd *exec.Cmd, err error) int {
	if cmd.ProcessState != nil {
		if c := cmd.ProcessState.ExitCode(); c >= 0 {
			return c
		}
	}
	if err != nil {
		return runs.ExitFailed
	}
	return 0
}

// write writes the run file; a failure is reported, not fatal: the agent's
// work matters more than the dashboard.
func (r *Runner) write(run runs.Run) string {
	p, err := runs.WriteFile(r.RunsDir, run)
	if err != nil {
		fmt.Fprintln(r.Stderr, "panal: could not write the run file:", err)
	}
	return p
}

// note writes a log with only panal's reason, for a link that never ran, so
// the dashboard's log view says why.
func (r *Runner) note(path, msg string) {
	if os.MkdirAll(filepath.Dir(path), 0o755) == nil {
		_ = os.WriteFile(path, []byte("panal: "+msg+"\n"), 0o644)
	}
}

// allocate picks a unique run id (millisecond time + random) and, when the
// task has more than one line, exclusive-creates <id>.task.md so two
// delegates in the same second cannot share a task file.
func (r *Runner) allocate(t time.Time, text string) (id, taskFile string, err error) {
	_, multi := firstLine(text)
	for i := 0; i < 64; i++ {
		id = runs.NewID(t)
		if !multi {
			return id, "", nil
		}
		taskFile, err = r.saveTask(id, text)
		if err == nil {
			return id, taskFile, nil
		}
		if !os.IsExist(err) {
			return "", "", err
		}
	}
	return "", "", fmt.Errorf("could not allocate a unique run id")
}

// saveTask writes the full task next to the run files as <id>.task.md.
// It fails with os.ErrExist if that name is taken.
func (r *Runner) saveTask(id, text string) (string, error) {
	p := filepath.Join(r.RunsDir, id+".task.md")
	return p, runs.CreateExclusive(p, []byte(text))
}

// uniqueID is base, or base-2, base-3… when this agent already has a run
// file with that ID (the same agent twice in a chain).
func (r *Runner) uniqueID(base, cli string, used map[string]bool) string {
	id := base
	for n := 2; used[id+"-"+cli] || fileExists(filepath.Join(r.RunsDir, id+"-"+cli+".json")); n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	used[id+"-"+cli] = true
	return id
}

// writeNew exclusive-creates the run file. If the name is taken it mints a
// new id and retries, so two processes that picked the same id do not
// overwrite each other.
func (r *Runner) writeNew(run *runs.Run, used map[string]bool) string {
	for i := 0; i < 16; i++ {
		p, err := runs.WriteFileExclusive(r.RunsDir, *run)
		if err == nil {
			return p
		}
		if !os.IsExist(err) {
			fmt.Fprintln(r.Stderr, "panal: could not write the run file:", err)
			return p
		}
		run.ID = r.uniqueID(runs.NewID(r.Now()), run.Agent, used)
		run.Log = filepath.Join(r.LogDir, run.ID+"-"+run.Agent+"-"+safeName(run.Model)+".txt")
	}
	return r.write(*run)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// safeName makes a model name usable in a file name.
func safeName(model string) string {
	if model == "" {
		return "default"
	}
	return strings.Trim(unsafeChars.ReplaceAllString(model, "_"), "_.")
}

// firstLine returns the first non-empty line of the task and whether the
// task has more than one line.
func firstLine(s string) (string, bool) {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	first, rest, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(first), strings.TrimSpace(rest) != ""
}

func stamp(t time.Time) string { return t.Format(time.RFC3339) }

func intp(i int) *int { return &i }

// syncWriter serializes writes from the stdout and stderr copiers.
type syncWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

// term wraps the terminal writers with one shared lock: stdout and stderr
// may be the same writer.
func (r *Runner) term(w io.Writer) io.Writer {
	r.termOnce.Do(func() { r.termMu = new(sync.Mutex) })
	return &syncWriter{w: w, mu: r.termMu}
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}
