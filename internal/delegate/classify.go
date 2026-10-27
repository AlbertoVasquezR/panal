package delegate

import (
	"regexp"

	"github.com/AlbertoVasquezR/panal/internal/runs"
)

// pattern is one thing a CLI prints when it gives up for a reason that is
// not a real error in the task.
type pattern struct {
	re     *regexp.Regexp
	status runs.Status // runs.OutOfQuota or runs.NoPermission
	// onSuccess: the pattern also counts when the CLI exited 0. Only for
	// messages that can only come from the CLI itself, never from the
	// agent's own work.
	onSuccess bool
}

func p(status runs.Status, onSuccess bool, expr string) pattern {
	return pattern{re: regexp.MustCompile("(?i)" + expr), status: status, onSuccess: onSuccess}
}

// Generic quota messages every provider uses in some form (HTTP 429, "rate
// limit", "quota exceeded", "usage limit", "out of credits"…). They are only
// matched against the CLI's own error output (see errorText) and only when
// it exited non-zero, so an agent that merely talks about rate limits in its
// work does not trigger them.
var genericQuota = []pattern{
	p(runs.OutOfQuota, false, `\b429\b.{0,40}(too many requests|rate|quota|limit)|(too many requests|status|code|http)\D{0,12}\b429\b`),
	p(runs.OutOfQuota, false, `rate[ _-]?limit(ed|s)?\b.{0,40}(reached|exceeded|hit)|rate_limit_(exceeded|error)|you('|’)?ve been rate[ -]limited`),
	p(runs.OutOfQuota, false, `quota.{0,30}(exceeded|exhausted|reached|used up)|exceeded.{0,30}quota|insufficient_quota`),
	p(runs.OutOfQuota, false, `usage[ _]limit`),
	p(runs.OutOfQuota, false, `out of credits|insufficient (credits|balance)|credit balance is too low|no credits (left|remaining)`),
	p(runs.OutOfQuota, false, `resource[ _]exhausted`),
}

// quotaPatterns: one table per CLI, checked before the generic ones.
var quotaPatterns = map[string][]pattern{
	// codex prints its errors to stderr and, with --json, as {"type":"error"}
	// or {"type":"turn.failed"} events. When the plan's window is used up it
	// says "You've hit your usage limit…" (error code usage_limit_reached);
	// the generic "usage limit" pattern covers that, these are the exact codes.
	"codex": {
		p(runs.OutOfQuota, false, `usage_limit_reached|usage_not_included`),
		p(runs.OutOfQuota, false, `you('|’)?ve hit your usage limit`),
		// codex refuses to start outside a git repository it does not trust.
		p(runs.NoPermission, false, `not inside a trusted directory`),
		p(runs.NoPermission, false, `sandbox.{0,40}(denied|not permitted|refused|failed to (apply|start))`),
	},
	// opencode: the same messages panal's opencode reader looks for in
	// opencode.log (internal/readers/opencode.go), for the Go plan and the
	// API. opencode can exit 0 after printing a provider error, so these
	// exact messages also count on success.
	"opencode": {
		p(runs.OutOfQuota, true, `5-hour usage limit reached`),
		p(runs.OutOfQuota, true, `Go usage limit exceeded`),
		p(runs.OutOfQuota, true, `API access paused for this organization`),
		p(runs.OutOfQuota, false, `AI_APICallError.{0,80}(429|quota|rate limit)`),
		p(runs.NoPermission, false, `permission.{0,30}(denied|rejected)|auto-reject`),
	},
	// agy talks to Google's Cloud Code API: quota errors come as gRPC
	// RESOURCE_EXHAUSTED / HTTP 429 (covered by the generic table), or as
	// "You have exhausted your capacity" / "quota … reset" messages.
	"agy": {
		p(runs.OutOfQuota, false, `exhausted your (capacity|quota)`),
		p(runs.OutOfQuota, false, `(model|quota) (capacity|limit).{0,30}(reached|exceeded)`),
		p(runs.NoPermission, false, `(permission|approval).{0,30}(denied|required|rejected)|not allowed in (plan|sandbox) mode`),
		// Print / -p cannot prompt: agy auto-denies the tool and still
		// exits 0 with no work (jetski on stdout; "Print mode:" in its log).
		p(runs.NoPermission, true, `headless mode cannot prompt.{0,80}auto-denied`),
		p(runs.NoPermission, true, `print mode: soft-denying tool confirmation`),
	},
	// cursor-agent (also invoked as agent): headless -p fails without
	// --trust in an untrusted workspace; team admins can block headless;
	// usage is the account's included-usage meters (interactive /usage
	// only — those messages still appear on stderr when a turn is refused).
	"cursor": {
		p(runs.OutOfQuota, false, `you('|’)?ve hit your usage limit`),
		p(runs.OutOfQuota, false, `included usage.{0,40}(exhausted|exceeded|used up)`),
		p(runs.NoPermission, false, `untrusted workspace|trust the workspace|pass --trust`),
		p(runs.NoPermission, false, `headless mode.{0,40}(disabled|blocked|not allowed)`),
		p(runs.NoPermission, false, `not (logged in|authenticated)|please (log in|login)|authentication required`),
		p(runs.NoPermission, false, `sandbox mode is enabled but not available|sandbox requires macos or linux`),
	},
}

// classify decides the status of a finished attempt from the CLI's exit code
// and its error output. rc is the agent's exit code; text is errorText's
// result.
func classify(cli string, rc int, text string) runs.Status {
	for _, tbl := range [][]pattern{quotaPatterns[cli], genericQuota} {
		for _, pt := range tbl {
			if (rc != 0 || pt.onSuccess) && pt.re.MatchString(text) {
				return pt.status
			}
		}
	}
	if rc == 0 {
		return runs.Done
	}
	return runs.Failed
}
