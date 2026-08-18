package service

import (
	"context"
	"strings"
	"time"

	"bedrock/internal/pkg"
)

const versionsLimit = 120

type VersionsResult struct {
	Items      []string `json:"items"`
	CatalogURL string   `json:"catalog_url"`
	Error      string   `json:"error,omitempty"`
}

func (s *DevEnvironmentService) ListVersions(id uint) (*VersionsResult, error) {
	item, err := s.repo.FindEnvironment(id)
	if err != nil {
		return nil, err
	}
	result := &VersionsResult{Items: []string{}, CatalogURL: catalogURLForEnv(item.Executable, item.Name)}
	command := strings.TrimSpace(item.VersionsScript)
	if command == "" {
		return result, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	output, runErr := executeCommand(ctx, command)
	if runErr != nil {
		result.Error = redact(runErr.Error())
		if strings.TrimSpace(output) != "" {
			result.Error = redact(strings.TrimSpace(output))
		}
		return result, nil
	}
	result.Items = pkg.ParseVersionLines(output, versionsLimit)
	return result, nil
}

func catalogURLForEnv(executable, name string) string {
	key := strings.ToLower(strings.TrimSpace(executable))
	if key == "" {
		key = strings.ToLower(strings.TrimSpace(name))
	}
	switch {
	case key == "go" || strings.Contains(key, "golang"):
		return "https://go.dev/dl/"
	case key == "node" || strings.Contains(key, "node"):
		return "https://nodejs.org/dist/"
	case key == "java":
		return "https://adoptium.net/temurin/releases/"
	case key == "python" || key == "python3" || strings.Contains(key, "python"):
		return "https://www.python.org/downloads/"
	default:
		return "https://mise.jdx.dev/registry.html"
	}
}
