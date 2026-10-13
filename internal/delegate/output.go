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

// codexEvents turns codex's JSONL events into one readable line each for the
// terminal (the raw events go to <log>.jsonl), and keeps the messages of
// error events for classify.
type codexEvents struct {
	out    io.Writer
	errors *tail
	buf    []byte
}

func (c *codexEvents) Write(p []byte) (int, error) {
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

func (c *codexEvents) line(l []byte) {
	if len(l) == 0 {
		return
	}
	var ev struct {
		Type    string `json:"type"`
		Message string `json:"message"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(l, &ev) != nil {
		io.WriteString(c.out, string(l)+"\n") // not an event: show it as is
		return
	}
	switch {
	case ev.Type == "error" || ev.Type == "turn.failed":
		msg := strings.TrimSpace(ev.Message + " " + ev.Error.Message)
		c.errors.Write([]byte(msg + "\n"))
		io.WriteString(c.out, "codex error: "+msg+"\n")
	case ev.Type == "item.completed":
		// activity already knows how to describe an event in a few words.
		if s, _ := activity.FromCodex(l); s != "" {
			io.WriteString(c.out, "  "+s+"\n")
		}
	case ev.Type == "tool_call" || ev.Type == "assistant" || ev.Type == "result":
		if s, _ := activity.FromCursor(l); s != "" {
			io.WriteString(c.out, "  "+s+"\n")
		}
	}
}
