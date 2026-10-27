package readers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AlbertoVasquezR/panal/internal/runs"
	"github.com/AlbertoVasquezR/panal/internal/state"
)

func TestDelegate_Testdata(t *testing.T) {
	dir := filepath.Join("testdata", "legacy_runs") // run files as delegar.sh wrote them

	// agy has 3 files in testdata; the newest is 20260924-081422-agy.json (failed)
	dAgy := &Delegate{
		Name:     "agy",
		Dirs:     []string{dir},
		PIDAlive: func(pid int) bool { return true },
		Now:      func() time.Time { return time.Now() },
	}
	fAgy := dAgy.Read()
	if fAgy.Agent != "agy" {
		t.Fatalf("expected agy, got %s", fAgy.Agent)
	}
	if fAgy.Status != state.Failed {
		t.Fatalf("expected Failed, got %v", fAgy.Status)
	}
	if fAgy.Model != "gemini-3.8-flash-low" {
		t.Fatalf("expected gemini-3.8-flash-low, got %s", fAgy.Model)
	}
	if fAgy.Task != "Escribe en d.txt los números del 1 al 300, uno por línea, pensando cada uno. No hagas git commit." {
		t.Fatalf("wrong task: %s", fAgy.Task)
	}
	if !strings.Contains(fAgy.Detail, localEnd("2026-09-24T08:14:33-06:00")) {
		t.Fatalf("detail has no end: %s", fAgy.Detail)
	}
	if !strings.Contains(fAgy.Detail, "rc: 1") {
		t.Fatalf("detail has no rc: 1: %s", fAgy.Detail)
	}
	if fAgy.End.IsZero() || fAgy.Start.IsZero() {
		t.Fatalf("fAgy should have Start and End: start=%v, end=%v", fAgy.Start, fAgy.End)
	}

	// codex has 20260924-081211-codex.json in testdata (out of quota)
	codexEnd, err := time.Parse(time.RFC3339, "2026-09-24T08:12:13-06:00")
	if err != nil {
		t.Fatal(err)
	}
	codexNow := codexEnd.Add(15 * time.Minute)
	dCodex := &Delegate{
		Name:     "codex",
		Dirs:     []string{dir},
		PIDAlive: func(pid int) bool { return true },
		Now:      func() time.Time { return codexNow },
	}
	fCodex := dCodex.Read()
	if fCodex.Agent != "codex" {
		t.Fatalf("expected codex, got %s", fCodex.Agent)
	}
	if fCodex.Status != state.OutOfQuota {
		t.Fatalf("expected OutOfQuota, got %v", fCodex.Status)
	}
	if fCodex.Model != "gpt-6-luna (low)" {
		t.Fatalf("expected gpt-6-luna (low), got %s", fCodex.Model)
	}
	if !fCodex.Quota.SeenAt.Equal(codexEnd) {
		t.Fatalf("expected SeenAt %v, got %v", codexEnd, fCodex.Quota.SeenAt)
	}
	if fCodex.Quota.Summary != "used up (15 min ago)" {
		t.Fatalf("expected 'used up (15 min ago)', got '%s'", fCodex.Quota.Summary)
	}
	if !strings.Contains(fCodex.Detail, localEnd("2026-09-24T08:12:13-06:00")) || !strings.Contains(fCodex.Detail, "rc: 75") {
		t.Fatalf("wrong detail: %s", fCodex.Detail)
	}
	if !fCodex.End.Equal(codexEnd) || fCodex.Start.IsZero() {
		t.Fatalf("fCodex should have Start and End: start=%v, end=%v", fCodex.Start, fCodex.End)
	}

	// opencode has no files in testdata -> NoData without error
	dOpencode := &Delegate{
		Name:     "opencode",
		Dirs:     []string{dir},
		PIDAlive: func(pid int) bool { return true },
		Now:      func() time.Time { return time.Now() },
	}
	fOpencode := dOpencode.Read()
	if fOpencode.Status != state.NoData {
		t.Fatalf("expected NoData, got %v", fOpencode.Status)
	}
	if fOpencode.Error != "" {
		t.Fatalf("expected empty Error, got %s", fOpencode.Error)
	}
}

