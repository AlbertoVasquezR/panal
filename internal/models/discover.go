package models

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/AlbertoVasquezR/panal/internal/config"
)

// How each CLI is asked for its models. Every command only lists: none of
// them starts a turn, spends quota or changes the CLI's own files.
//
//	agy       agy models              "id<TAB>Display name" per line (after a
//	                                  "Fetching available models..." line on stderr)
//	codex     codex debug models      the raw model catalog as JSON
//	opencode  opencode api model.list GET /api/model on opencode's own server,
//	                                  through its CLI (it brings its own auth)
//	          opencode models         fallback: "provider/model" per line
//	cursor    cursor-agent models     "id - Display name" per line (Windows
//	                                  listing), or id per line with optional
//	                                  tab / two spaces + name; JSON too
//	          cursor-agent --list-models  the same listing; the binary is
//	                                  cursor-agent, agent, or a .cmd shim
//
// opencode 2.0's `opencode models` prints nothing at all (its provider list
// comes back empty even when models are configured), whether or not stdout
// is a terminal, so panal asks its server's model list instead and keeps
// `opencode models` as the fallback for versions where it works.
type query struct {
	args  []string
	parse func([]byte) ([]Model, error)
}

var queries = map[string][]query{
	"agy":      {{[]string{"models"}, ParseAgy}},
	"codex":    {{[]string{"debug", "models"}, ParseCodex}},
	"opencode": {{[]string{"api", "model.list"}, ParseOpencodeAPI}, {[]string{"models"}, ParseOpencodeList}},
	"cursor":   {{[]string{"models"}, ParseCursor}, {[]string{"--list-models"}, ParseCursor}},
}

// Runner runs a CLI and returns what it printed on stdout. Tests replace it
// with a fake; Exec is the real one.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// Exec runs the command without a shell, in the user's home directory (so
// no project's own config changes the answer), with no stdin and, on
// Windows, without a console window of its own.
func Exec(ctx context.Context, name string, args ...string) ([]byte, error) {
	if len(config.Bins[name]) > 0 {
		if p, err := config.LookPath(name); err == nil {
			name = p
		}
	}
	// cursor-agent on Windows is a .cmd shim; Go 1.19+ can run those, and
	// we also route them through cmd.exe /c so stdout is captured the same
	// way as a terminal `cursor-agent models`.
	if runtime.GOOS == "windows" {
		name, args = windowsCmd(name, args)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	if h, err := os.UserHomeDir(); err == nil {
		cmd.Dir = h
	}
	hideWindow(cmd)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &limited{w: &errb, n: 4096}
	// opencode on Windows is an npm .cmd shim: when the deadline kills it,
	// its child may keep the pipes open. Don't wait on them for long.
	cmd.WaitDelay = 3 * time.Second
	err := cmd.Run()
	if ctx.Err() != nil {
		return out.Bytes(), fmt.Errorf("no answer in time")
	}
	if err != nil {
		if line := firstLine(errb.String()); line != "" {
			return out.Bytes(), fmt.Errorf("%v: %s", err, line)
		}
		return out.Bytes(), err
	}
	return out.Bytes(), nil
}

type limited struct {
	w *bytes.Buffer
	n int
}

func (l *limited) Write(p []byte) (int, error) {
	if room := l.n - l.w.Len(); room > 0 {
		if len(p) > room {
			l.w.Write(p[:room])
		} else {
			l.w.Write(p)
		}
	}
	return len(p), nil
}

// windowsCmd runs a .cmd / .bat shim through cmd.exe /c so model listing
// from %LOCALAPPDATA%\cursor-agent\cursor-agent.cmd captures stdout.
func windowsCmd(name string, args []string) (string, []string) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".cmd", ".bat":
		return "cmd.exe", append([]string{"/c", name}, args...)
	}
	return name, args
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			if r := []rune(l); len(r) > 160 {
				l = string(r[:159]) + "…"
			}
			return l
		}
	}
	return ""
}

// Command is how the CLI is asked, for messages ("codex debug models").
func Command(cli string) string {
	qs := queries[cli]
	if len(qs) == 0 {
		return ""
	}
	return cliLabel(cli) + " " + strings.Join(qs[0].args, " ")
}

func cliLabel(cli string) string {
	if cli == "cursor" {
		return "cursor-agent"
	}
	return cli
}

