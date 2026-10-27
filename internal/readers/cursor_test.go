package readers

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AlbertoVasquezR/panal/internal/runs"
	"github.com/AlbertoVasquezR/panal/internal/state"
)

func TestCursorReadFromRunFile(t *testing.T) {
	dir := t.TempDir()
	rc := 0
	r := runs.Run{
		Version: 2,
		ID:      "20261007-120000",
		Agent:   "cursor",
		Model:   "composer-2.5",
		Task:    "Add a test for the pager",
		Dir:     dir,
		Start:   "2026-10-07T12:00:00Z",
		End:     "2026-10-07T12:04:00Z",
		Status:  runs.Done,
		RC:      &rc,
		Log:     filepath.Join(dir, "cursor.txt"),
	}
	if _, err := runs.WriteFile(dir, r); err != nil {
		t.Fatal(err)
	}
	c := &Cursor{
		Delegate: &Delegate{
			Name:     "cursor",
			Dirs:     []string{dir},
			PIDAlive: func(int) bool { return false },
			Now:      func() time.Time { return time.Date(2026, 10, 7, 12, 10, 0, 0, time.UTC) },
		},
		Bin: "/opt/cursor-agent",
	}
	row := c.Read()
	if row.Agent != "cursor" || row.Status != state.Done || row.Model != "composer-2.5" {
		t.Fatalf("row: %+v", row)
	}
	if row.Task != "Add a test for the pager" {
		t.Fatalf("task: %s", row.Task)
	}
	if !strings.Contains(row.Quota.Summary, "no live quota") {
		t.Fatalf("quota note: %q", row.Quota.Summary)
	}
}

func TestCursorOutOfQuotaKeepsUsedUp(t *testing.T) {
	dir := t.TempDir()
	rc := 75
	r := runs.Run{
		Version: 2,
		ID:      "20261007-130000",
		Agent:   "cursor",
		Model:   "grok-4.7",
		Task:    "Migrate the config",
		Dir:     dir,
		Start:   "2026-10-07T13:00:00Z",
		End:     "2026-10-07T13:00:02Z",
		Status:  runs.OutOfQuota,
		RC:      &rc,
	}
	if _, err := runs.WriteFile(dir, r); err != nil {
		t.Fatal(err)
	}
	c := &Cursor{Delegate: &Delegate{
		Name: "cursor", Dirs: []string{dir},
		PIDAlive: func(int) bool { return false },
		Now:      func() time.Time { return time.Date(2026, 10, 7, 13, 15, 0, 0, time.UTC) },
	}}
	row := c.Read()
	if row.Status != state.OutOfQuota {
		t.Fatalf("status %v", row.Status)
	}
	if !strings.Contains(row.Quota.Summary, "used up") {
		t.Fatalf("quota: %q", row.Quota.Summary)
	}
}

func TestCursorNoData(t *testing.T) {
	c := &Cursor{Delegate: &Delegate{
		Name: "cursor", Dirs: []string{t.TempDir()},
		PIDAlive: func(int) bool { return false },
		Now:      time.Now,
	}}
	row := c.Read()
	if row.Status != state.NoData || row.Agent != "cursor" {
		t.Fatalf("%+v", row)
	}
	if !strings.Contains(row.Quota.Summary, "no live quota") {
		t.Fatalf("quota: %q", row.Quota.Summary)
	}
}

func TestCursorBinPathUsesLookPath(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	p := cursorBinPath()
	if p == "" {
		t.Fatal("empty path")
	}
}

func TestCursorAgentName(t *testing.T) {
	if NewCursor().Agent() != "cursor" {
		t.Fatal("agent name")
	}
}
