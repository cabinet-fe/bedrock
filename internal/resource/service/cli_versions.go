package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var npmInstallPackagePattern = regexp.MustCompile(`npm\s+install\s+-g\s+([@A-Za-z0-9._-]+(?:/[A-Za-z0-9._-]+)?)`)

func npmPackageFromTemplate(template string) string {
	m := npmInstallPackagePattern.FindStringSubmatch(template)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

type VersionsResult struct {
	Items      []string `json:"items"`
	CatalogURL string   `json:"catalog_url"`
	Error      string   `json:"error,omitempty"`
}

func (s *CLIService) ListVersions(ctx context.Context, key string) (*VersionsResult, error) {
	cli, err := s.repo.FindByKey(key)
	if err != nil {
		return nil, err
	}
	pkgName := npmPackageFromTemplate(cli.InstallTemplate)
	result := &VersionsResult{
		Items:      []string{},
		CatalogURL: npmCatalogURL(pkgName),
	}
	if pkgName == "" {
		result.Error = "该 CLI 未配置 npm 包"
		return result, nil
	}
	out, _, log, queryErr := s.runNPMViewOnSources(ctx, key, pkgName, "versions --json")
	if queryErr != nil {
		result.Error = queryErr.Error()
		if strings.TrimSpace(log) != "" {
			result.Error = strings.TrimSpace(log)
		}
		return result, nil
	}
	items, parseErr := parseNPMViewVersions(out, 120)
	if parseErr != nil {
		result.Error = parseErr.Error()
		return result, nil
	}
	result.Items = items
	return result, nil
}

func npmCatalogURL(pkgName string) string {
	if pkgName == "" {
		return "https://www.npmjs.com/"
	}
	return "https://www.npmjs.com/package/" + pkgName + "?activeTab=versions"
}

func (s *CLIService) queryLatestNPMVersion(ctx context.Context, key, pkgName string) (latest, registry, log string, err error) {
	out, registry, log, err := s.runNPMViewOnSources(ctx, key, pkgName, "version")
	if err != nil {
		return "", registry, log, err
	}
	ver := normalizeCLIVersion(firstNonEmptyLine(out))
	if ver == "" {
		ver = firstNonEmptyLine(out)
	}
	if ver == "" {
		return "", registry, log, errors.New("未能解析最新版本")
	}
	return ver, registry, log, nil
}

func (s *CLIService) runNPMViewOnSources(ctx context.Context, key, pkgName, field string) (output, registry, log string, err error) {
	sources, listErr := s.repo.ListEnabledSources(key)
	if listErr != nil {
		return "", "", "", listErr
	}
	var buf strings.Builder
	try := func(baseURL string) (string, error) {
		cmd := `command -v npm >/dev/null 2>&1 || { echo 'npm is required'; exit 1; }; npm view ` + quoteShell(pkgName) + " " + field
		if baseURL != "" {
			cmd += " --registry " + quoteShell(baseURL)
		}
		out, runErr := executeShell(ctx, cmd)
		buf.WriteString(out)
		if runErr != nil {
			return "", runErr
		}
		return strings.TrimSpace(out), nil
	}
	if len(sources) == 0 {
		out, runErr := try("")
		if runErr != nil {
			return "", "", buf.String(), runErr
		}
		return out, "", buf.String(), nil
	}
	var lastErr error
	for _, source := range sources {
		buf.WriteString(fmt.Sprintf("trying source %q (priority %d)\n", source.Name, source.Priority))
		out, runErr := try(source.BaseURL)
		if runErr == nil {
			return out, source.BaseURL, buf.String(), nil
		}
		lastErr = runErr
		buf.WriteString(fmt.Sprintf("source %q failed: %v\n", source.Name, runErr))
	}
	if lastErr == nil {
		lastErr = errors.New("所有安装源均失败")
	}
	return "", "", buf.String(), lastErr
}

func parseNPMViewVersions(output string, limit int) ([]string, error) {
	payload := extractJSONPayload(output)
	if payload == "" {
		return nil, errors.New("未能解析版本列表")
	}
	var many []string
	if err := json.Unmarshal([]byte(payload), &many); err == nil {
		return newestFirst(many, limit), nil
	}
	var one string
	if err := json.Unmarshal([]byte(payload), &one); err == nil && one != "" {
		return []string{one}, nil
	}
	return nil, errors.New("未能解析版本列表")
}

func extractJSONPayload(output string) string {
	s := strings.TrimSpace(output)
	idx := strings.IndexAny(s, `[{"`)
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(s[idx:])
}

func newestFirst(items []string, limit int) []string {
	if limit > 0 && len(items) > limit {
		items = items[len(items)-limit:]
	}
	out := make([]string, len(items))
	for i, item := range items {
		out[len(items)-1-i] = item
	}
	return out
}

func quoteShell(s string) string {
	return "'" + strings.ReplaceAll(s, `'`, `'\''`) + "'"
}