func TestDelegate_SpecialDirs(t *testing.T) {
	// Empty directory
	emptyDir := t.TempDir()
	dEmpty := &Delegate{
		Name:     "agy",
		Dirs:     []string{emptyDir},
		PIDAlive: func(int) bool { return true },
		Now:      time.Now,
	}
	fEmpty := dEmpty.Read()
	if fEmpty.Status != state.NoData || fEmpty.Error != "" {
		t.Fatalf("empty dir must be NoData without error, got %v, error: %s", fEmpty.Status, fEmpty.Error)
	}

	// Missing directory
	dMissing := &Delegate{
		Name:     "agy",
		Dirs:     []string{filepath.Join(t.TempDir(), "does-not-exist")},
		PIDAlive: func(int) bool { return true },
		Now:      time.Now,
	}
	fMissing := dMissing.Read()
	if fMissing.Status != state.NoData || fMissing.Error != "" {
		t.Fatalf("missing dir must be NoData without error, got %v, error: %s", fMissing.Status, fMissing.Error)
	}
}

func TestDelegate_Running_WorkingAndStuck(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "log.txt")
	if err := os.WriteFile(logPath, []byte("active log\n"), 0644); err != nil {
		t.Fatal(err)
	}

	startStr := "2026-09-24T10:00:00Z"
	startTime, _ := time.Parse(time.RFC3339, startStr)
	now := startTime.Add(30 * time.Minute)

	jsonContent := fmt.Sprintf(`{
		"version": 1,
		"agent": "agy",
		"model": "gemini-3.8",
		"task": "work",
		"dir": "C:\\wt",
		"pid": 12345,
		"log": %q,
		"start": %q,
		"status": "running"
	}`, logPath, startStr)

	jsonPath := filepath.Join(dir, "20260924-100000-agy.json")
	if err := os.WriteFile(jsonPath, []byte(jsonContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Case 1: modified 8 min ago (< 10 min) -> Working, Since = start
	recentMod := now.Add(-8 * time.Minute)
	if err := os.Chtimes(logPath, recentMod, recentMod); err != nil {
		t.Fatal(err)
	}

	d := &Delegate{
		Name:     "agy",
		Dirs:     []string{dir},
		PIDAlive: func(pid int) bool { return pid == 12345 },
		Now:      func() time.Time { return now },
	}

	f := d.Read()
	if f.Status != state.Working {
		t.Fatalf("expected Working for modified 8m ago, got %v", f.Status)
	}
	if !f.Since.Equal(startTime) {
		t.Fatalf("expected Since %v, got %v", startTime, f.Since)
	}
	if f.Error != "" {
		t.Fatalf("unexpected error: %s", f.Error)
	}

	// Case 2: modified exactly 10 min ago (>= 10 min) -> Stuck, Since = start
	mod10 := now.Add(-10 * time.Minute)
	if err := os.Chtimes(logPath, mod10, mod10); err != nil {
		t.Fatal(err)
	}
	f10 := d.Read()
	if f10.Status != state.Stuck {
		t.Fatalf("expected Stuck for modified exactly 10m ago, got %v", f10.Status)
	}
	if !f10.Since.Equal(startTime) {
		t.Fatalf("expected Since %v, got %v", startTime, f10.Since)
	}

	// Case 3: modified 15 min ago (> 10 min) -> Stuck, Since = start
	oldMod := now.Add(-15 * time.Minute)
	if err := os.Chtimes(logPath, oldMod, oldMod); err != nil {
		t.Fatal(err)
	}
	fOld := d.Read()
	if fOld.Status != state.Stuck {
		t.Fatalf("expected Stuck for modified 15m ago, got %v", fOld.Status)
	}
	if !fOld.Since.Equal(startTime) {
		t.Fatalf("expected Since %v, got %v", startTime, fOld.Since)
	}
}

func TestDelegate_Running_DeadPID(t *testing.T) {
	dir := t.TempDir()
	jsonContent := `{
		"version": 1,
		"id": "20260924-100000",
		"agent": "codex",
		"model": "gpt-6",
		"task": "something",
		"pid": 54321,
		"start": "2026-09-24T10:00:00Z",
		"status": "running"
	}`
	jsonPath := filepath.Join(dir, "20260924-100000-codex.json")
	if err := os.WriteFile(jsonPath, []byte(jsonContent), 0644); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 9, 24, 11, 0, 0, 0, time.UTC)
	d := &Delegate{
		Name:     "codex",
		Dirs:     []string{dir},
		PIDAlive: func(pid int) bool { return false }, // dead PID
		Now:      func() time.Time { return now },
	}

	f := d.Read()
	if f.Status != state.Failed {
		t.Fatalf("expected Failed for a dead pid, got %v", f.Status)
	}

	got, err := runs.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != runs.Failed {
		t.Fatalf("run file should be rewritten as failed, got %s", got.Status)
	}
	if got.End == "" || got.RC == nil || *got.RC != runs.ExitFailed {
		t.Fatalf("stale close: %+v", got)
	}

	// A second read keeps the persisted failed record (not "running").
	f2 := d.Read()
	if f2.Status != state.Failed {
		t.Fatalf("second read: %v", f2.Status)
	}
}