// Discover asks one CLI for its models: each of its queries in turn, until
// one finds models. The entry's Error says why none did.
func Discover(ctx context.Context, run Runner, cli string, now time.Time) *CLI {
	e := &CLI{Command: Command(cli), Tried: now}
	qs := queries[cli]
	if len(qs) == 0 {
		e.Error = "Panal does not know how to list " + cli + "'s models"
		return e
	}
	var why []string
	for _, q := range qs {
		cmd := cliLabel(cli) + " " + strings.Join(q.args, " ")
		out, err := run(ctx, cli, q.args...)
		if err != nil {
			why = append(why, cmd+": "+err.Error())
			if ctx.Err() != nil {
				break
			}
			continue
		}
		ms, err := q.parse(out)
		if err != nil {
			why = append(why, cmd+": "+err.Error())
			continue
		}
		if len(ms) == 0 {
			why = append(why, cmd+" listed no models")
			continue
		}
		e.Command, e.At, e.Models = cmd, now, ms
		return e
	}
	e.Error = strings.Join(why, "; ")
	return e
}

// Refresh asks every CLI in clis (in parallel, each with its own timeout)
// and merges the answers into a copy of cat. A CLI that is not installed is
// recorded as such; one that fails keeps the models it had, with the error.
func Refresh(ctx context.Context, run Runner, lookPath func(string) (string, error), cat Catalog, clis []string, timeout time.Duration, now time.Time) Catalog {
	out := Catalog{Version: Version, CLIs: map[string]*CLI{}}
	for k, v := range cat.CLIs {
		c := *v
		out.CLIs[k] = &c
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, cli := range clis {
		if _, err := lookPath(cli); err != nil {
			out.CLIs[cli] = &CLI{Command: Command(cli), Tried: now, Error: "not installed (or not in PATH)", Models: []Model{}}
			continue
		}
		wg.Add(1)
		go func(cli string) {
			defer wg.Done()
			c, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			e := Discover(c, run, cli, now)
			e.Error = strings.ReplaceAll(e.Error, "no answer in time", "no answer in "+timeout.String())
			mu.Lock()
			defer mu.Unlock()
			if old := out.CLIs[cli]; e.Error != "" && old != nil && len(old.Models) > 0 {
				old.Tried, old.Error = now, e.Error
				return
			}
			if e.Models == nil {
				e.Models = []Model{}
			}
			out.CLIs[cli] = e
		}(cli)
	}
	wg.Wait()
	return out
}

// Ensure loads the cache and refreshes only the installed CLIs it has no
// entry for (all of them when there is no cache yet), with a short timeout:
// what `panal route` and `panal delegate -c auto` use.
func Ensure(ctx context.Context, path string, run Runner, lookPath func(string) (string, error), timeout time.Duration, now time.Time) Catalog {
	cat, _ := Load(path)
	var missing []string
	for _, cli := range CLIs {
		if cat.Get(cli) != nil {
			continue
		}
		if _, err := lookPath(cli); err == nil {
			missing = append(missing, cli)
		}
	}
	if len(missing) == 0 {
		return cat
	}
	cat = Refresh(ctx, run, lookPath, cat, missing, timeout, now)
	_ = cat.Save(path)
	return cat
}

// RefreshInBackground refreshes, in its own goroutine, the CLIs whose
// catalog is older than maxAge, and saves the cache. It returns at once;
// the channel closes when it is done. The dashboard calls it on start.
func RefreshInBackground(ctx context.Context, path string, run Runner, lookPath func(string) (string, error), timeout, maxAge time.Duration, now func() time.Time) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		cat, _ := Load(path)
		var stale []string
		for _, cli := range CLIs {
			if cat.Stale(cli, now(), maxAge) {
				stale = append(stale, cli)
			}
		}
		if len(stale) == 0 {
			return
		}
		cat = Refresh(ctx, run, lookPath, cat, stale, timeout, now())
		if ctx.Err() != nil {
			return
		}
		// Merge with what may have been written meanwhile (panal models
		// -refresh in another terminal): only the CLIs refreshed here change.
		latest, _ := Load(path)
		for _, cli := range stale {
			if e := cat.Get(cli); e != nil {
				latest.CLIs[cli] = e
			}
		}
		_ = latest.Save(path)
	}()
	return done
}

// ------------------------------------------------------------- parsers --

