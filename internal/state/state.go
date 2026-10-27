// Package state is the shared model: each reader returns one Row per agent and
// the UI only draws rows. No reader knows about the UI, and vice versa.
package state

import (
	"strings"
	"time"
)

// Status of an agent, in the order it matters to see it.
type Status int

const (
	NoData        Status = iota // the reader has not found anything yet
	Idle                        // not running now and nothing looks wrong
	Working                     // its process is alive and making progress
	Stuck                       // process alive but no progress
	OutOfQuota                  // the last run bounced off the quota
	NoPermission                // the last run aborted on a permission
	Done                        // the last run finished (does not say whether well)
	Failed                      // the last run finished with an error
	Orchestrating               // Claude Code only
)

// statusNames are the English words for each Status, in order.
var statusNames = [...]string{
	"no data", "idle", "working", "stuck",
	"out of quota", "no permission", "done", "failed", "orchestrating",
}

func (e Status) String() string {
	return statusNames[e]
}

// ParseStatus turns a status word back into a Status. ok is false if the
// word is unknown.
func ParseStatus(s string) (st Status, ok bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	for i := range statusNames {
		if s == statusNames[i] {
			return Status(i), true
		}
	}
	return NoData, false
}

// Quota is what is known about an agent's allowance. Exact only when the CLI
// exposes it (codex); otherwise it is the last event seen and when it was seen.
type Quota struct {
	Exact        bool
	UsedPct      float64   // 0..100, only if Exact
	ResetsAt     time.Time // zero if unknown
	Credits      string    // balance as the CLI gives it; empty if not applicable
	Summary      string    // what is drawn when not exact: "ok*", "Go exhausted"
	SeenAt       time.Time // when the data was seen
	Bars         []Bar     // one per known quota window (5 h, week), to draw them
	Spend        string    // credit burn rate: "today 2 cr · ~14/day · ~71 days"
	Extra        string    // something else to say next to the credits: "1 reset saved"
	Live         bool      // the CLI gave it when asked just now (not the last run)
	SpendWarning bool      // the balance will not last long: drawn in amber
}

// Bar is a quota window: how much has been used and when it resets.
type Bar struct {
	Name       string // "5h", "sem"
	UsedPct    float64
	ResetsAt   time.Time
	ExhaustsAt time.Time // zero if unknown or it does not run out before the reset
	// AlreadyReset: the reset time the agent reported has passed, so the
	// window went back to zero; the next one will be known when the agent
	// reports again.
	AlreadyReset bool
}

// Expire brings up to date a quota the agent reported a while ago: every
// window whose reset time has passed goes back to 0 %. Without this, an agent
// that has not run since yesterday would keep showing yesterday's usage.
// freed says whether any of those windows was exhausted. It does not touch the
// original bars (readers cache them): it returns a copy.
func (c Quota) Expire(now time.Time) (out Quota, freed bool) {
	out = c
	passed := func(t time.Time) bool { return !t.IsZero() && !now.Before(t) }
	if c.Exact && passed(c.ResetsAt) {
		freed = freed || c.UsedPct >= 100
		out.UsedPct = 0
	}
	if len(c.Bars) > 0 {
		out.Bars = append([]Bar(nil), c.Bars...)
		for i := range out.Bars {
			b := &out.Bars[i]
			if passed(b.ResetsAt) {
				freed = freed || b.UsedPct >= 100
				b.UsedPct, b.ResetsAt, b.ExhaustsAt, b.AlreadyReset = 0, time.Time{}, time.Time{}, true
			}
		}
	}
	return out, freed
}

// Row is the state of one agent at one instant.
type Row struct {
	Agent       string // "claude", "agy", "codex", "opencode", "cursor"
	Status      Status
	Model       string
	Task        string // one line
	Dir         string // worktree it works in
	Since       time.Time
	Start       time.Time // when the last run started
	End         time.Time // when the last run finished
	Review      string    // empty = not reviewed or no problems
	Unseen      bool      // its run finished and the user has not opened its detail (set by the UI)
	Activity    string    // the last thing it did while working: «$ go test ./...», «Reading README.md»
	Streak      int       // consecutive runs that finished without failing, most recent first (set by the UI)
	HasContext  bool      // the session's context usage is known (claude only)
	ContextPct  float64   // % of the context window used
	SessionCost string    // what the session has cost so far: «$15.51»; empty if unknown
	ActivityAt  time.Time // when it did it
	Tests       string    // the last time it ran tests: «✓ go test ./...», «✗ 3 failures · …»
	TestsOK     bool      // whether they passed
	Repeating   string    // the action it keeps repeating, if any
	Times       int       // how many times in a row (1 = not repeating)
	FullTask    string    // full text if there is one
	Quota       Quota
	Detail      string // free text for the detail panel
	Error       string // if the reader itself failed, why
}
