package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"bedrock/internal/resource/model"
	"bedrock/internal/resource/repository"
)

// AuditWriter appends operation-log entries (implemented by system AuditService).
type AuditWriter interface {
	Write(userID uint, username, action, resourceType, resourceID, details, ip string) error
}

type CLIService struct {
	repo  *repository.CLIRepository
	audit AuditWriter
}

func NewCLIService(repo *repository.CLIRepository, audit ...AuditWriter) *CLIService {
	svc := &CLIService{repo: repo}
	if len(audit) > 0 {
		svc.audit = audit[0]
	}
	return svc
}

func (s *CLIService) ListCLIs() ([]model.CliRuntimeDefinition, error) {
	items, err := s.repo.List()
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].RiskNotice = model.RiskNoticeSameUID
	}
	return items, nil
}

// FindByKey satisfies the AI-domain CLILookup interface (agent CLI resolution).
func (s *CLIService) FindByKey(key string) (*model.CliRuntimeDefinition, error) {
	return s.repo.FindByKey(key)
}

type DetectResult struct {
	Detected   bool   `json:"detected"`
	Output     string `json:"output"`
	Path       string `json:"path"`
	Version    string `json:"version"`
	Healthy    bool   `json:"healthy"`
	RiskNotice string `json:"risk_notice"`
}

func (s *CLIService) Detect(key string) (*DetectResult, error) {
	cli, err := s.repo.FindByKey(key)
	if err != nil {
		return nil, err
	}
	cmd := strings.TrimSpace(cli.DetectCommand)
	if cmd == "" {
		cmd = "command -v " + cli.BinaryName
	}
	output, runErr := executeShell(context.Background(), cmd)
	result := &DetectResult{
		RiskNotice: model.RiskNoticeSameUID,
		Output:     strings.TrimSpace(output),
	}
	if runErr != nil {
		result.Detected = false
		result.Healthy = false
		cli.InstallStatus = "missing"
		cli.InstalledPath = ""
		cli.InstalledVersion = ""
		cli.Healthy = false
		if result.Output == "" {
			result.Output = runErr.Error()
		}
		_ = s.repo.Update(cli)
		return result, nil
	}
	version := extractCLIVersion(output, cli.BinaryName)
	if version == "" {
		version = probeCLIVersion(cli.BinaryName)
	}
	if isPathLine(version, cli.BinaryName) {
		version = ""
	}
	path := ""
	if p, lookErr := exec.LookPath(cli.BinaryName); lookErr == nil {
		path = p
	} else if p := extractCLIPath(output, cli.BinaryName); p != "" {
		path = p
	}
	result.Detected = true
	result.Healthy = true
	result.Path = path
	result.Version = version
	cli.InstallStatus = "installed"
	cli.InstalledPath = path
	cli.InstalledVersion = version
	cli.Healthy = true
	_ = s.repo.Update(cli)
	return result, nil
}

type CheckUpdateResult struct {
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version"`
	UpdateAvailable bool   `json:"update_available"`
	Package         string `json:"package"`
	Registry        string `json:"registry,omitempty"`
	Output          string `json:"output,omitempty"`
	Error           string `json:"error,omitempty"`
}

func (s *CLIService) CheckUpdate(ctx context.Context, key string) (*CheckUpdateResult, error) {
	cli, err := s.repo.FindByKey(key)
	if err != nil {
		return nil, err
	}
	pkgName := npmPackageFromTemplate(cli.InstallTemplate)
	if pkgName == "" {
		return nil, errors.New("该 CLI 未配置 npm 包")
	}
	result := &CheckUpdateResult{
		Package:        pkgName,
		CurrentVersion: normalizeCLIVersion(cli.InstalledVersion),
	}
	if result.CurrentVersion == "" {
		if detected, detErr := s.Detect(key); detErr == nil && detected.Detected {
			result.CurrentVersion = normalizeCLIVersion(detected.Version)
		}
	}

	latest, registry, log, queryErr := s.queryLatestNPMVersion(ctx, key, pkgName)
	result.Output = log
	if queryErr != nil {
		result.Error = queryErr.Error()
		return result, nil
	}
	result.LatestVersion = latest
	result.Registry = registry
	result.UpdateAvailable = result.CurrentVersion != "" && isNewerCLIVersion(latest, result.CurrentVersion)
	return result, nil
}

