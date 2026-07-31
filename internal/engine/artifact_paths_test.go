package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"

	"bedrock/internal/cicd/model"
	resourcemodel "bedrock/internal/resource/model"
)

func TestResolveUnderWorkDirRejectsEscape(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if _, _, err := resolveUnderWorkDir(root, "../escape"); err == nil {
		t.Fatal("expected escape reject")
	}
	if _, _, err := resolveUnderWorkDir(root, "/abs"); err == nil {
		t.Fatal("expected abs reject")
	}
}

func TestPrepareDeployRootMissingFails(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	_, err := prepareDeployRoot(root, []string{"missing-dir"})
	if err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("err=%v", err)
	}
}

func TestPrepareDeployRootSingleFileAndBundle(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "target"), 0755)
	_ = os.WriteFile(filepath.Join(root, "target", "app.jar"), []byte("jar"), 0644)
	_ = os.MkdirAll(filepath.Join(root, "conf"), 0755)
	_ = os.WriteFile(filepath.Join(root, "conf", "app.conf"), []byte("c"), 0644)

	prep, err := prepareDeployRoot(root, []string{"target/app.jar"})
	if err != nil {
		t.Fatal(err)
	}
	defer prep.Cleanup()
	if prep.Kind != artifactKindFile {
		t.Fatalf("kind=%s", prep.Kind)
	}
	body, err := os.ReadFile(filepath.Join(prep.DeployRoot, "app.jar"))
	if err != nil || string(body) != "jar" {
		t.Fatalf("file=%q err=%v", body, err)
	}

	bundle, err := prepareDeployRoot(root, []string{"target/app.jar", "conf/app.conf"})
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.Cleanup()
	if bundle.Kind != artifactKindBundle {
		t.Fatalf("kind=%s", bundle.Kind)
	}
	if _, err := os.Stat(filepath.Join(bundle.DeployRoot, "app.jar")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(bundle.DeployRoot, "app.conf")); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareDeployRootBasenameCollision(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "a"), 0755)
	_ = os.MkdirAll(filepath.Join(root, "b"), 0755)
	_ = os.WriteFile(filepath.Join(root, "a", "x.txt"), []byte("1"), 0644)
	_ = os.WriteFile(filepath.Join(root, "b", "x.txt"), []byte("2"), 0644)
	_, err := prepareDeployRoot(root, []string{"a/x.txt", "b/x.txt"})
	if err == nil || !strings.Contains(err.Error(), "冲突") {
		t.Fatalf("err=%v", err)
	}
}

func TestPipeline_SingleFileArtifactNoArchive(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repoDir := initLocalGitRepo(t)
	tmp := t.TempDir()
	run := &model.BuildRun{
		ID: 1, BuildJobID: 10, BuildNumber: 4,
		Status: "queued", Stage: "pending", Branch: "main",
	}
	store := newMemRunStore(run)
	jobStore := &memJobStore{
		job: &model.BuildJob{
			ID: 10, RepositoryID: 1, Branch: "main",
			BuildScript:    "mkdir -p target && echo jar > target/app.jar",
			ArtifactPaths:  []string{"target/app.jar"},
			ArtifactFormat: "gzip",
			MaxArtifacts:   5,
		},
	}
	p := NewPipeline(store, jobStore, &memRepoStore{
		repo: &resourcemodel.Repository{ID: 1, RepoURL: repoDir, AuthType: "none"},
	}, &memServerStore{}, nopSecrets{}, nil, zap.NewNop(),
		filepath.Join(tmp, "ws"), filepath.Join(tmp, "art"), filepath.Join(tmp, "logs"), filepath.Join(tmp, "cache"))

	p.Execute(context.Background(), 1)
	got, _ := store.FindByID(1)
	if got.Status != "success" {
		t.Fatalf("status=%s err=%q", got.Status, got.ErrorMessage)
	}
	if got.ArtifactKind != artifactKindFile {
		t.Fatalf("kind=%s", got.ArtifactKind)
	}
	if !strings.HasSuffix(got.ArtifactPath, "app.jar") {
		t.Fatalf("artifact_path=%q", got.ArtifactPath)
	}
	if strings.HasSuffix(got.ArtifactPath, ".tar.gz") || strings.HasSuffix(got.ArtifactPath, ".zip") {
		t.Fatalf("single file must not be archived: %s", got.ArtifactPath)
	}
	body, err := os.ReadFile(got.ArtifactPath)
	if err != nil || strings.TrimSpace(string(body)) != "jar" {
		t.Fatalf("body=%q err=%v", body, err)
	}
}

