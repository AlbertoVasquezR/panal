package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"

	"github.com/AlbertoVasquezR/panal/internal/history"
	"github.com/AlbertoVasquezR/panal/internal/readers"
	"github.com/AlbertoVasquezR/panal/internal/state"
)

func modelWithRuns(n int, agents ...string) Model {
	m := Model{width: 120, height: 30, now: time.Now(), inHistory: true}
	base := time.Now()
	for i := 0; i < n; i++ {
		m.runs = append(m.runs, history.Run{
			Agent: agents[i%len(agents)], Status: runDone,
			Start: base.Add(-time.Duration(i) * time.Minute),
		})
	}
	return m
}

// The cursor stays inside the list and the visible window follows it: if it
// got lost, the highlighted row would end up off screen.
func TestHistoryCursorAndScrolling(t *testing.T) {
	m := modelWithRuns(50, "agy")
	page := m.historyVisibleRows()
	for i := 0; i < 60; i++ {
		m.historyKey("down")
	}
	if m.historyCursor != 49 {
		t.Fatalf("after 60 downs over 50 runs, cursor = %d, want 49", m.historyCursor)
	}
	if m.historyCursor < m.historyOffset || m.historyCursor >= m.historyOffset+page {
		t.Fatalf("cursor %d ended up outside the window [%d, %d)", m.historyCursor, m.historyOffset, m.historyOffset+page)
	}
	m.historyKey("home")
	if m.historyCursor != 0 || m.historyOffset != 0 {
		t.Fatalf("home: cursor %d, offset %d; want 0, 0", m.historyCursor, m.historyOffset)
	}
	m.historyKey("up")
	if m.historyCursor != 0 {
		t.Fatalf("going up from the first one should not go negative: %d", m.historyCursor)
	}
	m.historyKey("pgdown")
	if m.historyCursor != page {
		t.Fatalf("pgdown: cursor %d, want %d", m.historyCursor, page)
	}
}

func TestHistoryFilterCyclesAndFilters(t *testing.T) {
	m := modelWithRuns(12, "claude", "agy", "codex", "opencode", "cursor")
	var seen []string
	for i := 0; i < 6; i++ {
		m.historyKey("f")
		seen = append(seen, m.historyFilter)
		for _, c := range m.filteredRuns() {
			if m.historyFilter != "" && c.Agent != m.historyFilter {
				t.Fatalf("filter %q let through a run from %q", m.historyFilter, c.Agent)
			}
		}
	}
	if got := strings.Join(seen, ","); got != "claude,agy,codex,opencode,cursor," {
		t.Fatalf("the filter does not cycle in order: %q", got)
	}
	if n := len(m.filteredRuns()); n != 12 {
		t.Fatalf("with no filter all 12 runs should show, %d do", n)
	}
}

func TestHistoryRendersWithAndWithoutRuns(t *testing.T) {
	// No runs at all
	vEmpty := modelWithRuns(0, "agy").historyView()
	if !strings.Contains(vEmpty, "No runs yet. Delegated runs show up here as soon as they start.") {
		t.Fatalf("an empty history should explain there are no runs: %s", vEmpty)
	}

	// With runs, but filtered down to 0
	mFilt := modelWithRuns(3, "agy")
	mFilt.historyFilter = "codex"
	vFilt := mFilt.historyView()
	if !strings.Contains(vFilt, "no runs match the filters · esc clears them") {
		t.Fatalf("filters with no match should say 'no runs match the filters · esc clears them': %s", vFilt)
	}

	m := modelWithRuns(3, "codex")
	m.historyDetail = true
	if v := m.historyView(); !strings.Contains(v, "CODEX") {
		t.Fatal("with the detail open it should show the run card")
	}
}

