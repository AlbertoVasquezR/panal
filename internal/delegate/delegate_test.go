package delegate

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AlbertoVasquezR/panal/internal/runs"
)

// env is a fake system: which CLIs are "installed" (all of them the test
// binary), in which mode each one behaves, and where files go.
type env struct {
	t         *testing.T
	runs, log string
	rec, dir  string
	out       bytes.Buffer
	r         *Runner
	interrupt chan os.Signal
}

func newEnv(t *testing.T, installed []string, modes map[string]string) *env {
	t.Helper()
	e := &env{t: t, runs: t.TempDir(), log: t.TempDir(), rec: t.TempDir(), dir: t.TempDir(), interrupt: make(chan os.Signal, 1)}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	extra := []string{"PANAL_FAKE_CLI=1", "PANAL_FAKE_RECORD=" + e.rec, "PANAL_FAKE_RUNS=" + e.runs}
	for cli, m := range modes {
		extra = append(extra, "PANAL_FAKE_"+strings.ToUpper(cli)+"="+m)
	}
	e.r = &Runner{
		Stdout: &e.out, Stderr: &e.out,
		RunsDir: e.runs, LogDir: e.log,
		LookPath: func(name string) (string, error) {
			if slices.Contains(installed, name) {
				return self, nil
			}
			return "", exec.ErrNotFound
		},
		Env:       extra,
		Now:       time.Now,
		Interrupt: e.interrupt,
		Grace:     200 * time.Millisecond,
	}
	return e
}

func (e *env) run(task, chain string) (int, []Result) {
	e.t.Helper()
	links, err := ParseChain(chain)
	if err != nil {
		e.t.Fatal(err)
	}
	return e.r.Run(Task{Text: task, Dir: e.dir, Timeout: 30 * time.Second, Chain: links})
}

// runFiles are the run files written, by agent.
func (e *env) runFiles() map[string]runs.Run {
	e.t.Helper()
	ms, _ := filepath.Glob(filepath.Join(e.runs, "*.json"))
	out := map[string]runs.Run{}
	for _, m := range ms {
		r, err := runs.ReadFile(m)
		if err != nil {
			e.t.Fatal(err)
		}
		out[r.Agent] = r
	}
	return out
}

