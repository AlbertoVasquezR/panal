package models

import (
	"bytes"
	"context"
	"errors"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AlbertoVasquezR/panal/internal/router"
)

// The samples in testdata are real answers captured on a dev machine:
//
//	agy-models.txt                agy models (stdout)
//	codex-debug-models.json       codex debug models, with each model's two
//	                              prompt fields (model_messages,
//	                              base_instructions; ~40 KB of instructions
//	                              each) dropped
//	opencode-api-model-list.json  opencode api model.list, with the
//	                              directory it reports replaced
//	cursor-models.txt             reconstructed id<TAB>name listing
//	cursor-models-listed.txt      live Windows `cursor-agent models`
//	                              ("id - Display name", header, annotation)
//
// They only hold catalogs: ids, names, efforts, prices.

var now = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

func sample(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// realCatalog is the catalog built from the three samples.
func realCatalog(t *testing.T) Catalog {
	t.Helper()
	cat := Catalog{Version: Version, CLIs: map[string]*CLI{}}
	for cli, f := range map[string]struct {
		file  string
		parse func([]byte) ([]Model, error)
	}{
		"agy":      {"agy-models.txt", ParseAgy},
		"codex":    {"codex-debug-models.json", ParseCodex},
		"cursor":   {"cursor-models.txt", ParseCursor},
		"opencode": {"opencode-api-model-list.json", ParseOpencodeAPI},
	} {
		ms, err := f.parse(sample(t, f.file))
		if err != nil {
			t.Fatal(err)
		}
		cat.CLIs[cli] = &CLI{Command: Command(cli), At: now, Tried: now, Models: ms}
	}
	return cat
}

// ------------------------------------------------------------- parsers --

func TestParseAgy(t *testing.T) {
	ms, err := ParseAgy(append([]byte("Fetching available models...\n"), sample(t, "agy-models.txt")...))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 14 {
		t.Fatalf("%d models: %+v", len(ms), ms)
	}
	if ms[0].ID != "gemini-3.8-flash-high" || ms[0].Name != "Gemini 3.8 Flash (High)" || len(ms[0].Efforts) != 0 {
		t.Errorf("first: %+v", ms[0])
	}
	if _, err := ParseAgy([]byte("Fetching available models...\r\n")); err != nil {
		t.Error(err)
	}
}

func TestParseCodex(t *testing.T) {
	ms, err := ParseCodex(sample(t, "codex-debug-models.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 10 {
		t.Fatalf("%d models", len(ms))
	}
	by := map[string]Model{}
	for _, m := range ms {
		by[m.ID] = m
	}
	luna := by["gpt-6-luna"]
	if strings.Join(luna.Efforts, " ") != "low medium high xhigh max" || luna.DefaultEffort != "medium" || luna.Priority != 4 || luna.Hidden || luna.Name != "GPT-6-Luna" {
		t.Errorf("gpt-6-luna: %+v", luna)
	}
	if !by["gpt-reserve"].Hidden || !by["codex-auto-review"].Hidden {
		t.Error("visibility hide is hidden")
	}
	if r := by["gpt-5.5"].Retiring; !strings.Contains(r, "2026-10-14") || !strings.Contains(r, "gpt-6.1-sol") {
		t.Errorf("gpt-5.5 retiring: %q", r)
	}
	if _, err := ParseCodex([]byte("Error: not logged in")); err == nil {
		t.Error("not JSON is an error")
	}
}

func TestParseOpencode(t *testing.T) {
	ms, err := ParseOpencodeAPI(sample(t, "opencode-api-model-list.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 39 {
		t.Fatalf("%d models", len(ms))
	}
	var luna *Model
	for i := range ms {
		if ms[i].ID == "opencode-go/gpt-6-luna" {
			luna = &ms[i]
		}
	}
	if luna == nil || luna.Price == nil || *luna.Price != 0.5 || !strings.Contains(strings.Join(luna.Efforts, " "), "low") {
		t.Fatalf("gpt-6-luna: %+v", luna)
	}
	// What opencode 2.0 answers with a private server (--standalone): no models.
	empty, err := ParseOpencodeAPI([]byte(`{"location":{"directory":"C:\\Users\\someone"},"data":[]}`))
	if err != nil || len(empty) != 0 {
		t.Errorf("empty list: %v %v", empty, err)
	}
	// `opencode models` printed nothing at all on the same machine.
	if ms, _ := ParseOpencodeList(nil); len(ms) != 0 {
		t.Error("nothing printed, no models")
	}
	if ms, _ := ParseOpencodeList([]byte("opencode/big-pickle\nopencode-go/glm-5.3\n\n")); len(ms) != 2 || ms[1].ID != "opencode-go/glm-5.3" {
		t.Errorf("provider/model lines: %+v", ms)
	}
}

func TestParseCursor(t *testing.T) {
	ms, err := ParseCursor(append([]byte("Available models\n"), sample(t, "cursor-models.txt")...))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 9 {
		t.Fatalf("%d models: %+v", len(ms), ms)
	}
	if ms[0].ID != "auto" || ms[0].Name != "Auto" {
		t.Errorf("first: %+v", ms[0])
	}
	if ms[1].ID != "composer-2.5" || ms[1].Name != "Composer 2.5" {
		t.Errorf("composer: %+v", ms[1])
	}
	jsonIDs, err := ParseCursor([]byte(`["composer-2.5","grok-4.7"]`))
	if err != nil || len(jsonIDs) != 2 || jsonIDs[1].ID != "grok-4.7" {
		t.Errorf("json ids: %v %+v", err, jsonIDs)
	}
	obj, err := ParseCursor([]byte(`{"models":[{"id":"composer-2.5","name":"Composer 2.5"}]}`))
	if err != nil || len(obj) != 1 || obj[0].Name != "Composer 2.5" {
		t.Errorf("json object: %v %+v", err, obj)
	}
	if ms, _ := ParseCursor([]byte("Available models\n\n")); len(ms) != 0 {
		t.Errorf("headers only: %+v", ms)
	}
}

func TestParseCursorWindowsListing(t *testing.T) {
	// The live `cursor-agent models` listing: header, blank line, "id - Name"
	// with a "(current, default)" annotation, CRLF, and ANSI on the header
	// and the first id. Repo testdata is LF (eol=lf); we rebuild the bytes
	// Windows actually prints.
	plain := sample(t, "cursor-models-listed.txt")
	var win []byte
	win = append(win, []byte("\x1b[1m")...)
	for i, line := range bytes.Split(plain, []byte("\n")) {
		if i == 2 && bytes.HasPrefix(line, []byte("auto")) {
			line = append(append([]byte("\x1b[32m"), line[:4]...), append([]byte("\x1b[0m"), line[4:]...)...)
		}
		win = append(win, line...)
		win = append(win, '\r', '\n')
	}
	ms, err := ParseCursor(win)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct{ id, name string }{
		{"auto", "Auto"},
		{"gpt-5.3-codex-low", "Codex 5.3 Low"},
		{"gpt-5.3-codex-low-fast", "Codex 5.3 Low Fast"},
		{"gpt-5.3-codex", "Codex 5.3"},
		{"gpt-5.3-codex-fast", "Codex 5.3 Fast"},
		{"gpt-5.3-codex-high", "Codex 5.3 High"},
	}
	if len(ms) != len(want) {
		t.Fatalf("%d models: %+v", len(ms), ms)
	}
	for i, w := range want {
		if ms[i].ID != w.id || ms[i].Name != w.name {
			t.Errorf("%d: %+v, want %s / %s", i, ms[i], w.id, w.name)
		}
	}
}

func TestWindowsCmdShim(t *testing.T) {
	name, args := windowsCmd(`C:\Users\me\AppData\Local\cursor-agent\cursor-agent.cmd`, []string{"models"})
	if name != "cmd.exe" || len(args) != 3 || args[0] != "/c" || args[2] != "models" {
		t.Errorf("cmd shim: %q %q", name, args)
	}
	name, args = windowsCmd("cursor-agent", []string{"models"})
	if name != "cursor-agent" || len(args) != 1 || args[0] != "models" {
		t.Errorf("plain binary: %q %q", name, args)
	}
}

// ----------------------------------------------------------- discovery --

// fake answers each CLI from a map ("cli args" → output); a missing key
// fails like a CLI that errors.
type fake struct {
	out   map[string]string
	calls atomic.Int32
	block chan struct{} // when set, every call waits for it
}

func (f *fake) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	f.calls.Add(1)
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return nil, errors.New("no answer in time")
		}
	}
	k := name + " " + strings.Join(args, " ")
	if o, ok := f.out[k]; ok {
		return []byte(o), nil
	}
	return nil, errors.New("exit status 1: unknown command")
}