func TestSummaryPanelsBarsWithoutRuns(t *testing.T) {
	m := Model{
		width:  100,
		height: 40,
		now:    time.Date(2026, 9, 26, 15, 0, 0, 0, time.Local),
	}
	// No runs today: every agent with 0 runs
	right := m.summaryPanels(100)
	if !strings.Contains(right, "no runs") {
		t.Fatalf("with 0 runs it should say 'no runs':\n%s", right)
	}
	if strings.Contains(right, "█") {
		t.Fatalf("with 0 runs the bars should have no █:\n%s", right)
	}
	if !strings.Contains(right, "░") {
		t.Fatalf("with 0 runs the bar should be all ░:\n%s", right)
	}

	// With one run for agy today
	m.runs = []history.Run{
		{
			Agent:  "agy",
			Status: runDone,
			Start:  m.now.Add(-10 * time.Minute),
			End:    m.now.Add(-5 * time.Minute),
		},
	}
	rightWith := m.summaryPanels(100)
	if !strings.Contains(rightWith, "█") {
		t.Fatalf("with runs agy's bar should have █:\n%s", rightWith)
	}
	if !strings.Contains(rightWith, "100% ok") {
		t.Fatalf("should show '100%% ok' for agy:\n%s", rightWith)
	}
	// The other agents still say no runs
	if !strings.Contains(rightWith, "no runs") {
		t.Fatalf("agents without runs should still say 'no runs':\n%s", rightWith)
	}
}

func TestAgentDetailSplitsKeyValue(t *testing.T) {
	f := state.Row{Agent: "codex", Status: state.OutOfQuota,
		Detail: "plan: prolite · window: 7 d\n⚠ weekly quota used up: every use costs credits"}
	v := Model{}.agentDetail(f, 100)
	for _, want := range []string{"plan", "prolite", "window", "7 d", "weekly quota used up"} {
		if !strings.Contains(v, want) {
			t.Fatalf("the detail is missing %q:\n%s", want, v)
		}
	}
}

func TestShortDate(t *testing.T) {
	d := time.Date(2026, 9, 24, 10, 0, 0, 0, time.Local)
	if got := shortDate(d); got != "Thu 24 Sep" {
		t.Fatalf("shortDate = %q, want 'Thu 24 Sep'", got)
	}
}

