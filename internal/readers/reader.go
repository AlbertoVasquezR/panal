// Package readers has one reader per data source. Each one reads local files
// or processes, never writes anything, and is tested against real samples in
// testdata/.
package readers

import (
	"time"

	"github.com/AlbertoVasquezR/panal/internal/live"
	"github.com/AlbertoVasquezR/panal/internal/state"
)

// Reader returns the current state of one agent. Read is called on every
// refresh (2 s), so it has to be cheap: no launching CLIs on every pass.
type Reader interface {
	Agent() string
	Read() state.Row
}

// All returns the readers in the order they are drawn, each one with its
// quotas kept up to date (Current).
func All() []Reader {
	return []Reader{
		Current(NewClaude()),
		Current(NewAgy()),
		Current(NewCodex()),
		Current(NewOpencode()),
		Current(NewCursor()),
	}
}

// Current wraps a reader so its quotas don't get stuck in the past: windows
// whose reset time has passed go back to 0 %, and if that frees the one that
// had it "out of quota", the agent becomes idle (available).
func Current(r Reader) Reader { return current{r, time.Now} }

type current struct {
	Reader
	now func() time.Time
}

func (c current) Read() state.Row {
	row := c.Reader.Read()
	var freed bool
	row.Quota, freed = row.Quota.Expire(c.now())
	if freed && row.Status == state.OutOfQuota {
		row.Status = state.Idle
	}
	return row
}

// WithCodexLive connects the live quota to the codex reader in the list.
func WithCodexLive(rs []Reader, v interface{ Latest() live.Reading }) {
	for _, r := range rs {
		if w, ok := r.(interface{ Unwrap() Reader }); ok {
			r = w.Unwrap()
		}
		if c, ok := r.(*Codex); ok {
			c.Live = v
		}
	}
}

// WithAgyLive connects the live quota to the agy reader in the list.
func WithAgyLive(rs []Reader, v interface{ LatestAgy() live.AgyReading }) {
	for _, r := range rs {
		if w, ok := r.(interface{ Unwrap() Reader }); ok {
			r = w.Unwrap()
		}
		if a, ok := r.(*Agy); ok {
			a.Live = v
		}
	}
}

// WithClaudeLive connects the live quota to the claude reader in the list.
func WithClaudeLive(rs []Reader, v interface{ LatestClaude() live.ClaudeReading }) {
	for _, r := range rs {
		if w, ok := r.(interface{ Unwrap() Reader }); ok {
			r = w.Unwrap()
		}
		if c, ok := r.(*Claude); ok {
			c.Live = v
		}
	}
}

// WithOpencodeLive connects the live quota to the opencode reader.
func WithOpencodeLive(rs []Reader, v interface{ LatestOpencode() live.OpencodeReading }) {
	for _, r := range rs {
		if w, ok := r.(interface{ Unwrap() Reader }); ok {
			r = w.Unwrap()
		}
		if o, ok := r.(*Opencode); ok {
			o.Live = v
		}
	}
}

// Unwrap returns the inner reader.
func (c current) Unwrap() Reader { return c.Reader }

// Source is a file or directory a reader reads from.
type Source struct {
	What string // "delegated runs", "Claude Code status line"…
	Path string
	Env  string // variable that moves it elsewhere
}

// Sources says where each reader reads from, without repeats: for the
// first-run screen, which says what it found and what is missing.
func Sources(rs []Reader) []Source {
	var out []Source
	seen := map[string]bool{}
	add := func(s Source) {
		if s.Path != "" && !seen[s.Path] {
			seen[s.Path] = true
			out = append(out, s)
		}
	}
	// The first directory is panal's own; any other one is the legacy
	// delegar.sh directory, listed only when it exists.
	runDirs := func(d *Delegate) {
		if d == nil {
			return
		}
		for i, dir := range d.Dirs {
			if i == 0 {
				add(Source{"delegated runs", dir, "PANAL_RUNS"})
			} else {
				add(Source{"legacy delegar.sh runs", dir, ""})
			}
		}
	}
	for _, r := range rs {
		if w, ok := r.(interface{ Unwrap() Reader }); ok {
			r = w.Unwrap()
		}
		switch x := r.(type) {
		case *Claude:
			add(Source{"Claude Code status line", x.Path, "PANAL_CLAUDE"})
		case *Agy:
			runDirs(x.Delegate)
			for i, dir := range x.LogDirs {
				if i == 0 {
					add(Source{"agy logs", dir, "PANAL_LOGS"})
				} else {
					add(Source{"legacy delegar.sh agy logs", dir, ""})
				}
			}
		case *Codex:
			runDirs(x.Delegate)
			add(Source{"codex sessions", x.SessionsDir, "CODEX_HOME"})
		case *Opencode:
			runDirs(x.Delegate)
			add(Source{"opencode log", x.LogPath, "OPENCODE_LOG"})
		case *Cursor:
			runDirs(x.Delegate)
			add(Source{"cursor-agent CLI", x.Bin, ""})
		}
	}
	return out
}
