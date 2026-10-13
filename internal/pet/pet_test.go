package pet

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/AlbertoVasquezR/panal/internal/mascots"
	"github.com/AlbertoVasquezR/panal/internal/readers"
	"github.com/AlbertoVasquezR/panal/internal/state"
	"github.com/AlbertoVasquezR/panal/internal/ui"
)

var update = flag.Bool("update", false, "update the golden files")

// A fixed instant, so frames (and the animation frame number) are stable.
var t0 = time.Date(2026, 9, 26, 15, 0, 0, 0, time.UTC)

type fakeReader struct {
	mu  *sync.Mutex
	row *state.Row
}

func fake(r state.Row) fakeReader { return fakeReader{&sync.Mutex{}, &r} }

func (f fakeReader) Agent() string { return f.row.Agent }
func (f fakeReader) Read() state.Row {
	f.mu.Lock()
	defer f.mu.Unlock()
	return *f.row
}
func (f fakeReader) set(st state.Status) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.row.Status = st
}

// scene: claude orchestrating for 12 min, agy done half an hour ago, codex
// out of quota, opencode idle.
func scene() []state.Row {
	return []state.Row{
		{Agent: "claude", Status: state.Orchestrating, Since: t0.Add(-12 * time.Minute)},
		{Agent: "agy", Status: state.Done, End: t0.Add(-30 * time.Minute)},
		{Agent: "codex", Status: state.OutOfQuota, End: t0.Add(-2 * time.Hour)},
		{Agent: "opencode", Status: state.Idle},
	}
}

func readersOf(rows []state.Row) []readers.Reader {
	out := make([]readers.Reader, len(rows))
	for i, r := range rows {
		out[i] = fake(r)
	}
	return out
}

func setup(t *testing.T) {
	t.Helper()
	t.Setenv("NO_COLOR", "")
	lipgloss.SetHasDarkBackground(true)
	ui.Disabled = nil
	ui.Conf = nil
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	if strings.ReplaceAll(string(want), "\r\n", "\n") != got {
		t.Errorf("%s changed:\n--- want\n%s\n--- got\n%s", name, want, got)
	}
}

func TestJSONContractGolden(t *testing.T) {
	setup(t)
	f := JSON(readersOf(scene()), Options{}, t0)
	golden(t, "frame.json", string(Encode(f))+"\n")

	// The field names are the contract: exactly these, in this order.
	var m map[string]json.RawMessage
	if err := json.Unmarshal(Encode(f), &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"v", "agent", "status", "glyph", "color", "mood", "line", "others", "agents", "raster"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing %q", k)
		}
	}
	if len(m) != 10 {
		t.Errorf("%d fields, want 10: %s", len(m), Encode(f))
	}
	if f.Agent != "claude" || f.Status != "orchestrating" || f.Glyph != "●" || f.Mood != MoodWork {
		t.Errorf("pet = %s %s %s %s", f.Agent, f.Status, f.Glyph, f.Mood)
	}
	if f.Line != "claude ● orchestrating · 12 min" {
		t.Errorf("line = %q", f.Line)
	}
	if f.Others != "agy ✔ · codex ◐ · opencode ○" {
		t.Errorf("others = %q", f.Others)
	}
	if f.Color != "#42A5F5" {
		t.Errorf("color = %q, want the dark theme's blue", f.Color)
	}
	if len(f.Agents) != 4 || f.Agents[2] != (AgentInfo{"codex", "out of quota", "◐", "#FFB300"}) {
		t.Errorf("agents = %+v", f.Agents)
	}
}

func TestRasterRoundTrip(t *testing.T) {
	modes := []mascots.Mode{mascots.Idle, mascots.Working, mascots.Sleeping, mascots.Stuck, mascots.Nap,
		mascots.Celebrate, mascots.Scared, mascots.WakeUp, mascots.Greet, mascots.Pet, mascots.Conduct}
	for name, s := range mascots.All {
		for _, m := range modes {
			for n := 0; n < 8; n++ {
				cells := s.Cells(m, n, mascots.Effects{Sweat: n%2 == 0})
				r := EncodeRaster(cells)
				if r.Columns != mascots.Width || r.Rows != mascots.Height/2 {
					t.Fatalf("%s: raster %d×%d", name, r.Columns, r.Rows)
				}
				got, err := DecodeRaster(r)
				if err != nil {
					t.Fatalf("%s mode %d frame %d: %v", name, m, n, err)
				}
				for y := range got {
					for x, c := range got[y] {
						want := cells[y][x]
						if c.Char != want.Char || !strings.ContainsRune(" ▀▄█", c.Char) {
							t.Fatalf("%s %d,%d: %q, want %q", name, y, x, c.Char, want.Char)
						}
						if c.Char == ' ' && (c.FG != DefaultColor || c.BG != DefaultColor) {
							t.Fatalf("%s %d,%d: an empty cell must use the default colors", name, y, x)
						}
						if c.Char != ' ' && (c.FG != rgb(want.FG) || c.FG == DefaultColor) {
							t.Fatalf("%s %d,%d: fg %06X, want %s", name, y, x, c.FG, want.FG)
						}
						if c.BG != rgb(want.BG) {
							t.Fatalf("%s %d,%d: bg %08X, want %q", name, y, x, c.BG, want.BG)
						}
					}
				}
			}
		}
	}
}

