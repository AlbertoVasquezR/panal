// Package config reads ~/.panal/panal.conf (or PANAL_CONF): which agents are
// off and the UI preferences (theme, animation, alerts, every).
//
// The off agents can be listed by hand (off = opencode) or found from the
// default chain of `panal delegate` (PANAL_CHAIN or chain = ...): whatever is
// not in the chain is off, and if someone puts it back in the chain, the
// dashboard notices on its own.
package config

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/AlbertoVasquezR/panal/internal/runs"
)

// Known are the agents `panal delegate` knows how to run.
var Known = []string{"agy", "codex", "cursor", "opencode"}

// Bins are PATH names to try for a Known CLI. A CLI not listed is looked
// up under its own name. cursor is installed as cursor-agent, or as the
// agent alias; on Windows the installer also drops .cmd shims under
// %LOCALAPPDATA%\cursor-agent.
var Bins = map[string][]string{
	"cursor": {"cursor-agent", "agent"},
}

// FileName is the config file's name inside runs.Home() (~/.panal).
const FileName = "panal.conf"

// Path of the file: PANAL_CONF, else ~/.panal/panal.conf.
func Path() string {
	if r := os.Getenv("PANAL_CONF"); r != "" {
		return r
	}
	return filepath.Join(runs.Home(), FileName)
}

// Conf is what the file says.
type Conf struct {
	Disabled    []string // "off = a, b"; wins over the chain
	HasDisabled bool     // the off key appeared (even empty: "none")

	// UI preferences; command-line options (and environment variables) win
	// over them. Empty = not said.
	Theme     string // "theme = contrast"
	Animation string // "animation = no"
	Alerts    string // "alerts = bell"
	Interval  string // "every = 5s"
	LiveQuota string // "live_quota = no": do not ask the CLIs for their quota
	Ntfy      string // "ntfy = https://ntfy.sh/my-topic": alerts also to the phone
	Webhook   string // "webhook = https://…": alerts also as a JSON POST (Slack, Discord…)
	Price     string // "claude_price = 15": $ per million tokens, to estimate savings

	// `panal delegate`: its default fallback chain, "cli:model[:effort] ...".
	// PANAL_CHAIN and -c win over it.
	Chain string // "chain = codex:gpt-5 agy opencode"
	// The router (panal delegate -c auto): the arms it may pick, cheapest
	// first (PANAL_POOL wins), and an optional external classifier command
	// (PANAL_ROUTER wins).
	Pool   string // "pool = codex::low agy codex::high", or "pool = auto" (the discovered models)
	Router string // "router = python classify.py"
	// The discovered models (internal/models): which ones `pool = auto` may
	// use, which never, and how many arms per CLI at most.
	Models        string // "models = codex:gpt-6-luna* agy:gemini-3.8-flash-*"
	Exclude       string // "exclude = agy:claude-*"
	PoolMaxPerCLI string // "pool_max_per_cli = 4"

	// Several machines.
	Serve      string   // "serve = :8765": the dashboard also serves its status there
	ServeToken string   // "serve_token = …": without it nothing is served
	Machines   []string // "machine = pc2 http://10.0.0.5:8765 token", one per line
}

// Read reads the file; if it does not exist, an empty Conf. Lines are
// "key = value"; # starts a comment, also at the end of a line.
func Read(path string) Conf {
	var c Conf
	f, err := os.Open(path)
	if err != nil {
		return c
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		l := strings.TrimSpace(s.Text())
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		// Comment at the end of the line: "key = value   # note".
		if i := strings.Index(v, " #"); i >= 0 {
			v = v[:i]
		}
		k, v = strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(v)
		switch k {
		case "off":
			c.HasDisabled = true
			c.Disabled = list(v)
		case "theme":
			c.Theme = v
		case "animation":
			c.Animation = v
		case "alerts":
			c.Alerts = v
		case "every":
			c.Interval = v
		case "live_quota":
			c.LiveQuota = v
		case "ntfy":
			c.Ntfy = v
		case "webhook":
			c.Webhook = v
		case "claude_price":
			c.Price = v
		case "chain":
			c.Chain = v
		case "pool":
			c.Pool = v
		case "router":
			c.Router = v
		case "models":
			c.Models = v
		case "exclude":
			c.Exclude = v
		case "pool_max_per_cli":
			c.PoolMaxPerCLI = v
		case "serve":
			c.Serve = v
		case "serve_token":
			c.ServeToken = v
		case "machine":
			c.Machines = append(c.Machines, v)
		}
	}
	return c
}

// IsOff says whether a yes/no value means no: no, off, false, 0. Empty is
// not off.
func IsOff(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "no", "off", "false", "0":
		return true
	}
	return false
}

