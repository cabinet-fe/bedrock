package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"bedrock/internal/pkg"
	"bedrock/internal/project/model"

	"gorm.io/gorm"
)

// BugAIExtractResult contains structured fields parsed from error logs or stack traces.
type BugAIExtractResult struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Severity    string `json:"severity"`
	Priority    string `json:"priority"`
}

// AgentLauncher creates an AgentRun for bug investigation.
type AgentLauncher interface {
	CreateRun(agentID uint, userID uint, projectID uint, prompt string) (uint, error)
}

// BugAIBridge abstracts AI extraction, root cause analysis, and agent dispatching.
type BugAIBridge interface {
	Extract(ctx context.Context, content string) (*BugAIExtractResult, error)
	Analyze(ctx context.Context, bug *model.ProjectBug, prompt string) (string, error)
	DispatchAgent(ctx context.Context, bug *model.ProjectBug, agentID, userID uint, userPrompt string) (uint, error)
}

// DefaultBugAIBridge implements BugAIBridge by querying active AI models from DB and executing HTTP chat completions.
type DefaultBugAIBridge struct {
	db            *gorm.DB
	httpClient    *http.Client
	agentLauncher AgentLauncher
}

// NewDefaultBugAIBridge constructs a DefaultBugAIBridge.
func NewDefaultBugAIBridge(db *gorm.DB, launcher AgentLauncher) *DefaultBugAIBridge {
	return &DefaultBugAIBridge{
		db:            db,
		httpClient:    &http.Client{},
		agentLauncher: launcher,
	}
}

func (b *DefaultBugAIBridge) SetHTTPClient(client *http.Client) {
	if client != nil {
		b.httpClient = client
	}
}

func (b *DefaultBugAIBridge) SetAgentLauncher(launcher AgentLauncher) {
	b.agentLauncher = launcher
}

type providerModelInfo struct {
	ModelID      string
	APIURL       string
	APIKeyCipher string
}

func (b *DefaultBugAIBridge) findActiveModel(ctx context.Context) (*providerModelInfo, error) {
	if b.db == nil {
		return nil, errors.New("系统未配置数据库连接")
	}
	type queryResult struct {
		ModelID      string `gorm:"column:model_id"`
		APIURL       string `gorm:"column:api_url"`
		APIKeyCipher string `gorm:"column:api_key_cipher"`
	}
	var res queryResult
	err := b.db.WithContext(ctx).Table("ai_models").
		Select("ai_models.model_id, ai_providers.api_url, ai_providers.api_key_cipher").
		Joins("JOIN ai_providers ON ai_providers.id = ai_models.provider_id").
		Where("ai_models.enabled = ? AND ai_providers.enabled = ?", true, true).
		Order("ai_models.sort_order ASC, ai_models.id ASC").
		First(&res).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("系统未配置可用的 AI 模型服务")
	}
	if err != nil {
		return nil, err
	}
	return &providerModelInfo{
		ModelID:      res.ModelID,
		APIURL:       res.APIURL,
		APIKeyCipher: res.APIKeyCipher,
	}, nil
}

