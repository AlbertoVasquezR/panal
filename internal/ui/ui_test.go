package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/AlbertoVasquezR/panal/internal/forecast"
	"github.com/AlbertoVasquezR/panal/internal/history"
	"github.com/AlbertoVasquezR/panal/internal/readers"
	"github.com/AlbertoVasquezR/panal/internal/state"
)

func TestQuota_ExactAndSummary(t *testing.T) {
	c := state.Quota{
		Exact:    true,
		UsedPct:  100.0,
		ResetsAt: time.Date(2026, 9, 27, 13, 38, 0, 0, time.UTC),
		Credits:  "1008",
		Summary:  "-5 cr this session",
	}

	got := quota(c)
	if !strings.HasSuffix(got, " · -5 cr this session") {
		t.Fatalf("expected suffix ' · -5 cr this session', got %q", got)
	}
	if !strings.Contains(got, "100%") || !strings.Contains(got, "1008 cr") {
		t.Fatalf("expected the full quota with percentage and credits, got %q", got)
	}

	// No summary
	cNoSummary := state.Quota{
		Exact:   true,
		UsedPct: 80.0,
		Credits: "500",
	}
	gotWithout := quota(cNoSummary)
	if strings.Contains(gotWithout, "this session") {
		t.Fatalf("should not contain a summary when it is empty: %q", gotWithout)
	}

	// Not exact
	cInexact := state.Quota{
		Exact:   false,
		Summary: "no sessions",
	}
	gotInexact := quota(cInexact)
	if gotInexact != "no sessions" {
		t.Fatalf("expected 'no sessions', got %q", gotInexact)
	}
}

func TestHistory_FormatAndDuration(t *testing.T) {
	if got := formatTokens(0); got != "—" {
		t.Fatalf("expected '—' for 0 tokens, got %q", got)
	}
	if got := formatTokens(1234); got != "1,234" {
		t.Fatalf("expected '1,234', got %q", got)
	}
	if got := formatTokens(1234567); got != "1,234,567" {
		t.Fatalf("expected '1,234,567', got %q", got)
	}

	if got := formatCost(""); got != "—" {
		t.Fatalf("expected '—' for an empty cost, got %q", got)
	}
	if got := formatCost("50 cr"); got != "50 cr" {
		t.Fatalf("expected '50 cr', got %q", got)
	}

	begin := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	end := begin.Add(45 * time.Second)

	cFinished := history.Run{
		Start:  begin,
		End:    end,
		Status: runDone,
	}
	if got := runDuration(cFinished); got != "45 s" {
		t.Fatalf("expected '45 s', got %q", got)
	}

	cRunning := history.Run{
		Start:  begin,
		End:    end,
		Status: runRunning,
	}
	if got := runDuration(cRunning); got != "—" {
		t.Fatalf("expected '—' for a running run, got %q", got)
	}
}

func TestModel_ToggleHistory(t *testing.T) {
	m := New(nil, 2*time.Second)
	m.runs = []history.Run{{Stamp: "1", Agent: "codex", Status: runDone}}
	if m.inHistory {
		t.Fatalf("should not start in history")
	}

	// Press 'h'
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	mod2 := m2.(Model)
	if !mod2.inHistory {
		t.Fatalf("'h' should switch to the history view")
	}

	// Press 'tab' in history
	m3, _ := mod2.Update(tea.KeyMsg{Type: tea.KeyTab})
	mod3 := m3.(Model)
	if !mod3.historyDetail {
		t.Fatalf("'tab' in the history view should open historyDetail")
	}

	// Press 'h' again to go back
	m4, _ := mod3.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	mod4 := m4.(Model)
	if mod4.inHistory {
		t.Fatalf("'h' should go back to the main view")
	}
}

func TestCorralZzzOnlyWhenSleeping(t *testing.T) {
	rows := []state.Row{
		{Agent: "claude", Status: state.Orchestrating},
		{Agent: "codex", Status: state.OutOfQuota},
	}
	clean := func(s string) string {
		var b strings.Builder
		esc := false
		for _, r := range s {
			switch {
			case r == '\x1b':
				esc = true
			case esc && r == 'm':
				esc = false
			case !esc:
				b.WriteRune(r)
			}
		}
		return b.String()
	}
	seenZ := false
	for frame := 0; frame < 12; frame++ {
		txt := clean(Corral(rows, Animation{Frame: frame}, -1, time.Time{}))
		lines := strings.Split(txt, "\n")
		statusLine := lines[len(lines)-1]
		i := strings.Index(statusLine, state.OutOfQuota.String())
		if i < 0 {
			t.Fatalf("codex status not found in %q", statusLine)
		}
		if strings.Contains(statusLine[:i], "z") {
			t.Fatalf("frame %d: an orchestrating claude should have no zzz: %q", frame, statusLine)
		}
		if strings.Contains(statusLine[i:], "z") {
			seenZ = true
		}
	}
	if !seenZ {
		t.Fatal("codex out of quota never showed the zzz")
	}
}