func TestPipeline_MultiPathBundle(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repoDir := initLocalGitRepo(t)
	tmp := t.TempDir()
	run := &model.BuildRun{
		ID: 1, BuildJobID: 10, BuildNumber: 5,
		Status: "queued", Stage: "pending", Branch: "main",
	}
	store := newMemRunStore(run)
	jobStore := &memJobStore{
		job: &model.BuildJob{
			ID: 10, RepositoryID: 1, Branch: "main",
			BuildScript:    "mkdir -p target conf && echo jar > target/app.jar && echo cfg > conf/app.conf",
			ArtifactPaths:  []string{"target/app.jar", "conf/app.conf"},
			ArtifactFormat: "gzip",
			MaxArtifacts:   5,
		},
	}
	p := NewPipeline(store, jobStore, &memRepoStore{
		repo: &resourcemodel.Repository{ID: 1, RepoURL: repoDir, AuthType: "none"},
	}, &memServerStore{}, nopSecrets{}, nil, zap.NewNop(),
		filepath.Join(tmp, "ws"), filepath.Join(tmp, "art"), filepath.Join(tmp, "logs"), filepath.Join(tmp, "cache"))

	p.Execute(context.Background(), 1)
	got, _ := store.FindByID(1)
	if got.Status != "success" {
		t.Fatalf("status=%s err=%q", got.Status, got.ErrorMessage)
	}
	if got.ArtifactKind != artifactKindBundle {
		t.Fatalf("kind=%s", got.ArtifactKind)
	}
	extractDir := filepath.Join(tmp, "extract")
	if err := extractArtifactArchive(got.ArtifactPath, extractDir, "gzip"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"app.jar", "app.conf"} {
		if _, err := os.Stat(filepath.Join(extractDir, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
}

func TestPipeline_MissingArtifactPathFails(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repoDir := initLocalGitRepo(t)
	tmp := t.TempDir()
	run := &model.BuildRun{
		ID: 1, BuildJobID: 10, BuildNumber: 1,
		Status: "queued", Stage: "pending", Branch: "main",
	}
	store := newMemRunStore(run)
	jobStore := &memJobStore{
		job: &model.BuildJob{
			ID: 10, RepositoryID: 1, Branch: "main",
			BuildScript:   "true",
			ArtifactPaths: []string{"dist"},
		},
	}
	p := NewPipeline(store, jobStore, &memRepoStore{
		repo: &resourcemodel.Repository{ID: 1, RepoURL: repoDir, AuthType: "none"},
	}, &memServerStore{}, nopSecrets{}, nil, zap.NewNop(),
		filepath.Join(tmp, "ws"), filepath.Join(tmp, "art"), filepath.Join(tmp, "logs"), filepath.Join(tmp, "cache"))

	p.Execute(context.Background(), 1)
	got, _ := store.FindByID(1)
	if got.Status != "failed" {
		t.Fatalf("status=%s want failed (err=%q)", got.Status, got.ErrorMessage)
	}
	if !strings.Contains(got.ErrorMessage, "不存在") {
		t.Fatalf("error=%q", got.ErrorMessage)
	}
}

func TestRedeploy_FileArtifactRoundTrip(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	artDir := filepath.Join(tmp, "art")
	_ = os.MkdirAll(artDir, 0755)
	artPath := filepath.Join(artDir, "app.jar")
	_ = os.WriteFile(artPath, []byte("jar-bytes"), 0644)
	dest := filepath.Join(tmp, "dest")

	run := &model.BuildRun{
		ID: 1, BuildJobID: 10, BuildNumber: 1,
		Status: "success", Stage: "idle", DistributionSummary: "all_success",
		ArtifactPath: artPath, ArtifactKind: artifactKindFile,
	}
	store := newMemRunStore(run)
	jobStore := &memJobStore{
		job:     &model.BuildJob{ID: 10, ArtifactFormat: "gzip"},
		targets: []model.DeployTarget{{ID: 1, BuildJobID: 10, Method: "local", RemotePath: dest}},
	}
	p := NewPipeline(store, jobStore, &memRepoStore{}, &memServerStore{}, nopSecrets{}, nil, zap.NewNop(), tmp, artDir, tmp, tmp)

	var log strings.Builder
	p.executeRedeployOnly(context.Background(), run, jobStore.job, func(s string) { log.WriteString(s + "\n") })
	got, _ := store.FindByID(1)
	if got.DistributionSummary != "all_success" {
		t.Fatalf("summary=%s log=%s", got.DistributionSummary, log.String())
	}
	body, err := os.ReadFile(filepath.Join(dest, "app.jar"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "jar-bytes" {
		t.Fatalf("content=%q", body)
	}
}
