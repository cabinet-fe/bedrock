package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bedrock/internal/ai/model"
	"bedrock/internal/pkg"
)

func TestMergeAgentEnvVars(t *testing.T) {
	existing := map[string]string{"KEEP": "old", "DROP": "x", "UPDATE": "v1"}
	keep := "kept"
	update := "v2"
	inputs := []EnvVarInput{
		{Key: "KEEP"},
		{Key: "UPDATE", Value: &update},
		{Key: "NEW", Value: &keep},
	}
	got, err := mergeAgentEnvVars(existing, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if got["KEEP"] != "old" || got["UPDATE"] != "v2" || got["NEW"] != "kept" {
		t.Fatalf("unexpected merge: %#v", got)
	}
	if _, ok := got["DROP"]; ok {
		t.Fatal("DROP should be deleted")
	}
}

func TestMergeAgentEnvVarsRejectsBadKey(t *testing.T) {
	_, err := mergeAgentEnvVars(nil, []EnvVarInput{{Key: "A=B", Value: new("1")}})
	if err == nil {
		t.Fatal("expected invalid key error")
	}
	_, err = mergeAgentEnvVars(nil, []EnvVarInput{{Key: "NEW"}})
	if err == nil {
		t.Fatal("expected missing value for new key")
	}
}

func TestEncryptDecryptAgentEnvVarsRoundTrip(t *testing.T) {
	if err := pkg.InitEncryption(strings.Repeat("ab", 32)); err != nil {
		t.Fatal(err)
	}
	cipher, err := encryptAgentEnvVars(map[string]string{"PAT": "br_secret", "HOST": "http://x"})
	if err != nil {
		t.Fatal(err)
	}
	if cipher == "" || strings.Contains(cipher, "br_secret") {
		t.Fatalf("cipher should be opaque, got %q", cipher)
	}
	agent := &model.AiAgent{EnvVarsCipher: cipher}
	projectAgentEnvVars(agent)
	if len(agent.EnvVars) != 2 {
		t.Fatalf("env_vars len = %d", len(agent.EnvVars))
	}
	for _, v := range agent.EnvVars {
		if !v.HasValue {
			t.Fatalf("expected has_value for %s", v.Key)
		}
	}
	vars, err := decryptAgentEnvVars(cipher)
	if err != nil {
		t.Fatal(err)
	}
	if vars["PAT"] != "br_secret" {
		t.Fatalf("decrypt PAT = %q", vars["PAT"])
	}
}

func TestWriteAgentEnvFile(t *testing.T) {
	if err := pkg.InitEncryption(strings.Repeat("cd", 32)); err != nil {
		t.Fatal(err)
	}
	cipher, err := encryptAgentEnvVars(map[string]string{
		"SIMPLE": "ok",
		"SPACE":  "hello world",
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := &AgentService{}
	root := t.TempDir()
	path, vars, err := svc.writeAgentEnvFile(&model.AiAgent{EnvVarsCipher: cipher}, root)
	if err != nil {
		t.Fatal(err)
	}
	if vars["SIMPLE"] != "ok" {
		t.Fatalf("vars=%#v", vars)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "SIMPLE=ok\n") {
		t.Fatalf("missing SIMPLE: %s", text)
	}
	if !strings.Contains(text, `SPACE="hello world"`) {
		t.Fatalf("SPACE should be quoted: %s", text)
	}
	if filepath.Base(path) != ".env" {
		t.Fatalf("path=%s", path)
	}
}
