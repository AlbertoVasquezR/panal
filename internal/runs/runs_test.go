package runs

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseCurrentFormat(t *testing.T) {
	rc := 0
	in := Run{ID: "20261002-101500", Agent: "codex", Model: "gpt-6-luna", Task: "Fix the table",
		Dir: `C:\wt`, Start: "2026-10-02T10:15:00-06:00", Status: Done, End: "2026-10-02T10:20:00-06:00", RC: &rc}
	dir := t.TempDir()
	path, err := WriteFile(dir, in)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "20261002-101500-codex.json" {
		t.Errorf("file name %q", filepath.Base(path))
	}
	got, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != Version || got.Status != Done || got.Task != "Fix the table" || got.RC == nil || *got.RC != 0 {
		t.Errorf("round trip: %+v", got)
	}
	if got.EndTime().Sub(got.StartTime()).Minutes() != 5 {
		t.Errorf("times: %v %v", got.StartTime(), got.EndTime())
	}
}

func TestParseLegacyFormat(t *testing.T) {
	data := []byte(`{"version":1,"sello":"20260924-081211","agente":"codex","modelo":"gpt-6-luna",
		"esfuerzo":"low","encargo":"Crea b.txt","encargo_archivo":"C:\\e.txt","carpeta":"C:\\wt","solo_leer":true,
		"pid":21152,"registro":"C:\\r.txt","inicio":"2026-09-24T08:12:11-06:00","estado":"sin_cuota",
		"fin":"2026-09-24T08:12:13-06:00","rc":75}`)
	r, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != "20260924-081211" || r.Agent != "codex" || r.Status != OutOfQuota || !r.ReadOnly ||
		r.TaskFile != `C:\e.txt` || r.Log != `C:\r.txt` || r.RC == nil || *r.RC != 75 {
		t.Errorf("legacy: %+v", r)
	}
	for es, en := range legacyStatus {
		r, err := Parse([]byte(`{"agente":"agy","estado":"` + es + `"}`))
		if err != nil || r.Status != en {
			t.Errorf("%s → %q (%v), want %q", es, r.Status, err, en)
		}
	}
}

func TestDirsIncludeLegacyOnlyWhenPresent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("PANAL_DATA", "")
	t.Setenv("PANAL_RUNS", "")
	t.Setenv("DELEGAR_ESTADOS", "")
	if d := Dirs(); len(d) != 1 || d[0] != filepath.Join(home, ".panal", "runs") {
		t.Errorf("without legacy: %v", d)
	}
	legacy := filepath.Join(home, ".ct-delegar", "estado")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if d := Dirs(); len(d) != 2 || d[1] != legacy {
		t.Errorf("with legacy: %v", d)
	}
}

func TestListMergesDirsNewestFirst(t *testing.T) {
	cur, old := t.TempDir(), t.TempDir()
	write := func(dir, name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The same run in both formats: the current one wins, wherever it is.
	write(old, "20261001-090000-codex.json", `{"sello":"20261001-090000","agente":"codex","inicio":"2026-10-01T09:00:00Z","estado":"termino"}`)
	write(cur, "20261001-090000-codex.json", `{"id":"20261001-090000","agent":"codex","start":"2026-10-01T09:00:00Z","status":"failed"}`)
	write(old, "20261001-080000-agy.json", `{"sello":"20261001-080000","agente":"agy","inicio":"2026-10-01T08:00:00Z","estado":"sin_cuota"}`)
	write(cur, "20261001-100000-agy.json", `{"id":"20261001-100000","agent":"agy","start":"2026-10-01T10:00:00Z","status":"running"}`)
	write(cur, "broken.json", `{`)
	write(cur, "notes.txt", `not a run`)

	fs := List([]string{cur, old, filepath.Join(cur, "missing")})
	if len(fs) != 4 {
		t.Fatalf("want 4 files, got %d: %+v", len(fs), fs)
	}
	want := []struct {
		id     string
		status Status
		legacy bool
	}{{"20261001-100000", Running, false}, {"20261001-090000", Failed, false}, {"20261001-080000", OutOfQuota, true}}
	for i, w := range want {
		if fs[i].Run.ID != w.id || fs[i].Run.Status != w.status || fs[i].Legacy != w.legacy {
			t.Errorf("%d: got %+v, want %+v", i, fs[i], w)
		}
	}
	if fs[3].Err == nil || filepath.Base(fs[3].Path) != "broken.json" {
		t.Errorf("the broken file goes last, with its error: %+v", fs[3])
	}
}

func TestNewIDSortableAndUnique(t *testing.T) {
	t0 := time.Date(2026, 10, 7, 12, 30, 26, 100*int(time.Millisecond), time.UTC)
	t1 := t0.Add(time.Millisecond)
	a, b := NewID(t0), NewID(t1)
	if !strings.HasPrefix(a, "20261007-123026.100-") || len(a) != len("20261007-123026.100-abcd1234") {
		t.Fatalf("format: %q", a)
	}
	if a >= b {
		t.Errorf("later millisecond should sort after: %q %q", a, b)
	}
	// Old second-resolution ids still sort before the same second's new ids.
	if old := "20261007-123026"; old >= a {
		t.Errorf("old id %q should sort before %q", old, a)
	}
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		id := NewID(t0)
		if seen[id] {
			t.Fatalf("duplicate NewID %q", id)
		}
		seen[id] = true
	}
}

func TestWriteFileExclusive(t *testing.T) {
	dir := t.TempDir()
	r := Run{ID: "20261007-123026.100-aaaa", Agent: "cursor", Status: Running, Start: "2026-10-07T12:30:26Z"}
	if _, err := WriteFileExclusive(dir, r); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteFileExclusive(dir, r); !os.IsExist(err) {
		t.Fatalf("second create: %v", err)
	}
	r.Status = Done
	if _, err := WriteFile(dir, r); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFile(filepath.Join(dir, r.ID+"-cursor.json"))
	if err != nil || got.Status != Done {
		t.Fatalf("overwrite after exclusive: %v %+v", err, got)
	}
}

func TestCreateExclusiveConcurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "same.task.md")
	const n = 16
	var wg sync.WaitGroup
	ok := make(chan bool, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := CreateExclusive(path, []byte(string(rune('A'+i))))
			ok <- err == nil
		}(i)
	}
	wg.Wait()
	close(ok)
	wins := 0
	for v := range ok {
		if v {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("exclusive creates that succeeded: %d, want 1", wins)
	}
}

func TestCloseIfDead(t *testing.T) {
	dir := t.TempDir()
	r := Run{ID: "20261007-123026", Agent: "cursor", Status: Running, PID: 99, Start: "2026-10-07T12:30:26Z"}
	path, err := WriteFile(dir, r)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 7, 12, 31, 0, 0, time.UTC)
	got, wrote := CloseIfDead(path, r, func(int) bool { return true }, now)
	if wrote || got.Status != Running {
		t.Fatalf("alive pid: wrote=%v %+v", wrote, got)
	}
	got, wrote = CloseIfDead(path, r, func(int) bool { return false }, now)
	if !wrote || got.Status != Failed || got.RC == nil || *got.RC != ExitFailed {
		t.Fatalf("dead pid: wrote=%v %+v", wrote, got)
	}
	onDisk, err := ReadFile(path)
	if err != nil || onDisk.Status != Failed {
		t.Fatalf("file: %v %+v", err, onDisk)
	}
	r.PID = 0
	if _, wrote := CloseIfDead(path, r, func(int) bool { return false }, now); wrote {
		t.Fatal("pid 0 should be left alone")
	}
}
