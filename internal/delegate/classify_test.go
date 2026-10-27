package delegate

import (
	"testing"

	"github.com/AlbertoVasquezR/panal/internal/runs"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		cli  string
		rc   int
		text string
		want runs.Status
	}{
		// codex
		{"codex", 1, "You've hit your usage limit. Upgrade to Pro or try again in 2 days.", runs.OutOfQuota},
		{"codex", 1, `{"code":"usage_limit_reached"}`, runs.OutOfQuota},
		{"codex", 1, "stream error: exceeded retry limit, last status: 429 Too Many Requests", runs.OutOfQuota},
		{"codex", 1, "Not inside a trusted directory and --skip-git-repo-check was not specified.", runs.NoPermission},
		{"codex", 1, "thread 'main' panicked at src/main.rs", runs.Failed},
		{"codex", 0, "usage limit", runs.Done}, // exit 0: the turn finished
		// opencode: the messages panal's opencode reader knows, even on exit 0
		{"opencode", 0, "Error: Go usage limit exceeded", runs.OutOfQuota},
		{"opencode", 0, "5-hour usage limit reached. Resets in 42min", runs.OutOfQuota},
		{"opencode", 1, "API access paused for this organization", runs.OutOfQuota},
		{"opencode", 1, "AI_APICallError: Rate limit exceeded", runs.OutOfQuota},
		{"opencode", 1, "Error: insufficient_quota", runs.OutOfQuota},
		{"opencode", 0, "I handled the case where the API says rate limit exceeded (429)", runs.Done},
		{"opencode", 1, "TypeError: undefined is not a function", runs.Failed},
		// agy
		{"agy", 1, "rpc error: code = ResourceExhausted desc = RESOURCE_EXHAUSTED", runs.OutOfQuota},
		{"agy", 1, "You have exhausted your capacity on this model.", runs.OutOfQuota},
		{"agy", 1, "Error 429: quota exceeded for the model", runs.OutOfQuota},
		{"agy", 1, "out of credits", runs.OutOfQuota},
		{"agy", 1, "tool permission denied", runs.NoPermission},
		{"agy", 0, `jetski: no output produced — a tool required the "read_file"/"command" permission that headless mode cannot prompt for, so it was auto-denied`, runs.NoPermission},
		{"agy", 0, `Print mode: soft-denying tool confirmation "RunCommand"`, runs.NoPermission},
		{"agy", 0, "I explained how print mode soft-denies a tool confirmation in the docs", runs.Done},
		{"agy", 2, "flag provided but not defined: -x", runs.Failed},
		// cursor-agent
		{"cursor", 1, "You've hit your usage limit. Upgrade or wait for the reset.", runs.OutOfQuota},
		{"cursor", 1, "Error: included usage exhausted for this billing cycle", runs.OutOfQuota},
		{"cursor", 1, "Untrusted workspace. Pass --trust to continue.", runs.NoPermission},
		{"cursor", 1, "headless mode is disabled for this team", runs.NoPermission},
		{"cursor", 1, "Not logged in. Run agent login.", runs.NoPermission},
		{"cursor", 1, "Error: Sandbox mode is enabled but not available on this system. Sandbox requires macOS or Linux.\nRun 'agent sandbox disable' to switch to allowlist mode.", runs.NoPermission},
		{"cursor", 0, "I added handling for the case where the API says rate limit exceeded (429)", runs.Done},
		{"cursor", 1, "TypeError: cannot read property", runs.Failed},
		// numbers that merely contain 429 are not quota
		{"agy", 1, "error at line 4290 of parser.go", runs.Failed},
	}
	for _, c := range cases {
		if got := classify(c.cli, c.rc, c.text); got != c.want {
			t.Errorf("%s rc=%d %q → %s, want %s", c.cli, c.rc, c.text, got, c.want)
		}
	}
}
