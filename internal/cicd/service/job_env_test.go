package service

import (
	"encoding/json"
	"strings"
	"testing"

	"bedrock/internal/cicd/model"
	"bedrock/internal/pkg"
)

func TestMergeJobEnvVars(t *testing.T) {
	existing := map[string]string{"KEEP": "old", "DROP": "x", "UPDATE": "v1"}
	keep := "kept"
	update := "v2"
	inputs := []EnvVarInput{
		{Key: "KEEP"},
		{Key: "UPDATE", Value: &update},
		{Key: "NEW", Value: &keep},
	}
	got, err := mergeJobEnvVars(existing, inputs)
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

func TestMergeJobEnvVarsRejectsBadKey(t *testing.T) {
	_, err := mergeJobEnvVars(nil, []EnvVarInput{{Key: "A=B", Value: new("1")}})
	if err == nil {
		t.Fatal("expected invalid key error")
	}
	_, err = mergeJobEnvVars(nil, []EnvVarInput{{Key: "NEW"}})
	if err == nil {
		t.Fatal("expected missing value for new key")
	}
}

func TestEncryptDecryptJobEnvVarsRoundTrip(t *testing.T) {
	if err := pkg.InitEncryption(strings.Repeat("ab", 32)); err != nil {
		t.Fatal(err)
	}
	cipher, err := encryptJobEnvVars(map[string]string{"PAT": "br_secret", "HOST": "http://x"})
	if err != nil {
		t.Fatal(err)
	}
	if cipher == "" || strings.Contains(cipher, "br_secret") {
		t.Fatalf("cipher should be opaque, got %q", cipher)
	}
	job := &model.BuildJob{EnvVarsCipher: cipher}
	projectJobEnvVars(job)
	if len(job.EnvVars) != 2 {
		t.Fatalf("env_vars len = %d", len(job.EnvVars))
	}
	for _, v := range job.EnvVars {
		if !v.HasValue {
			t.Fatalf("expected has_value for %s", v.Key)
		}
	}
	vars, err := decryptJobEnvVars(cipher)
	if err != nil {
		t.Fatal(err)
	}
	if vars["PAT"] != "br_secret" {
		t.Fatalf("decrypt PAT = %q", vars["PAT"])
	}
	keys := jobEnvVarKeys(job)
	if len(keys) != 2 || keys[0] != "HOST" || keys[1] != "PAT" {
		t.Fatalf("keys=%v", keys)
	}
}

func TestApplyJobEnvVarsInput_PreserveWithoutValue(t *testing.T) {
	if err := pkg.InitEncryption(strings.Repeat("ef", 32)); err != nil {
		t.Fatal(err)
	}
	job := &model.BuildJob{}
	secret := "s3cret"
	if err := applyJobEnvVarsInput(job, []EnvVarInput{{Key: "TOKEN", Value: &secret}}); err != nil {
		t.Fatal(err)
	}
	if err := applyJobEnvVarsInput(job, []EnvVarInput{{Key: "TOKEN"}}); err != nil {
		t.Fatal(err)
	}
	vars, err := decryptJobEnvVars(job.EnvVarsCipher)
	if err != nil {
		t.Fatal(err)
	}
	if vars["TOKEN"] != "s3cret" {
		t.Fatalf("TOKEN=%q", vars["TOKEN"])
	}
	raw, _ := json.Marshal(job)
	if strings.Contains(string(raw), "s3cret") {
		t.Fatalf("plaintext leaked in JSON: %s", raw)
	}
}