func (e *env) recorded(name string) string {
	b, _ := os.ReadFile(filepath.Join(e.rec, name))
	return string(b)
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func statuses(rs []Result) []runs.Status {
	var out []runs.Status
	for _, r := range rs {
		out = append(out, r.Status)
	}
	return out
}

func TestDoneWithCodex(t *testing.T) {
	e := newEnv(t, []string{"codex"}, map[string]string{"codex": "ok"})
	code, res := e.run("Fix the table", "codex:gpt-test:low")
	if code != 0 || len(res) != 1 || res[0].Status != runs.Done {
		t.Fatalf("code %d, results %+v\n%s", code, res, e.out.String())
	}
	if seen := e.recorded("codex.seen"); seen != string(runs.Running) {
		t.Errorf("while running the run file said %q, want running", seen)
	}
	r := e.runFiles()["codex"]
	if r.Status != runs.Done || r.RC == nil || *r.RC != 0 || r.End == "" || r.Start == "" ||
		r.Model != "gpt-test" || r.Effort != "low" || r.Task != "Fix the table" || r.TaskFile != "" || r.Dir != e.dir {
		t.Errorf("run file: %+v", r)
	}
	if r.PID <= 0 || r.PID == os.Getpid() {
		t.Errorf("pid %d should be the agent's", r.PID)
	}
	if want := filepath.Join(e.log, r.ID+"-codex-gpt-test.txt"); r.Log != want {
		t.Errorf("log %q, want %q", r.Log, want)
	}
	log := read(t, r.Log)
	if !strings.Contains(log, "codex stderr line") || !strings.Contains(log, "All done.") {
		t.Errorf("log:\n%s", log)
	}
	if ev := read(t, r.Log+".jsonl"); !strings.Contains(ev, `"thread.started"`) {
		t.Errorf("events:\n%s", ev)
	}
	if _, err := os.Stat(r.Log + ".last"); err == nil {
		t.Error("the -o file should be folded into the log")
	}
	if got := e.recorded("codex.stdin"); got != "Fix the table" {
		t.Errorf("stdin %q", got)
	}
	args := strings.Split(e.recorded("codex.args"), "\n")
	for _, want := range [][]string{{"exec", "--json"}, {"-s", "workspace-write"}, {"-m", "gpt-test"}, {"-c", `model_reasoning_effort="low"`}, {"-C", e.dir}} {
		if i := slices.Index(args, want[0]); i < 0 || args[i+1] != want[1] {
			t.Errorf("args %q lack %q", args, want)
		}
	}
	out := e.out.String()
	for _, want := range []string{"«working on the task»", "$ go test ./...", "All done."} {
		if !strings.Contains(out, want) {
			t.Errorf("terminal lacks %q:\n%s", want, out)
		}
	}
	if s := Summary(res, e.dir); !strings.Contains(s, "Done by codex:gpt-test:low") || !strings.Contains(s, "Review the diff") || !strings.Contains(s, r.Log) {
		t.Errorf("summary:\n%s", s)
	}
}

func TestQuotaFallsBackAlongTheChain(t *testing.T) {
	e := newEnv(t, []string{"codex", "agy", "opencode"}, map[string]string{"codex": "quota", "agy": "quota", "opencode": "ok"})
	code, res := e.run("Add a test", "codex agy:gem opencode:go/glm")
	if code != 0 || !slices.Equal(statuses(res), []runs.Status{runs.OutOfQuota, runs.OutOfQuota, runs.Done}) {
		t.Fatalf("code %d, %v\n%s", code, statuses(res), e.out.String())
	}
	fs := e.runFiles()
	for _, a := range []string{"codex", "agy"} {
		if r := fs[a]; r.Status != runs.OutOfQuota || r.RC == nil || *r.RC != runs.ExitOutOfQuota {
			t.Errorf("%s: %+v", a, r)
		}
	}
	if fs["opencode"].Status != runs.Done || !strings.HasSuffix(fs["opencode"].Log, "-opencode-go_glm.txt") {
		t.Errorf("opencode: %+v", fs["opencode"])
	}
	if !strings.Contains(read(t, fs["agy"].Log), "RESOURCE_EXHAUSTED") {
		t.Error("agy's error should be in its log")
	}
}

func TestAllOutOfQuota(t *testing.T) {
	e := newEnv(t, []string{"codex", "opencode"}, map[string]string{"codex": "quota", "opencode": "quota"})
	code, res := e.run("x", "codex opencode")
	if code != runs.ExitOutOfQuota || len(res) != 2 {
		t.Fatalf("code %d, %v", code, statuses(res))
	}
	if s := Summary(res, e.dir); !strings.Contains(s, "No agent in the chain could take the task") {
		t.Errorf("summary:\n%s", s)
	}
}

func TestNoPermissionStopsTheChain(t *testing.T) {
	e := newEnv(t, []string{"codex", "agy"}, map[string]string{"codex": "perm", "agy": "ok"})
	code, res := e.run("x", "codex agy")
	if code != runs.ExitNoPermission || len(res) != 1 || res[0].Status != runs.NoPermission {
		t.Fatalf("code %d, %v", code, statuses(res))
	}
	if _, ran := e.runFiles()["agy"]; ran {
		t.Error("agy should not run after a permission refusal")
	}
}

func TestAgyHeadlessDenialIsNoPermission(t *testing.T) {
	e := newEnv(t, []string{"agy", "codex"}, map[string]string{"agy": "perm", "codex": "ok"})
	code, res := e.run("x", "agy")
	if code != runs.ExitNoPermission || len(res) != 1 || res[0].Status != runs.NoPermission {
		t.Fatalf("code %d, %v\n%s", code, statuses(res), e.out.String())
	}
	r := e.runFiles()["agy"]
	if r.Status != runs.NoPermission || r.RC == nil || *r.RC != runs.ExitNoPermission {
		t.Errorf("run file: %+v", r)
	}
}

func TestRealFailureDoesNotFallBack(t *testing.T) {
	e := newEnv(t, []string{"codex", "agy"}, map[string]string{"codex": "fail", "agy": "ok"})
	code, res := e.run("x", "codex agy")
	if code != 3 || len(res) != 1 || res[0].Status != runs.Failed {
		t.Fatalf("code %d, %v", code, statuses(res))
	}
	r := e.runFiles()["codex"]
	if r.RC == nil || *r.RC != 3 || r.Status != runs.Failed {
		t.Errorf("run file: %+v", r)
	}
	if _, ran := e.runFiles()["agy"]; ran {
		t.Error("agy should not run after a real failure")
	}
}

func TestDoneWithCursor(t *testing.T) {
	e := newEnv(t, []string{"cursor"}, map[string]string{"cursor": "ok"})
	code, res := e.run("Fix the table", "cursor:composer-2.5:high")
	if code != 0 || len(res) != 1 || res[0].Status != runs.Done {
		t.Fatalf("code %d, results %+v\n%s", code, res, e.out.String())
	}
	r := e.runFiles()["cursor"]
	if r.Status != runs.Done || r.Model != "composer-2.5" || r.Effort != "high" || r.Task != "Fix the table" {
		t.Errorf("run file: %+v", r)
	}
	args := strings.Split(e.recorded("cursor.args"), "\n")
	for _, want := range [][]string{{"-p", "--output-format"}, {"--output-format", "stream-json"}, {"--workspace", e.dir}, {"--model", "composer-2.5[effort=high]"}} {
		if i := slices.Index(args, want[0]); i < 0 || i+1 >= len(args) || args[i+1] != want[1] {
			t.Errorf("args %q lack %q", args, want)
		}
	}
	if !slices.Contains(args, "--force") || !slices.Contains(args, "--trust") {
		t.Errorf("write mode flags: %q", args)
	}
	for _, w := range []string{"--auto-review", "--approve-mcps", "--mode"} {
		if slices.Contains(args, w) {
			t.Errorf("write mode should not pass %s: %q", w, args)
		}
	}
	if ev := read(t, r.Log+".jsonl"); !strings.Contains(ev, `"tool_call"`) {
		t.Errorf("events:\n%s", ev)
	}
}

func TestCursorQuotaFallsBack(t *testing.T) {
	e := newEnv(t, []string{"cursor", "agy"}, map[string]string{"cursor": "quota", "agy": "ok"})
	code, res := e.run("Add a test", "cursor agy")
	if code != 0 || !slices.Equal(statuses(res), []runs.Status{runs.OutOfQuota, runs.Done}) {
		t.Fatalf("code %d, %v\n%s", code, statuses(res), e.out.String())
	}
}

func TestTalkingAboutRateLimitsIsNotOutOfQuota(t *testing.T) {
	e := newEnv(t, []string{"opencode"}, map[string]string{"opencode": "talk"})
	if code, res := e.run("x", "opencode"); code != 0 || res[0].Status != runs.Done {
		t.Fatalf("code %d, %v", code, statuses(res))
	}
}

func TestTimeoutKillsTheAgent(t *testing.T) {
	e := newEnv(t, []string{"agy", "codex"}, map[string]string{"agy": "hang", "codex": "ok"})
	links, _ := ParseChain("agy codex")
	start := time.Now()
	code, res := e.r.Run(Task{Text: "x", Dir: e.dir, Timeout: 500 * time.Millisecond, Chain: links})
	if code != runs.ExitTimeout || len(res) != 1 || res[0].Status != runs.Timeout {
		t.Fatalf("code %d, %v\n%s", code, statuses(res), e.out.String())
	}
	if d := time.Since(start); d > 20*time.Second {
		t.Errorf("took %s", d)
	}
	r := e.runFiles()["agy"]
	if r.Status != runs.Timeout || r.RC == nil || *r.RC != runs.ExitTimeout || r.End == "" {
		t.Errorf("run file: %+v", r)
	}
	if !strings.Contains(read(t, r.Log), "timed out") {
		t.Error("the log should say it timed out")
	}
}

func TestInterruptStopsAndRecords(t *testing.T) {
	e := newEnv(t, []string{"agy", "codex"}, map[string]string{"agy": "hang", "codex": "ok"})
	go func() {
		// Once the agent is running, ctrl+c.
		for e.recorded("agy.seen") == "" {
			time.Sleep(20 * time.Millisecond)
		}
		e.interrupt <- os.Interrupt
	}()
	code, res := e.run("x", "agy codex")
	if code != runs.ExitInterrupted || len(res) != 1 || res[0].Status != runs.Interrupted {
		t.Fatalf("code %d, %v", code, statuses(res))
	}
	if r := e.runFiles()["agy"]; r.Status != runs.Interrupted || r.End == "" {
		t.Errorf("run file: %+v", r)
	}
}

func TestMissingCLIIsSkipped(t *testing.T) {
	e := newEnv(t, []string{"codex"}, map[string]string{"codex": "ok"})
	code, res := e.run("x", "agy:gem codex")
	if code != 0 || !slices.Equal(statuses(res), []runs.Status{runs.Skipped, runs.Done}) {
		t.Fatalf("code %d, %v", code, statuses(res))
	}
	r := e.runFiles()["agy"]
	if r.Status != runs.Skipped || r.Start == "" || r.End == "" || r.RC != nil {
		t.Errorf("skipped run file: %+v", r)
	}
	if !strings.Contains(read(t, r.Log), "not found in PATH") {
		t.Error("the skipped run's log should say why")
	}
}

func TestNothingInstalled(t *testing.T) {
	e := newEnv(t, nil, nil)
	if code, res := e.run("x", "agy codex"); code != runs.ExitFailed || len(res) != 2 {
		t.Fatalf("code %d, %v", code, statuses(res))
	}
}

func TestCodexOnPaidCreditsIsSkipped(t *testing.T) {
	e := newEnv(t, []string{"codex", "agy"}, map[string]string{"codex": "ok", "agy": "ok"})
	e.r.CodexUsedUp = func(time.Time) (bool, string) { return true, "weekly quota used up" }
	code, res := e.run("x", "codex agy")
	if code != 0 || !slices.Equal(statuses(res), []runs.Status{runs.OutOfQuota, runs.Done}) {
		t.Fatalf("code %d, %v", code, statuses(res))
	}
	if e.recorded("codex.args") != "" {
		t.Error("codex must not be launched")
	}
	if r := e.runFiles()["codex"]; r.RC == nil || *r.RC != runs.ExitOutOfQuota {
		t.Errorf("codex run file: %+v", r)
	}
}

func TestMultilineTask(t *testing.T) {
	e := newEnv(t, []string{"opencode", "agy"}, map[string]string{"opencode": "ok", "agy": "ok"})
	task := "Refactor the parser\n\n- keep the API\n- add tests \"quoted\" & 100%\n"
	code, _ := e.run(task, "opencode:go/glm:high")
	if code != 0 {
		t.Fatalf("code %d\n%s", code, e.out.String())
	}
	r := e.runFiles()["opencode"]
	if r.Task != "Refactor the parser" || r.TaskFile == "" || filepath.Dir(r.TaskFile) != e.runs {
		t.Fatalf("run file: %+v", r)
	}
	if got := read(t, r.TaskFile); got != task {
		t.Errorf("task file %q", got)
	}
	args := strings.Split(e.recorded("opencode.args"), "\n")
	if i := slices.Index(args, "--file"); i < 0 || args[i+1] != r.TaskFile {
		t.Errorf("opencode should get the task as a file: %q", args)
	}
	if i := slices.Index(args, "--model"); i < 0 || args[i+1] != "go/glm#high" {
		t.Errorf("model and effort: %q", args)
	}
	if !slices.Contains(args, "--auto") {
		t.Errorf("writes need --auto: %q", args)
	}

	// agy takes it on the command line, line breaks and all.
	e2 := newEnv(t, []string{"agy"}, map[string]string{"agy": "ok"})
	if code, _ := e2.run(task, "agy"); code != 0 {
		t.Fatal(code)
	}
	if args := strings.Split(e2.recorded("agy.args"), "\n"); args[0] != "-p" || !strings.HasPrefix(e2.recorded("agy.args"), "-p\n"+task) {
		t.Errorf("agy args %q", args)
	}
}

func TestSameAgentTwiceGetsTwoRunFiles(t *testing.T) {
	e := newEnv(t, []string{"codex"}, map[string]string{"codex": "quota"})
	_, res := e.run("x", "codex:a codex:b")
	if len(res) != 2 || res[0].RunFile == res[1].RunFile || res[0].Log == res[1].Log {
		t.Fatalf("results %+v", res)
	}
}

func TestConcurrentDelegationsSameSecond(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 30, 26, 0, time.UTC)
	shared := t.TempDir()
	const n = 12
	type got struct {
		id, taskFile, task string
		err                string
	}
	ch := make(chan got, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			e := newEnv(t, []string{"cursor"}, map[string]string{"cursor": "ok"})
			e.r.RunsDir = shared
			e.r.Now = func() time.Time { return now }
			task := fmt.Sprintf("Fix the table\n\nconcurrent run %d", i)
			code, res := e.run(task, "cursor")
			if code != 0 || len(res) != 1 {
				ch <- got{err: fmt.Sprintf("run %d: exit %d res %v\n%s", i, code, statuses(res), e.out.String())}
				return
			}
			r, err := runs.ReadFile(res[0].RunFile)
			if err != nil {
				ch <- got{err: err.Error()}
				return
			}
			body, _ := os.ReadFile(r.TaskFile)
			ch <- got{id: r.ID, taskFile: r.TaskFile, task: string(body)}
		}(i)
	}
	wg.Wait()
	close(ch)
	ids, files, texts := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for g := range ch {
		if g.err != "" {
			t.Error(g.err)
			continue
		}
		if !strings.Contains(g.id, ".") {
			t.Errorf("id %q should include milliseconds", g.id)
		}
		if ids[g.id] {
			t.Errorf("duplicate id %q", g.id)
		}
		ids[g.id] = true
		if g.taskFile == "" || files[g.taskFile] {
			t.Errorf("task file %q", g.taskFile)
		}
		files[g.taskFile] = true
		if texts[g.task] {
			t.Errorf("two runs shared task text %q", g.task)
		}
		texts[g.task] = true
	}
	if len(ids) != n {
		t.Fatalf("got %d unique ids, want %d", len(ids), n)
	}
}