func installed(names ...string) func(string) (string, error) {
	return func(n string) (string, error) {
		for _, x := range names {
			if x == n {
				return "/bin/" + n, nil
			}
		}
		return "", errors.New("not found")
	}
}

func TestDiscoverFallsBackAndSaysWhy(t *testing.T) {
	f := &fake{out: map[string]string{
		"opencode api model.list": `{"location":{},"data":[]}`,
		"opencode models":         "",
	}}
	e := Discover(context.Background(), f.run, "opencode", now)
	if len(e.Models) != 0 || !strings.Contains(e.Error, "opencode api model.list listed no models") || !strings.Contains(e.Error, "opencode models listed no models") {
		t.Errorf("%+v", e)
	}
	f.out["opencode models"] = "opencode/big-pickle\n"
	e = Discover(context.Background(), f.run, "opencode", now)
	if len(e.Models) != 1 || e.Command != "opencode models" || e.Error != "" || !e.At.Equal(now) {
		t.Errorf("fallback: %+v", e)
	}
}

func TestRefreshKeepsWhatItHadAndMarksMissingCLIs(t *testing.T) {
	old := Catalog{CLIs: map[string]*CLI{"codex": {Command: "codex debug models", At: now.Add(-48 * time.Hour), Models: []Model{{ID: "gpt-6-luna"}}}}}
	f := &fake{out: map[string]string{"agy models": string(sample(t, "agy-models.txt"))}}
	cat := Refresh(context.Background(), f.run, installed("agy", "codex"), old, CLIs, time.Second, now)
	if e := cat.Get("agy"); len(e.Models) != 14 || !e.At.Equal(now) {
		t.Errorf("agy: %+v", e)
	}
	if e := cat.Get("codex"); len(e.Models) != 1 || e.Error == "" || !e.Tried.Equal(now) || !e.At.Equal(now.Add(-48*time.Hour)) {
		t.Errorf("codex failed: it keeps its models and says why: %+v", e)
	}
	if e := cat.Get("opencode"); e.Error != "not installed (or not in PATH)" || len(e.Models) != 0 {
		t.Errorf("opencode: %+v", e)
	}
	if old.Get("codex").Error != "" {
		t.Error("Refresh changed its input")
	}
}

