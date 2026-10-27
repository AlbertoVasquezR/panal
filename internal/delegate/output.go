package delegate

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"sync"

	"github.com/AlbertoVasquezR/panal/internal/activity"
)

// tail keeps the last max bytes written to it: the CLI's error output, to
// classify the attempt when it ends.
type tail struct {
	mu  sync.Mutex
	max int
	b   []byte
}

func newTail(max int) *tail { return &tail{max: max} }

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > t.max {
		t.b = append([]byte(nil), t.b[len(t.b)-t.max:]...)
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.b)
}

// jsonEvents turns a CLI's JSONL events (codex --json, cursor-agent
// stream-json) into one readable line each for the terminal (the raw
// events go to <log>.jsonl), and keeps the messages of error events for
// classify.
type jsonEvents struct {
	cli    string
	out    io.Writer
	errors *tail
	buf    []byte
}

func (c *jsonEvents) Write(p []byte) (int, error) {
	c.buf = append(c.buf, p...)
	for {
		i := bytes.IndexByte(c.buf, '\n')
		if i < 0 {
			break
		}
		c.line(bytes.TrimSpace(c.buf[:i]))
		c.buf = c.buf[i+1:]
	}
	return len(p), nil
}

func (c *jsonEvents) line(l []byte) {
	if len(l) == 0 {
		return
	}
	// message and error are a string in some events and an object in
	// others (cursor's assistant message), so they are decoded later.
	var ev struct {
		Type    string          `json:"type"`
		Subtype string          `json:"subtype"`
		IsError bool            `json:"is_error"`
		Result  string          `json:"result"`
		Message json.RawMessage `json:"message"`
		Error   json.RawMessage `json:"error"`
	}
	if json.Unmarshal(l, &ev) != nil {
		io.WriteString(c.out, string(l)+"\n") // not an event: show it as is
		return
	}
	switch {
	case ev.Type == "error" || ev.Type == "turn.failed":
		c.failed(eventText(ev.Message) + " " + eventText(ev.Error))
	case ev.Type == "result" && (ev.IsError || strings.HasPrefix(ev.Subtype, "error")):
		c.failed(ev.Result + " " + eventText(ev.Error))
	case ev.Type == "item.completed":
		// activity already knows how to describe an event in a few words.
		if s, _ := activity.FromCodex(l); s != "" {
			io.WriteString(c.out, "  "+s+"\n")
		}
	case ev.Type == "tool_call" || ev.Type == "assistant":
		if s, _ := activity.FromCursor(l); s != "" {
			io.WriteString(c.out, "  "+s+"\n")
		}
	}
}

func (c *jsonEvents) failed(msg string) {
	msg = strings.TrimSpace(msg)
	c.errors.Write([]byte(msg + "\n"))
	io.WriteString(c.out, c.cli+" error: "+msg+"\n")
}

// eventText is a JSON string, or the "message" of a JSON object; else "".
func eventText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var o struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &o) == nil {
		return o.Message
	}
	return ""
}
