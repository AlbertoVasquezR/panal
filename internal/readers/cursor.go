package readers

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/AlbertoVasquezR/panal/internal/config"
	"github.com/AlbertoVasquezR/panal/internal/state"
)

// Cursor implements Reader for the Cursor CLI (cursor-agent, also invoked
// as agent). Status, model, task and last action come from panal delegate
// run files and their stream-json logs. cursor-agent has no documented
// usage query that spends nothing, so this reader never launches it and
// never invents a quota: the card stays without bars until a run records
// out_of_quota.
type Cursor struct {
	Delegate *Delegate
	Bin      string // resolved cursor-agent / agent path, for -doctor
}

// NewCursor creates the reader with the real system paths.
func NewCursor() *Cursor {
	return &Cursor{
		Delegate: NewDelegate("cursor"),
		Bin:      cursorBinPath(),
	}
}

func (c *Cursor) Agent() string {
	if c.Delegate != nil {
		return c.Delegate.Agent()
	}
	return "cursor"
}

// Read is what the last delegated cursor run says. There is no live quota.
func (c *Cursor) Read() state.Row {
	var row state.Row
	if c.Delegate != nil {
		row = c.Delegate.Read()
	} else {
		row = state.Row{Agent: "cursor", Status: state.NoData}
	}
	if row.Quota.Summary == "" && len(row.Quota.Bars) == 0 && row.Status != state.OutOfQuota {
		row.Quota.Summary = "no live quota (cursor-agent has none that spends nothing)"
	}
	return row
}

// cursorBinPath is the cursor-agent binary if it is installed, else a
// path -doctor can mark missing (the Windows installer dir, or
// ~/.local/bin/cursor-agent on Unix).
func cursorBinPath() string {
	if p, err := config.LookPath("cursor"); err == nil {
		return p
	}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		return filepath.Join(local, "cursor-agent", "cursor-agent.cmd")
	}
	if runtime.GOOS == "windows" {
		if u := os.Getenv("USERPROFILE"); u != "" {
			return filepath.Join(u, "AppData", "Local", "cursor-agent", "cursor-agent.cmd")
		}
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".local", "bin", "cursor-agent")
	}
	return "cursor-agent"
}
