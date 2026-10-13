package activity

import (
	"bytes"
	"encoding/json"
	"time"
)

// FromClaudeFile: the last thing Claude Code did according to its session
// transcript (transcript_path from the status line).
func FromClaudeFile(transcript string) (string, time.Time) {
	if transcript == "" {
		return "", time.Time{}
	}
	return fromFile(transcript, FromClaude)
}

type claudeLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type claudeBlock struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Input struct {
		Description string `json:"description"`
		Command     string `json:"command"`
		FilePath    string `json:"file_path"`
		Pattern     string `json:"pattern"`
		Prompt      string `json:"prompt"`
		URL         string `json:"url"`
		Query       string `json:"query"`
	} `json:"input"`
}

// FromClaude looks, from the end backwards, for the last tool it used
// (tool_use in an assistant message) and describes it briefly: the
// description Claude gave the command, or which file it read or edited.
func FromClaude(b []byte) (string, time.Time) {
	lines := bytes.Split(b, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		l := bytes.TrimSpace(lines[i])
		if !bytes.Contains(l, []byte(`"tool_use"`)) {
			continue
		}
		var lc claudeLine
		if json.Unmarshal(l, &lc) != nil || lc.Type != "assistant" {
			continue
		}
		var blocks []claudeBlock
		if json.Unmarshal(lc.Message.Content, &blocks) != nil {
			continue
		}
		for j := len(blocks) - 1; j >= 0; j-- {
			if blocks[j].Type != "tool_use" {
				continue
			}
			if txt := describeTool(blocks[j]); txt != "" {
				at, _ := time.Parse(time.RFC3339Nano, lc.Timestamp)
				return txt, at.Local()
			}
		}
	}
	return "", time.Time{}
}

func describeTool(b claudeBlock) string {
	in := b.Input
	base := baseName(in.FilePath)
	switch b.Name {
	case "Bash", "PowerShell":
		if in.Description != "" {
			return firstLine(in.Description)
		}
		return "$ " + CleanCommand(firstLine(in.Command))
	case "Read":
		return "reads " + base
	case "Edit", "MultiEdit":
		return "edits " + base
	case "Write":
		return "writes " + base
	case "NotebookEdit":
		return "edits " + base
	case "Grep", "Glob":
		return "searches " + in.Pattern
	case "Agent", "Task":
		if in.Description != "" {
			return "delegates: " + firstLine(in.Description)
		}
		return "delegates to an agent"
	case "WebFetch":
		return "reads " + in.URL
	case "WebSearch":
		return "searches the web: " + in.Query
	}
	if in.Description != "" {
		return firstLine(in.Description)
	}
	return b.Name
}