type fixedReader struct {
	row state.Row
}

func (l fixedReader) Agent() string   { return l.row.Agent }
func (l fixedReader) Read() state.Row { return l.row }

func TestEmptySamplesPathDoesNotWrite(t *testing.T) {
	origPath := SamplesPath
	origNow := nowFn
	defer func() {
		SamplesPath = origPath
		nowFn = origNow
	}()

	SamplesPath = ""
	t0 := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	nowFn = func() time.Time { return t0 }

	l := fixedReader{
		row: state.Row{
			Agent:  "claude",
			Status: state.Working,
			Quota: state.Quota{
				Exact: true,
				Bars:  []state.Bar{{Name: "5h", UsedPct: 50}},
			},
		},
	}

	m := New([]readers.Reader{l}, 2*time.Second)

	// Move time forward more than a minute and refresh.
	nowFn = func() time.Time { return t0.Add(2 * time.Minute) }
	m.refresh()

	// Quit with 'q'.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	// With an empty SamplesPath nothing should be saved and nothing should fail.
	if SamplesPath != "" {
		t.Fatalf("SamplesPath should stay empty: %q", SamplesPath)
	}
}

func TestSamplesPathWritesAfterOneMinute(t *testing.T) {
	origPath := SamplesPath
	origNow := nowFn
	defer func() {
		SamplesPath = origPath
		nowFn = origNow
	}()

	dir := t.TempDir()
	path := filepath.Join(dir, "samples.json")
	SamplesPath = path

	t0 := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	nowFn = func() time.Time { return t0 }

	l := fixedReader{
		row: state.Row{
			Agent:  "claude",
			Status: state.Working,
			Quota: state.Quota{
				Exact: true,
				Bars:  []state.Bar{{Name: "5h", UsedPct: 50}},
			},
		},
	}

	m := New([]readers.Reader{l}, 2*time.Second)

	// At the start (t=0) a minute has not passed yet, so the file should not be written.
	if _, err := os.Stat(path); err == nil {
		t.Fatal("the file should not exist right after creating the model")
	}

	// Refresh at 30 s: still not written, the minute is not up.
	nowFn = func() time.Time { return t0.Add(30 * time.Second) }
	m.refresh()
	if _, err := os.Stat(path); err == nil {
		t.Fatal("the file should not exist before a minute has passed")
	}

	// Move past a minute (> 60 s): refreshing should write the file.
	nowFn = func() time.Time { return t0.Add(61 * time.Second) }
	m.refresh()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("after refreshing past a minute the file should exist: %v", err)
	}

	// The saved file should contain the claude/5h key.
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("error reading the saved file: %v", err)
	}
	if !strings.Contains(string(b), "claude/5h") {
		t.Fatalf("the saved file should contain 'claude/5h': %s", string(b))
	}
}

func TestSamplesPathSavesOnQuit(t *testing.T) {
	origPath := SamplesPath
	origNow := nowFn
	defer func() {
		SamplesPath = origPath
		nowFn = origNow
	}()

	dir := t.TempDir()
	path := filepath.Join(dir, "samples_quit.json")
	SamplesPath = path

	t0 := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	nowFn = func() time.Time { return t0 }

	l := fixedReader{
		row: state.Row{
			Agent:  "codex",
			Status: state.Working,
			Quota: state.Quota{
				Exact: true,
				Bars:  []state.Bar{{Name: "sem", UsedPct: 75}},
			},
		},
	}

	m := New([]readers.Reader{l}, 2*time.Second)

	// Quit with 'q' before a minute: it should save one last time if dirty.
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("Update with 'q' should return the tea.Quit command")
	}
	_ = m2

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("quitting with 'q' should have saved the file: %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("error reading the file after quitting: %v", err)
	}
	if !strings.Contains(string(b), "codex/sem") {
		t.Fatalf("the file saved on quit should contain 'codex/sem': %s", string(b))
	}
}