func TestRasterSizeFromJSON(t *testing.T) {
	setup(t)
	for _, tc := range []struct{ cols, rows, wantC, wantR int }{
		{0, 0, 12, 5}, {40, 20, 12, 5}, {8, 3, 8, 3}, {12, 1, 12, 1},
	} {
		f := JSON(readersOf(scene()), Options{Cols: tc.cols, Rows: tc.rows}, t0)
		var back Frame
		if err := json.Unmarshal(Encode(f), &back); err != nil {
			t.Fatal(err)
		}
		cells, err := DecodeRaster(back.Raster)
		if err != nil {
			t.Fatal(err)
		}
		if back.Raster.Columns != tc.wantC || back.Raster.Rows != tc.wantR || len(cells) != tc.wantR {
			t.Errorf("-cols %d -rows %d: %d×%d", tc.cols, tc.rows, back.Raster.Columns, back.Raster.Rows)
		}
	}
	// Cropped from the top and centered: the first row keeps the
	// orchestrating claude's raised baton column if it is in range.
	full := JSON(readersOf(scene()), Options{}, t0).cells
	small := JSON(readersOf(scene()), Options{Cols: 8, Rows: 3}, t0).cells
	if small[0][0] != full[0][2] || small[2][7] != full[2][9] {
		t.Error("the crop is not centered horizontally and top-aligned")
	}
}

func TestDecodeRasterRejectsBadInput(t *testing.T) {
	if _, err := DecodeRaster(Raster{Columns: 2, Rows: 1, Cells: "AAAA"}); err == nil {
		t.Error("a short buffer must fail")
	}
	bad := EncodeRaster([][]mascots.Cell{{{Char: '\n'}}})
	if _, err := DecodeRaster(bad); err == nil {
		t.Error("a control character must fail")
	}
}

