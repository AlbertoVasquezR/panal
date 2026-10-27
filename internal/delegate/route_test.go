package delegate

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/AlbertoVasquezR/panal/internal/config"
	"github.com/AlbertoVasquezR/panal/internal/feedback"
	"github.com/AlbertoVasquezR/panal/internal/history"
	"github.com/AlbertoVasquezR/panal/internal/router"
	"github.com/AlbertoVasquezR/panal/internal/runs"
)

// fakeRouting reads the history of the env's run dir and never runs git.
func fakeRouting(e *env, seed uint64) routing {
	return routing{
		History: func() []history.Run { return history.New().Read(e.runs, 0) },
		Checks:  &router.Checks{Run: func(history.Run, time.Time) router.Check { return router.Check{} }},
		Now:     time.Now,
		Rand:    rand.New(rand.NewPCG(seed, seed+1)),
	}
}

// isolate keeps history's lookups (feedback, codex sessions, opencode's
// database) away from the machine's real files.
func isolate(t *testing.T) {
	d := t.TempDir()
	for _, v := range []string{"PANAL_DATA", "CODEX_SESSIONS", "OPENCODE_DB"} {
		t.Setenv(v, d)
	}
}

// past writes finished runs of an agent at a task into the env's run dir.
func past(t *testing.T, e *env, agent, task string, st runs.Status, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		at := time.Now().Add(-time.Duration(i+1) * time.Hour)
		r := runs.Run{ID: fmt.Sprintf("%s-%s-%d", at.Format("20060102-150405"), st, i), Agent: agent, Task: task, Dir: e.dir,
			Status: st, Start: stamp(at), End: stamp(at.Add(time.Minute))}
		if _, err := runs.WriteFile(e.runs, r); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAutoPicksFromHistoryAndRecordsTheRoute(t *testing.T) {
	isolate(t)
	e := newEnv(t, []string{"codex", "agy"}, map[string]string{"codex": "ok", "agy": "ok"})
	// codex is the cheaper arm, but it keeps failing simple fixes.
	past(t, e, "codex", "Fix the typo in docs/a.md", runs.Failed, 6)
	past(t, e, "agy", "Fix the typo in docs/b.md", runs.Done, 6)
	pool, _ := ParseChain("codex agy")

	task := Task{Text: "Fix the typo in README", Dir: e.dir, Timeout: 30 * time.Second}
	d, err := autoTask(fakeRouting(e, 1), &task, pool)
	if err != nil {
		t.Fatal(err)
	}
	if task.Chain[0].CLI != "agy" || len(task.Chain) != 2 || !task.Auto || task.Tier != router.Simple || task.TaskType != "fix" {
		t.Fatalf("task: %+v\n%s", task, d.Why)
	}
	if !strings.HasPrefix(d.Why, "auto: agy — fix · simple · 6/6 ok in the last 30 days") {
		t.Errorf("explanation: %q", d.Why)
	}
	code, res := e.r.Run(task)
	if code != 0 || len(res) != 1 || res[0].Status != runs.Done {
		t.Fatalf("code %d %+v\n%s", code, res, e.out.String())
	}
	r := e.runFiles()["agy"]
	if !r.Auto || r.Route != d.Why || r.Choice != 1 || r.TaskType != "fix" || r.Tier != router.Simple {
		t.Errorf("run file: %+v", r)
	}

	// Rated bad, that run now counts against agy.
	if err := feedback.Set(feedback.Path(), feedback.Key(r.ID, r.Agent), feedback.Bad, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	var mine history.Run
	for _, h := range history.New().Read(e.runs, 0) {
		if h.Key() == feedback.Key(r.ID, r.Agent) {
			mine = h
		}
	}
	if mine.Rating != feedback.Bad || router.OutcomeOf(router.Observe(mine, nil, time.Now())) != router.Failure {
		t.Errorf("rated run: %+v", mine)
	}
}

func TestAutoFallsBackAndTheReportCountsIt(t *testing.T) {
	isolate(t)
	e := newEnv(t, []string{"codex", "agy"}, map[string]string{"codex": "quota", "agy": "ok"})
	pool, _ := ParseChain("codex agy")
	// Lots of simple successes for both: the cheap one, codex, goes first.
	past(t, e, "codex", "Fix the typo in docs/a.md", runs.Done, 10)
	past(t, e, "agy", "Fix the typo in docs/b.md", runs.Done, 10)
	task := Task{Text: "Fix the typo in README", Dir: e.dir, Timeout: 30 * time.Second}
	if _, err := autoTask(fakeRouting(e, 2), &task, pool); err != nil {
		t.Fatal(err)
	}
	if task.Chain[0].CLI != "codex" {
		t.Fatalf("chain %v (%s)", task.Chain, task.Route)
	}
	code, res := e.r.Run(task)
	if code != 0 || len(res) != 2 || res[0].Status != runs.OutOfQuota || res[1].Status != runs.Done {
		t.Fatalf("code %d %v", code, statuses(res))
	}
	files := e.runFiles()
	codex, agy := runs.Run{}, files["agy"]
	for _, f := range runs.List([]string{e.runs}) {
		if f.Run.Agent == "codex" && f.Run.Auto {
			codex = f.Run
		}
	}
	if codex.Choice != 1 || agy.Choice != 2 || !agy.Auto || agy.Route != task.Route {
		t.Errorf("choices: codex %+v agy %+v", codex, agy)
	}
	s := router.Summarize(history.New().Read(e.runs, 0), time.Now().Add(-time.Hour), fakeRouting(e, 0).Checks, time.Now())
	if s.Decisions != 1 || s.FellBack != 1 || s.FirstChoiceRuns != 0 {
		t.Errorf("summary: %+v", s)
	}
}

func TestRecordCheckCachesTheLastRun(t *testing.T) {
	e := newEnv(t, []string{"codex"}, map[string]string{"codex": "ok"})
	_, res := e.run("Fix the table", "codex")
	checks := &router.Checks{}
	recordCheck(checks, res, "Fix the table", time.Now())
	r := e.runFiles()["codex"]
	called := false
	checks.Run = func(history.Run, time.Time) router.Check { called = true; return router.Check{} }
	c := checks.Get(history.Run{Stamp: r.ID, Agent: "codex", Status: runs.Done}, time.Now())
	if called || !c.TestsSeen || !c.TestsPassed {
		t.Errorf("check: %+v (recomputed: %v)", c, called)
	}
}

func TestPoolSpec(t *testing.T) {
	t.Setenv("PANAL_POOL", "")
	if s, _ := poolSpec("", config.Conf{}); s != "" {
		t.Errorf("nothing configured: %q", s)
	}
	if s, _ := poolSpec("codex agy", config.Conf{}); s != "codex agy" {
		t.Errorf("the chain is the default pool: %q", s)
	}
	if s, _ := poolSpec("auto", config.Conf{}); s != "" {
		t.Errorf("auto is not a pool: %q", s)
	}
	if s, _ := poolSpec("codex agy", config.Conf{Pool: "agy opencode"}); s != "agy opencode" {
		t.Errorf("pool wins over the chain: %q", s)
	}
	t.Setenv("PANAL_POOL", "opencode")
	if s, from := poolSpec("codex", config.Conf{Pool: "agy"}); s != "opencode" || from != "PANAL_POOL" {
		t.Errorf("PANAL_POOL wins: %q %q", s, from)
	}
}

func TestRouteDryRun(t *testing.T) {
	isolate(t)
	t.Setenv("PANAL_POOL", "")
	e := newEnv(t, nil, nil)
	past(t, e, "agy", "Fix the typo in docs/b.md", runs.Done, 3)
	past(t, e, "codex", "Migrate the whole repo to v2", runs.Failed, 2)
	lookPath := func(string) (string, error) { return "", exec.ErrNotFound }
	var out, errb bytes.Buffer
	code := routeMain([]string{"-seed", "3", "-p", "codex agy", "Fix the typo in README"}, &out, &errb, config.Conf{}, fakeRouting(e, 0), lookPath)
	if code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	txt := out.String()
	for _, want := range []string{
		"task   Fix the typo in README\n",
		"type   fix\n",
		"tier   simple  (score -3: short task -1, small change -2)\n",
		"pool   2 arms from -p, cheapest first\n",
		"agy ", "3/3 · 3/3 · 3/3",
		"codex ", "0/0 · 0/0 · 0/2",
		"\nchain  ",
		"\nauto: ",
	} {
		if !strings.Contains(txt, want) {
			t.Errorf("missing %q in:\n%s", want, txt)
		}
	}
	if strings.Count(txt, "→ ") != 1 {
		t.Errorf("exactly one arm is marked as the pick:\n%s", txt)
	}
	// The same seed gives the same answer.
	var again bytes.Buffer
	routeMain([]string{"-seed", "3", "-p", "codex agy", "Fix the typo in README"}, &again, &errb, config.Conf{}, fakeRouting(e, 0), lookPath)
	if again.String() != txt {
		t.Errorf("not repeatable:\n%s\n---\n%s", txt, again.String())
	}

	out.Reset()
	if code := routeMain([]string{"-stats", "-p", "codex agy"}, &out, &errb, config.Conf{}, fakeRouting(e, 0), lookPath); code != 0 {
		t.Fatalf("stats code %d: %s", code, errb.String())
	}
	stats := out.String()
	for _, want := range []string{"learned from 5 runs (5 with a signal", "arm ", "agy ", "fix ", "simple ", "3/3", "codex ", "complex ", "0/2"} {
		if !strings.Contains(stats, want) {
			t.Errorf("stats lack %q:\n%s", want, stats)
		}
	}

	if code := routeMain(nil, &out, &errb, config.Conf{}, fakeRouting(e, 0), lookPath); code != 2 {
		t.Errorf("no task: code %d", code)
	}
	if code := routeMain([]string{"x"}, &out, &errb, config.Conf{}, fakeRouting(e, 0), lookPath); code != 2 || !strings.Contains(errb.String(), "none of codex, agy, opencode, cursor is installed") {
		t.Errorf("no pool and nothing installed: code %d %s", code, errb.String())
	}
}