func (b *DefaultBugAIBridge) callLLM(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	pm, err := b.findActiveModel(ctx)
	if err != nil {
		return "", err
	}
	apiKey := ""
	if pm.APIKeyCipher != "" {
		dec, err := pkg.Decrypt(pm.APIKeyCipher)
		if err == nil {
			apiKey = dec
		}
	}

	targetURL := strings.TrimRight(strings.TrimSpace(pm.APIURL), "/")
	if !strings.HasSuffix(targetURL, "/chat/completions") {
		targetURL += "/chat/completions"
	}

	payload := map[string]any{
		"model": pm.ModelID,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"stream": false,
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := b.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("AI 服务请求失败: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取 AI 响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("AI 服务返回错误 [%d]: %s", resp.StatusCode, string(respBytes))
	}

	type chatChoiceMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type chatChoice struct {
		Message chatChoiceMsg `json:"message"`
	}
	type chatResp struct {
		Choices []chatChoice `json:"choices"`
	}
	var cr chatResp
	if err := json.Unmarshal(respBytes, &cr); err != nil || len(cr.Choices) == 0 {
		return "", errors.New("解析 AI 响应失败")
	}
	return cr.Choices[0].Message.Content, nil
}

// Extract parses error stack traces or logs into structured bug fields.
func (b *DefaultBugAIBridge) Extract(ctx context.Context, content string) (*BugAIExtractResult, error) {
	systemPrompt := `你是一个软件缺陷分析专家。请根据用户提供的错误日志、异常堆栈或缺陷描述，提取并总结结构化的缺陷信息。
请务必直接输出合法的 JSON 字符串（不要包含 markdown 代码块反引号，不要包含任何额外解释），格式如下：
{
  "title": "缺陷标题",
  "description": "详细描述与复现步骤",
  "severity": "low" | "normal" | "high" | "critical",
  "priority": "low" | "normal" | "high" | "urgent"
}`
	raw, err := b.callLLM(ctx, systemPrompt, content)
	if err != nil {
		return nil, err
	}
	cleaned := cleanJSONFence(raw)
	var res BugAIExtractResult
	if err := json.Unmarshal([]byte(cleaned), &res); err != nil {
		res.Title = fallbackTitle(content)
		res.Description = content
		res.Severity = model.BugSeverityNormal
		res.Priority = model.BugPriorityNormal
	}
	if strings.TrimSpace(res.Title) == "" {
		res.Title = fallbackTitle(content)
	}
	if strings.TrimSpace(res.Description) == "" {
		res.Description = content
	}
	if !model.IsValidBugSeverity(res.Severity) {
		res.Severity = model.BugSeverityNormal
	}
	if !model.IsValidBugPriority(res.Priority) {
		res.Priority = model.BugPriorityNormal
	}
	return &res, nil
}

// Analyze performs root-cause analysis based on bug context and user prompt.
func (b *DefaultBugAIBridge) Analyze(ctx context.Context, bug *model.ProjectBug, prompt string) (string, error) {
	systemPrompt := `你是一个资深软件架构师和故障排查专家。请根据提供的缺陷详情（标题、描述、严重程度、关联仓库与分支等上下文），深入分析缺陷产生的可能根因，并给出具体的排查步骤、代码修复建议及预防措施。请用中文回答，排版结构清晰。`
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("【缺陷标题】: %s\n", bug.Title))
	if bug.Description != "" {
		sb.WriteString(fmt.Sprintf("【缺陷描述】:\n%s\n", bug.Description))
	}
	sb.WriteString(fmt.Sprintf("【严重程度】: %s | 【优先级】: %s\n", bug.Severity, bug.Priority))
	if bug.RepositoryName != "" {
		sb.WriteString(fmt.Sprintf("【代码仓库】: %s\n", bug.RepositoryName))
	}
	if bug.Branch != "" {
		sb.WriteString(fmt.Sprintf("【分支】: %s\n", bug.Branch))
	}
	if strings.TrimSpace(prompt) != "" {
		sb.WriteString(fmt.Sprintf("【用户补充提示】:\n%s\n", strings.TrimSpace(prompt)))
	}

	analysis, err := b.callLLM(ctx, systemPrompt, sb.String())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(analysis), nil
}

// DispatchAgent creates an AgentRun for bug investigation.
func (b *DefaultBugAIBridge) DispatchAgent(ctx context.Context, bug *model.ProjectBug, agentID, userID uint, userPrompt string) (uint, error) {
	if b.agentLauncher == nil {
		return 0, errors.New("智能体派发器未配置")
	}
	var promptBuilder strings.Builder
	promptBuilder.WriteString(fmt.Sprintf("[Bug #%d 排查任务]\n", bug.ID))
	promptBuilder.WriteString(fmt.Sprintf("标题: %s\n", bug.Title))
	if bug.Branch != "" {
		promptBuilder.WriteString(fmt.Sprintf("分支: %s\n", bug.Branch))
	}
	if bug.Description != "" {
		promptBuilder.WriteString(fmt.Sprintf("描述: %s\n", bug.Description))
	}
	if strings.TrimSpace(userPrompt) != "" {
		promptBuilder.WriteString(fmt.Sprintf("排查要求: %s\n", strings.TrimSpace(userPrompt)))
	}

	return b.agentLauncher.CreateRun(agentID, userID, bug.ProjectID, promptBuilder.String())
}

func cleanJSONFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		lines := strings.Split(s, "\n")
		if len(lines) >= 2 && strings.HasPrefix(lines[0], "```") {
			lines = lines[1:]
		}
		if len(lines) >= 1 && strings.HasPrefix(lines[len(lines)-1], "```") {
			lines = lines[:len(lines)-1]
		}
		s = strings.Join(lines, "\n")
	}
	return strings.TrimSpace(s)
}

func fallbackTitle(content string) string {
	trimmed := strings.TrimSpace(content)
	lines := strings.Split(trimmed, "\n")
	if len(lines) > 0 {
		line := strings.TrimSpace(lines[0])
		if len([]rune(line)) > 80 {
			return string([]rune(line)[:80])
		}
		if line != "" {
			return line
		}
	}
	return "未命名缺陷"
}
