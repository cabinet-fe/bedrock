package oc

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// Approval modes mirrored from ai_agents.approval_mode.
const (
	// ApprovalModeManual asks the user for every permission request.
	ApprovalModeManual = "manual"
	// ApprovalModeAuto answers permission requests automatically (unattended).
	ApprovalModeAuto = "auto"
)

// AgentDefPrefix marks agent definitions compiled by bedrock; anything else
// in the agents directory belongs to the user or opencode itself.
const AgentDefPrefix = "bedrock-"

// agentsRelDir is the project-level agent definition directory inside a
// workspace; opencode discovers every *.md there as an agent.
const agentsRelDir = ".opencode/agents"

// descriptionMaxRunes bounds the frontmatter description summary.
const descriptionMaxRunes = 160

// AgentDefInput is the agent projection the compiler needs. The zero value
// compiles nothing (see NeedsAgentDef).
type AgentDefInput struct {
	Name          string
	Description   string
	SystemPrompt  string
	HasSkills     bool
	ModelProvider string
	ModelID       string
	ApprovalMode  string // manual | auto
}

// AgentDefFileName returns the compiled file name for an agent key
// (e.g. "agent-3" -> "bedrock-agent-3.md").
func AgentDefFileName(agentKey string) string {
	return AgentDefPrefix + agentKey + ".md"
}

// NeedsAgentDef reports whether the agent requires a compiled definition:
// any system prompt, bound skill, or model id does; a bare agent uses the
// builtin build agent instead.
func NeedsAgentDef(in AgentDefInput) bool {
	return strings.TrimSpace(in.SystemPrompt) != "" ||
		in.HasSkills ||
		in.ModelID != ""
}

// CompileAgentDef renders the agent definition and atomically writes it to
// {workspace}/.opencode/agents/bedrock-<agentKey>.md (tmp + rename). An input
// without customization compiles nothing; sessions then use the builtin
// build agent. Returns the written path, or "" when nothing was compiled.
func CompileAgentDef(workspace, agentKey string, in AgentDefInput) (string, error) {
	if agentKey == "" {
		return "", fmt.Errorf("oc compile: agent key is required")
	}
	if !NeedsAgentDef(in) {
		return "", nil
	}
	dir := filepath.Join(workspace, filepath.FromSlash(agentsRelDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("oc compile: agents dir: %w", err)
	}
	path := filepath.Join(dir, AgentDefFileName(agentKey))
	if err := writeFileAtomic(path, renderAgentDef(agentKey, in), 0o644); err != nil {
		return "", fmt.Errorf("oc compile %s: %w", path, err)
	}
	return path, nil
}

// ReconcileAgentDefs removes compiled bedrock-*.md artifacts under
// {workspace}/.opencode/agents that no referenced agent key claims anymore
// (including leftover compile tmp files). Foreign files are untouched.
func ReconcileAgentDefs(workspace string, referencedKeys []string) error {
	dir := filepath.Join(workspace, filepath.FromSlash(agentsRelDir))
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("oc reconcile: agents dir: %w", err)
	}
	keep := make(map[string]bool, len(referencedKeys))
	for _, key := range referencedKeys {
		keep[AgentDefFileName(key)] = true
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		stale := strings.HasPrefix(name, AgentDefPrefix) && strings.HasSuffix(name, ".md") && !keep[name]
		// tmp leftovers from interrupted atomic writes are dot-prefixed.
		tmpLeftover := strings.HasPrefix(name, "."+AgentDefPrefix) && strings.Contains(name, ".tmp-")
		if !stale && !tmpLeftover {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("oc reconcile %s: %w", name, err)
		}
	}
	return nil
}

// renderAgentDef builds the full markdown document: YAML frontmatter
// (description, model, permission) plus the system prompt body with the
// .env read hint appended.
func renderAgentDef(agentKey string, in AgentDefInput) []byte {
	var b bytes.Buffer
	b.WriteString("---\n")
	fmt.Fprintf(&b, "description: %s\n", yamlQuote(agentDefDescription(agentKey, in)))
	if in.ModelProvider != "" && in.ModelID != "" {
		fmt.Fprintf(&b, "model: %s\n", yamlQuote(in.ModelProvider+"/"+in.ModelID))
	}
	if in.ApprovalMode == ApprovalModeAuto {
		// Allow-rules exist only for unattended agents; manual mode keeps the
		// default ask behavior without compiled rules.
		b.WriteString("permission:\n  skill: allow\n")
	}
	b.WriteString("---\n\n")

	if body := strings.TrimSpace(in.SystemPrompt); body != "" {
		b.WriteString(body)
		b.WriteString("\n\n")
	}
	b.WriteString(envReadHint)
	b.WriteByte('\n')
	return b.Bytes()
}

// agentDefDescription summarizes the agent for agent pickers:
// "<name>: <description summary>" with whitespace collapsed and truncated.
func agentDefDescription(agentKey string, in AgentDefInput) string {
	summary := collapseSpace(in.Description)
	summary = truncateRunes(summary, descriptionMaxRunes)
	name := collapseSpace(in.Name)
	if name != "" && summary != "" {
		return name + ": " + summary
	}
	if name != "" {
		return name
	}
	if summary != "" {
		return summary
	}
	return "bedrock agent " + agentKey
}

// envReadHint teaches the session agent where its env vars live. The .env
// file is always present in the workspace (written by SyncAgentWorkspace).
const envReadHint = "运行所需环境变量已写入工作区根目录的 .env 文件（每行 KEY=VALUE）。" +
	"需要取值时读取该文件；其中的值可能包含密钥，不要原样展示给用户或提交到仓库。" +
	" Required environment variables live in the .env file at the workspace root (KEY=VALUE per line)." +
	" Read it when you need a value; values may contain secrets — never print or commit them."

// yamlQuote renders s as a double-quoted YAML scalar.
func yamlQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\', '"':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// collapseSpace trims s and reduces every whitespace run to one space.
func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// truncateRunes cuts s to max runes, appending an ellipsis when truncated.
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	cut := runes[:max]
	for len(cut) > 0 && unicode.IsSpace(cut[len(cut)-1]) {
		cut = cut[:len(cut)-1]
	}
	return string(cut) + "…"
}

// writeFileAtomic writes data to path via a tmp file in the same directory
// followed by rename, so readers never observe a partial document.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = tmp.Close(); _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}
