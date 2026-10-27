package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"testing"
	"time"

	"github.com/AlbertoVasquezR/panal/internal/mascots"
	"github.com/AlbertoVasquezR/panal/internal/readers"
	"github.com/AlbertoVasquezR/panal/internal/remote"
	"github.com/AlbertoVasquezR/panal/internal/state"
)

func TestStatusLineOneLine(t *testing.T) {
	fixedTime := time.Date(2026, 9, 26, 15, 4, 5, 0, time.Local)
	oldNow := nowFn
	nowFn = func() time.Time { return fixedTime }
	defer func() { nowFn = oldNow }()

	l := StatusLine(setupTestEnv(t, fixedTime))
	if strings.Contains(l, "\n") {
		t.Fatalf("should be a single line: %q", l)
	}
	for _, want := range []string{"claude ", "codex ", " · "} {
		if !strings.Contains(l, want) {
			t.Errorf("%q is missing from %q", want, l)
		}
	}
}

func TestApplyTheme(t *testing.T) {
	if err := ApplyTheme("nothing"); err == nil {
		t.Fatal("an unknown theme should return an error")
	}
	for _, tm := range []string{"", "auto"} {
		if err := ApplyTheme(tm); err != nil {
			t.Fatalf("%q: %v", tm, err)
		}
	}
}

func TestNoAnimationDoesNotReact(t *testing.T) {
	old := NoAnimation
	NoAnimation = true
	defer func() { NoAnimation = old }()
	m := New(nil, time.Second)
	m.react("codex", mascots.Celebrate)
	if len(m.reactions) != 0 {
		t.Fatal("without animation no reactions should start: with no pulse they would never end")
	}
	if cmd := m.Init(); cmd == nil {
		t.Fatal("Init should still ask for the data refresh")
	}
}

func TestNoColorRemovesMascots(t *testing.T) {
	fixedTime := time.Date(2026, 9, 26, 15, 4, 5, 0, time.Local)
	oldNow := nowFn
	nowFn = func() time.Time { return fixedTime }
	defer func() { nowFn = oldNow }()
	ls := setupTestEnv(t, fixedTime)

	t.Setenv("NO_COLOR", "")
	with := CaptureView(ls, 160, "")
	t.Setenv("NO_COLOR", "1")
	without := CaptureView(ls, 160, "")
	if !strings.Contains(with, "▀") || strings.Contains(without, "▀") {
		t.Fatal("with NO_COLOR the cards should have no mascots (half blocks)")
	}
	if !strings.Contains(without, state.Orchestrating.String()) {
		t.Fatal("without color the status is still shown as a word")
	}
}

func TestWindowTitle(t *testing.T) {
	fs := []state.Row{
		{Agent: "claude", Status: state.Orchestrating},
		{Agent: "agy", Status: state.Working},
		{Agent: "codex", Status: state.Done, Unseen: true},
		{Agent: "opencode", Status: state.OutOfQuota},
	}
	if got := windowTitle(fs); got != "✦ 1 unseen · ● 1 working · ✖ 1 with problems — Panal" {
		t.Fatalf("got %q", got)
	}
	if got := windowTitle(fs[:1]); got != "Panal" {
		t.Fatalf("when calm the title is just \"Panal\": %q", got)
	}
}

func TestDiagnostics(t *testing.T) {
	empty := isolateSources(t)
	ls := readers.All()
	txt := Diagnostics(ls, empty+"/panal.conf", nil, []string{"✔ codex: 10 models (cached 3 h ago, codex debug models)"})
	// Source labels and env names come from internal/readers: take them from there.
	first := readers.Sources(ls)[0]
	for _, want := range []string{"○ config", "○ " + first.What, first.Env + " moves it", "what it read", "claude", state.NoData.String(), "models (panal models", "✔ codex: 10 models"} {
		if !strings.Contains(txt, want) {
			t.Errorf("missing %q:\n%s", want, txt)
		}
	}
}

type fixedRemotes []remote.Reading

func (r fixedRemotes) Readings() []remote.Reading { return r }

func TestRemoteLines(t *testing.T) {
	now := time.Date(2026, 9, 29, 10, 0, 0, 0, time.Local)
	old := Remotes
	defer func() { Remotes = old }()
	Remotes = fixedRemotes{
		{Machine: remote.Machine{Name: "pc2"}, At: now.Add(-5 * time.Second), Status: remote.Status{Agents: []remote.Agent{
			{Agent: "codex", Status: state.Working.String(), Glyph: "●", Since: now.Add(-12 * time.Minute), Activity: "$ go test"},
			{Agent: "agy", Status: state.Done.String(), Glyph: "✔"},
			{Agent: "opencode", Status: state.NoData.String(), Glyph: "○"},
		}}},
		{Machine: remote.Machine{Name: "pc3"}, Error: "no answer yet"},
	}
	txt := stripANSI(remoteLines(now, 100))
	for _, want := range []string{"⇄ pc2", "codex ● 12 min › $ go test", "agy ✔", "5 s ago", "⇄ pc3 · no answer yet"} {
		if !strings.Contains(txt, want) {
			t.Errorf("missing %q:\n%s", want, txt)
		}
	}
	if strings.Contains(txt, "opencode") {
		t.Error("agents without data are not listed")
	}
	for _, l := range strings.Split(strings.TrimRight(remoteLines(now, 50), "\n"), "\n") {
		if w := lipgloss.Width(l); w > 50 {
			t.Errorf("at 50 columns a line is %d wide: %q", w, stripANSI(l))
		}
	}
}
