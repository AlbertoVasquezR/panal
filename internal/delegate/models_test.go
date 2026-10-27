package delegate

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AlbertoVasquezR/panal/internal/config"
	"github.com/AlbertoVasquezR/panal/internal/models"
)

// fakeCLIs answers the listing commands with the real samples of
// internal/models/testdata; opencode answers what opencode 2.0 does with a
// private server (no models) and `opencode models` prints nothing.
func fakeCLIs(t *testing.T) models.Runner {
	t.Helper()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("..", "models", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	out := map[string]string{
		"agy models":              read("agy-models.txt"),
		"codex debug models":      read("codex-debug-models.json"),
		"cursor models":           read("cursor-models.txt"),
		"opencode api model.list": `{"location":{"directory":"C:\\Users\\someone"},"data":[]}`,
		"opencode models":         "",
	}
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		if o, ok := out[name+" "+strings.Join(args, " ")]; ok {
			return []byte(o), nil
		}
		return nil, errors.New("unknown command")
	}
}

// squash turns every run of spaces into one, so tests don't depend on
// column widths.
func squash(s string) string {
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return s
}

func allInstalled(string) (string, error) { return "/bin/x", nil }

// catalogOf is the catalog the fake CLIs give.
func catalogOf(t *testing.T) models.Catalog {
	return models.Refresh(context.Background(), fakeCLIs(t), allInstalled, models.Catalog{}, models.CLIs, time.Second, time.Now())
}

const userRule = "codex:gpt-6-luna* agy:gemini-3.8-flash-* agy:gemini-3.1-pro-*"

func TestRouteWithAnAutoPool(t *testing.T) {
	isolate(t)
	t.Setenv("PANAL_POOL", "")
	e := newEnv(t, nil, nil)
	cat := catalogOf(t)
	rt := fakeRouting(e, 0)
	asked := 0
	rt.Catalog = func(refresh bool) models.Catalog {
		asked++
		if !refresh {
			t.Error("pool = auto may fill a missing cache")
		}
		return cat
	}
	conf := config.Conf{Pool: "auto", Models: userRule}
	var out, errb bytes.Buffer
	if code := routeMain([]string{"-seed", "3", "Migrate the whole repo from Bubble Tea v1 to v2"}, &out, &errb, conf, rt, allInstalled); code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	txt := out.String()
	for _, want := range []string{
		"tier   complex",
		"pool   7 arms from pool in ", "(auto: 7 of 8 allowed arms, at most 4 per CLI), cheapest first\n",
		"agy:gemini-3.8-flash-low ", "codex:gpt-6-luna:low ", "codex:gpt-6-luna:high ", "agy:gemini-3.1-pro-high ",
	} {
		if !strings.Contains(txt, want) {
			t.Errorf("missing %q in:\n%s", want, txt)
		}
	}
	for _, never := range []string{"gpt-6.1-sol", "gpt-6-astra", "claude-opus", "opencode"} {
		if strings.Contains(txt, never) {
			t.Errorf("%s is not allowed by models = %s:\n%s", never, userRule, txt)
		}
	}
	// The cheapest arm is listed first and the strongest last (cost rank).
	rows := strings.Split(txt, "\n")
	var arms []string
	for _, r := range rows {
		if f := strings.Fields(strings.TrimPrefix(strings.TrimSpace(r), "→ ")); len(f) > 1 && strings.Contains(f[0], ":") && strings.HasSuffix(f[1], "%") {
			arms = append(arms, f[0])
		}
	}
	if len(arms) != 7 || arms[0] != "agy:gemini-3.8-flash-low" || arms[6] != "agy:gemini-3.1-pro-high" {
		t.Errorf("order: %v", arms)
	}

	// Nothing allowed: a clear error, not an empty pool.
	out.Reset()
	errb.Reset()
	conf.Models = "codex:gpt-99*"
	if code := routeMain([]string{"Fix the typo"}, &out, &errb, conf, rt, allInstalled); code != 2 || !strings.Contains(errb.String(), "no discovered model is allowed") {
		t.Errorf("code %d: %s", code, errb.String())
	}
}

func TestRouteWarnsAboutAnExplicitPoolAndSuggests(t *testing.T) {
	isolate(t)
	e := newEnv(t, nil, nil)
	cat := catalogOf(t)
	rt := fakeRouting(e, 0)
	rt.Catalog = func(bool) models.Catalog { return cat }
	conf := config.Conf{Pool: "agy:gemini-3.8-flash-medium codex:gpt-6-luna:low codex:gpt-6-lunaa:high", Models: userRule}
	t.Setenv("PANAL_POOL", "")
	var out, errb bytes.Buffer
	if code := routeMain([]string{"-seed", "1", "Fix the typo in README"}, &out, &errb, conf, rt, allInstalled); code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	txt := out.String()
	for _, want := range []string{
		"pool   3 arms from pool in ",
		`warn   codex:gpt-6-lunaa:high: codex's catalog has no model "gpt-6-lunaa"`,
		"also   discovered, not in the pool: agy:gemini-3.8-flash-low codex:gpt-6-luna:medium codex:gpt-6-luna:high agy:gemini-3.1-pro-low agy:gemini-3.1-pro-high; pool = auto would use them",
	} {
		if !strings.Contains(txt, want) {
			t.Errorf("missing %q in:\n%s", want, txt)
		}
	}
	// The explicit pool is used as written, unknown model included.
	if !strings.Contains(txt, "codex:gpt-6-lunaa:high ") {
		t.Errorf("the explicit pool changed:\n%s", txt)
	}
}

