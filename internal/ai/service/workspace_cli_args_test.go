package service

import (
	"strings"
	"testing"
)

func TestAppendNonStreamingOutputArgs(t *testing.T) {
	got := appendNonStreamingOutputArgs("reasonix", []string{"run"})
	want := []string{"run", "-p"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("reasonix args=%v want=%v", got, want)
	}
	got = appendNonStreamingOutputArgs("claude_code", []string{"--print"})
	if strings.Join(got, " ") != "--print" {
		t.Fatalf("claude should stay unchanged, got %v", got)
	}
}

func TestAppendFullPermissionArgs(t *testing.T) {
	got := appendFullPermissionArgs("claude_code", []string{"--print"})
	want := []string{"--print", "--dangerously-skip-permissions"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("claude args=%v want=%v", got, want)
	}
	got = appendFullPermissionArgs("codex", nil)
	want = []string{"--dangerously-bypass-approvals-and-sandbox"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("codex args=%v want=%v", got, want)
	}
	got = appendFullPermissionArgs("opencode", []string{"run"})
	want = []string{"run", "--dangerously-skip-permissions"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("opencode args=%v want=%v", got, want)
	}
	got = appendFullPermissionArgs("reasonix", []string{"run"})
	want = []string{"run", "--permission-mode", "bypassPermissions"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("reasonix args=%v want=%v", got, want)
	}
	got = appendFullPermissionArgs("unknown", []string{"x"})
	if strings.Join(got, " ") != "x" {
		t.Fatalf("unknown cli should be no-op, got %v", got)
	}
}

func TestAgentWorkspaceScopeHint(t *testing.T) {
	hint := agentWorkspaceScopeHint()
	for _, want := range []string{
		"$BEDROCK_AGENT_WORKDIR",
		"$BEDROCK_AGENT_OUTPUT",
		"./repo-{id}-{branch}",
		"固定产出目录",
		"只能在该目录内读写",
		"禁止访问该目录之外的任意路径",
		"Do not access any path outside this directory",
		"Write deliverable files into $BEDROCK_AGENT_OUTPUT",
	} {
		if !strings.Contains(hint, want) {
			t.Fatalf("scope hint missing %q; got:\n%s", want, hint)
		}
	}
}

func TestComposeRunPrompt(t *testing.T) {
	got := composeRunPrompt("sys", "user", "hint")
	if got != "sys\n\nuser\n\nhint" {
		t.Fatalf("got %q", got)
	}
	got = composeRunPrompt("sys", "", "hint")
	if got != "sys\n\nhint" {
		t.Fatalf("empty user got %q", got)
	}
	got = composeRunPrompt("", "  user  ", "hint")
	if got != "user\n\nhint" {
		t.Fatalf("empty system got %q", got)
	}
	got = composeRunPrompt("", "", "hint")
	if got != "hint" {
		t.Fatalf("hint only got %q", got)
	}
}

func TestRepoDirName(t *testing.T) {
	cases := []struct {
		repoID uint
		branch string
		want   string
	}{
		{4, "master", "repo-4-master"},
		{4, "refactor", "repo-4-refactor"},
		{4, "feature/foo", "repo-4-feature-foo"},
		{4, "a  b", "repo-4-a-b"},
		{4, "a---b", "repo-4-a-b"},
		{4, "  ", "repo-4-main"},
		{4, "../evil", "repo-4-..-evil"},
	}
	for _, tc := range cases {
		if got := repoDirName(tc.repoID, tc.branch); got != tc.want {
			t.Fatalf("repoDirName(%d, %q)=%q want %q", tc.repoID, tc.branch, got, tc.want)
		}
	}
	long := strings.Repeat("x", 150)
	got := repoDirName(1, long)
	if !strings.HasPrefix(got, "repo-1-") {
		t.Fatalf("unexpected prefix: %q", got)
	}
	if len(got) > len("repo-1-")+100 {
		t.Fatalf("branch segment not truncated: len=%d", len(got))
	}
}

func TestResolveAgentOutputDir(t *testing.T) {
	root := "/tmp/agent-root"
	got, err := resolveAgentOutputDir(root, "")
	if err != nil || got != "/tmp/agent-root/output" {
		t.Fatalf("default output=%q err=%v", got, err)
	}
	got, err = resolveAgentOutputDir(root, "deliverables")
	if err != nil || got != "/tmp/agent-root/deliverables" {
		t.Fatalf("custom output=%q err=%v", got, err)
	}
	if _, err := resolveAgentOutputDir(root, "../escape"); err == nil {
		t.Fatal("expected escape rejection")
	}
	if _, err := resolveAgentOutputDir(root, "/abs"); err == nil {
		t.Fatal("expected absolute rejection")
	}
}