func TestRefreshTimeout(t *testing.T) {
	f := &fake{block: make(chan struct{})}
	defer close(f.block)
	start := time.Now()
	cat := Refresh(context.Background(), f.run, installed("codex"), Catalog{}, []string{"codex"}, 50*time.Millisecond, now)
	if time.Since(start) > 5*time.Second {
		t.Fatal("the timeout did not stop it")
	}
	if e := cat.Get("codex"); !strings.Contains(e.Error, "no answer in 50ms") {
		t.Errorf("%+v", e)
	}
}

func TestCacheAndStaleness(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", FileName)
	if _, err := Load(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("no cache: %v", err)
	}
	cat := realCatalog(t)
	if err := cat.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || len(got.Get("codex").Models) != 10 || !got.Get("agy").At.Equal(now) {
		t.Fatalf("round trip: %v", err)
	}
	if got.Stale("codex", now.Add(23*time.Hour), MaxAge) || !got.Stale("codex", now.Add(25*time.Hour), MaxAge) {
		t.Error("stale after 24 h")
	}
	if !got.Stale("claude", now, MaxAge) {
		t.Error("a CLI with no entry is stale")
	}
	failed := Catalog{CLIs: map[string]*CLI{"opencode": {Tried: now, Error: "x"}}}
	if failed.Stale("opencode", now.Add(5*time.Minute), MaxAge) || !failed.Stale("opencode", now.Add(11*time.Minute), MaxAge) {
		t.Error("a failed attempt is retried after 10 minutes, not before")
	}
}

func TestEnsureOnlyAsksWhatIsMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	old := Catalog{CLIs: map[string]*CLI{"codex": {At: now.Add(-72 * time.Hour), Models: []Model{{ID: "gpt-6-luna"}}}}}
	if err := old.Save(path); err != nil {
		t.Fatal(err)
	}
	f := &fake{out: map[string]string{"agy models": string(sample(t, "agy-models.txt"))}}
	cat := Ensure(context.Background(), path, f.run, installed("agy", "codex"), time.Second, now)
	if f.calls.Load() != 1 || len(cat.Get("agy").Models) != 14 || len(cat.Get("codex").Models) != 1 {
		t.Errorf("calls %d: only agy (missing) is asked, codex (old) is not", f.calls.Load())
	}
	saved, _ := Load(path)
	if saved.Get("agy") == nil {
		t.Error("Ensure saves what it found")
	}
	if Ensure(context.Background(), path, f.run, installed("agy", "codex"), time.Second, now); f.calls.Load() != 1 {
		t.Error("nothing missing, nothing asked")
	}
}

func TestRefreshInBackgroundDoesNotBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	f := &fake{block: make(chan struct{}), out: map[string]string{
		"agy models":         string(sample(t, "agy-models.txt")),
		"codex debug models": string(sample(t, "codex-debug-models.json")),
	}}
	start := time.Now()
	done := RefreshInBackground(context.Background(), path, f.run, installed("agy", "codex"), 10*time.Second, MaxAge, func() time.Time { return now })
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("it blocked its caller")
	}
	select {
	case <-done:
		t.Fatal("finished before the CLIs answered")
	case <-time.After(50 * time.Millisecond):
	}
	close(f.block)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("never finished")
	}
	cat, err := Load(path)
	if err != nil || len(cat.Get("agy").Models) != 14 || len(cat.Get("codex").Models) != 10 {
		t.Fatalf("saved: %v", err)
	}
	// Fresh now: a second start asks nothing.
	calls := f.calls.Load()
	<-RefreshInBackground(context.Background(), path, f.run, installed("agy", "codex"), 10*time.Second, MaxAge, func() time.Time { return now.Add(time.Hour) })
	if f.calls.Load() != calls {
		t.Error("a fresh cache is not refreshed")
	}
}

// --------------------------------------------------------------- cost --

func cost(t *testing.T, cat Catalog, cli, id, effort string) float64 {
	t.Helper()
	m, ok := cat.Find(cli, id)
	if !ok {
		t.Fatalf("%s:%s not in the sample", cli, id)
	}
	s, _ := Cost(cli, m, effort)
	return s
}