// No screen may exceed the terminal's width or height: if it does, lines wrap
// or the terminal scrolls and the UI breaks.
func TestViewsFitInTerminal(t *testing.T) {
	now := time.Now()
	length := "You work in this dir (Flask repo my-api). Do not commit or push. " + strings.Repeat("Do something long. ", 20)
	var rows []state.Row
	for _, a := range []string{"claude", "agy", "codex", "opencode"} {
		rows = append(rows, state.Row{
			Agent: a, Status: state.Working, Model: "a-model-with-a-rather-long-name (high)", Task: length, Since: now,
			Dir:    `C:\Users\someone\AppData\Local\Temp\claude\a-session-with-a-very-long-name\scratchpad`,
			Detail: "cost: $1 · context: 3%\nversion: 1\nsession_id: x\na note\nanother note\nand another",
			Quota: state.Quota{Bars: []state.Bar{{Name: "5h", UsedPct: 10}, {Name: "sem", UsedPct: 40}},
				Credits: "997"},
		})
	}
	var runs []history.Run
	for i := 0; i < 40; i++ {
		runs = append(runs, history.Run{
			Agent: []string{"claude", "agy", "codex", "opencode"}[i%4], Status: runDone,
			Model: "gemini-3.8-flash-medium", Task: length, Tokens: 123456, Cost: "12 cr",
			Dir:   `C:\Users\someone\AppData\Local\Temp\claude\a-session-with-a-very-long-name\scratchpad`,
			Log:   `C:\Users\someone\.panal\logs\20260925-135308-opencode-a-long-log.txt`,
			Start: now.Add(-time.Duration(i) * 3 * time.Hour), End: now.Add(-time.Duration(i)*3*time.Hour + time.Minute),
		})
	}
	logFile := filepath.Join(t.TempDir(), "log.txt")
	os.WriteFile(logFile, []byte(strings.Repeat(length+"\n\tindented\n", 100)), 0o644)
	views := []struct {
		name string
		fit  func(*Model)
	}{
		{"cards", func(m *Model) {}},
		{"detail", func(m *Model) { m.detail = true }},
		{"table", func(m *Model) { m.compact = true }},
		{"table-detail", func(m *Model) { m.compact, m.detail = true, true }},
		{"history", func(m *Model) { m.inHistory = true }},
		{"history-detail", func(m *Model) { m.inHistory, m.historyDetail = true, true }},
		{"history-search", func(m *Model) { m.inHistory, m.searching, m.query = true, true, "do something" }},
		{"with-alert", func(m *Model) { m.alert, m.alertTime, m.detail = "⚠ agy done — "+length, now, true }},
		{"log", func(m *Model) { m.reg = newViewer(history.Run{Agent: "agy", Log: logFile}) }},
	}
	base := New(nil, time.Second)
	base.hist = nil
	for _, v := range views {
		for _, width := range []int{50, 60, 80, 100, 132, 200} {
			for _, height := range []int{12, 24, 30, 40, 60} {
				m := base
				m.rows, m.runs, m.now, m.width, m.height = rows, runs, now, width, height
				m.table.SetRows(make([]table.Row, len(rows)))
				m.table.SetCursor(2)
				v.fit(&m)
				// History cursor near the bottom: the selected run must be visible.
				if m.inHistory {
					m.historyCursor = 24
					m.historyKey("down")
				}
				screen := m.View()
				if h := lipgloss.Height(screen); h > height {
					t.Errorf("%s %dx%d: %d lines tall", v.name, width, height, h)
				}
				for _, l := range strings.Split(screen, "\n") {
					if w := lipgloss.Width(l); w > width {
						t.Errorf("%s %dx%d: a line is %d columns wide: %q", v.name, width, height, w, l)
						break
					}
				}
				if m.inHistory && !m.historyDetail && height >= 24 {
					cs := m.filteredRuns()
					if len(cs) > 0 && m.historyCursor < len(cs) {
						clock := cs[m.historyCursor].Start.Local().Format("15:04")
						if !strings.Contains(screen, clock) {
							t.Errorf("%s %dx%d: the selected run is not visible", v.name, width, height)
						}
					}
				}
			}
		}
	}
}

// A disabled agent at rest gets no card, just a line; if it starts working,
// it gets its card back.
func TestDisabledOnOneLine(t *testing.T) {
	Disabled = map[string]bool{"opencode": true}
	defer func() { Disabled = nil }()
	rd := func(e state.Status) Model {
		m := New([]readers.Reader{fixed{state.Row{Agent: "codex", Status: state.Idle}}, fixed{state.Row{Agent: "opencode", Status: e}}}, time.Second)
		m.width, m.height = 120, 40
		return m
	}
	m := rd(state.OutOfQuota)
	if len(m.rows) != 1 || len(m.offRows) != 1 {
		t.Fatalf("at rest: %d cards, %d off; want 1 and 1", len(m.rows), len(m.offRows))
	}
	if !strings.Contains(m.View(), "off: opencode") {
		t.Fatal("the off line is missing")
	}
	if m := rd(state.Working); len(m.rows) != 2 {
		t.Fatalf("a working agent should have a card: %d cards", len(m.rows))
	}
}

type fixed struct{ f state.Row }

func (l fixed) Agent() string   { return l.f.Agent }
func (l fixed) Read() state.Row { return l.f }

