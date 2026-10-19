package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLookWithTriesCursorBins(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	var tried []string
	look := func(name string) (string, error) {
		tried = append(tried, name)
		if name == "agent" {
			return `/opt/cursor-agent/agent`, nil
		}
		return "", errors.New("no")
	}
	p, err := LookWith(look, "cursor")
	if err != nil || p != `/opt/cursor-agent/agent` {
		t.Fatalf("got %q %v, tried %v", p, err, tried)
	}
	if len(tried) < 2 || tried[0] != "cursor-agent" || tried[1] != "agent" {
		t.Fatalf("should try cursor-agent then agent: %v", tried)
	}
}

func TestLookWithRejectsUnrelatedAgent(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	other := filepath.Join(t.TempDir(), "bin", "agent")
	if err := os.MkdirAll(filepath.Dir(other), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	look := func(name string) (string, error) {
		if name == "agent" {
			return other, nil
		}
		return "", errors.New("no")
	}
	if p, err := LookWith(look, "cursor"); err == nil {
		t.Fatalf("an agent binary outside a Cursor install was accepted: %q", p)
	}
}

func TestLookWithAgentLinkedIntoCursorInstall(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	home := t.TempDir()
	real := filepath.Join(home, ".local", "share", "cursor-agent", "versions", "1.0", "cursor-agent")
	link := filepath.Join(home, ".local", "bin", "agent")
	for _, d := range []string{filepath.Dir(real), filepath.Dir(link)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(real, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks not available:", err)
	}
	look := func(name string) (string, error) {
		if name == "agent" {
			return link, nil
		}
		return "", errors.New("no")
	}
	p, err := LookWith(look, "cursor")
	if err != nil || p != link {
		t.Fatalf("got %q %v, want the agent link", p, err)
	}
}

func TestIsCursorInstall(t *testing.T) {
	for path, want := range map[string]bool{
		`/opt/homebrew/Caskroom/cursor-cli/1.0/agent`:                 true,
		`C:\Users\x\AppData\Local\cursor-agent\agent.cmd`:             true,
		`/home/x/.local/share/cursor-agent/versions/1.0/cursor-agent`: true,
		`/usr/local/bin/agent`:                                        false,
		`/opt/cursor/agent`:                                           false,
	} {
		if got := isCursorInstall(path); got != want {
			t.Errorf("isCursorInstall(%q) = %v, want %v", path, got, want)
		}
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
