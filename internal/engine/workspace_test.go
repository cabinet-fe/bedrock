package engine

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestJobWorkspace(t *testing.T) {
	t.Parallel()
	got := JobWorkspace("/data/workspaces", 42)
	want := filepath.Join("/data/workspaces", "jobs", "job-42")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestAbsoluteJobWorkspace(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	abs, err := AbsoluteJobWorkspace(root, 7)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(abs) {
		t.Fatalf("not absolute: %q", abs)
	}
	if !strings.HasSuffix(abs, filepath.Join("jobs", "job-7")) {
		t.Fatalf("unexpected path: %q", abs)
	}
}

func TestScriptWorkspace(t *testing.T) {
	t.Parallel()
	got := ScriptWorkspace("/data/workspaces", 9)
	want := filepath.Join("/data/workspaces", "scripts", "script-9")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestAbsoluteScriptWorkspace(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	abs, err := AbsoluteScriptWorkspace(root, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(abs) {
		t.Fatalf("not absolute: %q", abs)
	}
	if !strings.HasSuffix(abs, filepath.Join("scripts", "script-3")) {
		t.Fatalf("unexpected path: %q", abs)
	}
}
