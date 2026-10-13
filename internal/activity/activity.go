// Package activity tells the last thing an agent did while it works, by
// reading the tail of its run log: the command it ran, the file it
// opened or edited, or the last thing it said. Read-only.
//
//   - codex writes to <log>.jsonl the events of `codex exec --json`
//     (item.started / item.completed with command_execution, agent_message,
//     file_change…).
//   - agy writes to <log>.log its Go log, where each tool call shows up as a
//     "functionCall" with a readable toolAction («Reading README.md»).
package activity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// How much of the end of the log is read: the last thing it did is there.
const tailSize = 192 << 10

// Latest returns the last thing the agent did according to its log, and when.
// Empty if unknown.
func Latest(agent, logPath string) (string, time.Time) {
	if logPath == "" {
		return "", time.Time{}
	}
	switch agent {
	case "codex":
		return fromFile(logPath+".jsonl", FromCodex)
	case "agy":
		return fromFile(logPath+".log", FromAgy)
	case "cursor":
		return fromFile(logPath+".jsonl", FromCursor)
	}
	return "", time.Time{}
}

// The last thing read from each file: it is checked on every refresh and
// almost always has not changed.
type readEntry struct {
	size int64
	mod  time.Time
	txt  string
	at   time.Time
}

var (
	cacheMu sync.Mutex
	cache   = map[string]readEntry{}
)

func fromFile(path string, read func([]byte) (string, time.Time)) (string, time.Time) {
	return fromFileKey(path, path, read)
}

// fromFileKey is fromFile with another cache key, to read different things
// from the same file.
func fromFileKey(key, path string, read func([]byte) (string, time.Time)) (string, time.Time) {
	info, err := os.Stat(path)
	if err != nil {
		return "", time.Time{}
	}
	cacheMu.Lock()
	c, ok := cache[key]
	cacheMu.Unlock()
	if ok && c.size == info.Size() && c.mod.Equal(info.ModTime()) {
		return c.txt, c.at
	}
	b, err := readTail(path, info.Size())
	if err != nil {
		return "", time.Time{}
	}
	txt, at := read(b)
	if txt != "" && at.IsZero() {
		at = info.ModTime()
	}
	cacheMu.Lock()
	cache[key] = readEntry{info.Size(), info.ModTime(), txt, at}
	cacheMu.Unlock()
	return txt, at
}

func readTail(path string, size int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	from := max(size-tailSize, 0)
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return nil, err
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	if from > 0 { // the first line was cut in half
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			b = b[i+1:]
		}
	}
	return b, nil
}

// ------------------------------------------------------------------ codex --

type codexEvent struct {
	Type string `json:"type"`
	Item struct {
		Type    string `json:"type"`
		Command string `json:"command"`
		Text    string `json:"text"`
		Query   string `json:"query"`
		Tool    string `json:"tool"`
		Changes []struct {
			Path string `json:"path"`
		} `json:"changes"`
		Items []struct { // todo_list: the agent's plan
			Text      string `json:"text"`
			Completed bool   `json:"completed"`
		} `json:"items"`
	} `json:"item"`
}

// FromCodex looks, from the end backwards, for the last event that says
// something. Events carry no time: Latest uses the file's.
func FromCodex(b []byte) (string, time.Time) {
	lines := bytes.Split(b, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		var ev codexEvent
		if json.Unmarshal(bytes.TrimSpace(lines[i]), &ev) != nil || !strings.HasPrefix(ev.Type, "item.") {
			continue
		}
		it := ev.Item
		switch it.Type {
		case "command_execution":
			if c := CleanCommand(it.Command); c != "" {
				return "$ " + c, time.Time{}
			}
		case "file_change":
			var ps []string
			for _, c := range it.Changes {
				ps = append(ps, baseName(c.Path))
			}
			if len(ps) > 0 {
				return "edits " + strings.Join(ps, ", "), time.Time{}
			}
		case "agent_message":
			if t := stripMarkdown(firstLine(it.Text)); t != "" {
				return "«" + t + "»", time.Time{}
			}
		case "todo_list":
			if len(it.Items) > 0 {
				done, next := 0, ""
				for _, p := range it.Items {
					if p.Completed {
						done++
					} else if next == "" {
						next = firstLine(p.Text)
					}
				}
				t := fmt.Sprintf("plan %d/%d", done, len(it.Items))
				if next != "" {
					t += " · " + next
				}
				return t, time.Time{}
			}
		case "web_search":
			if it.Query != "" {
				return "searches " + it.Query, time.Time{}
			}
		case "mcp_tool_call":
			if it.Tool != "" {
				return "uses " + it.Tool, time.Time{}
			}
		}
	}
	return "", time.Time{}
}