type ExecuteInput struct {
	Version string `json:"version"`
}

type ExecuteResult struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

func (s *CLIService) Execute(ctx context.Context, key, operation string, input ExecuteInput, createdBy uint) (*ExecuteResult, error) {
	cli, err := s.repo.FindByKey(key)
	if err != nil {
		return nil, err
	}
	template := templateFor(cli, operation)
	if template == "" {
		return nil, errors.New("该 CLI 未配置此操作命令")
	}
	version := strings.TrimSpace(input.Version)

	if needsSource(operation, template) {
		return s.executeWithSources(ctx, key, operation, version, template, createdBy)
	}
	command := renderCLICommand(template, version, "")
	output, runErr := executeShell(ctx, command)
	if runErr != nil {
		s.auditExecute(key, operation, createdBy, false)
		return &ExecuteResult{Success: false, Output: output, Error: runErr.Error()}, nil
	}
	s.auditExecute(key, operation, createdBy, true)
	return &ExecuteResult{Success: true, Output: output}, nil
}

func (s *CLIService) executeWithSources(ctx context.Context, key, operation, version, template string, createdBy uint) (*ExecuteResult, error) {
	sources, err := s.repo.ListEnabledSources(key)
	if err != nil {
		return nil, err
	}
	// No configured sources → use the package manager default registry (no --registry).
	if len(sources) == 0 {
		command := renderCLICommand(template, version, "")
		output, runErr := executeShell(ctx, command)
		if runErr != nil {
			s.auditExecute(key, operation, createdBy, false)
			return &ExecuteResult{Success: false, Output: output, Error: runErr.Error()}, nil
		}
		s.auditExecute(key, operation, createdBy, true)
		return &ExecuteResult{Success: true, Output: output}, nil
	}
	var log strings.Builder
	for i, source := range sources {
		command := renderCLICommand(template, version, source.BaseURL)
		log.WriteString(fmt.Sprintf("trying source %q (priority %d)\n", source.Name, source.Priority))
		output, runErr := executeShell(ctx, command)
		log.WriteString(output)
		if runErr == nil {
			if i > 0 {
				log.WriteString("multi-source fallback succeeded after earlier failures\n")
			}
			s.auditExecute(key, operation, createdBy, true)
			return &ExecuteResult{Success: true, Output: log.String()}, nil
		}
		log.WriteString(fmt.Sprintf("source %q failed: %v\n", source.Name, runErr))
	}
	s.auditExecute(key, operation, createdBy, false)
	return &ExecuteResult{Success: false, Output: log.String(), Error: "所有安装源均失败"}, nil
}

func (s *CLIService) ListSources(cliKey string) ([]model.CliInstallSource, error) {
	return s.repo.ListSources(cliKey)
}

type SourceInput struct {
	CliKey   string `json:"cli_key"`
	Name     string `json:"name"`
	BaseURL  string `json:"base_url"`
	Priority int    `json:"priority"`
	Enabled  bool   `json:"enabled"`
}

func (s *CLIService) CreateSource(input SourceInput) (*model.CliInstallSource, error) {
	if strings.TrimSpace(input.CliKey) == "" || strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.BaseURL) == "" {
		return nil, errors.New("cli_key、名称和 Registry 地址不能为空")
	}
	if _, err := s.repo.FindByKey(input.CliKey); err != nil {
		return nil, errors.New("CLI 不存在")
	}
	item := &model.CliInstallSource{
		CliKey: input.CliKey, Name: input.Name, BaseURL: input.BaseURL,
		Priority: input.Priority, Enabled: input.Enabled,
	}
	if err := s.repo.CreateSource(item); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *CLIService) UpdateSource(id uint, input SourceInput) (*model.CliInstallSource, error) {
	item, err := s.repo.FindSource(id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Name) != "" {
		item.Name = input.Name
	}
	if strings.TrimSpace(input.BaseURL) != "" {
		item.BaseURL = input.BaseURL
	}
	item.Priority = input.Priority
	item.Enabled = input.Enabled
	if err := s.repo.UpdateSource(item); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *CLIService) DeleteSource(id uint) error {
	return s.repo.DeleteSource(id)
}

func (s *CLIService) auditExecute(cliKey, operation string, createdBy uint, success bool) {
	if s.audit == nil {
		return
	}
	status := "failed"
	if success {
		status = "success"
	}
	_ = s.audit.Write(createdBy, "", "cli_execute", "cli_runtime", cliKey,
		fmt.Sprintf("cli=%s op=%s status=%s", cliKey, operation, status), "")
}

