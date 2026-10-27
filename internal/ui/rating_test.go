package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/AlbertoVasquezR/panal/internal/feedback"
)

func key(m Model, k string) (Model, tea.Cmd) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	switch k {
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	}
	mod, cmd := m.Update(msg)
	return mod.(Model), cmd
}

func TestRateRunsFromHistory(t *testing.T) {
	fixedTime := time.Date(2026, 9, 26, 15, 4, 5, 0, time.Local)
	oldNow := nowFn
	nowFn = func() time.Time { return fixedTime }
	defer func() { nowFn = oldNow }()
	m := New(setupTestEnv(t, fixedTime), 2*time.Second)
	mod, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = mod.(Model)
	m, _ = key(m, "h")

	// The first run is still going: it can't be rated yet.
	m, cmd := key(m, "+")
	if cmd != nil || !strings.Contains(m.alert, "still going") {
		t.Fatalf("running run rated: %q", m.alert)
	}

	// Newest first: running agy, claude, cursor, then the codex run (rated bad).
	m, _ = key(m, "down")
	m, _ = key(m, "down")
	m, _ = key(m, "down")
	m, _ = key(m, "enter")
	plain := reANSI.ReplaceAllString(m.View(), "")
	if !strings.Contains(plain, "rating   ▼ bad · touched violation.go") {
		t.Errorf("run detail without its rating:\n%s", plain)
	}

	// + rates it good (from the detail), and the file says so.
	m, cmd = key(m, "+")
	if cmd == nil {
		t.Fatal("no command to save the rating")
	}
	if msg := cmd().(ratedMsg); msg.err != nil || msg.rating != feedback.Good {
		t.Fatalf("saved: %+v", msg)
	}
	if e := feedback.Cached(feedback.Path()).Get("20260926-111500-codex", "codex"); e.Rating != feedback.Good {
		t.Errorf("file: %+v", e)
	}
	if !strings.Contains(m.alert, "▲ rated good: codex") {
		t.Errorf("alert: %q", m.alert)
	}
	m.refresh()
	if c := m.filteredRuns()[m.historyCursor]; c.Rating != feedback.Good || c.RatingNote != "" {
		t.Errorf("after the refresh: %+v", c)
	}

	// The same key again clears it.
	m, cmd = key(m, "+")
	cmd()
	if e := feedback.Cached(feedback.Path()).Get("20260926-111500-codex", "codex"); e.Rating != "" {
		t.Errorf("not cleared: %+v", e)
	}
	if !strings.Contains(m.alert, "rating cleared") {
		t.Errorf("alert: %q", m.alert)
	}
}