// codex commands on Windows come wrapped:
// "C:\...\powershell.exe" -NoProfile -Command 'git fetch origin'.
var reWrapper = regexp.MustCompile(`(?is)^(?:"[^"]*?|\S*?)(?:powershell(?:\.exe)?|pwsh(?:\.exe)?|bash(?:\.exe)?|cmd(?:\.exe)?|sh)"?\s+(?:-\S+\s+)*?(?:-Command|-c|-lc|/c)\s+(.*)$`)

// CleanCommand removes the interpreter wrapping the command and the quotes,
// and puts it on one line.
func CleanCommand(c string) string {
	c = strings.TrimSpace(c)
	if m := reWrapper.FindStringSubmatch(c); m != nil {
		c = strings.TrimSpace(m[1])
		if len(c) >= 2 && (c[0] == '\'' || c[0] == '"') && c[len(c)-1] == c[0] {
			c = c[1 : len(c)-1]
		}
	}
	return strings.Join(strings.Fields(c), " ")
}

// -------------------------------------------------------------------- agy --

type agyCall struct {
	Name string `json:"name"`
	Args struct {
		ToolAction  string `json:"toolAction"`
		ToolSummary string `json:"toolSummary"`
	} `json:"args"`
}

var agyMark = []byte(`"functionCall"`)

// FromAgy looks for the last tool call in the log and uses its readable
// description. The time comes from the glog line («I0926 13:39:33…»).
func FromAgy(b []byte) (string, time.Time) {
	for end := len(b); end > 0; {
		i := bytes.LastIndex(b[:end], agyMark)
		if i < 0 {
			break
		}
		end = i
		rest := b[i+len(agyMark):]
		j := bytes.IndexByte(rest, '{')
		if j < 0 {
			continue
		}
		var call agyCall
		if json.NewDecoder(bytes.NewReader(rest[j:])).Decode(&call) != nil {
			continue
		}
		txt := call.Args.ToolAction
		if txt == "" {
			txt = call.Args.ToolSummary
		}
		if txt == "" {
			txt = call.Name
		}
		if txt == "" {
			continue
		}
		return firstLine(txt), glogTime(b, i)
	}
	return "", time.Time{}
}

// glogTime reads the time from the line holding position i. glog has no
// year: the current one is used (and the previous one if the date would be
// in the future).
func glogTime(b []byte, i int) time.Time {
	start := bytes.LastIndexByte(b[:i], '\n') + 1
	l := b[start:]
	if len(l) < 21 || (l[0] != 'I' && l[0] != 'W' && l[0] != 'E') {
		return time.Time{}
	}
	now := time.Now()
	t, err := time.ParseInLocation("20060102 15:04:05.000000", itoa4(now.Year())+string(l[1:5])+" "+string(l[6:21]), time.Local)
	if err != nil {
		return time.Time{}
	}
	if t.After(now.Add(24 * time.Hour)) {
		t = t.AddDate(-1, 0, 0)
	}
	return t
}

func itoa4(n int) string {
	d := []byte("0000")
	for i := 3; i >= 0; i-- {
		d[i] = byte('0' + n%10)
		n /= 10
	}
	return string(d)
}

