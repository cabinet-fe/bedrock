package service

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"bedrock/internal/cicd/model"
)

const maxArtifactPaths = 10

// resolveArtifactPathsInput prefers artifact_paths; if empty and output_dir set, uses single-element.
func resolveArtifactPathsInput(paths []string, outputDir string) []string {
	cleaned := cleanArtifactPaths(paths)
	if len(cleaned) > 0 {
		return cleaned
	}
	if dir := strings.TrimSpace(outputDir); dir != "" {
		return []string{dir}
	}
	return []string{}
}

func cleanArtifactPaths(paths []string) []string {
	if paths == nil {
		return []string{}
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

// validateRelPath requires a non-empty relative path without ".." segments.
func validateRelPath(p, label string) error {
	p = strings.TrimSpace(p)
	if p == "" {
		return fmt.Errorf("%s不能为空", label)
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "\\") {
		return fmt.Errorf("%s须为相对路径: %s", label, p)
	}
	// Reject Windows drive paths (C:\...) even on non-Windows hosts.
	if len(p) >= 2 && p[1] == ':' {
		return fmt.Errorf("%s须为相对路径: %s", label, p)
	}
	clean := filepath.ToSlash(filepath.Clean(p))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return fmt.Errorf("无效的%s: %s", label, p)
	}
	if strings.Contains(p, "..") {
		// Catch mixed separators / odd forms before Clean collapses them.
		parts := strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' })
		for _, part := range parts {
			if part == ".." {
				return fmt.Errorf("无效的%s: %s", label, p)
			}
		}
	}
	return nil
}

func validateArtifactRelPath(p string) error {
	return validateRelPath(p, "制品路径")
}

func validateOptionalRelPath(p, label string) error {
	if strings.TrimSpace(p) == "" {
		return nil
	}
	return validateRelPath(p, label)
}

func parseJobCachePaths(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var paths []string
	if err := json.Unmarshal([]byte(raw), &paths); err == nil {
		return cleanArtifactPaths(paths)
	}
	var result []string
	for line := range strings.SplitSeq(raw, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

func validateJobCachePaths(raw string) error {
	for _, p := range parseJobCachePaths(raw) {
		if err := validateRelPath(p, "缓存路径"); err != nil {
			return err
		}
	}
	return nil
}

func validateArtifactPaths(paths []string) error {
	if len(paths) > maxArtifactPaths {
		return fmt.Errorf("制品路径最多 %d 条", maxArtifactPaths)
	}
	seen := map[string]struct{}{}
	for _, p := range paths {
		if err := validateArtifactRelPath(p); err != nil {
			return err
		}
		key := filepath.ToSlash(filepath.Clean(p))
		if _, ok := seen[key]; ok {
			return fmt.Errorf("制品路径重复: %s", p)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func encodeArtifactPaths(job *model.BuildJob, paths []string) error {
	cleaned := cleanArtifactPaths(paths)
	if err := validateArtifactPaths(cleaned); err != nil {
		return err
	}
	b, err := json.Marshal(cleaned)
	if err != nil {
		return err
	}
	job.ArtifactPathsJSON = string(b)
	job.ArtifactPaths = cleaned
	if len(cleaned) > 0 {
		job.OutputDir = cleaned[0]
	} else {
		job.OutputDir = ""
	}
	return nil
}

func decodeArtifactPaths(job *model.BuildJob) {
	if job == nil {
		return
	}
	paths := []string{}
	if strings.TrimSpace(job.ArtifactPathsJSON) != "" {
		if err := json.Unmarshal([]byte(job.ArtifactPathsJSON), &paths); err != nil {
			paths = []string{}
		}
	}
	paths = cleanArtifactPaths(paths)
	if len(paths) == 0 {
		if dir := strings.TrimSpace(job.OutputDir); dir != "" {
			paths = []string{dir}
		}
	}
	job.ArtifactPaths = paths
	if len(paths) > 0 {
		job.OutputDir = paths[0]
	} else {
		job.OutputDir = ""
	}
}