// ParseAgy reads `agy models`: "id<TAB>Display name" per line. Other lines
// (the "Fetching available models..." notice) are skipped.
func ParseAgy(b []byte) ([]Model, error) {
	var out []Model
	s := bufio.NewScanner(bytes.NewReader(b))
	for s.Scan() {
		id, name, ok := strings.Cut(strings.TrimRight(s.Text(), "\r"), "\t")
		id = strings.TrimSpace(id)
		if !ok || id == "" || strings.ContainsAny(id, " /") {
			continue
		}
		out = append(out, Model{ID: id, Name: strings.TrimSpace(name)})
	}
	return out, s.Err()
}

// ParseCodex reads `codex debug models`: {"models": [{slug, display_name,
// description, default_reasoning_level, supported_reasoning_levels[].effort,
// visibility, priority, upgrade}, ...]}.
func ParseCodex(b []byte) ([]Model, error) {
	var doc struct {
		Models []struct {
			Slug         string `json:"slug"`
			DisplayName  string `json:"display_name"`
			Description  string `json:"description"`
			DefaultLevel string `json:"default_reasoning_level"`
			Levels       []struct {
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
			Visibility string `json:"visibility"`
			Priority   int    `json:"priority"`
			Upgrade    *struct {
				Model        string `json:"model"`
				Migration    string `json:"migration_markdown"`
				RetirementAt string `json:"retirement_at"`
			} `json:"upgrade"`
		} `json:"models"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("not the JSON catalog Panal expects: %v", err)
	}
	var out []Model
	for _, m := range doc.Models {
		if m.Slug == "" {
			continue
		}
		x := Model{ID: m.Slug, Name: m.DisplayName, Description: m.Description,
			DefaultEffort: m.DefaultLevel, Priority: m.Priority, Hidden: m.Visibility == "hide"}
		for _, l := range m.Levels {
			if l.Effort != "" {
				x.Efforts = append(x.Efforts, strings.ToLower(l.Effort))
			}
		}
		if u := m.Upgrade; u != nil {
			switch {
			case u.RetirementAt != "" && len(u.RetirementAt) >= 10:
				x.Retiring = "retires " + u.RetirementAt[:10]
			case u.Migration != "":
				x.Retiring = firstLine(u.Migration)
			default:
				x.Retiring = "replaced by " + u.Model
			}
			if u.Model != "" && !strings.Contains(x.Retiring, u.Model) {
				x.Retiring += " (use " + u.Model + ")"
			}
		}
		out = append(out, x)
	}
	return out, nil
}

// ParseOpencodeAPI reads `opencode api model.list`: {"location": {...},
// "data": [{id, providerID, name, status, enabled, cost[{input, output,
// tier?}], variants[{id}]}, ...]}. Settings (base URLs, keys) are never read.
func ParseOpencodeAPI(b []byte) ([]Model, error) {
	var doc struct {
		Data []struct {
			ID       string `json:"id"`
			Provider string `json:"providerID"`
			Name     string `json:"name"`
			Status   string `json:"status"`
			Enabled  *bool  `json:"enabled"`
			Cost     []struct {
				Output *float64        `json:"output"`
				Tier   json.RawMessage `json:"tier"`
			} `json:"cost"`
			Variants []struct {
				ID string `json:"id"`
			} `json:"variants"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("not the JSON model list Panal expects: %v", err)
	}
	var out []Model
	for _, m := range doc.Data {
		if m.ID == "" || m.Provider == "" {
			continue
		}
		x := Model{ID: m.Provider + "/" + m.ID, Name: m.Name, Hidden: m.Enabled != nil && !*m.Enabled}
		if m.Status == "deprecated" {
			x.Retiring = "deprecated"
		}
		for _, c := range m.Cost {
			if len(c.Tier) == 0 && c.Output != nil {
				p := *c.Output
				x.Price = &p
				break
			}
		}
		for _, v := range m.Variants {
			if v.ID != "" {
				x.Efforts = append(x.Efforts, strings.ToLower(v.ID))
			}
		}
		out = append(out, x)
	}
	return out, nil
}

// ParseOpencodeList reads `opencode models`: "provider/model" per line.
func ParseOpencodeList(b []byte) ([]Model, error) {
	var out []Model
	s := bufio.NewScanner(bytes.NewReader(b))
	for s.Scan() {
		l := strings.TrimSpace(s.Text())
		if l == "" || strings.ContainsAny(l, " \t") || !strings.Contains(l, "/") {
			continue
		}
		out = append(out, Model{ID: l})
	}
	return out, s.Err()
}

// ansiCSI strips ECMA-48 / ANSI color and cursor codes. cursor-agent's
// models listing on Windows may color the header and ids.
var ansiCSI = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)

