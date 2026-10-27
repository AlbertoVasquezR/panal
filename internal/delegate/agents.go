package delegate

import (
	"fmt"
	"runtime"
	"strings"
)

// attempt is everything an agent's command line needs.
type attempt struct {
	Link     Link
	Task     string // full task text
	TaskFile string // file holding the full task, when one was written ("" otherwise)
	Dir      string // where the agent works
	ReadOnly bool
	Log      string // the attempt's log; codex and agy also write next to it
}

// invocation is how to run one CLI non-interactively.
type invocation struct {
	Args  []string // after the program name
	Stdin string   // fed to the CLI's stdin ("" = no stdin)
	// The CLI's stdout is JSONL events (codex --json): it goes to Log+".jsonl"
	// and the terminal gets a readable line per event.
	JSONEvents bool
	// LastMessage is a file the CLI writes its final answer to; it is
	// appended to the log when the attempt ends.
	LastMessage string
	// NeedsTaskFile: the task must be in TaskFile (the command refers to it).
	NeedsTaskFile bool
}

// agent knows one CLI.
type agent struct {
	// build returns the command line for a. When the task cannot go on the
	// command line as is and no task file exists yet, it returns
	// NeedsTaskFile and is called again once one exists.
	build func(a attempt) invocation
}

var agents = map[string]agent{
	"codex":    {build: codexArgs},
	"agy":      {build: agyArgs},
	"opencode": {build: opencodeArgs},
	"cursor":   {build: cursorArgs},
}

// maxArgvTask: a task longer than this does not go on the command line
// (Windows caps a whole command line at 32767 characters).
const maxArgvTask = 24000

// codexArgs: `codex exec` reads the prompt from stdin when it is "-". --json
// streams events (what panal's activity and history readers parse from
// <log>.jsonl), -o writes the final answer, -s is codex's own sandbox:
// read-only, or workspace-write (it may edit files and run commands inside
// -d only).
//
//	codex exec --json -o <log>.last -C <dir> -s workspace-write|read-only
//	           [-m <model>] [-c model_reasoning_effort="<effort>"] -   < task
func codexArgs(a attempt) invocation {
	sandbox := "workspace-write"
	if a.ReadOnly {
		sandbox = "read-only"
	}
	args := []string{"exec", "--json", "-o", a.Log + ".last", "-C", a.Dir, "-s", sandbox}
	if a.Link.Model != "" {
		args = append(args, "-m", a.Link.Model)
	}
	if a.Link.Effort != "" {
		args = append(args, "-c", fmt.Sprintf("model_reasoning_effort=%q", a.Link.Effort))
	}
	args = append(args, "-")
	return invocation{Args: args, Stdin: a.Task, JSONEvents: true, LastMessage: a.Log + ".last"}
}

// agyArgs: `agy -p <prompt>` runs one turn and prints the answer. Its own log
// goes to <log>.log (panal's agy readers look for *-agy-*.txt.log). Writes:
// --dangerously-skip-permissions (print mode cannot ask). Read-only: plan
// mode plus agy's terminal sandbox.
//
//	agy -p <task> --log-file <log>.log [--model <model>] [--effort <effort>]
//	    --dangerously-skip-permissions | --mode plan --sandbox
func agyArgs(a attempt) invocation {
	prompt := a.Task
	inv := invocation{}
	if len(prompt) > maxArgvTask {
		if a.TaskFile == "" {
			return invocation{NeedsTaskFile: true}
		}
		prompt = fileTaskPrompt(a.TaskFile)
		inv.NeedsTaskFile = true
	}
	args := []string{"-p", prompt, "--log-file", a.Log + ".log"}
	if a.Link.Model != "" {
		args = append(args, "--model", a.Link.Model)
	}
	if a.Link.Effort != "" {
		args = append(args, "--effort", a.Link.Effort)
	}
	if a.ReadOnly {
		args = append(args, "--mode", "plan", "--sandbox")
	} else {
		args = append(args, "--dangerously-skip-permissions")
	}
	inv.Args = args
	return inv
}

