package pkg

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEnsurePATH(t *testing.T) {
	home := t.TempDir()
	bunBin := filepath.Join(home, ".bun", "bin")
	goBin := filepath.Join(home, "go", "bin")
	for _, dir := range []string{bunBin, goBin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	orig := ToolPathDirs
	ToolPathDirs = []string{".bun/bin", ".cargo/bin", "go/bin"}
	t.Cleanup(func() { ToolPathDirs = orig })

	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "appends existing dirs after current path",
			path: "/usr/local/bin:/usr/bin:/bin",
			want: "/usr/local/bin:/usr/bin:/bin:" + bunBin + ":" + goBin,
		},
		{
			name: "keeps existing entry first without duplicate",
			path: bunBin + ":/usr/bin",
			want: bunBin + ":/usr/bin:" + goBin,
		},
		{
			name: "skips missing dirs",
			path: "/usr/bin",
			want: "/usr/bin:" + bunBin + ":" + goBin,
		},
		{
			name: "builds path when empty",
			path: "",
			want: bunBin + ":" + goBin,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := map[string]string{"PATH": tt.path}
			EnsurePATH(env, home)
			if env["PATH"] != tt.want {
				t.Fatalf("PATH=%q want %q", env["PATH"], tt.want)
			}
		})
	}
}

func TestEnsurePATH_SkipsHomeRelativeWhenHomeEmpty(t *testing.T) {
	orig := ToolPathDirs
	ToolPathDirs = []string{".bun/bin", "/usr/local/bin"}
	t.Cleanup(func() { ToolPathDirs = orig })

	env := map[string]string{"PATH": "/usr/bin"}
	EnsurePATH(env, "")
	if env["PATH"] != "/usr/bin:/usr/local/bin" {
		t.Fatalf("PATH=%q, home-relative dir must be skipped and absolute dir kept", env["PATH"])
	}
}

// systemd services often run without HOME; the fallback must still locate
// home-relative tool dirs such as ~/.bun/bin.
func TestResolveHomeDir_FallsBackWhenHomeUnset(t *testing.T) {
	t.Setenv("HOME", "")
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", "")
	}
	home := ResolveHomeDir()
	if home == "" {
		t.Fatal("ResolveHomeDir must fall back to the passwd db when HOME is unset")
	}
	if !filepath.IsAbs(home) {
		t.Fatalf("home not absolute: %q", home)
	}
}