func TestRunDetail_FullTask(t *testing.T) {
	m := Model{now: time.Now()}

	// Case 1: empty FullTask -> no "full task" section
	c1 := history.Run{
		Agent:    "codex",
		Start:    time.Now(),
		Task:     "first line only",
		FullTask: "",
	}
	v1 := m.runDetail(c1, 100)
	if strings.Contains(v1, "full task") {
		t.Errorf("with an empty FullTask there should be no 'full task' section:\n%s", v1)
	}
	if !strings.Contains(v1, "first line only") {
		t.Errorf("the 'task' pair should be there:\n%s", v1)
	}

	// Case 2: single-line FullTask -> no section either
	c2 := history.Run{
		Agent:    "codex",
		Start:    time.Now(),
		Task:     "a single line",
		FullTask: "a single line",
	}
	v2 := m.runDetail(c2, 100)
	if strings.Contains(v2, "full task") {
		t.Errorf("with 1 line there should be no 'full task' section:\n%s", v2)
	}

	// Case 3: FullTask with 3 lines -> adds a section with all of them
	c3 := history.Run{
		Agent:    "codex",
		Start:    time.Now(),
		Task:     "first line",
		FullTask: "first line\nsecond line\nthird line",
	}
	v3 := m.runDetail(c3, 100)
	if !strings.Contains(v3, "full task") {
		t.Fatalf("should include a 'full task' section:\n%s", v3)
	}
	for _, l := range []string{"first line", "second line", "third line"} {
		if !strings.Contains(v3, l) {
			t.Errorf("line %q is missing from the detail:\n%s", l, v3)
		}
	}
	if strings.Contains(v3, "more lines") {
		t.Errorf("with 3 lines there should be no 'more lines'")
	}

	// Case 4: FullTask with 15 lines -> shows 12 and '… 3 more lines'
	var lines []string
	for i := 1; i <= 15; i++ {
		lines = append(lines, fmt.Sprintf("line %02d", i))
	}
	c4 := history.Run{
		Agent:    "agy",
		Start:    time.Now(),
		Task:     lines[0],
		FullTask: strings.Join(lines, "\n"),
	}
	v4 := m.runDetail(c4, 100)
	if !strings.Contains(v4, "full task") {
		t.Fatalf("should include a 'full task' section:\n%s", v4)
	}
	// Lines 1 to 12 should be visible
	for i := 1; i <= 12; i++ {
		r := fmt.Sprintf("line %02d", i)
		if !strings.Contains(v4, r) {
			t.Errorf("%q should be visible:\n%s", r, v4)
		}
	}
	// Line 13 should not show as such
	if strings.Contains(v4, "line 13") {
		t.Errorf("line 13 should not be visible:\n%s", v4)
	}
	if !strings.Contains(v4, "… 3 more lines") {
		t.Errorf("expected '… 3 more lines':\n%s", v4)
	}
}

func TestRunChanges_FullTask(t *testing.T) {
	// With FullTask: uses FullTask and Complete=true
	c1 := history.Run{
		Stamp:    "s1",
		Agent:    "agy",
		Task:     "first line",
		FullTask: "first line\nsecond line",
	}
	cc1 := RunChanges(c1)
	if cc1.Task != c1.FullTask || !cc1.Complete {
		t.Errorf("with FullTask: Task=%q Complete=%v", cc1.Task, cc1.Complete)
	}

	// Without FullTask: uses Task and Complete=false
	c2 := history.Run{
		Stamp: "s2",
		Agent: "agy",
		Task:  "first line",
	}
	cc2 := RunChanges(c2)
	if cc2.Task != c2.Task || cc2.Complete {
		t.Errorf("without FullTask: Task=%q Complete=%v", cc2.Task, cc2.Complete)
	}
}

// Lines copied from the log ("- `go vet`: passed") are not label: value
// pairs: they go in their own section.
func TestAgentDetailLogSeparate(t *testing.T) {
	f := state.Row{Agent: "agy", Status: state.Done,
		Detail: "end: 26/09 12:11:05 · rc: 0\n" + readers.LogPrefix + "- `go vet ./...`: Passed with no warnings."}
	v := reANSI.ReplaceAllString(Model{}.agentDetail(f, 100), "")
	if !strings.Contains(v, "last lines of the log") {
		t.Fatalf("the log section is missing:\n%s", v)
	}
	for _, l := range strings.Split(v, "\n") {
		if strings.Contains(l, "`go vet ./...`") && !strings.Contains(l, "│ │") {
			t.Fatalf("the log line was drawn as a pair: %q", l)
		}
	}
}
