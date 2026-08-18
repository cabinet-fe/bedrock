package service

import (
	"strings"
	"testing"
)

func TestNpmPackageFromTemplate(t *testing.T) {
	got := npmPackageFromTemplate(npmInstallLike("@anthropic-ai/claude-code"))
	if got != "@anthropic-ai/claude-code" {
		t.Fatalf("got %q", got)
	}
	if npmPackageFromTemplate(`curl -fsSL "$base/install.sh" | sh`) != "" {
		t.Fatal("expected empty for non-npm template")
	}
	if npmPackageFromTemplate(`mise use -g "npm:@openai/codex@$version"`) != "" {
		t.Fatal("mise npm backend templates must not be parsed")
	}
	if npmPackageFromTemplate(`npm install -g leftover`) != "leftover" {
		t.Fatal("expected leftover package name")
	}
}

func npmInstallLike(pkg string) string {
	return `npm install -g ` + pkg + `${version:+@$version}`
}

func TestParseNPMViewVersions(t *testing.T) {
	got, err := parseNPMViewVersions(`npm warn x
["1.0.0","1.1.0","2.0.0"]
`, 10)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "2.0.0,1.1.0,1.0.0" {
		t.Fatalf("got %#v", got)
	}
	got, err = parseNPMViewVersions(`"3.4.5"`, 10)
	if err != nil || len(got) != 1 || got[0] != "3.4.5" {
		t.Fatalf("single version: %#v %v", got, err)
	}
}

func TestIsNewerCLIVersion(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"2.0.0", "1.9.9", true},
		{"1.2.3", "1.2.3", false},
		{"1.2.3", "1.2.4", false},
		{"1.2.4-beta", "1.2.4", false},
		{"1.2.4", "1.2.4-beta", true},
		{"v2.1.0", "2.0.9", true},
		{"", "1.0.0", false},
		{"1.0.0", "", false},
	}
	for _, tc := range cases {
		if got := isNewerCLIVersion(tc.latest, tc.current); got != tc.want {
			t.Fatalf("isNewer(%q,%q)=%v want %v", tc.latest, tc.current, got, tc.want)
		}
	}
}

func TestNormalizeCLIVersion(t *testing.T) {
	if got := normalizeCLIVersion("claude version 1.2.3"); got != "1.2.3" {
		t.Fatalf("got %q", got)
	}
	if got := normalizeCLIVersion("/usr/local/bin/claude"); got != "" {
		t.Fatalf("path should be empty, got %q", got)
	}
}
