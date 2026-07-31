package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"bedrock/internal/pkg"
)

// graphEnvCipherPrefix marks AES-GCM-encrypted env values inside graph_json.
const graphEnvCipherPrefix = "enc:v1:"

// EncryptGraphEnvVars prepares a client-submitted graph_json for persistence:
// plaintext env values are encrypted, value-less keys inherit the stored cipher
// from oldGraphJSON (same node id + key), already-prefixed values pass through.
func EncryptGraphEnvVars(newGraphJSON, oldGraphJSON string) (string, error) {
	g, err := ParsePipelineGraph(newGraphJSON)
	if err != nil {
		return "", err
	}
	old := storedGraphEnvValues(oldGraphJSON)
	changed := false
	for ni := range g.Nodes {
		for ei := range g.Nodes[ni].Data.EnvVars {
			kv := &g.Nodes[ni].Data.EnvVars[ei]
			if err := validateEnvVarKey(kv.Key); err != nil {
				return "", fmt.Errorf("节点 %s 变量无效: %w", g.Nodes[ni].ID, err)
			}
			kv.HasValue = false
			switch {
			case kv.Value == "":
				stored, ok := old[g.Nodes[ni].ID][kv.Key]
				if !ok {
					return "", fmt.Errorf("节点 %s 新建环境变量 %s 必须提供 value", g.Nodes[ni].ID, kv.Key)
				}
				kv.Value = stored
				changed = true
			case strings.HasPrefix(kv.Value, graphEnvCipherPrefix):
				// already encrypted (e.g. copied snapshot) — keep verbatim
			default:
				cipher, err := pkg.Encrypt(kv.Value)
				if err != nil {
					return "", err
				}
				kv.Value = graphEnvCipherPrefix + cipher
				changed = true
			}
		}
	}
	if !changed {
		return newGraphJSON, nil
	}
	b, err := json.Marshal(g)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// SanitizeGraphEnvVars strips cipher values for read APIs: {key, has_value}.
// On any parse failure the input is returned unchanged (fail-open for reads).
func SanitizeGraphEnvVars(graphJSON string) string {
	g, err := ParsePipelineGraph(graphJSON)
	if err != nil {
		return graphJSON
	}
	changed := false
	for ni := range g.Nodes {
		for ei := range g.Nodes[ni].Data.EnvVars {
			kv := &g.Nodes[ni].Data.EnvVars[ei]
			if kv.Value != "" || kv.HasValue {
				kv.HasValue = kv.Value != ""
				kv.Value = ""
				changed = true
			}
		}
	}
	if !changed {
		return graphJSON
	}
	b, err := json.Marshal(g)
	if err != nil {
		return graphJSON
	}
	return string(b)
}

// DecryptNodeEnvVars resolves a node's env overrides to plaintext for enqueue.
func DecryptNodeEnvVars(vars []PipelineNodeEnvVar) (map[string]string, error) {
	if len(vars) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(vars))
	for _, kv := range vars {
		key := strings.TrimSpace(kv.Key)
		if key == "" {
			continue
		}
		v := kv.Value
		if strings.HasPrefix(v, graphEnvCipherPrefix) {
			plain, err := pkg.Decrypt(strings.TrimPrefix(v, graphEnvCipherPrefix))
			if err != nil {
				return nil, fmt.Errorf("解密节点变量 %s 失败: %w", key, err)
			}
			v = plain
		}
		out[key] = v
	}
	return out, nil
}

// storedGraphEnvValues maps nodeID → key → stored (encrypted) value.
func storedGraphEnvValues(graphJSON string) map[string]map[string]string {
	out := map[string]map[string]string{}
	g, err := ParsePipelineGraph(graphJSON)
	if err != nil {
		return out
	}
	for _, n := range g.Nodes {
		for _, kv := range n.Data.EnvVars {
			if kv.Value == "" {
				continue
			}
			m, ok := out[n.ID]
			if !ok {
				m = map[string]string{}
				out[n.ID] = m
			}
			m[kv.Key] = kv.Value
		}
	}
	return out
}
