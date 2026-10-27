package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLookWithTriesCursorBins(t *testing.T) {
	var tried []string
	look := func(name string) (string, error) {
		tried = append(tried, name)
		if name == "agent" {
			return `/opt/cursor/agent`, nil
		}
		return "", errors.New("no")
	}
	p, err := LookWith(look, "cursor")
	if err != nil || p != `/opt/cursor/agent` {
		t.Fatalf("got %q %v, tried %v", p, err, tried)
	}
	if len(tried) < 2 || tried[0] != "cursor-agent" || tried[1] != "agent" {
		t.Fatalf("should try cursor-agent then agent: %v", tried)
	}
}

func TestLookWithPlainCLI(t *testing.T) {
	look := func(name string) (string, error) {
		if name == "agy" {
			return "/bin/agy", nil
		}
		return "", errors.New("no")
	}
	p, err := LookWith(look, "agy")
	if err != nil || p != "/bin/agy" {
		t.Fatalf("got %q %v", p, err)
	}
}

func TestWindowsCursorInstall(t *testing.T) {
	d := t.TempDir()
	t.Setenv("LOCALAPPDATA", d)
	if p := windowsCursorInstall(); p != "" {
		t.Fatalf("empty install dir: %q", p)
	}
	shim := filepath.Join(d, "cursor-agent", "cursor-agent.cmd")
	if err := os.MkdirAll(filepath.Dir(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shim, []byte("@echo off\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if p := windowsCursorInstall(); p != shim {
		t.Fatalf("got %q, want %q", p, shim)
	}
	look := func(string) (string, error) { return "", errors.New("no PATH") }
	p, err := LookWith(look, "cursor")
	if err != nil || p != shim {
		t.Fatalf("fallback %q %v", p, err)
	}
}