func TestReadOnlyArgs(t *testing.T) {
	a := attempt{Link: Link{CLI: "x"}, Task: "look", Dir: "D", ReadOnly: true, Log: "L"}
	cases := map[string][]string{
		"codex":    {"-s", "read-only"},
		"agy":      {"--mode", "plan", "--sandbox"},
		"opencode": {"--agent", "plan"},
		"cursor":   cursorReadOnlyFlags(runtime.GOOS),
	}
	for cli, want := range cases {
		a.Link.CLI = cli
		args := agents[cli].build(a).Args
		i := slices.Index(args, want[0])
		if i < 0 || !slices.Equal(args[i:i+len(want)], want) {
			t.Errorf("%s read-only args %q lack %q", cli, args, want)
		}
		for _, w := range []string{"workspace-write", "--auto", "--dangerously-skip-permissions", "--force"} {
			if slices.Contains(args, w) {
				t.Errorf("%s read-only args contain %s", cli, w)
			}
		}
	}
}

func TestCursorReadOnlyFlags(t *testing.T) {
	cases := []struct {
		goos string
		want []string
	}{
		{"linux", []string{"--auto-review", "--sandbox", "enabled"}},
		{"darwin", []string{"--auto-review", "--sandbox", "enabled"}},
		{"windows", []string{"--auto-review"}},
		{"js", []string{"--auto-review"}},
	}
	for _, c := range cases {
		got := cursorReadOnlyFlags(c.goos)
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: %q, want %q", c.goos, got, c.want)
		}
		if slices.Contains(got, "--force") {
			t.Errorf("%s: write flag in read-only args: %q", c.goos, got)
		}
	}
}

