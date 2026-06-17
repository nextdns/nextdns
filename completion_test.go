package main

import (
	"strings"
	"testing"
)

func TestCompletionScript(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		s, err := completionScript(shell)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", shell, err)
		}
		if s == "" {
			t.Fatalf("%s: empty script", shell)
		}
		if !strings.Contains(s, "nextdns") {
			t.Errorf("%s: script missing program name", shell)
		}
		if !strings.Contains(s, "install") {
			t.Errorf("%s: script missing known command", shell)
		}
		if !strings.Contains(s, "list set edit wizard") {
			t.Errorf("%s: script missing config subcommands", shell)
		}
	}
	if _, err := completionScript("powershell"); err == nil {
		t.Error("expected error for unsupported shell")
	}
	if _, err := completionScript(""); err == nil {
		t.Error("expected error for missing shell")
	}
}