func templateFor(cli *model.CliRuntimeDefinition, operation string) string {
	switch operation {
	case "install":
		return cli.InstallTemplate
	case "upgrade":
		return cli.UpgradeTemplate
	case "uninstall":
		return cli.UninstallTemplate
	default:
		return ""
	}
}

func needsSource(operation, template string) bool {
	return (operation == "install" || operation == "upgrade") && strings.Contains(template, "{{base_url}}")
}

func firstNonEmptyLine(output string) string {
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func normalizeCLIVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	if v == "" || strings.Contains(v, "/") || strings.Contains(v, `\`) {
		return ""
	}
	if m := versionPattern.FindStringSubmatch(v); len(m) > 1 {
		return m[1]
	}
	return v
}

// isNewerCLIVersion reports whether latest is strictly newer than current.
func isNewerCLIVersion(latest, current string) bool {
	l := normalizeCLIVersion(latest)
	c := normalizeCLIVersion(current)
	if l == "" || c == "" {
		return false
	}
	return compareSemver(l, c) > 0
}

// compareSemver returns 1 if a>b, -1 if a<b, 0 if equal. Non-numeric suffixes
// are compared lexicographically after the numeric core.
func compareSemver(a, b string) int {
	aCore, aPre := splitSemver(a)
	bCore, bPre := splitSemver(b)
	n := max(len(bCore), len(aCore))
	for i := 0; i < n; i++ {
		avar, bvar := 0, 0
		if i < len(aCore) {
			avar = aCore[i]
		}
		if i < len(bCore) {
			bvar = bCore[i]
		}
		if avar > bvar {
			return 1
		}
		if avar < bvar {
			return -1
		}
	}
	if aPre == bPre {
		return 0
	}
	if aPre == "" {
		return 1 // release > pre-release
	}
	if bPre == "" {
		return -1
	}
	return strings.Compare(aPre, bPre)
}

func splitSemver(v string) (core []int, prerelease string) {
	main := v
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		main = v[:i]
		prerelease = v[i+1:]
	}
	for part := range strings.SplitSeq(main, ".") {
		n, err := strconv.Atoi(part)
		if err != nil {
			n = 0
		}
		core = append(core, n)
	}
	return core, prerelease
}

func renderCLICommand(template, version, baseURL string) string {
	out := strings.ReplaceAll(template, "{{version}}", shellQuote(version))
	out = strings.ReplaceAll(out, "{{base_url}}", shellQuote(baseURL))
	return out
}

func shellQuote(s string) string {
	if s == "" {
		return ""
	}
	return strings.ReplaceAll(s, `'`, `'\''`)
}

func executeShell(ctx context.Context, command string) (string, error) {
	if runtime.GOOS == "windows" {
		cmd := exec.CommandContext(ctx, "cmd", "/C", command)
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		err := cmd.Run()
		return buf.String(), err
	}
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

var versionPattern = regexp.MustCompile(`(?i)(?:version\s+)?v?(\d+\.\d+(?:\.\d+)?(?:[-+][\w.]+)?)`)

func probeCLIVersion(binaryName string) string {
	for _, probe := range []string{
		binaryName + " --version",
		binaryName + " -v",
		binaryName + " version",
	} {
		out, err := executeShell(context.Background(), probe)
		if err != nil {
			continue
		}
		if v := extractCLIVersion(out, binaryName); v != "" && !isPathLine(v, binaryName) {
			return v
		}
	}
	return ""
}

func extractCLIVersion(output, binaryName string) string {
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || isPathLine(line, binaryName) {
			continue
		}
		if m := versionPattern.FindStringSubmatch(line); len(m) > 1 {
			return m[1]
		}
		return line
	}
	return ""
}

func extractCLIPath(output, binaryName string) string {
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if isPathLine(line, binaryName) {
			return line
		}
	}
	return ""
}

func isPathLine(line, binaryName string) bool {
	if strings.HasPrefix(line, "/") {
		return true
	}
	if len(line) >= 2 && line[1] == ':' {
		return true
	}
	if strings.Contains(line, "/") && !strings.Contains(line, " ") {
		return true
	}
	if filepath.Base(line) == binaryName {
		return true
	}
	return false
}