func TestPermissionHint(t *testing.T) {
	msg := "Error: Sandbox mode is enabled but not available on this system. Sandbox requires macOS or Linux."
	if h := permissionHint("cursor", msg); !strings.Contains(h, "Windows") || !strings.Contains(h, "--auto-review") {
		t.Errorf("hint: %q", h)
	}
	if h := permissionHint("cursor", "Untrusted workspace. Pass --trust"); h != "" {
		t.Errorf("no hint for trust: %q", h)
	}
	if h := permissionHint("agy", msg); h != "" {
		t.Errorf("agy should not get the cursor hint: %q", h)
	}
}

func TestParseChain(t *testing.T) {
	ls, err := ParseChain("codex:gpt-x:low, agy  opencode:ollama/llama3:8b opencode:p/m#v:high codex::medium codex:high")
	if err != nil {
		t.Fatal(err)
	}
	want := []Link{
		{"codex", "gpt-x", "low"}, {"agy", "", ""}, {"opencode", "ollama/llama3:8b", ""},
		{"opencode", "p/m#v", "high"}, {"codex", "", "medium"}, {"codex", "", "high"},
	}
	if !slices.Equal(ls, want) {
		t.Errorf("got %+v\nwant %+v", ls, want)
	}
	for _, l := range want {
		if back, err := ParseChain(l.String()); err != nil || back[0] != l {
			t.Errorf("round trip %v → %q → %v", l, l.String(), back)
		}
	}
	if _, err := ParseChain("cursor:composer-2.5:high"); err != nil {
		t.Errorf("cursor link: %v", err)
	}
	if _, err := ParseChain("claude:x"); err == nil {
		t.Error("unknown agent should fail")
	}
	if _, err := ParseChain("  "); err == nil {
		t.Error("empty chain should fail")
	}
}

