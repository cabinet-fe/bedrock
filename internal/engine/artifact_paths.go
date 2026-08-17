package engine

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"bedrock/internal/cicd/model"
)

const (
	artifactKindFile    = "file"
	artifactKindArchive = "archive"
	artifactKindBundle  = "bundle"
)

// jobArtifactPaths returns configured relative paths from artifact_paths / JSON.
func jobArtifactPaths(job *model.BuildJob) []string {
	if job == nil {
		return nil
	}
	paths := job.ArtifactPaths
	if len(paths) == 0 && strings.TrimSpace(job.ArtifactPathsJSON) != "" {
		_ = json.Unmarshal([]byte(job.ArtifactPathsJSON), &paths)
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func decodeJobArtifactPaths(job *model.BuildJob) {
	if job == nil {
		return
	}
	job.ArtifactPaths = jobArtifactPaths(job)
	if len(job.ArtifactPaths) > 0 {
		job.OutputDir = job.ArtifactPaths[0]
	}
}

// resolveUnderWorkDir joins rel under workDir and rejects path escape / abs / missing.
func resolveUnderWorkDir(workDir, rel string) (abs string, info os.FileInfo, err error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", nil, fmt.Errorf("制品路径为空")
	}
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "\\") {
		return "", nil, fmt.Errorf("制品路径须为相对路径: %s", rel)
	}
	if strings.Contains(rel, "..") {
		parts := strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == '\\' })
		for _, part := range parts {
			if part == ".." {
				return "", nil, fmt.Errorf("无效的制品路径: %s", rel)
			}
		}
	}
	workAbs, err := filepath.Abs(workDir)
	if err != nil {
		return "", nil, err
	}
	candidate := filepath.Join(workAbs, filepath.Clean(rel))
	candidateAbs, err := filepath.Abs(candidate)
	if err != nil {
		return "", nil, err
	}
	relToRoot, err := filepath.Rel(workAbs, candidateAbs)
	if err != nil {
		return "", nil, err
	}
	if relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(os.PathSeparator)) {
		return "", nil, fmt.Errorf("制品路径越出仓库根: %s", rel)
	}
	info, err = os.Stat(candidateAbs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil, fmt.Errorf("制品路径不存在: %s", rel)
		}
		return "", nil, fmt.Errorf("无法访问制品路径 %s: %w", rel, err)
	}
	return candidateAbs, info, nil
}

type preparedArtifact struct {
	DeployRoot string // directory used for distribution
	Kind       string // file|archive|bundle|""
	Cleanup    func()
}

// prepareDeployRoot builds a deploy directory from configured artifact paths under workDir.
// Missing paths fail. Basename collisions among multi-path configs fail.
func prepareDeployRoot(workDir string, relPaths []string) (preparedArtifact, error) {
	noop := func() {}
	if len(relPaths) == 0 {
		return preparedArtifact{Cleanup: noop}, nil
	}

	type entry struct {
		abs  string
		info os.FileInfo
		rel  string
	}
	entries := make([]entry, 0, len(relPaths))
	for _, rel := range relPaths {
		abs, info, err := resolveUnderWorkDir(workDir, rel)
		if err != nil {
			return preparedArtifact{Cleanup: noop}, err
		}
		entries = append(entries, entry{abs: abs, info: info, rel: rel})
	}

	if len(entries) == 1 {
		e := entries[0]
		if e.info.IsDir() {
			return preparedArtifact{DeployRoot: e.abs, Kind: artifactKindArchive, Cleanup: noop}, nil
		}
		staging, err := os.MkdirTemp("", "bedrock-artifact-*")
		if err != nil {
			return preparedArtifact{Cleanup: noop}, err
		}
		dst := filepath.Join(staging, filepath.Base(e.abs))
		if err := copyFile(e.abs, dst, e.info.Mode()); err != nil {
			_ = os.RemoveAll(staging)
			return preparedArtifact{Cleanup: noop}, fmt.Errorf("复制制品文件失败: %w", err)
		}
		return preparedArtifact{
			DeployRoot: staging,
			Kind:       artifactKindFile,
			Cleanup:    func() { _ = os.RemoveAll(staging) },
		}, nil
	}

	// Multi-path: merge by basename into staging.
	staging, err := os.MkdirTemp("", "bedrock-artifact-bundle-*")
	if err != nil {
		return preparedArtifact{Cleanup: noop}, err
	}
	cleanup := func() { _ = os.RemoveAll(staging) }
	seenBase := map[string]string{}
	for _, e := range entries {
		base := filepath.Base(e.abs)
		if prev, ok := seenBase[base]; ok {
			cleanup()
			return preparedArtifact{Cleanup: noop}, fmt.Errorf("制品路径 basename 冲突: %s 与 %s 均为 %s", prev, e.rel, base)
		}
		seenBase[base] = e.rel
		dst := filepath.Join(staging, base)
		if e.info.IsDir() {
			if err := copyDir(e.abs, dst); err != nil {
				cleanup()
				return preparedArtifact{Cleanup: noop}, fmt.Errorf("合并制品目录失败 (%s): %w", e.rel, err)
			}
		} else {
			if err := copyFile(e.abs, dst, e.info.Mode()); err != nil {
				cleanup()
				return preparedArtifact{Cleanup: noop}, fmt.Errorf("合并制品文件失败 (%s): %w", e.rel, err)
			}
		}
	}
	return preparedArtifact{DeployRoot: staging, Kind: artifactKindBundle, Cleanup: cleanup}, nil
}

