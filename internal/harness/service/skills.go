// Skill and agent-definition sync into an agent workspace. These are the
// workspace artifacts opencode discovers natively: skills under .agents/skills/
// — opencode (verified against v1.18.31) scans .claude/.agents dirs with the
// recursive glob skills/**/SKILL.md from the session directory up to the
// worktree, so the per-agent workspace is covered even when it is not the
// project root — and the compiled bedrock-* agent definitions under
// .opencode/agents/.
package service

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"bedrock/internal/harness/provider/oc"
)

// skillMDFile is the per-skill entrypoint opencode discovers.
const skillMDFile = "SKILL.md"

// skillsRelDir is the skill directory inside an agent workspace. It matches
// the bedrock skill-library convention and opencode's .agents scan; only the
// plural "skills" is discovered there (the singular form exists only for
// .opencode/skill).
const skillsRelDir = ".agents/skills"

// legacySkillsRelDir is the injection directory used before the .agents/
// switch. Removed on every sync so opencode cannot discover stale copies.
const legacySkillsRelDir = ".opencode/skills"

// maxSkillNameRunes bounds the normalized skill directory name.
const maxSkillNameRunes = 64

// SkillSource is one bedrock skill package to inject into an agent
// workspace: the bedrock-side name (normalized on copy) plus the source
// directory of the skill's working copy.
type SkillSource struct {
	Name string
	Dir  string
}

// SyncAgentSkills rebuilds {agentWorkspace}/.opencode/skills/ from sources.
// Each skill is copied into a normalized lowercase-hyphen directory and the
// copied SKILL.md frontmatter name is aligned with that directory (opencode
// identifies skills by the frontmatter name; bedrock is the naming source of
// truth). Directories with no source are dropped; an empty source list wipes
// the skills root.
func SyncAgentSkills(agentWorkspace string, sources []SkillSource) error {
	_ = os.RemoveAll(filepath.Join(agentWorkspace, filepath.FromSlash(legacySkillsRelDir)))
	skillsRoot := filepath.Join(agentWorkspace, filepath.FromSlash(skillsRelDir))
	if err := os.RemoveAll(skillsRoot); err != nil {
		return fmt.Errorf("harness skills: reset %s: %w", skillsRoot, err)
	}
	if len(sources) == 0 {
		return nil
	}
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		return fmt.Errorf("harness skills: mkdir %s: %w", skillsRoot, err)
	}
	seen := make(map[string]bool, len(sources))
	for _, src := range sources {
		name := NormalizeSkillName(src.Name)
		if seen[name] {
			return fmt.Errorf("技能名规范化后冲突: %q 与已有目录 %s 重名", src.Name, name)
		}
		seen[name] = true
		dest := filepath.Join(skillsRoot, name)
		if err := copyDir(src.Dir, dest); err != nil {
			return fmt.Errorf("复制技能 %s: %w", src.Name, err)
		}
		if err := alignSkillFrontmatter(filepath.Join(dest, skillMDFile), name); err != nil {
			return fmt.Errorf("校验技能 %s: %w", src.Name, err)
		}
	}
	return nil
}

// SyncAgentDefinition compiles the agent definition for agent into
// {agentWorkspace}/.opencode/agents/ when the agent has customization, and
// always reconciles: bedrock-* artifacts no agent references anymore
// (including this agent's own after it dropped its customization) are
// removed. The definition name sessions should use is AgentDefName.
func SyncAgentDefinition(agentWorkspace string, agent AgentSpec) error {
	input := oc.AgentDefInput{
		Name:          agent.Name,
		Description:   agent.Description,
		SystemPrompt:  agent.SystemPrompt,
		HasSkills:     len(agent.SkillIDs) > 0,
		ModelProvider: agent.ModelProvider,
		ModelID:       agent.ModelID,
		ApprovalMode:  agent.ApprovalMode,
	}
	var referenced []string
	if agent.hasCustomization() {
		referenced = append(referenced, AgentDefKey(agent.ID))
		if _, err := oc.CompileAgentDef(agentWorkspace, AgentDefKey(agent.ID), input); err != nil {
			return err
		}
	}
	return oc.ReconcileAgentDefs(agentWorkspace, referenced)
}

// NormalizeSkillName turns a bedrock skill name into a lowercase-hyphen
// single path segment (spaces, underscores and separators become hyphens;
// other characters drop; runs collapse). Empty names fall back to "skill".
func NormalizeSkillName(name string) string {
	var b strings.Builder
	prevHyphen := false
	for _, r := range strings.TrimSpace(strings.ToLower(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevHyphen = false
		case r == '-', r == ' ', r == '_', r == '.', r == '/', r == '\\', r == '\t':
			if !prevHyphen && b.Len() > 0 {
				b.WriteByte('-')
				prevHyphen = true
			}
		default:
			if unicode.IsSpace(r) && !prevHyphen && b.Len() > 0 {
				b.WriteByte('-')
				prevHyphen = true
			}
			// any other character is dropped
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "skill"
	}
	if runes := []rune(out); len(runes) > maxSkillNameRunes {
		out = strings.Trim(string(runes[:maxSkillNameRunes]), "-")
		if out == "" {
			return "skill"
		}
	}
	return out
}

// alignSkillFrontmatter validates the injected SKILL.md and makes its
// frontmatter name identical to the (normalized) directory name: an
// existing name line is rewritten, a missing one inserted, a missing
// frontmatter block prepended.
func alignSkillFrontmatter(path, name string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取 %s: %w", skillMDFile, err)
	}
	if err := os.WriteFile(path, alignFrontmatterName(raw, name), 0o644); err != nil {
		return fmt.Errorf("写回 %s: %w", skillMDFile, err)
	}
	return nil
}

// alignFrontmatterName rewrites raw so its frontmatter carries
// "name: <name>" (existing name line replaced, missing one inserted, missing
// frontmatter block prepended). Lines keep their order otherwise.
func alignFrontmatterName(raw []byte, name string) []byte {
	nameLine := []byte("name: " + name)
	text := strings.TrimPrefix(string(raw), "\xEF\xBB\xBF")
	text = strings.TrimLeft(text, "\r\n \t")

	if !isFenceLine(text) {
		return []byte("---\n" + string(nameLine) + "\n---\n\n" + string(raw))
	}
	lines := strings.Split(text, "\n")
	close := -1
	for i := 1; i < len(lines); i++ {
		if isFenceLine(strings.TrimRight(lines[i], "\r")) {
			close = i
			break
		}
	}
	if close < 0 {
		// Malformed (unclosed fence): prepend a fresh frontmatter instead of
		// failing the whole skill over broken metadata.
		return []byte("---\n" + string(nameLine) + "\n---\n\n" + string(raw))
	}
	front := lines[1:close]
	body := strings.Join(lines[close+1:], "\n")
	replaced := false
	for i, line := range front {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "name:") {
			front[i] = string(nameLine)
			replaced = true
			break
		}
	}
	if !replaced {
		front = append([]string{string(nameLine)}, front...)
	}
	var b strings.Builder
	b.WriteString("---\n")
	for _, line := range front {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteString("---\n")
	b.WriteString(body)
	return []byte(b.String())
}

// isFenceLine reports whether the first line of s is a bare "---" fence
// (trailing whitespace tolerated).
func isFenceLine(s string) bool {
	line := s
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	return strings.TrimRight(line, "\r \t") == "---"
}

// copyDir recursively copies src into dest (created 0755; files 0644).
func copyDir(src, dest string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		in.Close()
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}
