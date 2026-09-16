package oc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWorkspaceConfigGolden snapshots the rendered opencode.json for the
// catalog matrix: providers with/without keys, default model, model effort.
func TestWorkspaceConfigGolden(t *testing.T) {
	cases := []struct {
		name string
		in   ProviderConfigInput
	}{
		{
			name: "full",
			in: ProviderConfigInput{
				Providers: []ProviderConfigEntry{
					{
						Key: "bedrock-p1", Name: "DeepSeek 官方",
						BaseURL: "http://127.0.0.1:8080/api/v1/ai",
						APIKey:  "br_harness_loopback",
						Models: []ModelConfigEntry{
							{ID: "deepseek-chat", Name: "DeepSeek Chat"},
							{ID: "deepseek-reasoner", Name: "DeepSeek Reasoner", ReasoningEffort: "high"},
						},
					},
					{
						Key: "bedrock-p2", Name: "本地网关",
						BaseURL: "http://127.0.0.1:8080/api/v1/ai",
						APIKey:  "br_harness_loopback",
						Models: []ModelConfigEntry{{ID: "qwen-max", Name: "Qwen Max"}},
					},
				},
				DefaultModel: "bedrock-p1/deepseek-chat",
			},
		},
		{
			name: "empty",
			in:   ProviderConfigInput{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := RenderWorkspaceConfig(tc.in)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			goldenPath := filepath.Join("testdata", "providerconfig", tc.name+".json")
			if *updateGolden {
				if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
					t.Fatalf("mkdir golden dir: %v", err)
				}
				if err := os.WriteFile(goldenPath, data, 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				return
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden (run with -update to create): %v", err)
			}
			if string(data) != string(want) {
				t.Fatalf("workspace config drifted from golden %s;\n--- got ---\n%s\n--- want ---\n%s",
					goldenPath, data, want)
			}
		})
	}
}

// TestWriteWorkspaceConfigAtomicRewrite asserts rewrites replace the file,
// carry the literal key only inside the 0600 file, and leave no tmp files.
func TestWriteWorkspaceConfigAtomicRewrite(t *testing.T) {
	dir := t.TempDir()
	in := ProviderConfigInput{
		Providers: []ProviderConfigEntry{{
			Key: "bedrock-p1", Name: "p", BaseURL: "https://api.example.com/v1",
			APIKey: "sk-literal",
			Models: []ModelConfigEntry{{ID: "m1", Name: "M1"}},
		}},
		DefaultModel: "bedrock-p1/m1",
	}
	path, err := WriteWorkspaceConfig(dir, in)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if filepath.Base(path) != WorkspaceConfigFileName {
		t.Fatalf("path = %s", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config file mode = %o, want 0600 (carries the literal key)", info.Mode().Perm())
	}
	got := readFile(t, path)
	if !strings.Contains(got, `"apiKey": "sk-literal"`) {
		t.Fatalf("literal key missing: %s", got)
	}
	in.Providers[0].BaseURL = "https://v2.example.com/v1"
	if _, err := WriteWorkspaceConfig(dir, in); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	got = readFile(t, path)
	if !strings.Contains(got, "v2.example.com") {
		t.Fatalf("rewrite did not replace: %s", got)
	}
	assertNoTmpLeftovers(t, dir)
}

// TestRenderWorkspaceConfigRejectsBadInput guards the required ids.
func TestRenderWorkspaceConfigRejectsBadInput(t *testing.T) {
	if _, err := RenderWorkspaceConfig(ProviderConfigInput{
		Providers: []ProviderConfigEntry{{Key: "", Name: "x", BaseURL: "u"}},
	}); err == nil {
		t.Fatal("expected empty provider key to fail")
	}
	if _, err := RenderWorkspaceConfig(ProviderConfigInput{
		Providers: []ProviderConfigEntry{{Key: "k", Name: "x", BaseURL: "u",
			Models: []ModelConfigEntry{{ID: "", Name: "m"}}}},
	}); err == nil {
		t.Fatal("expected empty model id to fail")
	}
}