func TestSelectAgent(t *testing.T) {
	cases := []struct {
		name   string
		rows   []state.Row
		forced string
		want   string
	}{
		{"claude orchestrating wins", []state.Row{
			{Agent: "claude", Status: state.Orchestrating},
			{Agent: "codex", Status: state.Working, Since: t0},
		}, "", "claude"},
		{"the working one that started last", []state.Row{
			{Agent: "claude", Status: state.Idle},
			{Agent: "agy", Status: state.Working, Since: t0.Add(-time.Hour)},
			{Agent: "codex", Status: state.Working, Since: t0.Add(-time.Minute)},
		}, "", "codex"},
		{"stuck counts as active", []state.Row{
			{Agent: "agy", Status: state.Done, End: t0},
			{Agent: "opencode", Status: state.Stuck, Start: t0.Add(-time.Hour)},
		}, "", "opencode"},
		{"working beats a later finish", []state.Row{
			{Agent: "agy", Status: state.Done, End: t0},
			{Agent: "codex", Status: state.Working, Since: t0.Add(-time.Hour)},
		}, "", "codex"},
		{"else the latest finished", []state.Row{
			{Agent: "claude", Status: state.Idle},
			{Agent: "agy", Status: state.Done, End: t0.Add(-time.Hour)},
			{Agent: "codex", Status: state.Failed, End: t0.Add(-time.Minute)},
		}, "", "codex"},
		{"else claude", []state.Row{
			{Agent: "agy", Status: state.NoData},
			{Agent: "claude", Status: state.Idle},
		}, "", "claude"},
		{"else the first", []state.Row{
			{Agent: "agy", Status: state.NoData},
			{Agent: "codex", Status: state.NoData},
		}, "", "agy"},
		{"forced", []state.Row{
			{Agent: "claude", Status: state.Orchestrating},
			{Agent: "opencode", Status: state.Idle},
		}, "opencode", "opencode"},
		{"nothing", nil, "", ""},
	}
	for _, c := range cases {
		if got := SelectAgent(c.rows, c.forced); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestSelectAgentSkipsDisabledAtRest(t *testing.T) {
	setup(t)
	ui.Disabled = map[string]bool{"agy": true}
	defer func() { ui.Disabled = nil }()
	rows := []state.Row{
		{Agent: "claude", Status: state.Idle},
		{Agent: "agy", Status: state.Done, End: t0},
	}
	f := New(Options{}).Frame(rows, t0.Add(time.Hour))
	if f.Agent != "claude" || f.Others != "" || len(f.Agents) != 1 {
		t.Errorf("disabled agy should be left out: %s · %q · %d", f.Agent, f.Others, len(f.Agents))
	}
	if f := New(Options{Agent: "agy"}).Frame(rows, t0.Add(time.Hour)); f.Agent != "agy" || f.Status != "done" {
		t.Errorf("a forced agent shows even if it is off: %s %s", f.Agent, f.Status)
	}
}

func TestMoodFromStatus(t *testing.T) {
	setup(t)
	now := t0
	cases := []struct {
		name string
		row  state.Row
		want string
	}{
		{"working", state.Row{Agent: "codex", Status: state.Working, Since: now.Add(-time.Minute)}, MoodWork},
		{"orchestrating", state.Row{Agent: "claude", Status: state.Orchestrating}, MoodWork},
		{"stuck shakes", state.Row{Agent: "codex", Status: state.Stuck}, MoodScared},
		{"failed long ago sleeps in gray", state.Row{Agent: "codex", Status: state.Failed, End: now.Add(-time.Hour)}, MoodSleep},
		{"out of quota sleeps", state.Row{Agent: "codex", Status: state.OutOfQuota}, MoodSleep},
		{"idle", state.Row{Agent: "agy", Status: state.Idle}, MoodIdle},
		{"done a while ago", state.Row{Agent: "agy", Status: state.Done, End: now.Add(-10 * time.Minute)}, MoodIdle},
		{"at rest for hours naps", state.Row{Agent: "agy", Status: state.Done, End: now.Add(-3 * time.Hour)}, MoodSleep},
		{"just finished ok", state.Row{Agent: "agy", Status: state.Done, End: now.Add(-2 * time.Second)}, MoodCelebrate},
		{"just failed", state.Row{Agent: "agy", Status: state.Failed, End: now.Add(-2 * time.Second)}, MoodScared},
		{"finished JustNow ago", state.Row{Agent: "agy", Status: state.Done, End: now.Add(-JustNow)}, MoodIdle},
	}
	for _, c := range cases {
		if got := New(Options{}).Frame([]state.Row{c.row}, now).Mood; got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	// Without animation there are no reactions: only the status.
	row := state.Row{Agent: "agy", Status: state.Done, End: now.Add(-time.Second)}
	if got := New(Options{NoAnimation: true}).Frame([]state.Row{row}, now).Mood; got != MoodIdle {
		t.Errorf("no animation: %q, want idle", got)
	}
}

func TestMoodReactsToChanges(t *testing.T) {
	setup(t)
	codex := state.Row{Agent: "codex", Status: state.Idle, End: t0.Add(-time.Hour)}
	rows := func(st state.Status) []state.Row {
		r := codex
		r.Status = st
		if st == state.Working {
			r.Since = t0
		}
		return []state.Row{r}
	}
	p := New(Options{Agent: "codex"})
	p.Observe(rows(state.Idle), t0)
	p.Observe(rows(state.Working), t0.Add(2*time.Second))
	if got := p.Frame(rows(state.Working), t0.Add(2500*time.Millisecond)).Mood; got != MoodGreet {
		t.Errorf("starting to work: %q, want greet (wake up)", got)
	}
	if got := p.Frame(rows(state.Working), t0.Add(10*time.Second)).Mood; got != MoodWork {
		t.Errorf("after waking up: %q, want work", got)
	}
	// The run ends; the reader still has the old End: the observed change
	// is what celebrates.
	p.Observe(rows(state.Done), t0.Add(20*time.Second))
	if got := p.Frame(rows(state.Done), t0.Add(21*time.Second)).Mood; got != MoodCelebrate {
		t.Errorf("run finished: %q, want celebrate", got)
	}
	if got := p.Frame(rows(state.Done), t0.Add(40*time.Second)).Mood; got != MoodIdle {
		t.Errorf("after celebrating: %q, want idle", got)
	}
	p.Observe(rows(state.Working), t0.Add(50*time.Second))
	p.Observe(rows(state.Stuck), t0.Add(60*time.Second))
	if got := p.Frame(rows(state.Stuck), t0.Add(61*time.Second)).Mood; got != MoodScared {
		t.Errorf("got stuck: %q, want scared", got)
	}

	// An old run re-read (idle → done, ended an hour ago) is not news.
	q := New(Options{Agent: "codex"})
	q.Observe(rows(state.Idle), t0)
	q.Observe(rows(state.Done), t0.Add(2*time.Second))
	if got := q.Frame(rows(state.Done), t0.Add(3*time.Second)).Mood; got != MoodIdle {
		t.Errorf("old run re-read: %q, want idle", got)
	}
}

func TestPetCheersForTheOthers(t *testing.T) {
	setup(t)
	claude := state.Row{Agent: "claude", Status: state.Orchestrating, Since: t0.Add(-time.Hour)}
	codex := state.Row{Agent: "codex", Status: state.Working, Since: t0.Add(-time.Minute)}
	p := New(Options{})
	p.Observe([]state.Row{claude, codex}, t0)
	codex.Status = state.Failed
	p.Observe([]state.Row{claude, codex}, t0.Add(2*time.Second))
	f := p.Frame([]state.Row{claude, codex}, t0.Add(2500*time.Millisecond))
	if f.Agent != "claude" || f.Mood != MoodScared {
		t.Errorf("codex failed while claude orchestrates: %s %s, want claude scared", f.Agent, f.Mood)
	}
	if f.Line != "claude ● orchestrating · 1 h" {
		t.Errorf("the line still says the pet's status: %q", f.Line)
	}
}

func TestGreetsWhenThePetChanges(t *testing.T) {
	setup(t)
	agy := state.Row{Agent: "agy", Status: state.Done, End: t0.Add(-time.Hour)}
	codex := state.Row{Agent: "codex", Status: state.Done, End: t0.Add(-2 * time.Hour)}
	p := New(Options{NoAnimation: false})
	p.Observe([]state.Row{agy, codex}, t0)
	codex.End = t0.Add(-30 * time.Minute) // codex's later run shows up, no status change
	p.Observe([]state.Row{agy, codex}, t0.Add(2*time.Second))
	f := p.Frame([]state.Row{agy, codex}, t0.Add(2200*time.Millisecond))
	if f.Agent != "codex" || f.Mood != MoodGreet {
		t.Errorf("new pet: %s %s, want codex greet", f.Agent, f.Mood)
	}
}

func TestStreamHeartbeatAndClosedPipe(t *testing.T) {
	setup(t)
	pr, pw := io.Pipe()
	ls := readersOf(scene())
	done := make(chan error, 1)
	go func() {
		done <- Stream(context.Background(), pw, ls, Options{NoAnimation: true},
			StreamConfig{Every: 10 * time.Millisecond, Frame: 10 * time.Millisecond, Heartbeat: 60 * time.Millisecond})
	}()
	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	var stamps []time.Time
	for len(stamps) < 4 && sc.Scan() {
		var f Frame
		if err := json.Unmarshal(sc.Bytes(), &f); err != nil {
			t.Fatalf("not one JSON object per line: %v: %s", err, sc.Bytes())
		}
		stamps = append(stamps, time.Now())
	}
	// Nothing changes (no animation, same rows): every line after the first
	// is a heartbeat, never further apart than the heartbeat (plus slack).
	for i := 1; i < len(stamps); i++ {
		if gap := stamps[i].Sub(stamps[i-1]); gap > 200*time.Millisecond {
			t.Errorf("heartbeat gap %v", gap)
		}
	}

	// A change is written right away, without waiting for the heartbeat.
	ls[2].(fakeReader).set(state.Working)
	deadline := time.Now().Add(2 * time.Second)
	for sc.Scan() && time.Now().Before(deadline) {
		if strings.Contains(sc.Text(), `"name":"codex","status":"working"`) {
			break
		}
	}

	pr.Close() // the consumer goes away
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Stream: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stream did not stop when the pipe closed")
	}
}

func TestStreamStopsOnContext(t *testing.T) {
	setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Stream(ctx, io.Discard, readersOf(scene()), Options{}, StreamConfig{Frame: 10 * time.Millisecond})
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stream did not stop on cancel (SIGINT)")
	}
}

func TestViewGolden(t *testing.T) {
	setup(t)
	f := JSON(readersOf(scene()), Options{}, t0)
	for _, sz := range [][2]int{{20, 8}, {40, 12}} {
		v := View(f, sz[0], sz[1])
		lines := strings.Split(v, "\n")
		if len(lines) != sz[1] {
			t.Errorf("%dx%d: %d lines", sz[0], sz[1], len(lines))
		}
		for _, l := range lines {
			if w := lipgloss.Width(l); w > sz[0] {
				t.Errorf("%dx%d: line of %d columns: %q", sz[0], sz[1], w, l)
			}
		}
		golden(t, fmt.Sprintf("view-%dx%d.txt", sz[0], sz[1]), v+"\n")
	}
}

func TestViewShrinks(t *testing.T) {
	setup(t)
	f := JSON(readersOf(scene()), Options{}, t0)
	if v := View(f, 20, 8); !strings.Contains(v, "▀") || !strings.Contains(v, "claude ● orchestrat") && !strings.Contains(v, "claude ●") {
		t.Errorf("20x8 should keep the mascot and the status:\n%s", v)
	}
	v := View(f, 20, 6) // no room for the mascot: text only
	if strings.ContainsAny(v, "▀▄█") || !strings.Contains(v, "claude ●") || !strings.Contains(v, "agy ✔") {
		t.Errorf("20x6:\n%s", v)
	}
	v = View(f, 12, 1)
	if strings.TrimSpace(v) != "claude ●" {
		t.Errorf("12x1: %q", v)
	}
	if v := View(f, 14, 1); strings.TrimSpace(v) != "claude ● orch…" {
		t.Errorf("14x1: %q", v)
	}
	if got := othersLine(f, 20); lipgloss.Width(got) > 20 || !strings.HasSuffix(got, "…") {
		t.Errorf("others at 20: %q", got)
	}
}

func TestViewNoColor(t *testing.T) {
	setup(t)
	t.Setenv("NO_COLOR", "1")
	f := JSON(readersOf(scene()), Options{}, t0)
	v := View(f, 40, 12)
	if strings.ContainsAny(v, "▀▄█") {
		t.Errorf("NO_COLOR draws no mascot:\n%s", v)
	}
	if !strings.Contains(v, "claude ● orchestrating · 12 min") || !strings.Contains(v, "agy ✔ · codex ◐ · opencode ○") {
		t.Errorf("NO_COLOR keeps glyph and word:\n%s", v)
	}
}

func TestModelKeysAndResize(t *testing.T) {
	setup(t)
	m := newModel(New(Options{}), readersOf(scene()), time.Second, func() time.Time { return t0 })
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 20, Height: 8})
	if v := nm.View(); len(strings.Split(v, "\n")) != 8 {
		t.Errorf("after resize the view is not 8 lines:\n%s", v)
	}
	for _, k := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune("q")}, {Type: tea.KeyEsc}, {Type: tea.KeyCtrlC}} {
		if _, cmd := nm.Update(k); cmd == nil {
			t.Errorf("%s should quit", k)
		}
	}
}