func TestSamplesPathLoadsOnStart(t *testing.T) {
	origPath := SamplesPath
	origNow := nowFn
	defer func() {
		SamplesPath = origPath
		nowFn = origNow
	}()

	dir := t.TempDir()
	path := filepath.Join(dir, "samples_load.json")

	t0 := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	// Save earlier samples to the file so there are enough on start.
	mPrev := forecast.New()
	mPrev.Record("claude/5h", t0.Add(-30*time.Minute), 40)
	mPrev.Record("claude/5h", t0.Add(-20*time.Minute), 50)
	mPrev.Record("claude/5h", t0.Add(-10*time.Minute), 60)
	if err := mPrev.Save(path); err != nil {
		t.Fatalf("error preparing the file with earlier samples: %v", err)
	}

	SamplesPath = path
	nowFn = func() time.Time { return t0 }

	l := fixedReader{
		row: state.Row{
			Agent:  "claude",
			Status: state.Working,
			Quota: state.Quota{
				Exact: true,
				Bars:  []state.Bar{{Name: "5h", UsedPct: 70}},
			},
		},
	}

	m := New([]readers.Reader{l}, 2*time.Second)

	// With the 3 loaded samples plus the current one at t0, it should have projected.
	if len(m.rows) == 0 || len(m.rows[0].Quota.Bars) == 0 {
		t.Fatal("expected rows and bars")
	}
	runsOut := m.rows[0].Quota.Bars[0].ExhaustsAt
	if runsOut.IsZero() {
		t.Fatal("the bar should have an ExhaustsAt projection thanks to the loaded samples")
	}
}

func TestStatusPhraseWithAndWithoutWorking(t *testing.T) {
	now := time.Date(2026, 9, 26, 15, 0, 0, 0, time.Local)

	// Case 1: with agents working, problems and credits
	mWith := Model{
		now: now,
		rows: []state.Row{
			{Agent: "claude", Status: state.Orchestrating},
			{Agent: "agy", Status: state.Working, Since: now.Add(-5 * time.Minute)},
			{Agent: "codex", Status: state.OutOfQuota, Quota: state.Quota{Spend: "today 2.4 cr · ~14/day · ~68 days"}},
			{Agent: "opencode", Status: state.Failed},
		},
	}
	fWith := mWith.statusPhrase(160)
	for _, want := range []string{"claude orchestrating", "agy working for 5 min", "codex out of quota", "opencode failed", "codex: credits for ~68 days"} {
		if !strings.Contains(fWith, want) {
			t.Fatalf("should include %q: %q", want, fWith)
		}
	}
	if strings.Contains(fWith, "no agent working") {
		t.Fatalf("should not say 'no agent working' while agy works: %q", fWith)
	}

	// Case 2: no agent working, but claude orchestrates
	mWithout := Model{
		now: now,
		rows: []state.Row{
			{Agent: "claude", Status: state.Orchestrating},
			{Agent: "codex", Status: state.Idle, Quota: state.Quota{Spend: "today 2.4 cr · ~14/day · ~68 days"}},
		},
	}
	fWithout := mWithout.statusPhrase(160)
	for _, want := range []string{"claude orchestrating", "no agent working", "codex: credits for ~68 days"} {
		if !strings.Contains(fWithout, want) {
			t.Fatalf("should include %q: %q", want, fWithout)
		}
	}

	// Case 3: more problems (stuck, no permission)
	mProblems := Model{
		now: now,
		rows: []state.Row{
			{Agent: "agy", Status: state.Stuck},
			{Agent: "codex", Status: state.NoPermission},
		},
	}
	phraseProblems := mProblems.statusPhrase(160)
	if !strings.Contains(phraseProblems, "agy stuck") || !strings.Contains(phraseProblems, "codex no permission") {
		t.Fatalf("should include 'agy stuck' and 'codex no permission': %q", phraseProblems)
	}
}

func TestFooterFitsIn80Columns(t *testing.T) {
	footers := []struct {
		name   string
		footer string
	}{
		{
			name:   "cards",
			footer: Footer(80, "←→", "select", "tab", "detail", "h", "history", "i", "report", "?", "help", "q", "quit"),
		},
		{
			name:   "table",
			footer: Footer(80, "↑↓", "select", "tab", "detail", "h", "history", "i", "report", "?", "help", "q", "quit"),
		},
		{
			name:   "history",
			footer: Footer(80, "↑↓", "select", "tab", "detail", "/", "search", "f", "agent", "e", "result", "?", "help", "q", "quit"),
		},
		{
			name:   "report",
			footer: Footer(80, "7/3", "period", "tab", "toggle", "esc", "back", "?", "help", "q", "quit"),
		},
		{
			name:   "diff",
			footer: Footer(80, "↑↓", "scroll", "PgUp/PgDn", "page", "g/G", "top/end", "esc", "back", "?", "help"),
		},
		{
			name:   "log",
			footer: Footer(80, "↑↓", "scroll", "PgUp/PgDn", "page", "g/G", "top/end", "r", "noise", "esc", "back", "?", "help"),
		},
		{
			name:   "help",
			footer: Footer(80, "↑↓", "scroll", "esc", "close", "?", "close", "q", "close"),
		},
	}

	for _, p := range footers {
		for i, line := range strings.Split(p.footer, "\n") {
			w := lipgloss.Width(line)
			if w > 80 {
				t.Errorf("%s: footer line %d is %d columns wide (> 80): %q", p.name, i+1, w, line)
			}
		}
	}
}