func TestCostHeuristicOrder(t *testing.T) {
	cat := realCatalog(t)
	chains := [][]struct{ cli, id, effort string }{
		// codex's lines: luna (affordable) < sol (workhorse) < astra (frontier)
		{{"codex", "gpt-6-luna", "high"}, {"codex", "gpt-6.1-sol", "low"}, {"codex", "gpt-6-astra", "low"}},
		// effort, same model
		{{"codex", "gpt-6-luna", "low"}, {"codex", "gpt-6-luna", "medium"}, {"codex", "gpt-6-luna", "high"}, {"codex", "gpt-6-luna", "xhigh"}, {"codex", "gpt-6-luna", "max"}},
		// a newer version of the same line ranks a little stronger
		{{"codex", "gpt-5.6-luna", "low"}, {"codex", "gpt-6-luna", "low"}},
		// agy: the effort is in the id; flash < sonnet < opus/pro
		{{"agy", "gemini-3.8-flash-low", ""}, {"agy", "gemini-3.8-flash-medium", ""}, {"agy", "gemini-3.8-flash-high", ""}, {"agy", "claude-sonnet-4-6", ""}, {"agy", "gemini-3.1-pro-low", ""}, {"agy", "gemini-3.1-pro-high", ""}},
		// opencode: real prices; free < cheap < expensive
		{{"opencode", "opencode/big-pickle", ""}, {"opencode", "opencode-go/mimo-v2.5", ""}, {"opencode", "opencode-go/gpt-6-luna", "low"}, {"opencode", "opencode-go/kimi-k3", ""}},
	}
	for _, ch := range chains {
		for i := 1; i < len(ch); i++ {
			a, b := ch[i-1], ch[i]
			if ca, cb := cost(t, cat, a.cli, a.id, a.effort), cost(t, cat, b.cli, b.id, b.effort); ca >= cb {
				t.Errorf("%s:%s:%s (%.3f) should be cheaper than %s:%s:%s (%.3f)", a.cli, a.id, a.effort, ca, b.cli, b.id, b.effort, cb)
			}
		}
	}
	// A size is not a version, and "max" in a name is a class word when no price says otherwise.
	if _, why := Cost("agy", Model{ID: "gpt-oss-120b-medium"}, ""); strings.Contains(why, "v120") || !strings.Contains(why, "oss → class 2") || !strings.Contains(why, "medium +0.4") {
		t.Errorf("gpt-oss-120b-medium: %s", why)
	}
	if s, why := Cost("opencode", Model{ID: "opencode-go/qwen3.8-max", Efforts: []string{"low"}}, "low"); s < 4 || s >= 5 {
		t.Errorf("qwen3.8-max: %.2f %s", s, why)
	}
	if _, why := Cost("codex", Model{ID: "gpt-x", Description: "Fast and affordable model for easier tasks."}, "low"); !strings.Contains(why, `"affordable" → class 2`) {
		t.Errorf("description hint: %s", why)
	}
}

// --------------------------------------------------------------- rules --

func arms(as []Arm) string {
	var s []string
	for _, a := range as {
		s = append(s, a.String())
	}
	return strings.Join(s, " ")
}

func TestAllowAndExcludeGlobs(t *testing.T) {
	cat := realCatalog(t)
	cases := []struct {
		allow, exclude, want string
	}{
		{"codex:gpt-6-luna*", "", "codex:gpt-6-luna:low codex:gpt-6-luna:medium codex:gpt-6-luna:high"},
		{"codex:gpt-6-luna:*", "", "codex:gpt-6-luna:low codex:gpt-6-luna:medium codex:gpt-6-luna:high codex:gpt-6-luna:xhigh codex:gpt-6-luna:max"},
		{"codex:gpt-6-luna:x*", "", "codex:gpt-6-luna:xhigh"},
		{"codex:GPT-6-Luna", "codex:*:high", "codex:gpt-6-luna:low codex:gpt-6-luna:medium"},
		{"agy:gemini-3.8-flash-*", "", "agy:gemini-3.8-flash-low agy:gemini-3.8-flash-medium agy:gemini-3.8-flash-high"},
		{"agy:gemini-3.8-flash-*,agy:gemini-3.1-pro-*", "agy:*-high", "agy:gemini-3.8-flash-low agy:gemini-3.8-flash-medium agy:gemini-3.1-pro-low"},
		// * crosses opencode's provider/model slash
		{"opencode:*luna", "", "opencode:opencode-go/gpt-6-luna:low opencode:opencode-go/gpt-5.6-luna:low opencode:opencode-go/gpt-6-luna:medium opencode:opencode-go/gpt-5.6-luna:medium opencode:opencode-go/gpt-6-luna:high opencode:opencode-go/gpt-5.6-luna:high"},
		// kimi-k3 only takes "max": without an effort part it runs at opencode's default
		{"opencode:opencode-go/kimi-k3", "", "opencode:opencode-go/kimi-k3"},
		{"opencode:opencode-go/kimi-k3:max", "", "opencode:opencode-go/kimi-k3:max"},
		// hidden models only when named exactly
		{"codex:gpt-r*", "", ""},
		{"codex:gpt-reserve", "", "codex:gpt-reserve:low codex:gpt-reserve:medium codex:gpt-reserve:high"},
		{"codex:gpt-5.5", "", "codex:gpt-5.5:low codex:gpt-5.5:medium codex:gpt-5.5:high"},
		// a whole CLI, minus a model
		{"codex", "codex:gpt-6.1-*,codex:gpt-6-astra,codex:gpt-6-sol,codex:gpt-5.6-*", "codex:gpt-6-luna:low codex:gpt-6-luna:medium codex:gpt-6-luna:high"},
	}
	for _, c := range cases {
		r, err := ParseRules(c.allow, c.exclude, "")
		if err != nil {
			t.Fatal(err)
		}
		got := r.Allowed(cat)
		var names []string
		for _, a := range got {
			names = append(names, a.String())
		}
		want := strings.Fields(c.want)
		if strings.Join(sorted(names), " ") != strings.Join(sorted(want), " ") {
			t.Errorf("models = %s, exclude = %s:\n got %s\nwant %s", c.allow, c.exclude, arms(got), c.want)
		}
	}
}

