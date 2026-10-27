package activity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func read(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestFromCodexLastCommand(t *testing.T) {
	txt, _ := FromCodex(read(t, "codex.jsonl"))
	if txt != "$ go test ./internal/ui -run TestPantallas" {
		t.Fatalf("got %q", txt)
	}
}

func TestFromCodexMessage(t *testing.T) {
	b := read(t, "codex.jsonl")
	// Only up to the agent's message: its first line comes out. The message
	// is real codex output, so it stays in the language the agent used.
	cut := strings.Index(string(b), `{"type":"item.started","item":{"id":"item_1"`)
	txt, _ := FromCodex(b[:cut])
	if txt != "«Voy a traer origin y revisar el diff pedido.»" {
		t.Fatalf("got %q", txt)
	}
}

func TestFromCodexPlan(t *testing.T) {
	b := []byte(`{"type":"item.updated","item":{"id":"item_3","type":"todo_list","items":[{"text":"read the code","completed":true},{"text":"write tests","completed":false},{"text":"run them","completed":false}]}}` + "\n")
	if txt, _ := FromCodex(b); txt != "plan 1/3 · write tests" {
		t.Fatalf("got %q", txt)
	}
}

func TestFromCodexFileChange(t *testing.T) {
	b := []byte(`{"type":"item.completed","item":{"id":"item_4","type":"file_change","changes":[{"path":"C:\\x\\a.go"},{"path":"C:\\x\\b.go"}]}}` + "\n")
	if txt, _ := FromCodex(b); txt != "edits a.go, b.go" {
		t.Fatalf("got %q", txt)
	}
}

func TestFromAgyLastTool(t *testing.T) {
	txt, at := FromAgy(read(t, "agy.log"))
	if txt != "Reading README.md" {
		t.Fatalf("got %q", txt)
	}
	if at.Hour() != 13 || at.Minute() != 39 || at.Second() != 40 || at.Month() != time.September || at.Day() != 26 {
		t.Fatalf("time read wrong: %v", at)
	}
}

func TestFromAgyNoCalls(t *testing.T) {
	if txt, _ := FromAgy([]byte("I0926 13:39:30.101010 771 main.go:88] nothing\n")); txt != "" {
		t.Fatalf("with no calls there is no activity, got %q", txt)
	}
}

func TestCleanCommand(t *testing.T) {
	cases := map[string]string{
		`"C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe" -NoProfile -Command 'git status'`: "git status",
		`/bin/bash -lc "ls -la"`:      "ls -la",
		`pwsh -Command Get-ChildItem`: "Get-ChildItem",
		`"C:\Program Files\PowerShell\7\pwsh.exe" -NoProfile -Command 'git status'`: "git status",
		`go   test ./...`: "go test ./...",
	}
	for in, want := range cases {
		if got := CleanCommand(in); got != want {
			t.Errorf("CleanCommand(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLatestReadsLogTail(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "20260926-133922-agy-gemini.txt")
	// Lots of noise before: the call must be found even though only the end is read.
	noise := strings.Repeat("I0926 13:00:00.000000 1 x.go:1] noise noise noise\n", 8000)
	if err := os.WriteFile(logPath+".log", append([]byte(noise), read(t, "agy.log")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if txt, at := Latest("agy", logPath); txt != "Reading README.md" || at.IsZero() {
		t.Fatalf("got (%q, %v)", txt, at)
	}
	if txt, _ := Latest("opencode", logPath); txt != "" {
		t.Fatalf("opencode has no activity reader, got %q", txt)
	}
	if txt, _ := Latest("codex", filepath.Join(dir, "does-not-exist")); txt != "" {
		t.Fatalf("with no file there is no activity, got %q", txt)
	}
}

func TestFromClaude(t *testing.T) {
	b := read(t, "claude.jsonl")
	txt, at := FromClaude(b)
	if txt != "Run all tests" || at.UTC().Format("15:04:05") != "01:41:10" {
		t.Fatalf("got (%q, %v)", txt, at)
	}
	// Before the Bash call: the read of ui.go.
	cut := strings.Index(string(b), `{"type":"assistant","timestamp":"2026-09-29T01:41:10`)
	if txt, _ := FromClaude(b[:cut]); txt != "reads ui.go" {
		t.Fatalf("got %q", txt)
	}
	for _, c := range []struct{ name, want string }{
		{"Edit", "edits a.go"}, {"Write", "writes a.go"}, {"Grep", "searches foo"}, {"Agent", "delegates to an agent"}, {"Odd", "Odd"},
	} {
		bl := claudeBlock{Type: "tool_use", Name: c.name}
		bl.Input.FilePath, bl.Input.Pattern = `C:\x\a.go`, "foo"
		if got := describeTool(bl); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestFromCodexTests(t *testing.T) {
	done := func(cmd string, rc int, output string) string {
		b, _ := json.Marshal(map[string]any{"type": "item.completed", "item": map[string]any{
			"type": "command_execution", "command": cmd, "exit_code": rc, "aggregated_output": output}})
		return string(b)
	}
	b := []byte(strings.Join([]string{
		done("go test ./...", 1, "--- FAIL: TestA\n--- FAIL: TestB\nFAIL"),
		done("git status", 0, ""),
	}, "\n"))
	if r, ok, found := FromCodexTests(b); !found || ok || r != "✗ 2 failures · go test ./..." {
		t.Fatalf("got (%q, %v, %v)", r, ok, found)
	}
	b = append(b, []byte("\n"+done(`"powershell.exe" -Command 'python -m pytest -q'`, 0, "5 passed"))...)
	if r, ok, _ := FromCodexTests(b); !ok || r != "✓ python -m pytest -q" {
		t.Fatalf("the last test run wins: (%q, %v)", r, ok)
	}
	if _, _, found := FromCodexTests([]byte(done("git diff", 0, ""))); found {
		t.Fatal("with no test commands there is no result")
	}
	if r, _, _ := FromCodexTests([]byte(done("npx vitest run", 1, "Tests  3 failed | 10 passed"))); r != "✗ 3 failures · npx vitest run" {
		t.Fatalf("vitest: %q", r)
	}
	if r, _, _ := FromCodexTests([]byte(done("go test ./x", 1, "--- FAIL: TestOnly"))); r != "✗ 1 failure · go test ./x" {
		t.Fatalf("one failure: %q", r)
	}
	if r, _, _ := FromCodexTests([]byte(done("go test ./x", 2, "build failed"))); r != "✗ failed · go test ./x" {
		t.Fatalf("no count: %q", r)
	}
}

func TestFromCursorLastTool(t *testing.T) {
	txt, _ := FromCursor(read(t, "cursor.jsonl"))
	if txt != "$ go test ./internal/ui -run TestPager" {
		t.Fatalf("got %q", txt)
	}
}

func TestFromCursorEarlierEvents(t *testing.T) {
	b := read(t, "cursor.jsonl")
	cut := strings.Index(string(b), `"shellToolCall"`)
	if cut < 0 {
		t.Fatal("sample has no shell call")
	}
	if txt, _ := FromCursor(b[:cut]); txt != "writes pager_test.go" {
		t.Fatalf("got %q", txt)
	}
	cut = strings.Index(string(b), `"writeToolCall"`)
	if txt, _ := FromCursor(b[:cut]); txt != "reads pager.go" {
		t.Fatalf("read: %q", txt)
	}
}

func TestLatestCursorJSONL(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "20261007-120000-cursor-composer.txt")
	if err := os.WriteFile(logPath+".jsonl", read(t, "cursor.jsonl"), 0o644); err != nil {
		t.Fatal(err)
	}
	if txt, _ := Latest("cursor", logPath); txt != "$ go test ./internal/ui -run TestPager" {
		t.Fatalf("got %q", txt)
	}
	if act, n := Repetition("cursor", logPath); !strings.HasPrefix(act, "$ go test") || n < 1 {
		t.Fatalf("repetition (%q, %d)", act, n)
	}
}
