package service

import (
	"strings"
	"testing"

	"bedrock/internal/cicd/model"
)

func TestValidateArtifactRelPath(t *testing.T) {
	t.Parallel()
	ok := []string{"dist", "target/app.jar", "out/nested/file"}
	for _, p := range ok {
		if err := validateArtifactRelPath(p); err != nil {
			t.Fatalf("%q: %v", p, err)
		}
	}
	bad := []string{"", ".", "..", "../escape", "foo/../bar", "/abs", "\\abs", "C:\\win"}
	for _, p := range bad {
		if err := validateArtifactRelPath(p); err == nil {
			t.Fatalf("%q: expected error", p)
		}
	}
}

func TestEncodeDecodeArtifactPaths(t *testing.T) {
	t.Parallel()
	job := &model.BuildJob{}
	if err := encodeArtifactPaths(job, []string{" dist ", "target/app.jar"}); err != nil {
		t.Fatal(err)
	}
	if job.OutputDir != "dist" {
		t.Fatalf("output_dir=%q", job.OutputDir)
	}
	if job.ArtifactPathsJSON == "" {
		t.Fatal("expected json")
	}
	job2 := &model.BuildJob{ArtifactPathsJSON: job.ArtifactPathsJSON}
	decodeArtifactPaths(job2)
	if len(job2.ArtifactPaths) != 2 || job2.ArtifactPaths[0] != "dist" {
		t.Fatalf("paths=%v", job2.ArtifactPaths)
	}
}

func TestDecodeArtifactPathsFallsBackToOutputDir(t *testing.T) {
	t.Parallel()
	job := &model.BuildJob{OutputDir: "legacy-dist"}
	decodeArtifactPaths(job)
	if len(job.ArtifactPaths) != 1 || job.ArtifactPaths[0] != "legacy-dist" {
		t.Fatalf("paths=%v", job.ArtifactPaths)
	}
}

func TestValidateArtifactPathsMax(t *testing.T) {
	t.Parallel()
	paths := make([]string, maxArtifactPaths+1)
	for i := range paths {
		paths[i] = "p" + strings.Repeat("x", i)
	}
	if err := validateArtifactPaths(paths); err == nil {
		t.Fatal("expected max error")
	}
}

func TestResolveArtifactPathsInput(t *testing.T) {
	t.Parallel()
	got := resolveArtifactPathsInput([]string{"a"}, "ignored")
	if len(got) != 1 || got[0] != "a" {
		t.Fatalf("%v", got)
	}
	got = resolveArtifactPathsInput(nil, "out")
	if len(got) != 1 || got[0] != "out" {
		t.Fatalf("%v", got)
	}
}

func TestEncodeArtifactPathsRejectsEscape(t *testing.T) {
	t.Parallel()
	job := &model.BuildJob{}
	if err := encodeArtifactPaths(job, []string{"../escape"}); err == nil {
		t.Fatal("expected reject")
	}
}

func TestValidateOptionalRelPath(t *testing.T) {
	t.Parallel()
	if err := validateOptionalRelPath("", "工作目录"); err != nil {
		t.Fatal(err)
	}
	if err := validateOptionalRelPath("src/app", "工作目录"); err != nil {
		t.Fatal(err)
	}
	if err := validateOptionalRelPath("../x", "工作目录"); err == nil {
		t.Fatal("expected reject")
	}
}

func TestValidateJobCachePaths(t *testing.T) {
	t.Parallel()
	if err := validateJobCachePaths(`[".m2/repository","node_modules"]`); err != nil {
		t.Fatal(err)
	}
	if err := validateJobCachePaths(`["../escape"]`); err == nil {
		t.Fatal("expected reject")
	}
	if err := validateJobCachePaths(""); err != nil {
		t.Fatal(err)
	}
}