func sorted(s []string) []string {
	out := append([]string(nil), s...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func TestJudgeSaysWhy(t *testing.T) {
	cat := realCatalog(t)
	r, _ := ParseRules("codex:gpt-*", "codex:gpt-6.1-sol", "")
	why := map[string]string{}
	for _, v := range r.Judge(cat, "codex") {
		why[v.Model.ID] = v.Why
	}
	for id, want := range map[string]string{
		"gpt-6.1-sol":       "exclude: codex:gpt-6.1-sol",
		"gpt-reserve":       "hidden in codex's catalog",
		"gpt-5.5":           "retiring: retires 2026-10-14",
		"codex-auto-review": "not in models",
		"gpt-6-luna":        "",
	} {
		if !strings.HasPrefix(why[id], want) || (want == "" && why[id] != "") {
			t.Errorf("%s: %q, want %q", id, why[id], want)
		}
	}
}

func TestSpreadKeepsCheapestMiddleAndStrongest(t *testing.T) {
	var as []Arm
	for i := 0; i < 10; i++ {
		as = append(as, Arm{CLI: "x", Model: string(rune('a' + i)), Score: float64(i)})
	}
	if got := arms(Spread(as, 4)); got != "x:a x:d x:g x:j" {
		t.Errorf("4 of 10: %s", got)
	}
	if got := arms(Spread(as, 3)); got != "x:a x:f x:j" {
		t.Errorf("3 of 10: %s", got)
	}
	if got := arms(Spread(as[:5], 4)); got != "x:a x:b x:d x:e" {
		t.Errorf("4 of 5: %s", got)
	}
	if got := len(Spread(as[:3], 4)); got != 3 {
		t.Errorf("under the cap, all: %d", got)
	}
	if got := arms(Spread(as, 1)); got != "x:a" {
		t.Errorf("1: %s", got)
	}
}

func TestNewestDropsOlderVersionsOfALine(t *testing.T) {
	in := []Arm{
		{CLI: "agy", Model: "gemini-3.6-flash-low"}, {CLI: "agy", Model: "gemini-3.8-flash-low"},
		{CLI: "agy", Model: "gemini-3.7-flash-low"}, {CLI: "agy", Model: "gemini-3.8-flash-high"},
		{CLI: "codex", Model: "gpt-5.6-luna", Effort: "low"}, {CLI: "codex", Model: "gpt-6-luna", Effort: "low"},
		{CLI: "codex", Model: "gpt-6-luna", Effort: "high"}, {CLI: "codex", Model: "gpt-6.1-sol", Effort: "low"},
		{CLI: "opencode", Model: "opencode/big-pickle"},
	}
	want := "agy:gemini-3.8-flash-low agy:gemini-3.8-flash-high codex:gpt-6-luna:low codex:gpt-6-luna:high codex:gpt-6.1-sol:low opencode:opencode/big-pickle"
	if got := arms(Newest(in)); got != want {
		t.Errorf("\n got %s\nwant %s", got, want)
	}
}

func TestAutoPool(t *testing.T) {
	cat := realCatalog(t)
	r, _ := ParseRules("", "", "")
	pool := r.AutoPool(cat)
	per := map[string][]Arm{}
	for i, a := range pool {
		per[a.CLI] = append(per[a.CLI], a)
		if i > 0 && pool[i-1].Score > a.Score {
			t.Errorf("not cheapest first at %d: %s", i, arms(pool))
		}
	}
	for _, cli := range CLIs {
		if len(per[cli]) != DefaultMaxPerCLI {
			t.Errorf("%s: %d arms, want the cap (%d): %s", cli, len(per[cli]), DefaultMaxPerCLI, arms(per[cli]))
		}
	}
	// The cap keeps each CLI's cheapest and strongest (newest versions).
	for cli, want := range map[string][2]string{
		"agy":   {"agy:gemini-3.8-flash-low", "agy:gemini-3.1-pro-high"},
		"codex": {"codex:gpt-6-luna:low", "codex:gpt-6-astra:high"},
	} {
		as := per[cli]
		if as[0].String() != want[0] || as[len(as)-1].String() != want[1] {
			t.Errorf("%s: %s", cli, arms(as))
		}
	}
	// The user's rule "codex only with gpt-6-luna; agy flash for cheap work and pro for hard work".
	r, _ = ParseRules("codex:gpt-6-luna* agy:gemini-3.8-flash-* agy:gemini-3.1-pro-*", "", "")
	want := "agy:gemini-3.8-flash-low codex:gpt-6-luna:low agy:gemini-3.8-flash-medium codex:gpt-6-luna:medium codex:gpt-6-luna:high agy:gemini-3.1-pro-low agy:gemini-3.1-pro-high"
	if got := arms(r.AutoPool(cat)); got != want {
		t.Errorf("\n got %s\nwant %s", got, want)
	}
	r.MaxPerCLI = 2
	if got := arms(r.AutoPool(cat)); got != "agy:gemini-3.8-flash-low codex:gpt-6-luna:low codex:gpt-6-luna:high agy:gemini-3.1-pro-high" {
		t.Errorf("cap 2: %s", got)
	}
}

func TestParseRules(t *testing.T) {
	r, err := ParseRules("codex:gpt-6-luna*, agy", " agy:claude-* ", "2")
	if err != nil || len(r.Allow) != 2 || len(r.Exclude) != 1 || r.Cap() != 2 {
		t.Errorf("%+v %v", r, err)
	}
	for _, bad := range []string{"0", "-1", "four"} {
		if r, err := ParseRules("", "", bad); err == nil || r.Cap() != DefaultMaxPerCLI {
			t.Errorf("pool_max_per_cli = %s: %v, cap %d", bad, err, r.Cap())
		}
	}
}

func TestCheckAndSuggestForAnExplicitPool(t *testing.T) {
	cat := realCatalog(t)
	r, _ := ParseRules("codex:gpt-6-luna* agy:gemini-3.8-flash-*", "", "")
	pool := []string{"agy:gemini-3.8-flash-medium", "codex:gpt-6-luna:low", "codex:gpt-9-nova:low", "codex:gpt-6-luna:ultra",
		"codex:gpt-5.5:low", "codex:gpt-6.1-sol:low", "codex::low", "opencode"}
	warns := strings.Join(r.Check(cat, pool), "\n")
	for _, want := range []string{
		`codex:gpt-9-nova:low: codex's catalog has no model "gpt-9-nova"`,
		"codex:gpt-5.5:low: retires 2026-10-14",
		"codex:gpt-6.1-sol:low: in the pool, but models/exclude leave it out",
	} {
		if !strings.Contains(warns, want) {
			t.Errorf("missing %q in:\n%s", want, warns)
		}
	}
	// "ultra" is not an effort a chain link takes, so it is part of the model name.
	if !strings.Contains(warns, `no model "gpt-6-luna:ultra"`) {
		t.Errorf("ultra:\n%s", warns)
	}
	for _, quiet := range []string{"gemini-3.8-flash-medium", "gpt-6-luna:low:", "codex::low", "opencode:"} {
		if strings.Contains(warns, quiet) {
			t.Errorf("%q should not warn:\n%s", quiet, warns)
		}
	}
	if got := arms(r.Suggest(cat, pool)); got != "agy:gemini-3.8-flash-low codex:gpt-6-luna:medium agy:gemini-3.8-flash-high codex:gpt-6-luna:high" {
		t.Errorf("suggest: %s", got)
	}
}

func TestDoctorLines(t *testing.T) {
	cat := realCatalog(t)
	cat.CLIs["opencode"] = &CLI{Command: Command("opencode"), Tried: now, Error: "opencode api model.list listed no models", Models: []Model{}}
	delete(cat.CLIs, "agy")
	got := strings.Join(DoctorLines(cat, now.Add(3*time.Hour)), "\n")
	for _, want := range []string{
		"○ agy: not asked yet (panal models -refresh)",
		"✔ codex: 10 models (cached 3 h ago, codex debug models)",
		"○ opencode: no models · opencode api model.list listed no models",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// ------------------------------------------------------------- router --

// A strong arm discovered in the catalog, with no runs of its own, gets
// the complex tasks the cheap arms keep failing.
func TestDiscoveredStrongArmWinsComplexTasks(t *testing.T) {
	cat := realCatalog(t)
	r, _ := ParseRules("codex:gpt-6-luna* agy:gemini-3.8-flash-* agy:gemini-3.1-pro-*", "", "")
	var pool []string
	for _, a := range r.AutoPool(cat) {
		pool = append(pool, a.String())
	}
	strong := pool[len(pool)-1]
	if strong != "agy:gemini-3.1-pro-high" {
		t.Fatalf("the strongest arm: %s", strong)
	}
	task := "Migrate the whole repo from Bubble Tea v1 to v2"
	var ev []router.Evidence
	for _, arm := range pool[:4] { // the cheap ones: flash and luna, low and medium
		for i := 0; i < 3; i++ {
			ev = append(ev, router.Evidence{Arm: arm, CLI: strings.SplitN(arm, ":", 2)[0], Type: "other", Tier: router.Complex,
				Outcome: router.Failure, At: now.Add(-time.Duration(i+1) * time.Hour)})
		}
	}
	good := 0
	for seed := uint64(1); seed <= 10; seed++ {
		d, err := router.Decide(router.Request{Task: task, Pool: pool, Evidence: ev, Now: now, Rand: rand.New(rand.NewPCG(seed, seed+1))})
		if err != nil {
			t.Fatal(err)
		}
		if d.Tier != router.Complex {
			t.Fatalf("tier %s", d.Tier)
		}
		if seed == 1 {
			// The favorite is a strong arm without runs: the pro ones (the
			// cheaper of them, when their estimates are within the margin).
			e, _ := d.Estimate(d.Favorite)
			if !strings.HasPrefix(d.Favorite, "agy:gemini-3.1-pro") || e.Arm1.N != 0 {
				t.Errorf("favorite %s (%d runs), want an untried pro arm", d.Favorite, e.Arm1.N)
			}
		}
		if strings.HasPrefix(d.Chain[0], "agy:gemini-3.1-pro") || d.Chain[0] == "codex:gpt-6-luna:high" {
			good++
		}
	}
	if good < 8 {
		t.Errorf("an untried strong arm went first in %d of 10 seeds", good)
	}
}
