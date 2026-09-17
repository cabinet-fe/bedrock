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
// 带 value：设置/更新；已有键未带 value：保留；请求中消失的键删除。
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
		// 键存在即 has_value=true，前端用占位符表示「留空保留」
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

// mergeAgentEnvVars 按全量键列表合并：带 value 则写入；无 value 则保留旧值；缺键删除。
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

// writeAgentEnvFile 解密环境变量，写入 {agentRoot}/.env（权限固定 0600，纠正历史残留的宽权限），返回绝对路径与明文 map。
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

// escapeDotEnvValue 对含空白/特殊字符的值做双引号基础转义。
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