func TestModelsCommand(t *testing.T) {
	isolate(t)
	t.Setenv("PANAL_POOL", "")
	t.Setenv("PANAL_CHAIN", "")
	path := filepath.Join(t.TempDir(), models.FileName)
	now := func() time.Time { return time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC) }
	conf := config.Conf{Pool: "auto", Models: userRule, Exclude: "agy:gemini-3.1-pro-low", PoolMaxPerCLI: "3"}
	var out, errb bytes.Buffer
	// No cache yet: it is filled first.
	if code := modelsMain(nil, &out, &errb, conf, path, fakeCLIs(t), allInstalled, now); code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	txt := squash(out.String())
	for _, want := range []string{
		"cache " + path + "\n",
		"rules models = " + userRule + "\n",
		" exclude = agy:gemini-3.1-pro-low\n",
		" pool_max_per_cli = 3\n",
		"(auto: 6 of 7 allowed arms, at most 3 per CLI)",
		" 1 agy:gemini-3.8-flash-low 2.20 flash → class 2 · low +0.2 · v3.8\n",
		"agy — 14 models, cached 0 s ago (agy models)\n",
		" gemini-3.8-flash-medium - allowed, over the cap (pool_max_per_cli)\n",
		" gemini-3.1-pro-low - exclude: agy:gemini-3.1-pro-low\n",
		" gemini-3.6-flash-low - not in models\n",
		" gpt-6-luna low medium high xhigh max in pool #2 low, #3 medium, #5 high\n",
		" gpt-reserve low medium high xhigh max not in models\n",
		"opencode — no models: opencode api model.list listed no models; opencode models listed no models\n",
	} {
		if !strings.Contains(txt, want) {
			t.Errorf("missing %q in:\n%s", want, txt)
		}
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the cache was not written: %v", err)
	}

	// -refresh asks again even with a cache; an explicit pool lists its own order.
	out.Reset()
	calls := 0
	run := fakeCLIs(t)
	counting := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls++
		return run(ctx, name, args...)
	}
	conf = config.Conf{Pool: "codex:gpt-6-luna:high agy:gemini-3.8-flash-medium", Models: userRule}
	if code := modelsMain([]string{"-refresh"}, &out, &errb, conf, path, counting, allInstalled, now); code != 0 || calls < 3 {
		t.Fatalf("code %d, %d calls: %s", code, calls, errb.String())
	}
	txt = squash(out.String())
	for _, want := range []string{
		"pool 2 arms from pool in ",
		" 1 codex:gpt-6-luna:high 2.61 luna → class 2 · high +0.6 · v6\n",
		" 2 agy:gemini-3.8-flash-medium 2.40 ",
		"also discovered, not in the pool: agy:gemini-3.8-flash-low codex:gpt-6-luna:low",
		"allowed, not in the pool",
	} {
		if !strings.Contains(txt, want) {
			t.Errorf("missing %q in:\n%s", want, txt)
		}
	}
	if code := modelsMain([]string{"extra"}, &out, &errb, conf, path, counting, allInstalled, now); code != 2 {
		t.Errorf("an argument: code %d", code)
	}
}

func TestPoolAutoNeedsNoInstalledCheckOfItsOwn(t *testing.T) {
	// A CLI that is not installed has no models in the catalog, so an auto
	// pool never names it.
	onlyAgy := func(n string) (string, error) {
		if n == "agy" {
			return "/bin/agy", nil
		}
		return "", exec.ErrNotFound
	}
	cat := models.Refresh(context.Background(), fakeCLIs(t), onlyAgy, models.Catalog{}, models.CLIs, time.Second, time.Now())
	rt := routing{Catalog: func(bool) models.Catalog { return cat }}
	pool, info, err := rt.resolvePool("auto", "PANAL_POOL", config.Conf{}, onlyAgy, true)
	if err != nil || !info.Auto {
		t.Fatal(err)
	}
	for _, l := range pool {
		if l.CLI != "agy" {
			t.Errorf("%s is in the auto pool", l)
		}
	}
	if len(pool) != models.DefaultMaxPerCLI {
		t.Errorf("%d arms", len(pool))
	}
}