func TestDefaultChainIsTheInstalledCLIs(t *testing.T) {
	look := func(n string) (string, error) {
		if n == "codex" {
			return "", errors.New("no")
		}
		return n, nil
	}
	if got := defaultChain(look); !slices.Equal(got, []Link{{CLI: "agy"}, {CLI: "opencode"}, {CLI: "cursor"}}) {
		t.Errorf("default chain %v", got)
	}
}

func TestMainArguments(t *testing.T) {
	var out, errb bytes.Buffer
	if code := Main([]string{"-h"}, &out, &errb); code != 0 || !strings.Contains(errb.String(), "Exit codes:") || !strings.Contains(errb.String(), "does NOT mean the work is") {
		t.Errorf("-h: code %d\n%s", code, errb.String())
	}
	errb.Reset()
	if code := Main([]string{"do it"}, &out, &errb); code != 2 || !strings.Contains(errb.String(), "-d DIR is required") {
		t.Errorf("no -d: code %d %s", code, errb.String())
	}
	errb.Reset()
	if code := Main([]string{"-d", t.TempDir()}, &out, &errb); code != 2 || !strings.Contains(errb.String(), "no task") {
		t.Errorf("no task: code %d %s", code, errb.String())
	}
	errb.Reset()
	if code := Main([]string{"-d", t.TempDir(), "-c", "nope:x", "task"}, &out, &errb); code != 2 || !strings.Contains(errb.String(), "unknown agent") {
		t.Errorf("bad chain: code %d %s", code, errb.String())
	}
}