func TestDelegate_TerminalStates(t *testing.T) {
	cases := []struct {
		jsonStatus   string
		wantStatus   state.Status
		rc           int
		wantQuota    bool
		minutesAfter int
		quotaSummary string
	}{
		{jsonStatus: "done", wantStatus: state.Done, rc: 0},
		{jsonStatus: "out_of_quota", wantStatus: state.OutOfQuota, rc: 75, wantQuota: true, minutesAfter: 20, quotaSummary: "used up (20 min ago)"},
		{jsonStatus: "no_permission", wantStatus: state.NoPermission, rc: 126},
		{jsonStatus: "skipped", wantStatus: state.Idle, rc: 0},
		{jsonStatus: "timeout", wantStatus: state.Failed, rc: 124},
		{jsonStatus: "failed", wantStatus: state.Failed, rc: 1},
		{jsonStatus: "interrupted", wantStatus: state.Failed, rc: 130},
	}

	for _, tc := range cases {
		t.Run(tc.jsonStatus, func(t *testing.T) {
			dir := t.TempDir()

			// Create a log file with 7 lines
			logPath := filepath.Join(dir, "reg.log")
			var lines []string
			for i := 1; i <= 7; i++ {
				lines = append(lines, fmt.Sprintf("log line %d", i))
			}
			if err := os.WriteFile(logPath, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
				t.Fatal(err)
			}

			endTime, _ := time.Parse(time.RFC3339, "2026-09-24T12:00:00Z")
			nowTime := endTime.Add(time.Duration(tc.minutesAfter) * time.Minute)

			jsonContent := fmt.Sprintf(`{
				"version": 1,
				"agent": "opencode",
				"model": "mimo",
				"effort": "high",
				"task": "terminal task",
				"dir": "C:\\wt",
				"pid": 1111,
				"log": %q,
				"start": "2026-09-24T11:55:00Z",
				"status": %q,
				"end": %q,
				"rc": %d
			}`, logPath, tc.jsonStatus, endTime.Format(time.RFC3339), tc.rc)

			jsonPath := filepath.Join(dir, "20260924-115500-opencode.json")
			if err := os.WriteFile(jsonPath, []byte(jsonContent), 0644); err != nil {
				t.Fatal(err)
			}

			d := &Delegate{
				Name:     "opencode",
				Dirs:     []string{dir},
				PIDAlive: func(int) bool { return false },
				Now:      func() time.Time { return nowTime },
			}

			f := d.Read()
			if f.Status != tc.wantStatus {
				t.Fatalf("wrong status for %s: expected %v, got %v", tc.jsonStatus, tc.wantStatus, f.Status)
			}
			if f.Model != "mimo (high)" {
				t.Fatalf("wrong model: expected 'mimo (high)', got %s", f.Model)
			}

			// Check Detail
			if !strings.Contains(f.Detail, localEnd(endTime.Format(time.RFC3339))) {
				t.Errorf("detail must include the end: %s", f.Detail)
			}
			if !strings.Contains(f.Detail, fmt.Sprintf("rc: %d", tc.rc)) {
				t.Errorf("detail must include rc: %s", f.Detail)
			}
			if !strings.Contains(f.Detail, logPath) {
				t.Errorf("detail must include the log path: %s", f.Detail)
			}

			// It must include the last 5 log lines (3 to 7), not the first 2
			if strings.Contains(f.Detail, "log line 1") || strings.Contains(f.Detail, "log line 2") {
				t.Errorf("detail must not contain lines outside the last 5: %s", f.Detail)
			}
			for i := 3; i <= 7; i++ {
				line := fmt.Sprintf("log line %d", i)
				if !strings.Contains(f.Detail, line) {
					t.Errorf("detail must contain %q: %s", line, f.Detail)
				}
			}

			// Check the quota for OutOfQuota
			if tc.wantQuota {
				if !f.Quota.SeenAt.Equal(endTime) {
					t.Errorf("expected Quota.SeenAt %v, got %v", endTime, f.Quota.SeenAt)
				}
				if f.Quota.Summary != tc.quotaSummary {
					t.Errorf("expected Quota.Summary %q, got %q", tc.quotaSummary, f.Quota.Summary)
				}
			}
		})
	}
}

