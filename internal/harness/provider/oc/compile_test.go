package oc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// updateGolden is declared in contract_test.go (shared -update flag).

// goldenCase is one cell of the compile matrix: {prompt} x {skills} x {model}
// x {manual,auto}. The 2x2x2 prompt/skills/model cells without any
// customization must not compile at all.
type goldenCase struct {
	prompt   bool
	skills   bool
	model    bool
	approval string
}

func (c goldenCase) name() string {
	part := func(on bool, on_, off string) string {
		if on {
			return on_
		}
		return off
	}
	return fmt.Sprintf("%s-%s-%s-%s",
		part(c.prompt, "prompt", "noprompt"),
		part(c.skills, "skills", "noskills"),
		part(c.model, "model", "nomodel"),
		c.approval,
	)
}

func (c goldenCase) input() AgentDefInput {
	in := AgentDefInput{
		Name:                "Release Helper",
		Description:         " Assists with   release notes\nand changelogs. ",
		ApprovalMode:        c.approval,
		HasSkills:           c.skills,
		InjectDefaultPrompt: true,
	}
	if c.prompt {
		in.SystemPrompt = "你是发布助手，负责整理变更日志。"
	}
	if c.model {
		in.ModelProvider = "anthropic"
		in.ModelID = "claude-sonnet-4-5"
	}
	return in
}

// TestAgentDefGoldenMatrix snapshots bedrock-<key>.md for every
// {prompt x skills x model x approval} combination.
func TestAgentDefGoldenMatrix(t *testing.T) {
	for _, approval := range []string{ApprovalModeManual, ApprovalModeAuto} {
		for _, prompt := range []bool{false, true} {
			for _, skills := range []bool{false, true} {
				for _, model := range []bool{false, true} {
					c := goldenCase{prompt: prompt, skills: skills, model: model, approval: approval}
					t.Run(c.name(), func(t *testing.T) {
						in := c.input()
						if !NeedsAgentDef(in) {
							if c.prompt || c.skills || c.model {
								t.Fatalf("%s: NeedsAgentDef must hold for customized input", c.name())
							}
							// No customization: nothing is compiled even when called.
							workspace := t.TempDir()
							path, err := CompileAgentDef(workspace, "agent-7", in)
							if err != nil {
								t.Fatalf("compile: %v", err)
							}
							if path != "" {
								t.Fatalf("no-customization input compiled %q", path)
							}
							assertNoAgentDefs(t, workspace)
							return
						}

						workspace := t.TempDir()
						path, err := CompileAgentDef(workspace, "agent-7", in)
						if err != nil {
							t.Fatalf("compile: %v", err)
						}
						if path == "" {
							t.Fatalf("customized input compiled nothing")
						}
						got, err := os.ReadFile(path)
						if err != nil {
							t.Fatalf("read artifact: %v", err)
						}
						goldenPath := filepath.Join("testdata", "agentdefs", c.name()+".md")
						if *updateGolden {
							if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
								t.Fatalf("mkdir golden dir: %v", err)
							}
							if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
								t.Fatalf("write golden: %v", err)
							}
							return
						}
						want, err := os.ReadFile(goldenPath)
						if err != nil {
							t.Fatalf("read golden (run with -update to create): %v", err)
						}
						if string(got) != string(want) {
							t.Fatalf("agent def drifted from golden %s;\n--- got ---\n%s\n--- want ---\n%s",
								goldenPath, got, want)
						}
					})
				}
			}
		}
	}
}

func assertNoAgentDefs(t *testing.T, workspace string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(workspace, ".opencode", "agents"))
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("agents dir must stay empty, got %v", entries)
	}
}

// TestCompileAgentDefAtomicRewrite asserts recompiles replace the artifact
// and never leave tmp files behind.
func TestCompileAgentDefAtomicRewrite(t *testing.T) {
	workspace := t.TempDir()
	in := AgentDefInput{Name: "n", SystemPrompt: "v1", ApprovalMode: ApprovalModeManual}
	path, err := CompileAgentDef(workspace, "agent-1", in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(readFile(t, path), "v1") != 1 {
		t.Fatalf("v1 artifact wrong: %s", readFile(t, path))
	}
	in.SystemPrompt = "v2"
	if _, err := CompileAgentDef(workspace, "agent-1", in); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, path)
	if strings.Contains(got, "v1") || !strings.Contains(got, "v2") {
		t.Fatalf("artifact not replaced: %s", got)
	}
	assertNoTmpLeftovers(t, filepath.Dir(path))
}

// TestReconcileAgentDefs asserts stale bedrock artifacts are removed while
// referenced ones and foreign files survive.
func TestReconcileAgentDefs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".opencode", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := []string{
		"bedrock-agent-1.md", // referenced, stays
		"bedrock-agent-2.md", // unreferenced, removed
		"bedrock-orphan.md",  // unreferenced, removed
		"plan.md",            // foreign, stays
		"notes.txt",          // foreign, stays
		".bedrock-agent-9.md.tmp-123",
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := ReconcileAgentDefs(filepath.Dir(filepath.Dir(dir)), []string{"agent-1"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bedrock-agent-1.md", "plan.md", "notes.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("%s must survive: %v", name, err)
		}
	}
	for _, name := range []string{"bedrock-agent-2.md", "bedrock-orphan.md", ".bedrock-agent-9.md.tmp-123"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s must be removed, err=%v", name, err)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestWriteFileAtomicSkipsIdentical(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	data := []byte("{\n  \"x\": 1\n}\n")
	if err := writeFileAtomic(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("identical rewrite bumped mtime: %s -> %s", before.ModTime(), after.ModTime())
	}
	if err := writeFileAtomic(path, []byte("{\n  \"x\": 2\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); !strings.Contains(got, `"x": 2`) {
		t.Fatalf("content not replaced: %s", got)
	}
}

func assertNoTmpLeftovers(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("tmp leftover: %s", e.Name())
		}
	}
}
