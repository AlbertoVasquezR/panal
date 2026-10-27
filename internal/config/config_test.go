package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const chain = "agy:gemini-3.8-flash-medium codex:gpt-6-luna:low"

func write(t *testing.T, dir, name, text string) string {
	t.Helper()
	r := filepath.Join(dir, name)
	if err := os.WriteFile(r, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestDisabledFromChain(t *testing.T) {
	got, ok := DisabledFromChain(chain)
	if !ok || !reflect.DeepEqual(got, []string{"cursor", "opencode"}) {
		t.Fatalf("got %v ok=%v, want [cursor opencode]", got, ok)
	}
	if _, ok := DisabledFromChain("  "); ok {
		t.Fatal("an empty chain must give ok=false")
	}
}

func TestResolver(t *testing.T) {
	d := t.TempDir()
	t.Setenv("PANAL_CHAIN", "")
	conf := write(t, d, "panal.conf", "# comment\nchain = "+chain+"   # trailing note\n")
	r := &Resolver{Path: conf}
	if got := r.Disabled(); !reflect.DeepEqual(got, []string{"cursor", "opencode"}) {
		t.Fatalf("from the chain: %v", got)
	}
	// PANAL_CHAIN wins over the file.
	t.Setenv("PANAL_CHAIN", "codex opencode")
	if got := r.Disabled(); !reflect.DeepEqual(got, []string{"agy", "cursor"}) {
		t.Fatalf("from PANAL_CHAIN: %v", got)
	}
	t.Setenv("PANAL_CHAIN", "")
	// The explicit list wins over the chain, and an empty "off =" means none.
	write(t, d, "panal.conf", "off = codex, agy\nchain = "+chain+"\n")
	r.signature = "" // the mtime may not change within the same clock tick
	if got := r.Disabled(); !reflect.DeepEqual(got, []string{"agy", "codex"}) {
		t.Fatalf("explicit: %v", got)
	}
	write(t, d, "panal.conf", "off =\nchain = "+chain+"\n")
	r.signature = ""
	if got := r.Disabled(); len(got) != 0 {
		t.Fatalf("empty must be none: %v", got)
	}
	// No file: none.
	if got := (&Resolver{Path: filepath.Join(d, "missing")}).Disabled(); len(got) != 0 {
		t.Fatalf("no file: %v", got)
	}
}

func TestReadEnglishKeys(t *testing.T) {
	d := t.TempDir()
	p := write(t, d, "panal.conf", `off = codex
theme = contrast
animation = no
alerts = bell
every = 5s
live_quota = no
ntfy = https://ntfy.sh/t
webhook = https://hook
claude_price = 15
chain = codex:gpt-6-luna:low opencode:opencode-go/glm-5
pool = codex::low agy codex::high
router = python classify.py   # a local classifier
serve = :8765
serve_token = s3cret
machine = pc2 http://10.0.0.5:8765 tok
machine = pc3 http://10.0.0.6:8765 tok
`)
	want := Conf{Disabled: []string{"codex"}, HasDisabled: true,
		Theme: "contrast", Animation: "no", Alerts: "bell", Interval: "5s", LiveQuota: "no",
		Ntfy: "https://ntfy.sh/t", Webhook: "https://hook", Price: "15", Chain: "codex:gpt-6-luna:low opencode:opencode-go/glm-5",
		Pool: "codex::low agy codex::high", Router: "python classify.py", Serve: ":8765", ServeToken: "s3cret",
		Machines: []string{"pc2 http://10.0.0.5:8765 tok", "pc3 http://10.0.0.6:8765 tok"}}
	if got := Read(p); !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestPathEnvVar(t *testing.T) {
	t.Setenv("PANAL_DATA", "/data")
	t.Setenv("PANAL_CONF", "")
	if got := Path(); got != filepath.Join("/data", FileName) {
		t.Fatalf("default is in runs.Home(): %s", got)
	}
	t.Setenv("PANAL_CONF", "/new/panal.conf")
	if got := Path(); got != "/new/panal.conf" {
		t.Fatalf("PANAL_CONF wins: %s", got)
	}
}

func TestValues(t *testing.T) {
	for in, want := range map[string]string{" Dark ": "dark", "contrast": "contrast", "auto": "auto", "": ""} {
		if got := Theme(in); got != want {
			t.Errorf("Theme(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]bool{"no": true, "off": true, "false": true, "0": true, "OFF": true, "": false, "yes": false, "on": false} {
		if got := IsOff(in); got != want {
			t.Errorf("IsOff(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestDelegateArms(t *testing.T) {
	t.Setenv("PANAL_CHAIN", "")
	t.Setenv("PANAL_POOL", "")
	cases := []struct {
		c    Conf
		want string
	}{
		{Conf{Chain: "codex agy"}, "codex agy"},
		{Conf{Chain: "auto", Pool: "codex::low agy"}, "codex::low agy"},
		{Conf{Pool: "agy opencode"}, "agy opencode"},
		{Conf{Chain: "codex", Pool: "agy"}, "codex"},
		{Conf{Chain: "auto"}, ""}, // auto without a pool: the default chain, so nothing is off
	}
	for _, c := range cases {
		if got := DelegateArms(c.c); got != c.want {
			t.Errorf("%+v: got %q, want %q", c.c, got, c.want)
		}
	}
	t.Setenv("PANAL_POOL", "opencode")
	if got := DelegateArms(Conf{Chain: "auto", Pool: "agy"}); got != "opencode" {
		t.Errorf("PANAL_POOL wins: %q", got)
	}
}

func TestAutoPoolOffAgents(t *testing.T) {
	t.Setenv("PANAL_CHAIN", "")
	t.Setenv("PANAL_POOL", "")
	cases := []struct {
		c    Conf
		want string // DelegateArms
		off  string // the agents left out of it
	}{
		{Conf{Pool: "auto"}, "", ""},
		{Conf{Pool: "auto", Models: "codex:gpt-6-luna* agy:gemini-3.8-flash-*"}, "agy codex", "cursor opencode"},
		{Conf{Pool: "auto", Models: "*:gpt-*"}, "", ""},
		{Conf{Pool: "auto", Exclude: "opencode agy:claude-*"}, "agy codex cursor", "opencode"},
		{Conf{Pool: "auto", Models: "codex", Exclude: "codex:*"}, "none", "agy codex cursor opencode"},
		{Conf{Chain: "auto", Pool: "Auto", Models: "agy:*"}, "agy", "codex cursor opencode"},
	}
	for _, c := range cases {
		got := DelegateArms(c.c)
		if got != c.want {
			t.Errorf("%+v: got %q, want %q", c.c, got, c.want)
		}
		off, _ := DisabledFromChain(got)
		if strings.Join(off, " ") != c.off {
			t.Errorf("%+v: off %v, want %q", c.c, off, c.off)
		}
	}
}

func TestReadModelKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "panal.conf")
	body := "pool = auto\nmodels = codex:gpt-6-luna*  agy:gemini-3.8-flash-*   # cheap ones\nexclude = agy:claude-*\npool_max_per_cli = 3\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c := Read(p)
	if c.Pool != "auto" || c.Models != "codex:gpt-6-luna*  agy:gemini-3.8-flash-*" || c.Exclude != "agy:claude-*" || c.PoolMaxPerCLI != "3" {
		t.Errorf("%+v", c)
	}
}