func TestDelegate_BrokenJSON(t *testing.T) {
	dir := t.TempDir()

	// Only a broken file for agy
	brokenPath := filepath.Join(dir, "20260924-100000-agy.json")
	if err := os.WriteFile(brokenPath, []byte(`{this json is broken`), 0644); err != nil {
		t.Fatal(err)
	}

	d := &Delegate{
		Name:     "agy",
		Dirs:     []string{dir},
		PIDAlive: func(int) bool { return true },
		Now:      time.Now,
	}

	f := d.Read()
	if f.Status != state.NoData {
		t.Fatalf("expected NoData when there is only broken json, got %v", f.Status)
	}
	if f.Error == "" {
		t.Fatal("expected Row.Error to note the broken json")
	}

	// Add a newer valid file
	validContent := `{
		"version": 1,
		"agent": "agy",
		"model": "gemini",
		"task": "valid task",
		"start": "2026-09-24T12:00:00Z",
		"status": "done",
		"end": "2026-09-24T12:01:00Z",
		"rc": 0
	}`
	validPath := filepath.Join(dir, "20260924-120000-agy.json")
	if err := os.WriteFile(validPath, []byte(validContent), 0644); err != nil {
		t.Fatal(err)
	}

	f2 := d.Read()
	if f2.Status != state.Done {
		t.Fatalf("expected Done from the valid file, got %v", f2.Status)
	}
	if f2.Task != "valid task" {
		t.Fatalf("expected 'valid task', got %s", f2.Task)
	}
	if f2.Error == "" {
		t.Fatal("expected Row.Error to still carry the broken file note")
	}
}