func stripANSI(s string) string { return ansiCSI.ReplaceAllString(s, "") }

// ParseCursor reads `cursor-agent models` or `cursor-agent --list-models`.
// Live Windows output is:
//
//	Available models
//
//	auto - Auto (current, default)
//	gpt-5.3-codex-low - Codex 5.3 Low
//
// with optional ANSI codes and CRLF. Older listings (id per line, optional
// tab or two spaces + display name) and a JSON array / {models:[{id,name}]}
// are still accepted. Headers, blank lines and trailing "(current, default)"
// annotations are skipped.
func ParseCursor(b []byte) ([]Model, error) {
	b = []byte(stripANSI(string(bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")))))
	if ms, err := parseCursorJSON(b); err == nil && len(ms) > 0 {
		return ms, nil
	}
	var out []Model
	seen := map[string]bool{}
	s := bufio.NewScanner(bytes.NewReader(b))
	for s.Scan() {
		l := strings.TrimSpace(strings.TrimRight(s.Text(), "\r"))
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		low := strings.ToLower(l)
		if cursorListingHeader(low) {
			continue
		}
		id, name := splitCursorModelLine(l)
		id = strings.TrimSpace(id)
		if !cursorModelID(id) || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, Model{ID: id, Name: cursorModelName(name)})
	}
	return out, s.Err()
}

func cursorListingHeader(low string) bool {
	switch {
	case strings.HasPrefix(low, "available model"):
		return true
	case low == "available models", low == "models":
		return true
	case strings.HasPrefix(low, "model") && !strings.Contains(low, "-"):
		return true
	}
	return false
}

// splitCursorModelLine: "id - Name", then tab, then two-or-more spaces,
// else the whole line is the id.
func splitCursorModelLine(l string) (id, name string) {
	if id, name, ok := strings.Cut(l, "\t"); ok {
		return id, name
	}
	if i := strings.Index(l, " - "); i > 0 {
		return l[:i], l[i+3:]
	}
	if i := strings.Index(l, "  "); i > 0 {
		return l[:i], strings.TrimSpace(l[i:])
	}
	return l, ""
}

func cursorModelName(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.LastIndex(name, " ("); i > 0 && strings.HasSuffix(name, ")") {
		return strings.TrimSpace(name[:i])
	}
	return name
}

func parseCursorJSON(b []byte) ([]Model, error) {
	trim := bytes.TrimSpace(b)
	if len(trim) == 0 || (trim[0] != '{' && trim[0] != '[') {
		return nil, fmt.Errorf("not JSON")
	}
	var ids []string
	if err := json.Unmarshal(trim, &ids); err == nil {
		var out []Model
		for _, id := range ids {
			if cursorModelID(id) {
				out = append(out, Model{ID: id})
			}
		}
		return out, nil
	}
	var doc struct {
		Models []struct {
			ID    string `json:"id"`
			Slug  string `json:"slug"`
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"models"`
	}
	if err := json.Unmarshal(trim, &doc); err != nil {
		var arr []struct {
			ID   string `json:"id"`
			Slug string `json:"slug"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(trim, &arr); err != nil {
			return nil, err
		}
		var out []Model
		for _, m := range arr {
			id := firstNonEmpty(m.ID, m.Slug)
			if cursorModelID(id) {
				out = append(out, Model{ID: id, Name: m.Name})
			}
		}
		return out, nil
	}
	var out []Model
	for _, m := range doc.Models {
		id := firstNonEmpty(m.ID, m.Slug, m.Model)
		if cursorModelID(id) {
			out = append(out, Model{ID: id, Name: m.Name})
		}
	}
	return out, nil
}

func cursorModelID(id string) bool {
	if id == "" || strings.ContainsAny(id, " \t") {
		return false
	}
	if strings.ContainsAny(id, "[](){}") && !strings.Contains(id, "[") {
		return false
	}
	for _, r := range id {
		if r > 127 {
			return false
		}
	}
	return strings.ContainsAny(id, "-_./") || id == "auto"
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
