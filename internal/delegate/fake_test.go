package delegate

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AlbertoVasquezR/panal/internal/runs"
)

// The test binary doubles as a fake codex, agy, opencode and cursor: when
// PANAL_FAKE_CLI=1 it behaves like the CLI its arguments belong to, in the
// mode PANAL_FAKE_<CLI> says (ok, quota, perm, fail, hang, talk). It never
// talks to any real agent.

func TestMain(m *testing.M) {
	if os.Getenv("PANAL_FAKE_CLI") == "1" {
		os.Exit(fakeCLI(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func fakeCLI(args []string) int {
	cli := "agy"
	switch {
	case len(args) > 0 && args[0] == "exec":
		cli = "codex"
	case len(args) > 0 && args[0] == "run":
		cli = "opencode"
	case hasFlag(args, "--output-format") || hasFlag(args, "--workspace") || hasFlag(args, "--trust"):
		cli = "cursor"
	}
	stdin := ""
	if len(args) > 0 && args[len(args)-1] == "-" {
		b, _ := io.ReadAll(os.Stdin)
		stdin = string(b)
	}
	rec := os.Getenv("PANAL_FAKE_RECORD")
	_ = os.WriteFile(filepath.Join(rec, cli+".args"), []byte(strings.Join(args, "\n")), 0o644)
	_ = os.WriteFile(filepath.Join(rec, cli+".stdin"), []byte(stdin), 0o644)
	// What the run file said while the agent was running.
	if ms, _ := filepath.Glob(filepath.Join(os.Getenv("PANAL_FAKE_RUNS"), "*-"+cli+".json")); len(ms) > 0 {
		r, _ := runs.ReadFile(ms[len(ms)-1])
		_ = os.WriteFile(filepath.Join(rec, cli+".seen"), []byte(string(r.Status)), 0o644)
	}
	flagValue := func(name string) string {
		for i, a := range args {
			if a == name && i+1 < len(args) {
				return args[i+1]
			}
		}
		return ""
	}

	mode := os.Getenv("PANAL_FAKE_" + strings.ToUpper(cli))
	switch mode {
	case "hang":
		fmt.Println("thinking…")
		time.Sleep(5 * time.Minute)
		return 0
	case "fail":
		fmt.Fprintln(os.Stderr, "panic: boom")
		return 3
	case "perm":
		if cli == "cursor" {
			fmt.Fprintln(os.Stderr, "Untrusted workspace. Pass --trust to continue.")
			return 1
		}
		if cli == "agy" {
			fmt.Println(`jetski: no output produced — a tool required the "read_file"/"command" permission that headless mode cannot prompt for, so it was auto-denied`)
			return 0
		}
		fmt.Fprintln(os.Stderr, "Not inside a trusted directory and --skip-git-repo-check was not specified.")
		return 1
	case "quota":
		switch cli {
		case "codex":
			fmt.Println(`{"type":"thread.started","thread_id":"t-1"}`)
			fmt.Println(`{"type":"error","message":"You've hit your usage limit. Try again in 3 days."}`)
			fmt.Println(`{"type":"turn.failed","error":{"message":"usage_limit_reached"}}`)
			return 1
		case "opencode":
			// opencode may exit 0 after a provider error.
			fmt.Fprintln(os.Stderr, "Error: Go usage limit exceeded")
			return 0
		case "cursor":
			fmt.Fprintln(os.Stderr, "Error: You've hit your usage limit. Included usage is exhausted.")
			return 1
		default:
			fmt.Fprintln(os.Stderr, "error: RESOURCE_EXHAUSTED: 429 Too Many Requests")
			return 1
		}
	}

	// ok and talk: the agent finishes its turn. talk mentions rate limits in
	// its work, which must not count as being out of quota.
	said := "working on the task"
	if mode == "talk" {
		said = "I added handling for the case where the API says rate limit exceeded (429)"
	}
	switch cli {
	case "codex":
		fmt.Println(`{"type":"thread.started","thread_id":"t-1"}`)
		fmt.Println(`{"type":"item.completed","item":{"id":"i0","type":"agent_message","text":"` + said + `"}}`)
		fmt.Println(`{"type":"item.completed","item":{"id":"i1","type":"command_execution","command":"go test ./...","exit_code":0,"status":"completed"}}`)
		fmt.Println(`{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":2}}`)
		fmt.Fprintln(os.Stderr, "codex stderr line")
		_ = os.WriteFile(flagValue("-o"), []byte("All done."), 0o644)
	case "agy":
		_ = os.WriteFile(flagValue("--log-file"), []byte("I1002 10:15:00.000000 1 main.go:1] agy starts\n"), 0o644)
		fmt.Println(said)
	case "cursor":
		fmt.Println(`{"type":"system","subtype":"init","model":"composer-2.5"}`)
		fmt.Println(`{"type":"assistant","message":{"content":[{"type":"text","text":"` + said + `"}]}}`)
		fmt.Println(`{"type":"tool_call","subtype":"started","tool_call":{"shellToolCall":{"args":{"command":"go test ./..."}}}}`)
		fmt.Println(`{"type":"result","duration_ms":12}`)
	default:
		fmt.Println(said)
	}
	return 0
}

func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == name || strings.HasPrefix(a, name+"=") {
			return true
		}
	}
	return false
}
