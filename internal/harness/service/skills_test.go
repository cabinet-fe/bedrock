package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSkillDir creates a source skill directory with the given SKILL.md
// content plus one extra file.
func writeSkillDir(t *testing.T, parent, name, skillMD string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	if err := os.MkdirAll(filepath.Join(dir, "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "references", "notes.md"), []byte("refs"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestNormalizeSkillName(t *testing.T) {
	cases := map[string]string{
		"Deploy Helper":     "deploy-helper",
		"  Java API Docs ":  "java-api-docs",
		"java_api.docs":     "java-api-docs",
		"JS/TS Lint":        "js-ts-lint",
		"a\\b":              "a-b",
		"--hello--":         "hello",
		"中文技能":              "skill",
		"":                  "skill",
		"UPPER":             "upper",
		"multiple   spaces": "multiple-spaces",
	}
	for in, want := range cases {
		if got := NormalizeSkillName(in); got != want {
			t.Fatalf("NormalizeSkillName(%q) = %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("a", 100)
	if got := NormalizeSkillName(long); len(got) != maxSkillNameRunes {
		t.Fatalf("long name not capped: %d", len(got))
	}
}

// TestSyncAgentSkillsNormalizesNamesAndFrontmatter covers the P4 skill
// injection acceptance: lowercase-hyphen directories, SKILL.md frontmatter
// name aligned with the directory, full rebuild dropping stale dirs.
func TestSyncAgentSkillsNormalizesNamesAndFrontmatter(t *testing.T) {
	parent := t.TempDir()
	srcs := []SkillSource{
		{
			// mismatching frontmatter name gets rewritten to the dir name
			Name: "Deploy Helper",
			Dir:  writeSkillDir(t, parent, "a", "---\nname: Old Name\ndescription: d\n---\n\n# body\n"),
		},
		{
			// no frontmatter at all gets one prepended
			Name: "raw",
			Dir:  writeSkillDir(t, parent, "b", "# just a body\n"),
		},
		{
			// already aligned stays untouched
			Name: "aligned",
			Dir:  writeSkillDir(t, parent, "c", "---\nname: aligned\n---\n\n# ok\n"),
		},
	}

	ws := t.TempDir()
	// stale leftovers from a previous bind must disappear, and so must the
	// legacy .opencode/skills injection dir from before the .agents switch.
	stale := filepath.Join(ws, ".agents", "skills", "stale-skill")
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(ws, ".opencode", "skills", "legacy-skill")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := SyncAgentSkills(ws, srcs); err != nil {
		t.Fatal(err)
	}

	skills := filepath.Join(ws, ".agents", "skills")
	for _, dir := range []string{"deploy-helper", "raw", "aligned"} {
		data, err := os.ReadFile(filepath.Join(skills, dir, "SKILL.md"))
		if err != nil {
			t.Fatalf("skill %s missing: %v", dir, err)
		}
		if !strings.HasPrefix(string(data), "---\nname: "+dir+"\n") {
			t.Fatalf("skill %s frontmatter not aligned with dir:\n%s", dir, data)
		}
	}
	if _, err := os.Stat(filepath.Join(skills, "deploy-helper", "references", "notes.md")); err != nil {
		t.Fatalf("extra skill files not copied: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale skill dir must be removed, err=%v", err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy .opencode/skills dir must be removed, err=%v", err)
	}
	entries, err := os.ReadDir(skills)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("skills dir = %v, want exactly the 3 injected", entries)
	}
}

// TestSyncAgentSkillsEmptyWipesRoot asserts an unbound agent keeps no skill
// dirs across syncs.
func TestSyncAgentSkillsEmptyWipesRoot(t *testing.T) {
	ws := t.TempDir()
	old := filepath.Join(ws, ".agents", "skills", "old")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "SKILL.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(ws, ".opencode", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SyncAgentSkills(ws, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws, ".agents", "skills")); !os.IsNotExist(err) {
		t.Fatalf("skills root must be wiped, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, ".opencode", "skills")); !os.IsNotExist(err) {
		t.Fatalf("legacy skills root must be wiped, err=%v", err)
	}
}

func TestSyncAgentSkillsSkipsUnchanged(t *testing.T) {
	parent := t.TempDir()
	src := writeSkillDir(t, parent, "a", "---\nname: aligned\n---\n\n# ok\n")
	ws := t.TempDir()
	sources := []SkillSource{{Name: "aligned", Dir: src}}
	if err := SyncAgentSkills(ws, sources); err != nil {
		t.Fatal(err)
	}
	skillMD := filepath.Join(ws, ".agents", "skills", "aligned", "SKILL.md")
	before, err := os.Stat(skillMD)
	if err != nil {
		t.Fatal(err)
	}
	if err := SyncAgentSkills(ws, sources); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(skillMD)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("unchanged skill was rewritten: mtime %s -> %s", before.ModTime(), after.ModTime())
	}
}

// TestSyncAgentSkillsMissingSkillMDFails asserts a source without SKILL.md
// fails the sync instead of injecting a broken skill.
func TestSyncAgentSkillsMissingSkillMDFails(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "broken")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ws := t.TempDir()
	err := SyncAgentSkills(ws, []SkillSource{{Name: "broken", Dir: dir}})
	if err == nil || !strings.Contains(err.Error(), "SKILL.md") {
		t.Fatalf("want SKILL.md error, got %v", err)
	}
}

// TestSyncAgentSkillsNameConflictFails asserts two skills normalizing to the
// same directory fail loudly instead of silently overwriting each other.
func TestSyncAgentSkillsNameConflictFails(t *testing.T) {
	parent := t.TempDir()
	srcs := []SkillSource{
		{Name: "deploy helper", Dir: writeSkillDir(t, parent, "a", "# a")},
		{Name: "Deploy_Helper", Dir: writeSkillDir(t, parent, "b", "# b")},
	}
	ws := t.TempDir()
	err := SyncAgentSkills(ws, srcs)
	if err == nil || !strings.Contains(err.Error(), "冲突") {
		t.Fatalf("want conflict error, got %v", err)
	}
}

// TestSyncAgentDefinitionCompileAndReconcile covers the P4 agent-def
// acceptance: customized agents compile bedrock-agent-<id>.md, plain agents
// compile nothing, and dropped customization removes the stale artifact.
func TestSyncAgentDefinitionCompileAndReconcile(t *testing.T) {
	ws := t.TempDir()
	agentsDir := filepath.Join(ws, ".opencode", "agents")

	customized := AgentSpec{
		ID: 7, Name: "Rel", Description: "release helper",
		SystemPrompt: "sp", SkillIDs: []uint{1},
		ModelProvider: "anthropic", ModelID: "claude-sonnet-4-5",
		ApprovalMode: "auto",
	}
	if err := SyncAgentDefinition(ws, customized); err != nil {
		t.Fatal(err)
	}
	def := filepath.Join(agentsDir, "bedrock-agent-7.md")
	data, err := os.ReadFile(def)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		`description: "Rel: release helper"`,
		`model: "anthropic/claude-sonnet-4-5"`,
		"permission:\n  skill: allow",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("compiled def misses %q:\n%s", want, text)
		}
	}
	if AgentDefName(customized) != "bedrock-agent-7" {
		t.Fatalf("AgentDefName = %q", AgentDefName(customized))
	}

	// Dropping all customization must remove the stale artifact.
	plain := AgentSpec{ID: 7, ApprovalMode: "manual"}
	if err := SyncAgentDefinition(ws, plain); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(def); !os.IsNotExist(err) {
		t.Fatalf("stale bedrock def must be reconciled away, err=%v", err)
	}
	if AgentDefName(plain) != "build" {
		t.Fatalf("plain agent must use builtin build, got %q", AgentDefName(plain))
	}
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("agents dir must be empty after reconcile, got %v", entries)
	}
}

// TestSyncAgentDefinitionManualOmitsPermission asserts manual approval keeps
// the default ask behavior (no compiled allow rules).
func TestSyncAgentDefinitionManualOmitsPermission(t *testing.T) {
	ws := t.TempDir()
	agent := AgentSpec{ID: 9, SystemPrompt: "sp", ApprovalMode: "manual"}
	if err := SyncAgentDefinition(ws, agent); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(ws, ".opencode", "agents", "bedrock-agent-9.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "permission") {
		t.Fatalf("manual def must not compile permission rules:\n%s", data)
	}
}