// storePreparedArtifact writes the downloadable artifact for a prepared deploy root.
// 0 paths: no artifact. 1 file: copy as-is. 1 dir / 2+: zip/tar.gz per format.
func storePreparedArtifact(artifactDir string, buildNumber int, format string, prep preparedArtifact) (artifactPath string, err error) {
	if prep.Kind == "" || prep.DeployRoot == "" {
		return "", nil
	}
	if err := os.MkdirAll(artifactDir, 0755); err != nil {
		return "", err
	}
	switch prep.Kind {
	case artifactKindFile:
		entries, err := os.ReadDir(prep.DeployRoot)
		if err != nil {
			return "", err
		}
		var fileName string
		for _, e := range entries {
			if !e.IsDir() {
				fileName = e.Name()
				break
			}
		}
		if fileName == "" {
			return "", fmt.Errorf("单文件制品目录为空")
		}
		src := filepath.Join(prep.DeployRoot, fileName)
		dst := filepath.Join(artifactDir, fileName)
		info, err := os.Stat(src)
		if err != nil {
			return "", err
		}
		if err := copyFile(src, dst, info.Mode()); err != nil {
			return "", err
		}
		return dst, nil
	case artifactKindArchive, artifactKindBundle:
		path := filepath.Join(artifactDir, artifactArchiveName(buildNumber, format))
		if err := CreateArtifactArchive(path, prep.DeployRoot, format); err != nil {
			return "", err
		}
		return path, nil
	default:
		return "", fmt.Errorf("未知制品类型: %s", prep.Kind)
	}
}

// materializeDeployRootFromArtifact rebuilds a deploy directory from a stored artifact.
func materializeDeployRootFromArtifact(artifactPath, kind, format string) (deployRoot string, cleanup func(), err error) {
	noop := func() {}
	kind = strings.TrimSpace(strings.ToLower(kind))
	if kind == "" {
		// Legacy archives (pre artifact_kind): treat as archive.
		if isLikelyArchive(artifactPath) {
			kind = artifactKindArchive
		} else {
			kind = artifactKindFile
		}
	}
	switch kind {
	case artifactKindFile:
		staging, err := os.MkdirTemp("", "bedrock-redeploy-file-*")
		if err != nil {
			return "", noop, err
		}
		dst := filepath.Join(staging, filepath.Base(artifactPath))
		if err := copyFilePlain(artifactPath, dst); err != nil {
			_ = os.RemoveAll(staging)
			return "", noop, err
		}
		return staging, func() { _ = os.RemoveAll(staging) }, nil
	case artifactKindArchive, artifactKindBundle:
		staging, err := os.MkdirTemp("", "bedrock-redeploy-*")
		if err != nil {
			return "", noop, err
		}
		if err := extractArtifactArchive(artifactPath, staging, format); err != nil {
			_ = os.RemoveAll(staging)
			return "", noop, err
		}
		return staging, func() { _ = os.RemoveAll(staging) }, nil
	default:
		return "", noop, fmt.Errorf("未知制品类型: %s", kind)
	}
}

func isLikelyArchive(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".zip") || strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz")
}

func copyFilePlain(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
