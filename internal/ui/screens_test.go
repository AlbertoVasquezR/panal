package ui

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/AlbertoVasquezR/panal/internal/feedback"
	"github.com/AlbertoVasquezR/panal/internal/readers"
	"github.com/AlbertoVasquezR/panal/internal/runs"
	"github.com/AlbertoVasquezR/panal/internal/state"
)

var update = flag.Bool("update", false, "update the golden files")

type screenReader struct {
	row state.Row
}

func (l screenReader) Agent() string   { return l.row.Agent }
func (l screenReader) Read() state.Row { return l.row }

func setupTestEnv(t *testing.T, fixedTime time.Time) []readers.Reader {
	tmp := t.TempDir()
	statesDir := filepath.Join(tmp, "states")
	if err := os.MkdirAll(statesDir, 0o755); err != nil {
		t.Fatalf("could not create statesDir: %v", err)
	}
	emptyDir := filepath.Join(tmp, "empty")
	if err := os.MkdirAll(emptyDir, 0o755); err != nil {
		t.Fatalf("could not create emptyDir: %v", err)
	}
	runsDir := filepath.Join(os.TempDir(), "panal-screens-runs")
	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		t.Fatalf("could not create runsDir: %v", err)
	}
	for _, sub := range []string{"claude", "agy", "codex", "opencode", "cursor"} {
		if err := os.MkdirAll(filepath.Join(runsDir, sub), 0o755); err != nil {
			t.Fatalf("could not create subdir %s: %v", sub, err)
		}
	}

	_ = exec.Command("git", "init", runsDir).Run()
	_ = exec.Command("git", "-C", runsDir, "config", "user.name", "test").Run()
	_ = exec.Command("git", "-C", runsDir, "config", "user.email", "test@test.com").Run()
	_ = os.WriteFile(filepath.Join(runsDir, "README.md"), []byte("# base"), 0o644)
	_ = exec.Command("git", "-C", runsDir, "add", "README.md").Run()
	_ = exec.Command("git", "-C", runsDir, "commit", "-m", "init").Run()

	forbidden := filepath.Join(runsDir, "violation.go")
	_ = os.WriteFile(forbidden, []byte("package violation"), 0o644)
	tCodexMod := fixedTime.Add(-225 * time.Minute)
	_ = os.Chtimes(forbidden, tCodexMod, tCodexMod)

	isolateSources(t)
	t.Setenv("PANAL_RUNS", statesDir)
	t.Setenv("PANAL_LOGS", emptyDir)
	t.Setenv("CODEX_SESSIONS", emptyDir)
	t.Setenv("CODEX_HOME", emptyDir)
	t.Setenv("PANAL_CONF", emptyDir)
	t.Setenv("PANAL_CLAUDE", emptyDir)
	t.Setenv("OPENCODE_LOG", emptyDir)
	t.Setenv("OPENCODE_DB", emptyDir)

	taskFile := filepath.Join(tmp, "task.txt")
	taskText := `You work in C:\Codigo\wt\tui-screens, the panal terminal dashboard (Go, Bubble Tea, read-only).
Do not commit or push. You can only modify internal/ui/screens_test.go.
Goal: golden screen tests to catch layout breakage.
Check widths 80, 100, 132 and 160 at 40 lines.`
	if err := os.WriteFile(taskFile, []byte(taskText), 0o644); err != nil {
		t.Fatalf("could not write task.txt: %v", err)
	}

	rcZero := 0
	rcOne := 1
	rcTwo := 2

	// Run files as panal delegate writes them.
	runsJSON := []struct {
		name string
		data any
	}{
		{
			name: "20260926-144500-agy.json",
			data: map[string]any{
				"version":   2,
				"id":        "20260926-144500-agy",
				"agent":     "agy",
				"model":     "gemini-2.5-pro",
				"task":      `You work in C:\Codigo\wt\tui-screens, the panal terminal dashboard (Go, Bubble Tea, read-only).`,
				"task_file": taskFile,
				"dir":       runsDir,
				"start":     fixedTime.Add(-19 * time.Minute).Format(time.RFC3339),
				"status":    runRunning,
				"task_type": "tests",
				"tier":      "medium",
				"auto":      true,
				"choice":    1,
				"route":     "auto: agy:gemini-2.5-pro — tests · medium · 7/9 ok in the last 30 days (exploring 1 in 8) · out of quota now: codex",
			},
		},
		{
			name: "20260926-133000-claude.json",
			data: map[string]any{
				"version":   2,
				"id":        "20260926-133000-claude",
				"agent":     "claude",
				"model":     "claude-3-7-sonnet",
				"task":      "Coordinate syncing the agents' status",
				"task_file": taskFile,
				"dir":       runsDir,
				"start":     fixedTime.Add(-94 * time.Minute).Format(time.RFC3339),
				"end":       fixedTime.Add(-70 * time.Minute).Format(time.RFC3339),
				"status":    runDone,
				"rc":        &rcZero,
			},
		},
		{
			name: "20260926-111500-codex.json",
			data: map[string]any{
				"version":   2,
				"id":        "20260926-111500-codex",
				"agent":     "codex",
				"model":     "o3-mini",
				"task":      "Review slow database queries",
				"task_file": taskFile,
				"dir":       runsDir,
				"start":     fixedTime.Add(-229 * time.Minute).Format(time.RFC3339),
				"end":       fixedTime.Add(-220 * time.Minute).Format(time.RFC3339),
				"status":    runFailed,
				"rc":        &rcOne,
			},
		},
		{
			name: "20260925-182000-opencode.json",
			data: map[string]any{
				"version":   2,
				"id":        "20260925-182000-opencode",
				"agent":     "opencode",
				"model":     "claude-3-5-sonnet",
				"task":      "Generate OpenAPI specs for internal routes",
				"task_file": taskFile,
				"dir":       runsDir,
				"start":     fixedTime.Add(-21 * time.Hour).Format(time.RFC3339),
				"end":       fixedTime.Add(-20*time.Hour - 58*time.Minute).Format(time.RFC3339),
				"status":    runOutOfQuota,
				"rc":        &rcTwo,
			},
		},
		{
			name: "20260926-120000-cursor.json",
			data: map[string]any{
				"version":   2,
				"id":        "20260926-120000-cursor",
				"agent":     "cursor",
				"model":     "composer-2.5",
				"task":      "Add a golden screen for the cursor card",
				"task_file": taskFile,
				"dir":       runsDir,
				"start":     fixedTime.Add(-3 * time.Hour).Format(time.RFC3339),
				"end":       fixedTime.Add(-2*time.Hour - 50*time.Minute).Format(time.RFC3339),
				"status":    runDone,
				"rc":        &rcZero,
				"task_type": "tests",
				"tier":      "medium",
			},
		},
		{
			name: "20260925-100000-agy.json",
			data: map[string]any{
				"version":   2,
				"id":        "20260925-100000-agy",
				"agent":     "agy",
				"model":     "gemini-2.5-flash",
				"task":      "Refactor the color palette in the stylesheets",
				"task_file": taskFile,
				"dir":       runsDir,
				"start":     fixedTime.Add(-29 * time.Hour).Format(time.RFC3339),
				"end":       fixedTime.Add(-28*time.Hour - 40*time.Minute).Format(time.RFC3339),
				"status":    runDone,
				"rc":        &rcZero,
			},
		},
	}

	// Ratings (panal feedback): the failed codex run was rated bad, the agy
	// run from yesterday good.
	ratings := `{"version": 1, "ratings": {
  "20260926-111500-codex-codex": {"rating": "bad", "note": "touched violation.go", "at": "2026-09-26T12:00:00Z"},
  "20260925-100000-agy-agy": {"rating": "good", "at": "2026-09-25T12:00:00Z"}}}`
	if err := os.MkdirAll(runs.Home(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(feedback.Path(), []byte(ratings), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, cj := range runsJSON {
		b, err := json.MarshalIndent(cj.data, "", "  ")
		if err != nil {
			t.Fatalf("could not marshal %s: %v", cj.name, err)
		}
		if err := os.WriteFile(filepath.Join(statesDir, cj.name), b, 0o644); err != nil {
			t.Fatalf("could not write %s: %v", cj.name, err)
		}
	}

	return []readers.Reader{
		screenReader{
			row: state.Row{
				Agent:  "claude",
				Status: state.Orchestrating,
				Model:  "claude-3-7-sonnet",
				Task:   "Orchestrating agents and handing out dashboard tasks",
				Since:  fixedTime.Add(-40 * time.Minute),
				Dir:    filepath.Join(runsDir, "claude"),
				Detail: "cost: $0.85 · context: 18%\nsession: sess-01-claude\ntasks: 14 completed",
				Quota:  state.Quota{Summary: "active"},
			},
		},
		screenReader{
			row: state.Row{
				Agent:    "agy",
				Status:   state.Working,
				Model:    "gemini-2.5-pro",
				Task:     `You work in C:\Codigo\wt\tui-screens, the panal terminal dashboard (Go, Bubble Tea, read-only). Do not commit. Goal: golden screen tests to catch layout breakage.`,
				FullTask: taskText,
				Since:    fixedTime.Add(-18 * time.Minute),
				Dir:      filepath.Join(runsDir, "agy"),
				Quota: state.Quota{
					Bars: []state.Bar{
						{
							Name:     "5h",
							UsedPct:  62.0,
							ResetsAt: fixedTime.Add(1*time.Hour + 35*time.Minute),
						},
						{
							Name:     "sem",
							UsedPct:  35.0,
							ResetsAt: fixedTime.Add(68 * time.Hour),
						},
					},
				},
				Detail: "pid: 4812\ntask: generate golden files\nbranch: main",
			},
		},
		screenReader{
			row: state.Row{
				Agent:  "codex",
				Status: state.OutOfQuota,
				Model:  "o3-mini",
				Task:   "Optimize SQL queries in reports",
				Since:  fixedTime.Add(-55 * time.Minute),
				End:    fixedTime.Add(-220 * time.Minute),
				Start:  fixedTime.Add(-229 * time.Minute),
				Review: "changed 1 file outside what was allowed",
				Dir:    filepath.Join(runsDir, "codex"),
				Quota: state.Quota{
					Exact:    true,
					UsedPct:  100.0,
					ResetsAt: fixedTime.Add(2*time.Hour + 15*time.Minute),
					Credits:  "320",
					Spend:    "today 2.4 cr · ~14/day · ~23 days",
					Summary:  "weekly quota used up",
				},
				Detail: "plan: prolite · quota used up\nretry after the reset at 17:19",
			},
		},
		screenReader{
			row: state.Row{
				Agent:  "opencode",
				Status: state.Idle,
				Model:  "claude-3-5-sonnet",
				Task:   "Waiting for instructions for new tests",
				Since:  fixedTime.Add(-2 * time.Hour),
				End:    fixedTime.Add(-2 * time.Hour),
				Start:  fixedTime.Add(-2*time.Hour - 15*time.Minute),
				Dir:    filepath.Join(runsDir, "opencode"),
				Quota:  state.Quota{Summary: "Go plan available"},
				Detail: "mode: idle\nno active process",
			},
		},
		screenReader{
			row: state.Row{
				Agent:  "cursor",
				Status: state.Idle,
				Model:  "composer-2.5",
				Task:   "Waiting for a delegated task",
				Since:  fixedTime.Add(-45 * time.Minute),
				End:    fixedTime.Add(-45 * time.Minute),
				Start:  fixedTime.Add(-50 * time.Minute),
				Dir:    filepath.Join(runsDir, "cursor"),
				Quota:  state.Quota{Summary: "no live quota"},
				Detail: "cursor-agent has no usage query that spends nothing",
			},
		},
	}
}

func TestScreens(t *testing.T) {
	// Goldens include mascots; the host may have NO_COLOR set.
	t.Setenv("NO_COLOR", "")
	// A fixed time so the views don't depend on when the tests run.
	fixedTime := time.Date(2026, 9, 26, 15, 4, 5, 0, time.Local)
	oldNow := nowFn
	nowFn = func() time.Time { return fixedTime }
	defer func() { nowFn = oldNow }()

	fakeReaders := setupTestEnv(t, fixedTime)

	screens := []string{"cards", "table", "detail", "history", "history-detail", "report", "timeline", "help"}
	widths := []int{80, 100, 132, 160}

	for _, screen := range screens {
		for _, width := range widths {
			tc := fmt.Sprintf("%s-%d", screen, width)
			t.Run(tc, func(t *testing.T) {
				m := New(fakeReaders, 2*time.Second)
				for _, c := range m.runs {
					if c.Agent == "codex" {
						m.checkChanges(c)
						break
					}
				}
				m.refresh()
				mod, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
				m = mod.(Model)
				switch screen {
				case "cards":
					// The default view.
				case "table":
					mod, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
					m = mod.(Model)
				case "detail":
					mod, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
					m = mod.(Model)
				case "history":
					mod, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
					m = mod.(Model)
				case "history-detail":
					mod, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
					m = mod.(Model)
					mod, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
					m = mod.(Model)
				case "report":
					mod, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
					m = mod.(Model)
				case "timeline":
					mod, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
					m = mod.(Model)
				case "help":
					mod, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
					m = mod.(Model)
				}

				output := m.View()

				// Check the maximum height.
				if h := lipgloss.Height(output); h > 40 {
					t.Errorf("%s: %d lines tall, 40 at most", tc, h)
				}

				// Strip ANSI sequences and normalize line endings.
				plain := reANSI.ReplaceAllString(output, "")
				plain = strings.ReplaceAll(plain, "\r\n", "\n")

				// Check the width of each line.
				for i, line := range strings.Split(plain, "\n") {
					if w := lipgloss.Width(line); w > width {
						t.Errorf("%s: line %d is %d columns wide (max %d): %q", tc, i+1, w, width, line)
					}
				}

				plain = hideDir(plain)

				path := filepath.Join("testdata", "screens", fmt.Sprintf("%s-%d.txt", screen, width))
				if *update {
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatalf("could not create testdata/screens: %v", err)
					}
					if err := os.WriteFile(path, []byte(plain), 0o644); err != nil {
						t.Fatalf("could not write %s: %v", path, err)
					}
					return
				}

				expectedBytes, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("could not read %s: %v (run with -update to generate it)", path, err)
				}
				expected := strings.ReplaceAll(string(expectedBytes), "\r\n", "\n")

				if plain != expected {
					wantLines := strings.Split(expected, "\n")
					gotLines := strings.Split(plain, "\n")
					maxL := max(len(wantLines), len(gotLines))
					firstLine := -1
					var wantR, gotR string
					for i := 0; i < maxL; i++ {
						var eL, oL string
						if i < len(wantLines) {
							eL = wantLines[i]
						}
						if i < len(gotLines) {
							oL = gotLines[i]
						}
						if eL != oL {
							firstLine = i + 1
							wantR = eL
							gotR = oL
							break
						}
					}
					t.Errorf("%s: first differing line (line %d):\nwant: %q\ngot:  %q",
						path, firstLine, wantR, gotR)
				}
			})
		}
	}
}