// stripMarkdown removes what looks like garbage in a terminal: **bold** and
// `code`.
func stripMarkdown(s string) string {
	return strings.NewReplacer("**", "", "`", "").Replace(s)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// baseName is the last element of a Windows or Unix path. Agent logs mix
// both; filepath.Base only splits on this OS's separator.
func baseName(p string) string {
	p = strings.TrimRight(p, `/\`)
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// ------------------------------------------------------------- repetition --

// Repetition tells whether the agent keeps doing the same thing: the last
// action and how many times in a row it did it (1 if it does not repeat). A
// stuck agent often runs the same command over and over without progress.
func Repetition(agent, logPath string) (string, int) {
	if logPath == "" {
		return "", 0
	}
	var path string
	var actions func([]byte) []string
	switch agent {
	case "codex":
		path, actions = logPath+".jsonl", codexActions
	case "agy":
		path, actions = logPath+".log", agyActions
	case "cursor":
		path, actions = logPath+".jsonl", cursorActions
	default:
		return "", 0
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", 0
	}
	cacheMu.Lock()
	c, ok := repCache[path]
	cacheMu.Unlock()
	if ok && c.size == info.Size() && c.mod.Equal(info.ModTime()) {
		return c.action, c.times
	}
	b, err := readTail(path, info.Size())
	if err != nil {
		return "", 0
	}
	a, n := trailingRepetition(actions(b))
	cacheMu.Lock()
	repCache[path] = repeated{info.Size(), info.ModTime(), a, n}
	cacheMu.Unlock()
	return a, n
}

type repeated struct {
	size   int64
	mod    time.Time
	action string
	times  int
}

var repCache = map[string]repeated{}

// trailingRepetition: the last action and how many times in a row it shows
// up at the end.
func trailingRepetition(actions []string) (string, int) {
	if len(actions) == 0 {
		return "", 0
	}
	last := actions[len(actions)-1]
	n := 0
	for i := len(actions) - 1; i >= 0 && actions[i] == last; i-- {
		n++
	}
	return last, n
}

// codexActions: every command that started (item.started) and every edit, in
// order. An edit between two equal commands is progress, not repetition.
func codexActions(b []byte) []string {
	var out []string
	for _, l := range bytes.Split(b, []byte("\n")) {
		var ev codexEvent
		if json.Unmarshal(bytes.TrimSpace(l), &ev) != nil {
			continue
		}
		switch {
		case ev.Type == "item.started" && ev.Item.Type == "command_execution":
			if c := CleanCommand(ev.Item.Command); c != "" {
				out = append(out, "$ "+c)
			}
		case ev.Type == "item.completed" && ev.Item.Type == "file_change":
			out = append(out, "edits")
		}
	}
	return out
}

// agyActions: every tool call the model emitted. Only the «Cortex API Chunk»
// lines count (what the model answers): every new request copies the whole
// conversation again and would repeat everything before.
func agyActions(b []byte) []string {
	var out []string
	for _, l := range bytes.Split(b, []byte("\n")) {
		if !bytes.Contains(l, []byte("Cortex API Chunk")) {
			continue
		}
		for rest := l; ; {
			i := bytes.Index(rest, agyMark)
			if i < 0 {
				break
			}
			rest = rest[i+len(agyMark):]
			j := bytes.IndexByte(rest, '{')
			if j < 0 {
				break
			}
			var call struct {
				Name string `json:"name"`
				Args struct {
					CommandLine  string `json:"CommandLine"`
					AbsolutePath string `json:"AbsolutePath"`
					ToolSummary  string `json:"toolSummary"`
				} `json:"args"`
			}
			if json.NewDecoder(bytes.NewReader(rest[j:])).Decode(&call) != nil || call.Name == "" {
				continue
			}
			id := call.Name + " " + call.Args.CommandLine + call.Args.AbsolutePath
			if call.Args.CommandLine == "" && call.Args.AbsolutePath == "" {
				id = call.Name + " " + call.Args.ToolSummary
			}
			if call.Args.CommandLine != "" {
				id = "$ " + CleanCommand(call.Args.CommandLine)
			}
			out = append(out, strings.TrimSpace(id))
		}
	}
	return out
}

// ----------------------------------------------------------------- cursor --
// cursor-agent -p --output-format stream-json: one JSON object per line,
// Claude Code-compatible. Tool calls use tool_call.{shell,read,write,…}ToolCall.

type cursorEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Model   string `json:"model"`
	Message struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
	ToolCall map[string]struct {
		Args struct {
			Command string `json:"command"`
			Path    string `json:"path"`
		} `json:"args"`
	} `json:"tool_call"`
}

// FromCursor looks, from the end backwards, for the last stream-json event
// that says something: a tool call or the last assistant line.
func FromCursor(b []byte) (string, time.Time) {
	lines := bytes.Split(b, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		if s := cursorLine(lines[i]); s != "" {
			return s, time.Time{}
		}
	}
	return "", time.Time{}
}

func cursorLine(l []byte) string {
	var ev cursorEvent
	if json.Unmarshal(bytes.TrimSpace(l), &ev) != nil {
		return ""
	}
	switch ev.Type {
	case "tool_call":
		if ev.Subtype != "" && ev.Subtype != "started" {
			return ""
		}
		for name, call := range ev.ToolCall {
			kind := strings.TrimSuffix(name, "ToolCall")
			if call.Args.Command != "" {
				if c := CleanCommand(call.Args.Command); c != "" {
					return "$ " + c
				}
			}
			if call.Args.Path != "" {
				base := baseName(call.Args.Path)
				switch {
				case strings.Contains(strings.ToLower(kind), "write"):
					return "writes " + base
				case strings.Contains(strings.ToLower(kind), "read"):
					return "reads " + base
				default:
					return kind + " " + base
				}
			}
			if kind != "" {
				return "uses " + kind
			}
		}
	case "assistant":
		for _, c := range ev.Message.Content {
			if t := stripMarkdown(firstLine(c.Text)); t != "" {
				return "«" + t + "»"
			}
		}
	}
	return ""
}

func cursorActions(b []byte) []string {
	var out []string
	for _, l := range bytes.Split(b, []byte("\n")) {
		if s := cursorLine(l); strings.HasPrefix(s, "$ ") || strings.HasPrefix(s, "writes ") || strings.HasPrefix(s, "reads ") {
			out = append(out, s)
		}
	}
	return out
}

// ------------------------------------------------------------------ tests --

// Commands that run tests.
var reTests = regexp.MustCompile(`(?i)\b(go test|pytest|python -m pytest|npm (run )?test|pnpm (run )?test|yarn test|vitest|jest|cargo test|dotnet test|mvn test|gradle test|phpunit|rspec)\b`)

// Failure counters in the output: «--- FAIL:» from go, «N failed» from pytest
// and jest/vitest.
var (
	reGoFail  = regexp.MustCompile(`(?m)^\s*--- FAIL:`)
	reNFailed = regexp.MustCompile(`(?i)\b(\d+) failed\b`)
)

// LatestTests: the last time the agent ran tests in its run, whether they
// passed and a summary («✓ go test ./...», «✗ 3 failures · go test ./...»).
// found = false if it did not run tests (or the agent does not leave the
// output in its log: only codex for now).
func LatestTests(agent, logPath string) (summary string, passed, found bool) {
	if agent != "codex" || logPath == "" {
		return "", false, false
	}
	txt, _ := fromFileKey(logPath+".jsonl#tests", logPath+".jsonl", func(b []byte) (string, time.Time) {
		r, ok, found := FromCodexTests(b)
		if !found {
			return "", time.Time{}
		}
		return map[bool]string{true: "1", false: "0"}[ok] + r, time.Time{}
	})
	if txt == "" {
		return "", false, false
	}
	return txt[1:], txt[0] == '1', true
}

// FromCodexTests: the last test command that finished.
func FromCodexTests(b []byte) (summary string, passed, found bool) {
	lines := bytes.Split(b, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		var ev struct {
			Type string `json:"type"`
			Item struct {
				Type     string `json:"type"`
				Command  string `json:"command"`
				Output   string `json:"aggregated_output"`
				ExitCode *int   `json:"exit_code"`
			} `json:"item"`
		}
		if json.Unmarshal(bytes.TrimSpace(lines[i]), &ev) != nil || ev.Type != "item.completed" || ev.Item.Type != "command_execution" || ev.Item.ExitCode == nil {
			continue
		}
		cmd := CleanCommand(ev.Item.Command)
		if !reTests.MatchString(cmd) {
			continue
		}
		if *ev.Item.ExitCode == 0 {
			return "✓ " + cmd, true, true
		}
		failures := len(reGoFail.FindAllString(ev.Item.Output, -1))
		if m := reNFailed.FindStringSubmatch(ev.Item.Output); m != nil && failures == 0 {
			fmt.Sscan(m[1], &failures)
		}
		if failures == 1 {
			return "✗ 1 failure · " + cmd, false, true
		}
		if failures > 1 {
			return fmt.Sprintf("✗ %d failures · %s", failures, cmd), false, true
		}
		return "✗ failed · " + cmd, false, true
	}
	return "", false, false
}
