package engine

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"bedrock/internal/cicd/model"
	"bedrock/internal/pkg"
)

func TestValidateBuildWorkDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := validateBuildWorkDir(dir); err != nil {
		t.Fatalf("existing dir: %v", err)
	}

	missing := filepath.Join(dir, "no-such-subdir")
	err := validateBuildWorkDir(missing)
	if err == nil {
		t.Fatal("expected missing dir error")
	}
	if !strings.Contains(err.Error(), "工作目录不存在") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), missing) {
		t.Fatalf("error should include path: %v", err)
	}
	if strings.Contains(err.Error(), "fork/exec") || strings.Contains(err.Error(), "no such file or directory") {
		t.Fatalf("must not look like sh missing: %v", err)
	}

	filePath := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(filePath, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	err = validateBuildWorkDir(filePath)
	if err == nil || !strings.Contains(err.Error(), "工作目录不是目录") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMergeBuildEnv_OverridesHost(t *testing.T) {
	const key = "BEDROCK_TEST_BUILD_ENV_MERGE"
	t.Setenv(key, "from-host")
	got := mergeBuildEnv([]string{key}, map[string]string{key: "from-job"})
	found := ""
	for _, e := range got {
		if k, v, ok := strings.Cut(e, "="); ok && k == key {
			found = v
		}
	}
	if found != "from-job" {
		t.Fatalf("override want from-job, got %q (env=%v)", found, got)
	}
}

func TestMergeBuildEnv_NamesFromHost(t *testing.T) {
	const key = "BEDROCK_TEST_BUILD_ENV_NAME"
	t.Setenv(key, "host-value")
	got := mergeBuildEnv([]string{key}, nil)
	found := ""
	for _, e := range got {
		if k, v, ok := strings.Cut(e, "="); ok && k == key {
			found = v
		}
	}
	if found != "host-value" {
		t.Fatalf("want host-value, got %q", found)
	}
}

func TestBuildScriptTemplateVars(t *testing.T) {
	const hostKey = "BEDROCK_TEST_TMPL_HOST"
	t.Setenv(hostKey, "from-host")
	job := &model.BuildJob{
		ID:          5,
		Name:        "demo",
		EnvVarNames: []string{hostKey},
	}
	run := &model.BuildRun{ID: 9, BuildNumber: 2, CommitHash: "deadbeef"}
	vars := buildScriptTemplateVars(job, run, "/tmp/ws", "develop", map[string]string{
		"TOKEN": "secret",
		hostKey: "from-job",
	})
	if vars["job.id"] != "5" || vars["job.name"] != "demo" {
		t.Fatalf("job vars=%v", vars)
	}
	if vars["run.id"] != "9" || vars["run.build_number"] != "2" {
		t.Fatalf("run vars=%v", vars)
	}
	if vars["run.branch"] != "develop" || vars["run.commit"] != "deadbeef" {
		t.Fatalf("branch/commit=%v", vars)
	}
	if !filepath.IsAbs(vars["workspace"]) {
		t.Fatalf("workspace not abs: %q", vars["workspace"])
	}
	if vars["env."+hostKey] != "from-job" {
		t.Fatalf("kv should override host: %q", vars["env."+hostKey])
	}
	if vars["env.TOKEN"] != "secret" {
		t.Fatalf("env.TOKEN=%q", vars["env.TOKEN"])
	}
}

func TestDecryptJobEnvVarsCipher(t *testing.T) {
	if err := pkg.InitEncryption(strings.Repeat("cd", 32)); err != nil {
		t.Fatal(err)
	}
	plain := `{"FOO":"bar","BAZ":"qux"}`
	cipher, err := pkg.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	vars, err := decryptJobEnvVarsCipher(cipher)
	if err != nil {
		t.Fatal(err)
	}
	if vars["FOO"] != "bar" || vars["BAZ"] != "qux" {
		t.Fatalf("vars=%#v", vars)
	}
	empty, err := decryptJobEnvVarsCipher("")
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty cipher: %#v %v", empty, err)
	}
}

func TestResolvePOSIXShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	path, err := resolvePOSIXShell()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "sh" {
		t.Fatalf("path=%q", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("shell not usable: %v", err)
	}
}
