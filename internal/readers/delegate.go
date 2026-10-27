package readers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AlbertoVasquezR/panal/internal/activity"
	"github.com/AlbertoVasquezR/panal/internal/runs"
	"github.com/AlbertoVasquezR/panal/internal/state"
)

// Delegate implements Reader for agents run by `panal delegate` (or by the
// older delegar.sh helper): it reads their run files.
type Delegate struct {
	Name     string
	Dirs     []string // run directories, read in order (runs.Dirs())
	PIDAlive func(int) bool
	Now      func() time.Time
}

// NewDelegate creates a reader for the given agent.
func NewDelegate(agent string) *Delegate {
	return &Delegate{
		Name:     agent,
		Dirs:     runs.Dirs(),
		PIDAlive: realPIDAlive,
		Now:      time.Now,
	}
}

func (d *Delegate) Agent() string {
	return d.Name
}

func (d *Delegate) Read() state.Row {
	row := state.Row{
		Agent:  d.Name,
		Status: state.NoData,
	}

	nowFn := d.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	pidAliveFn := d.PIDAlive
	if pidAliveFn == nil {
		pidAliveFn = realPIDAlive
	}

	var (
		newest      *runs.Run
		newestStart time.Time
		errs        []string
	)

	for _, f := range runs.List(d.Dirs) {
		name := filepath.Base(f.Path)
		if f.Err != nil {
			if belongsToAgent(name, d.Name) {
				errs = append(errs, fmt.Sprintf("%s: broken JSON (%v)", name, f.Err))
			}
			continue
		}
		st := f.Run
		if st.Agent != "" && st.Agent != d.Name {
			continue
		}
		if st.Agent == "" && !belongsToAgent(name, d.Name) {
			continue
		}

		start, err := time.Parse(time.RFC3339, st.Start)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: invalid start time (%v)", name, err))
			continue
		}

		if st.Status == runs.Running && !f.Legacy {
			if closed, ok := runs.CloseIfDead(f.Path, st, pidAliveFn, nowFn()); ok {
				st = closed
			}
		}

		if newest == nil || start.After(newestStart) {
			newest = &st
			newestStart = start
		}
	}

	if len(errs) > 0 {
		row.Error = strings.Join(errs, "; ")
	}

	if newest == nil {
		return row
	}

	model := newest.Model
	if newest.Effort != "" {
		model = fmt.Sprintf("%s (%s)", newest.Model, newest.Effort)
	}
	row.Model = model
	row.Task = newest.Task
	row.Dir = newest.Dir
	row.Start = newestStart
	if newest.End != "" {
		if end, err := time.Parse(time.RFC3339, newest.End); err == nil {
			row.End = end
		}
	}

	now := nowFn()
	row.Tests, row.TestsOK, _ = activity.LatestTests(d.Name, newest.Log)

	switch newest.Status {
	case runs.Running:
		if !pidAliveFn(newest.PID) {
			row.Status = state.Failed
			row.Detail = "interrupted: its process is gone"
		} else {
			row.Since = newestStart
			row.Activity, row.ActivityAt = activity.Latest(d.Name, newest.Log)
			row.Repeating, row.Times = activity.Repetition(d.Name, newest.Log)
			if now.Sub(latestActivity(newest.Log, newestStart)) < 10*time.Minute {
				row.Status = state.Working
			} else {
				row.Status = state.Stuck
			}
		}
	case runs.Done:
		row.Status = state.Done
		row.Detail = finishedDetail(newest)
	case runs.OutOfQuota:
		row.Status = state.OutOfQuota
		row.Detail = finishedDetail(newest)
		if !row.End.IsZero() {
			row.Quota.SeenAt = row.End
			row.Quota.Summary = "used up (" + Ago(now.Sub(row.End)) + " ago)"
		} else {
			row.Quota.Summary = "used up"
		}
	case runs.NoPermission:
		row.Status = state.NoPermission
		row.Detail = finishedDetail(newest)
	case runs.Skipped:
		row.Status = state.Idle
		row.Detail = finishedDetail(newest)
	case runs.Timeout, runs.Failed, runs.Interrupted:
		row.Status = state.Failed
		row.Detail = finishedDetail(newest)
	default:
		row.Status = state.Failed
		row.Detail = finishedDetail(newest)
	}

	return row
}

// latestActivity is the most recent change among what each CLI writes while
// it works: codex fills <log>.jsonl and writes <log> only at the end; agy
// writes to <log>.log; opencode, to <log>. If none exists yet, it counts from
// the start of the attempt.
func latestActivity(log string, start time.Time) time.Time {
	return LatestActivity(log, start)
}

// LatestActivity is latestActivity, for whoever wants to close a run that was
// left running because its process died without writing its end.
func LatestActivity(log string, start time.Time) time.Time {
	latest := start
	if log == "" {
		return latest
	}
	for _, path := range []string{log, log + ".jsonl", log + ".log"} {
		if info, err := os.Stat(path); err == nil && info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	return latest
}

// Ago gives a readable duration: "40 s", "12 min", "3 h 5 min", "2 d".
func Ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d s", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 24*time.Hour:
		h := int(d.Hours())
		if m := int(d.Minutes()) % 60; m > 0 {
			return fmt.Sprintf("%d h %d min", h, m)
		}
		return fmt.Sprintf("%d h", h)
	default:
		return fmt.Sprintf("%d d", int(d.Hours()/24))
	}
}

func belongsToAgent(fileName, agent string) bool {
	if strings.HasSuffix(fileName, "-"+agent+".json") ||
		strings.HasPrefix(fileName, agent+"-") ||
		fileName == agent+".json" {
		return true
	}
	if strings.Contains(fileName, agent) {
		return true
	}
	others := []string{"agy", "codex", "opencode", "claude", "cursor"}
	for _, other := range others {
		if other != agent && strings.Contains(fileName, other) {
			return false
		}
	}
	return true
}

// LogPrefix marks the lines in Row.Detail that were copied from the log.
const LogPrefix = "\x00log\x00"

func finishedDetail(st *runs.Run) string {
	var b strings.Builder
	rcStr := "—"
	if st.RC != nil {
		rcStr = fmt.Sprintf("%d", *st.RC)
	}
	end := st.End
	if t, err := time.Parse(time.RFC3339, st.End); err == nil {
		end = t.Local().Format("02/01 15:04:05")
	}
	fmt.Fprintf(&b, "end: %s · rc: %s", end, rcStr)
	if st.Log != "" {
		fmt.Fprintf(&b, "\nlog: %s", st.Log)
		// Log lines carry LogPrefix: they are the agent's own text
		// ("- `go vet`: passed") and must not be read as label: value pairs.
		for _, l := range LastLines(st.Log, 5) {
			b.WriteString("\n" + LogPrefix + l)
		}
	}
	return b.String()
}

// LastLines returns the last n lines of a text file.
func LastLines(path string, n int) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	content := strings.TrimRight(string(data), "\r\n")
	if len(content) == 0 {
		return nil
	}
	lines := strings.Split(content, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], "\r")
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}