func TestHex(t *testing.T) {
	lipgloss.SetHasDarkBackground(true)
	for in, want := range map[lipgloss.TerminalColor]string{
		lipgloss.Color("245"):     "#8A8A8A",
		lipgloss.Color("39"):      "#00AFFF",
		lipgloss.Color("9"):       "#FF0000",
		lipgloss.Color("#d97757"): "#D97757",
		lipgloss.AdaptiveColor{Light: "240", Dark: "#66BB6A"}: "#66BB6A",
	} {
		if got := Hex(in); got != want {
			t.Errorf("Hex(%v) = %s, want %s", in, got, want)
		}
	}
	lipgloss.SetHasDarkBackground(false)
	defer lipgloss.SetHasDarkBackground(true)
	if got := Hex(lipgloss.AdaptiveColor{Light: "240", Dark: "245"}); got != "#585858" {
		t.Errorf("light theme: %s", got)
	}
}

// With All, every shown agent gets its own mascot, in reader order, and
// without it the field is left out of the JSON entirely.
func TestFramePetsWithAll(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	rows := []state.Row{
		{Agent: "claude", Status: state.Orchestrating},
		{Agent: "codex", Status: state.Done, End: now.Add(-2 * time.Second)},
	}
	f := New(Options{All: true, NoAnimation: true}).Frame(rows, now)
	if len(f.Pets) != 2 || f.Pets[0].Name != "claude" || f.Pets[1].Name != "codex" {
		t.Fatalf("pets: %+v", f.Pets)
	}
	for _, pi := range f.Pets {
		if _, err := DecodeRaster(pi.Raster); err != nil || pi.Raster.Columns == 0 {
			t.Errorf("%s raster: %v %+v", pi.Name, err, pi.Raster)
		}
	}
	if b := Encode(New(Options{NoAnimation: true}).Frame(rows, now)); bytes.Contains(b, []byte(`"pets"`)) {
		t.Errorf("without All there must be no pets field: %s", b)
	}
}
