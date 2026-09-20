package pkg

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestParseVersionLines(t *testing.T) {
	raw := "mise WARN x\n# comment\n1.0.0 extra\n1.0.0\nerror boom\n2.0.0\n"
	got := ParseVersionLines(raw, 10)
	if strings.Join(got, ",") != "2.0.0,1.0.0" {
		t.Fatalf("got %#v", got)
	}
	var numbered strings.Builder
	for i := 1; i <= 8; i++ {
		fmt.Fprintf(&numbered, "1.0.%d\n", i)
	}
	got = ParseVersionLines(numbered.String(), 5)
	if strings.Join(got, ",") != "1.0.8,1.0.7,1.0.6,1.0.5,1.0.4" {
		t.Fatalf("limit newest-first = %#v", got)
	}
}

func TestWrapShellWithProfile(t *testing.T) {
	cmd := "echo 1"
	wrapped := WrapShellWithProfile(cmd)
	if runtime.GOOS == "windows" {
		if wrapped != cmd {
			t.Fatalf("expected untouched command on windows: %q", wrapped)
		}
	} else {
		if !strings.Contains(wrapped, ".bashrc") || !strings.HasSuffix(wrapped, cmd) {
			t.Fatalf("expected profile wrap: %q", wrapped)
		}
	}
}

func TestApplyMisePath(t *testing.T) {
	cmd := exec.Command("echo")
	ApplyMisePath(cmd)
	hasPath := false
	hasMiseYes := false
	for _, env := range cmd.Env {
		if strings.HasPrefix(env, "PATH=") && strings.Contains(env, "shims") {
			hasPath = true
		}
		if env == "MISE_YES=1" {
			hasMiseYes = true
		}
	}
	if !hasPath {
		t.Fatal("missing shims in PATH")
	}
	if !hasMiseYes {
		t.Fatal("missing MISE_YES=1")
	}
}