func TestCaptureView(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	fixedTime := time.Date(2026, 9, 26, 15, 4, 5, 0, time.Local)
	oldNow := nowFn
	nowFn = func() time.Time { return fixedTime }
	defer func() { nowFn = oldNow }()

	fakeReaders := setupTestEnv(t, fixedTime)

	views := []string{
		"", "table", "detail", "agent-detail",
		"history", "history-detail", "run-detail",
		"report", "timeline", "statusline", "help",
	}
	widths := []int{80, 100, 132, 160}

	for _, v := range views {
		for _, width := range widths {
			tc := fmt.Sprintf("%s-%d", v, width)
			t.Run(tc, func(t *testing.T) {
				output := CaptureView(fakeReaders, width, v)
				if h := lipgloss.Height(output); h > 40 {
					t.Errorf("%s: %d lines tall, 40 at most", tc, h)
				}
				plain := reANSI.ReplaceAllString(output, "")
				plain = strings.ReplaceAll(plain, "\r\n", "\n")
				for i, line := range strings.Split(plain, "\n") {
					if w := lipgloss.Width(line); w > width {
						t.Errorf("%s: line %d is %d columns wide (max %d): %q", tc, i+1, w, width, line)
					}
				}
				switch v {
				case "detail", "agent-detail":
					if !strings.Contains(plain, "‹") || !strings.Contains(plain, "›") {
						t.Errorf("%s: the agent selector is missing from the detail", tc)
					}
					if !strings.Contains(plain, "╰") && !strings.Contains(plain, "┗") {
						t.Errorf("%s: the detail panel has no bottom border", tc)
					}
				case "history-detail", "run-detail":
					if !strings.Contains(plain, "‹ previous · next ›") {
						t.Errorf("%s: the run selector is missing from the run detail", tc)
					}
					if !strings.Contains(plain, "╰") && !strings.Contains(plain, "┗") {
						t.Errorf("%s: the run detail has no bottom border", tc)
					}
				case "timeline", "statusline":
					if !strings.Contains(plain, "TIMELINE") {
						t.Errorf("%s: should show the timeline", tc)
					}
				}
			})
		}
	}
}

// hideDir replaces the path on the "dir" line with a fixed mark of the same
// width: the runs dir lives in os.TempDir(), which changes from one machine to
// another, and without this the golden tests would only pass on the machine
// that generated them.
func hideDir(s string) string {
	const prefix = "│ dir"
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if !strings.HasPrefix(l, prefix) {
			continue
		}
		rest := strings.TrimPrefix(l, prefix)
		value := strings.TrimLeft(rest, " ")
		indent := rest[:len(rest)-len(value)]
		body := strings.TrimSuffix(value, "│")
		end := value[len(body):]
		width := lipgloss.Width(body)
		mark := "<temp dir>"
		if width < lipgloss.Width(mark) {
			mark = strings.Repeat("·", width)
		}
		lines[i] = prefix + indent + mark + strings.Repeat(" ", width-lipgloss.Width(mark)) + end
	}
	return strings.Join(lines, "\n")
}