// Theme normalizes a theme value: lower case, no surrounding spaces.
func Theme(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

func list(v string) []string {
	var out []string
	for _, a := range strings.Split(v, ",") {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return out
}

// DisabledFromChain returns the known agents that do not appear in a
// `panal delegate` chain ("cli:model[:effort] ..."). ok=false if the chain is empty.
func DisabledFromChain(chain string) ([]string, bool) {
	links := strings.Fields(chain)
	if len(links) == 0 {
		return nil, false
	}
	in := map[string]bool{}
	for _, link := range links {
		cli, _, _ := strings.Cut(link, ":")
		in[cli] = true
	}
	var out []string
	for _, a := range Known {
		if !in[a] {
			out = append(out, a)
		}
	}
	return out, true
}

// Resolver gives the current off agents and re-reads the file only when it
// (or PANAL_CHAIN) changes, because it is asked on every refresh.
type Resolver struct {
	Path string

	mu        sync.Mutex
	signature string
	last      []string
}

func signature(paths ...string) string {
	var b strings.Builder
	for _, r := range paths {
		if st, err := os.Stat(r); err == nil {
			b.WriteString(r + st.ModTime().Format(time.RFC3339Nano))
		}
		b.WriteString("|")
	}
	return b.String()
}

// DelegateArms is what panal delegate may run, for finding the off agents:
// its chain (PANAL_CHAIN wins over chain = ...), or, when the chain is the
// router ("auto", or no chain with a pool), the router's pool (PANAL_POOL
// wins over pool = ...). With `pool = auto` that is the CLIs the models
// key names (see AutoPoolCLIs).
func DelegateArms(c Conf) string {
	chain := os.Getenv("PANAL_CHAIN")
	if strings.TrimSpace(chain) == "" {
		chain = c.Chain
	}
	if IsAuto(chain) || strings.TrimSpace(chain) == "" {
		pool := os.Getenv("PANAL_POOL")
		if strings.TrimSpace(pool) == "" {
			pool = c.Pool
		}
		if IsAuto(pool) {
			return AutoPoolCLIs(c)
		}
		if strings.TrimSpace(pool) != "" || IsAuto(chain) {
			return pool
		}
	}
	return chain
}

// AutoPoolCLIs: the CLIs a `pool = auto` may use, as a space-separated
// list: those the models key names (all of them when it is unset or uses a
// wildcard CLI), minus those exclude drops whole ("exclude = opencode").
// Empty means every CLI.
func AutoPoolCLIs(c Conf) string {
	split := func(s string) []string {
		return strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' })
	}
	in := map[string]bool{}
	all := true
	for _, p := range split(c.Models) {
		cli, _, _ := strings.Cut(strings.ToLower(p), ":")
		if strings.ContainsAny(cli, "*?") {
			all = true
			in = map[string]bool{}
			break
		}
		all = false
		in[cli] = true
	}
	if all {
		for _, a := range Known {
			in[a] = true
		}
	}
	for _, p := range split(c.Exclude) {
		cli, model, has := strings.Cut(strings.ToLower(p), ":")
		if !has || model == "*" {
			delete(in, cli)
		}
	}
	if all && len(in) == len(Known) {
		return ""
	}
	var out []string
	for _, a := range Known {
		if in[a] {
			out = append(out, a)
		}
	}
	if len(out) == 0 {
		return "none"
	}
	return strings.Join(out, " ")
}

// IsAuto: the chain is "auto", the router.
func IsAuto(chain string) bool { return strings.EqualFold(strings.TrimSpace(chain), "auto") }

// Disabled according to the file: the explicit list, or the agents left out of
// the delegate chain (see DelegateArms).
func (r *Resolver) Disabled() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := Read(r.Path)
	chain := DelegateArms(c)
	f := signature(r.Path) + chain
	if f == r.signature {
		return r.last
	}
	var out []string
	switch {
	case c.HasDisabled:
		out = c.Disabled
	default:
		out, _ = DisabledFromChain(chain)
	}
	sort.Strings(out)
	r.signature, r.last = f, out
	return out
}

// LookPath finds a Known CLI on PATH. For cursor it tries cursor-agent,
// then agent, then the Windows install directory.
func LookPath(name string) (string, error) {
	return LookWith(exec.LookPath, name)
}

// LookWith is LookPath with a custom lookup, for tests.
func LookWith(look func(string) (string, error), name string) (string, error) {
	if look == nil {
		look = exec.LookPath
	}
	names := Bins[name]
	if len(names) == 0 {
		return look(name)
	}
	var first error
	for _, n := range names {
		p, err := look(n)
		if err == nil {
			return p, nil
		}
		if first == nil {
			first = err
		}
	}
	if p := windowsCursorInstall(); p != "" {
		return p, nil
	}
	if first == nil {
		first = exec.ErrNotFound
	}
	return "", first
}

// windowsCursorInstall is %LOCALAPPDATA%\cursor-agent\cursor-agent.cmd
// (or agent.cmd / .exe), the Windows installer's default location. It is
// also checked when LOCALAPPDATA is set on other OSes, so tests can point
// at a fake tree.
func windowsCursorInstall() string {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" && runtime.GOOS == "windows" {
		if u := os.Getenv("USERPROFILE"); u != "" {
			local = filepath.Join(u, "AppData", "Local")
		}
	}
	if local == "" {
		return ""
	}
	dir := filepath.Join(local, "cursor-agent")
	for _, n := range []string{"cursor-agent.cmd", "cursor-agent.exe", "agent.cmd", "agent.exe"} {
		p := filepath.Join(dir, n)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}
