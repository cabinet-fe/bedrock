package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"bedrock/internal/ai/model"
	"bedrock/internal/pkg"
)

// EnvVarInput is one env var in create/update payloads.
// With value: set/update; existing key without value: keep; keys missing from the request are deleted.
type EnvVarInput struct {
	Key   string  `json:"key"`
	Value *string `json:"value"`
}

func projectAgentEnvVars(agent *model.AiAgent) {
	if agent == nil {
		return
	}
	vars, err := decryptAgentEnvVars(agent.EnvVarsCipher)
	if err != nil {
		agent.EnvVars = []model.EnvVarView{}
		return
	}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]model.EnvVarView, 0, len(keys))
	for _, k := range keys {
		// Key present means has_value=true; the frontend uses a placeholder for "keep existing"
		out = append(out, model.EnvVarView{Key: k, HasValue: true})
	}
	agent.EnvVars = out
}

func decryptAgentEnvVars(cipherText string) (map[string]string, error) {
	cipherText = strings.TrimSpace(cipherText)
	if cipherText == "" {
		return map[string]string{}, nil
	}
	plain, err := pkg.Decrypt(cipherText)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(plain) == "" {
		return map[string]string{}, nil
	}
	var vars map[string]string
	if err := json.Unmarshal([]byte(plain), &vars); err != nil {
		return nil, err
	}
	if vars == nil {
		vars = map[string]string{}
	}
	return vars, nil
}

func encryptAgentEnvVars(vars map[string]string) (string, error) {
	if vars == nil {
		vars = map[string]string{}
	}
	b, err := json.Marshal(vars)
	if err != nil {
		return "", err
	}
	return pkg.Encrypt(string(b))
}

func validateEnvVarKey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("环境变量 key 不能为空")
	}
	if strings.ContainsAny(key, "=\n\r") {
		return errors.New("环境变量 key 不能包含 = 或换行")
	}
	return nil
}

// mergeAgentEnvVars merges by the full key list: a value writes it; no value
// keeps the old one; a missing key deletes it.
func mergeAgentEnvVars(existing map[string]string, inputs []EnvVarInput) (map[string]string, error) {
	if existing == nil {
		existing = map[string]string{}
	}
	if inputs == nil {
		inputs = []EnvVarInput{}
	}
	out := make(map[string]string, len(inputs))
	seen := map[string]struct{}{}
	for _, in := range inputs {
		key := strings.TrimSpace(in.Key)
		if err := validateEnvVarKey(key); err != nil {
			return nil, err
		}
		if _, dup := seen[key]; dup {
			return nil, fmt.Errorf("环境变量 key 重复: %s", key)
		}
		seen[key] = struct{}{}
		if in.Value != nil {
			out[key] = *in.Value
			continue
		}
		old, ok := existing[key]
		if !ok {
			return nil, fmt.Errorf("新建环境变量 %s 必须提供 value", key)
		}
		out[key] = old
	}
	return out, nil
}

func applyAgentEnvVarsInput(agent *model.AiAgent, inputs []EnvVarInput) error {
	existing, err := decryptAgentEnvVars(agent.EnvVarsCipher)
	if err != nil {
		return err
	}
	merged, err := mergeAgentEnvVars(existing, inputs)
	if err != nil {
		return err
	}
	cipher, err := encryptAgentEnvVars(merged)
	if err != nil {
		return err
	}
	agent.EnvVarsCipher = cipher
	return nil
}

func envVarKeys(agent *model.AiAgent) []string {
	vars, err := decryptAgentEnvVars(agent.EnvVarsCipher)
	if err != nil {
		return nil
	}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// writeAgentEnvFile decrypts env vars, writes {agentRoot}/.env (mode 0600,
// correcting historically too-wide permissions), and returns the absolute
// path plus the plaintext map.
func (s *AgentService) writeAgentEnvFile(agent *model.AiAgent, agentRoot string) (envFile string, vars map[string]string, err error) {
	vars, err = decryptAgentEnvVars(agent.EnvVarsCipher)
	if err != nil {
		return "", nil, fmt.Errorf("解密智能体环境变量失败: %w", err)
	}
	content := formatDotEnv(vars)
	path := filepath.Join(agentRoot, ".env")
	if err := writeFileIfUnchanged(path, []byte(content), 0o600); err != nil {
		return "", nil, err
	}
	abs, absErr := filepath.Abs(path)
	if absErr != nil {
		abs = path
	}
	return abs, vars, nil
}

func formatDotEnv(vars map[string]string) string {
	if len(vars) == 0 {
		return ""
	}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(escapeDotEnvValue(vars[k]))
		b.WriteByte('\n')
	}
	return b.String()
}

// escapeDotEnvValue applies basic double-quote escaping to values with
// whitespace or special characters.
func escapeDotEnvValue(value string) string {
	if value == "" {
		return `""`
	}
	needQuote := false
	for _, r := range value {
		if r <= ' ' || r == '"' || r == '\'' || r == '\\' || r == '#' || r == '=' || r == '$' || r == '`' {
			needQuote = true
			break
		}
	}
	if !needQuote {
		return value
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '\\', '"', '$', '`':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