func TestDelegate_Newest(t *testing.T) {
	dir := t.TempDir()

	writeJSON := func(name, start, task, st string) {
		content := fmt.Sprintf(`{
			"version": 1,
			"agent": "opencode",
			"model": "mimo",
			"task": %q,
			"start": %q,
			"status": %q,
			"end": "2026-09-24T12:00:00Z",
			"rc": 0
		}`, task, start, st)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Three files with starts out of order
	writeJSON("1.json", "2026-09-24T08:00:00Z", "first", "done")
	writeJSON("2.json", "2026-09-24T14:00:00Z", "newest", "skipped")
	writeJSON("3.json", "2026-09-24T11:00:00Z", "middle", "failed")

	d := &Delegate{
		Name:     "opencode",
		Dirs:     []string{dir},
		PIDAlive: func(int) bool { return true },
		Now:      time.Now,
	}

	f := d.Read()
	if f.Task != "newest" {
		t.Fatalf("expected task 'newest', got %s", f.Task)
	}
	if f.Status != state.Idle {
		t.Fatalf("expected Idle status (from skipped), got %v", f.Status)
	}
}

// Codex writes <log> only at the end; while it works, <log>.jsonl grows.
// Without looking at the .jsonl, a working codex would show "stuck" from the start.
func TestDelegate_ActivityInJsonlAndNoLog(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "codex.txt")
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.Local)
	start := now.Add(-30 * time.Minute)
	write := func() {
		js := fmt.Sprintf(`{"version":1,"agent":"codex","model":"gpt-6-luna","pid":42,
			"log":%q,"start":%q,"status":"running"}`, log, start.Format(time.RFC3339))
		if err := os.WriteFile(filepath.Join(dir, "20260924-113000-codex.json"), []byte(js), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write()
	d := &Delegate{Name: "codex", Dirs: []string{dir}, PIDAlive: func(int) bool { return true }, Now: func() time.Time { return now }}

	// No files at all and started 30 min ago: stuck.
	if f := d.Read(); f.Status != state.Stuck {
		t.Fatalf("no files and 30 min: expected stuck, got %v", f.Status)
	}
	// Only the .jsonl, touched 1 min ago: working.
	if err := os.WriteFile(log+".jsonl", []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oneMinAgo := now.Add(-time.Minute)
	if err := os.Chtimes(log+".jsonl", oneMinAgo, oneMinAgo); err != nil {
		t.Fatal(err)
	}
	if f := d.Read(); f.Status != state.Working {
		t.Fatalf("jsonl touched 1 min ago: expected working, got %v", f.Status)
	}
	// Just started (2 min ago) with no files yet: working.
	os.Remove(log + ".jsonl")
	start = now.Add(-2 * time.Minute)
	write()
	if f := d.Read(); f.Status != state.Working {
		t.Fatalf("just started with no files: expected working, got %v", f.Status)
	}
}

func TestAgo(t *testing.T) {
	for d, want := range map[time.Duration]string{
		40 * time.Second: "40 s", 12 * time.Minute: "12 min",
		3*time.Hour + 5*time.Minute: "3 h 5 min", 2 * time.Hour: "2 h", 50 * time.Hour: "2 d",
	} {
		if got := Ago(d); got != want {
			t.Errorf("Ago(%v) = %q, want %q", d, got, want)
		}
	}
}

// localEnd: how the end time shows up in the detail (readable and in local time).
func localEnd(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return "end: " + s
	}
	return "end: " + t.Local().Format("02/01 15:04:05")
}

type fixedReader struct{ f state.Row }

func (r fixedReader) Agent() string   { return r.f.Agent }
func (r fixedReader) Read() state.Row { return r.f }

func TestCurrentReleasesOutOfQuota(t *testing.T) {
	now := time.Date(2026, 9, 29, 7, 0, 0, 0, time.Local)
	f := state.Row{Agent: "codex", Status: state.OutOfQuota, Quota: state.Quota{
		Exact: true, UsedPct: 100, ResetsAt: now.Add(-time.Hour),
		Bars: []state.Bar{{Name: "sem", UsedPct: 100, ResetsAt: now.Add(-time.Hour)}},
	}}
	c := current{fixedReader{f}, func() time.Time { return now }}
	got := c.Read()
	if got.Status != state.Idle || got.Quota.Bars[0].UsedPct != 0 {
		t.Fatalf("with the week already reset it must be available and at 0 %%: %+v", got)
	}
	if ss := Sources([]Reader{Current(NewCodex())}); len(ss) == 0 {
		t.Fatal("Sources must see through Current")
	}
}

// Runs are read from panal's dir and the legacy one together; when the same
// run is in both, the current-format file wins.
func TestDelegate_MergesCurrentAndLegacyDirs(t *testing.T) {
	legacy := filepath.Join("testdata", "legacy_runs")
	current := t.TempDir()
	rc := 0
	if _, err := runs.WriteFile(current, runs.Run{ID: "20260924-081422", Agent: "agy", Model: "gemini-3.8-flash-low",
		Task: "Write d.txt", Start: "2026-09-24T08:14:22-06:00", Status: runs.Done,
		End: "2026-09-24T08:14:40-06:00", RC: &rc}); err != nil {
		t.Fatal(err)
	}
	d := &Delegate{Name: "agy", Dirs: []string{current, legacy}, PIDAlive: func(int) bool { return true }}
	if f := d.Read(); f.Status != state.Done || f.Task != "Write d.txt" {
		t.Fatalf("the current-format copy must win: %v %q", f.Status, f.Task)
	}
	// codex only has a legacy file: it is still read.
	d = &Delegate{Name: "codex", Dirs: []string{current, legacy}, PIDAlive: func(int) bool { return true }}
	if f := d.Read(); f.Status != state.OutOfQuota {
		t.Fatalf("legacy runs must still be read: %v", f.Status)
	}
	// A newer run in the current dir wins over older legacy ones.
	if _, err := runs.WriteFile(current, runs.Run{ID: "20260925-090000", Agent: "codex", Model: "gpt-6-luna",
		Task: "Newer", Start: "2026-09-25T09:00:00-06:00", Status: runs.Done, RC: &rc}); err != nil {
		t.Fatal(err)
	}
	if f := d.Read(); f.Status != state.Done || f.Task != "Newer" {
		t.Fatalf("the newest run must win: %v %q", f.Status, f.Task)
	}
}
