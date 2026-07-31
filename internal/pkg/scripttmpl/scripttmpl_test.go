package scripttmpl

import (
	"strings"
	"testing"
)

func TestExpand_BuiltinsAndEnv(t *testing.T) {
	t.Parallel()
	vars := map[string]string{
		"job.id":           "12",
		"job.name":         "api",
		"run.id":           "99",
		"run.build_number": "3",
		"run.branch":       "main",
		"run.commit":       "abc123",
		"workspace":        "/data/jobs/job-12",
		"env.FOO":          "bar",
		"env.API_KEY":      "secret",
	}
	in := `echo ${{ job.name }} #${{ run.build_number }}
cd ${{ workspace }}
export KEY=${{ env.API_KEY }}
echo ${{env.FOO}}`
	got, err := Expand(in, vars)
	if err != nil {
		t.Fatal(err)
	}
	want := `echo api #3
cd /data/jobs/job-12
export KEY=secret
echo bar`
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestExpand_UnknownVariable(t *testing.T) {
	t.Parallel()
	_, err := Expand("echo ${{ missing }} and ${{ env.NOPE }}", map[string]string{
		"workspace": "/tmp",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "env.NOPE") || !strings.Contains(msg, "missing") {
		t.Fatalf("error should list unknowns: %v", err)
	}
}

func TestExpand_NoReexpand(t *testing.T) {
	t.Parallel()
	got, err := Expand("x=${{ env.NESTED }}", map[string]string{
		"env.NESTED": "${{ workspace }}",
		"workspace":  "/should-not-appear",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "x=${{ workspace }}" {
		t.Fatalf("got %q", got)
	}
}

func TestExpand_CoexistsWithShellPythonPowerShell(t *testing.T) {
	t.Parallel()
	in := `echo $HOME ${VAR}
print(f"{name}")
$x = 1; Write-Host $env:PATH
echo ${{ job.id }}`
	got, err := Expand(in, map[string]string{"job.id": "7"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "$HOME ${VAR}") {
		t.Fatalf("bash vars altered: %q", got)
	}
	if !strings.Contains(got, `print(f"{name}")`) {
		t.Fatalf("python braces altered: %q", got)
	}
	if !strings.Contains(got, `$x = 1; Write-Host $env:PATH`) {
		t.Fatalf("powershell altered: %q", got)
	}
	if !strings.Contains(got, "echo 7") {
		t.Fatalf("template not replaced: %q", got)
	}
}

func TestExpand_NilVarsEmptyScript(t *testing.T) {
	t.Parallel()
	got, err := Expand("plain", nil)
	if err != nil || got != "plain" {
		t.Fatalf("got %q err=%v", got, err)
	}
	_, err = Expand("${{ a }}", nil)
	if err == nil {
		t.Fatal("expected unknown")
	}
}
