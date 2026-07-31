package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"bedrock/internal/cicd/model"
	"bedrock/internal/pkg"
)

// EnvVarInput is one Key-Value env var in create/update payloads.
// 带 value：设置/更新；已有键未带 value：保留；请求中消失的键删除。
type EnvVarInput struct {
	Key   string  `json:"key"`
	Value *string `json:"value"`
}

func projectJobEnvVars(job *model.BuildJob) {
	if job == nil {
		return
	}
	vars, err := decryptJobEnvVars(job.EnvVarsCipher)
	if err != nil {
		job.EnvVars = []model.EnvVarView{}
		return
	}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]model.EnvVarView, 0, len(keys))
	for _, k := range keys {
		out = append(out, model.EnvVarView{Key: k, HasValue: true})
	}
	job.EnvVars = out
}

func decryptJobEnvVars(cipherText string) (map[string]string, error) {
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

func encryptJobEnvVars(vars map[string]string) (string, error) {
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

// mergeJobEnvVars 按全量键列表合并：带 value 则写入；无 value 则保留旧值；缺键删除。
func mergeJobEnvVars(existing map[string]string, inputs []EnvVarInput) (map[string]string, error) {
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

func applyJobEnvVarsInput(job *model.BuildJob, inputs []EnvVarInput) error {
	existing, err := decryptJobEnvVars(job.EnvVarsCipher)
	if err != nil {
		return err
	}
	merged, err := mergeJobEnvVars(existing, inputs)
	if err != nil {
		return err
	}
	cipher, err := encryptJobEnvVars(merged)
	if err != nil {
		return err
	}
	job.EnvVarsCipher = cipher
	return nil
}

// jobEnvVarKeys returns sorted Key-Value env keys (no values) for run snapshots.
func jobEnvVarKeys(job *model.BuildJob) []string {
	if job == nil {
		return []string{}
	}
	vars, err := decryptJobEnvVars(job.EnvVarsCipher)
	if err != nil {
		return []string{}
	}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