// opencodeArgs: `opencode run <message>` runs one turn in the current
// directory. On Windows opencode is usually an npm .cmd shim, which goes
// through cmd.exe: a task with quotes, %, &, |, <, >, ^, ! or line breaks
// does not survive that, so such a task is attached with --file and the
// message only points to it. Model: provider/model, with #variant for the
// effort. Writes: --auto (approve what is not explicitly denied). Read-only:
// the built-in plan agent, without --auto.
//
//	opencode run [--model <provider/model>[#<effort>]] --auto | --agent plan
//	             (<task> | --file <task file> "<pointer message>")
func opencodeArgs(a attempt) invocation {
	args := []string{"run"}
	if a.Link.Model != "" {
		m := a.Link.Model
		if a.Link.Effort != "" && !strings.Contains(m, "#") {
			m += "#" + a.Link.Effort
		}
		args = append(args, "--model", m)
	}
	if a.ReadOnly {
		args = append(args, "--agent", "plan")
	} else {
		args = append(args, "--auto")
	}
	if shellSafe(a.Task) && len(a.Task) <= 2000 {
		return invocation{Args: append(args, a.Task)}
	}
	if a.TaskFile == "" {
		return invocation{NeedsTaskFile: true}
	}
	args = append(args, "--file", a.TaskFile, "The task is in the attached file. Do what it says.")
	return invocation{Args: args, NeedsTaskFile: true}
}

// cursorArgs: `cursor-agent -p` (also invoked as `agent`) runs one
// non-interactive turn. --output-format stream-json writes JSONL events
// that panal's activity reader parses from <log>.jsonl. --trust and
// --workspace are always set so headless does not stall on the workspace
// trust prompt. Writes: --force (also spelled --yolo on the CLI); that
// is not confined to -d. Read-only: --auto-review lets the server
// classifier run safe reads and commands and deny the rest. --sandbox
// enabled is only passed on macOS and Linux: the CLI refuses it on
// Windows ("Sandbox mode is enabled but not available on this system").
// --mode plan is not used for -r: it rejects even harmless shell
// commands. Effort is not a separate flag: it is folded into --model as
// model[effort=…] when the model id does not already have brackets.
//
//	cursor-agent -p --output-format stream-json --trust --workspace <dir>
//	             [--model <model> | --model <model>[effort=<effort>]]
//	             --force | --auto-review [--sandbox enabled]
//	             (<task> | a pointer at the task file)
func cursorArgs(a attempt) invocation {
	args := []string{"-p", "--output-format", "stream-json", "--trust", "--workspace", a.Dir}
	if a.Link.Model != "" {
		m := a.Link.Model
		if a.Link.Effort != "" && !strings.Contains(m, "[") {
			m += "[effort=" + a.Link.Effort + "]"
		}
		args = append(args, "--model", m)
	}
	if a.ReadOnly {
		args = append(args, cursorReadOnlyFlags(runtime.GOOS)...)
	} else {
		args = append(args, "--force")
	}
	inv := invocation{JSONEvents: true}
	if shellSafe(a.Task) && len(a.Task) <= maxArgvTask {
		return invocation{Args: append(args, a.Task), JSONEvents: true}
	}
	if a.TaskFile == "" {
		return invocation{NeedsTaskFile: true, JSONEvents: true}
	}
	args = append(args, fileTaskPrompt(a.TaskFile))
	inv.Args = args
	inv.NeedsTaskFile = true
	return inv
}

func fileTaskPrompt(path string) string {
	return "Your task is in the file " + path + ". Read it and do what it says."
}

// cursorReadOnlyFlags: --auto-review on every OS; --sandbox enabled only
// where the CLI's sandbox exists (linux, darwin).
func cursorReadOnlyFlags(goos string) []string {
	args := []string{"--auto-review"}
	if goos == "linux" || goos == "darwin" {
		args = append(args, "--sandbox", "enabled")
	}
	return args
}

// shellSafe: s can go through cmd.exe as one quoted argument unchanged.
func shellSafe(s string) bool {
	return !strings.ContainsAny(s, "\"%&|<>^!\r\n")
}
