package service

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"bedrock/internal/ai/model"
)

type UpstreamHTTPError struct {
	StatusCode int
	Message    string
}

func (e *UpstreamHTTPError) Error() string {
	return fmt.Sprintf("上游服务商错误 [%d]: %s", e.StatusCode, e.Message)
}

type ChatProxy struct {
	providerSvc *ProviderService
	chatSvc     *ChatService
	httpClient  *http.Client
}

func NewChatProxy(providerSvc *ProviderService, chatSvc *ChatService) *ChatProxy {
	return &ChatProxy{
		providerSvc: providerSvc,
		chatSvc:     chatSvc,
		httpClient:  &http.Client{},
	}
}

// SetHTTPClient sets a custom HTTP client (primarily for testing).
func (p *ChatProxy) SetHTTPClient(client *http.Client) {
	if client != nil {
		p.httpClient = client
	}
}

type streamChunkChoiceDelta struct {
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content"`
}

type streamChunkChoice struct {
	Delta streamChunkChoiceDelta `json:"delta"`
}

type streamChunk struct {
	Choices []streamChunkChoice `json:"choices"`
}

// ProxyCompletions handles proxying an OpenAI-compatible chat completion request with SSE streaming.
func (p *ChatProxy) ProxyCompletions(c *gin.Context, userID uint, req model.ChatCompletionRequest) error {
	modelID := strings.TrimSpace(req.Model)
	if modelID == "" {
		return errors.New("缺少 model 参数")
	}

	aiModel, aiProvider, err := p.providerSvc.FindEnabledModelWithProvider(modelID)
	if err != nil || aiModel == nil || aiProvider == nil {
		return fmt.Errorf("未找到可用模型配置或对应服务商已禁用: %s", modelID)
	}

	apiKey, err := p.providerSvc.DecryptAPIKey(aiProvider.ID)
	if err != nil {
		return fmt.Errorf("解密 API Key 失败: %w", err)
	}

	// Prepare upstream payload: inject default params, extra params, reasoning_effort, and stream
	upstream := make(map[string]any)
	for k, v := range aiModel.DefaultParams {
		upstream[k] = v
	}
	for k, v := range req.Extra {
		upstream[k] = v
	}
	upstream["model"] = modelID
	upstream["messages"] = req.Messages
	upstream["stream"] = true
	if req.ReasoningEffort != "" {
		upstream["reasoning_effort"] = req.ReasoningEffort
	}

	targetURL := strings.TrimRight(strings.TrimSpace(aiProvider.APIURL), "/")
	if !strings.HasSuffix(targetURL, "/chat/completions") {
		targetURL += "/chat/completions"
	}

	payloadBytes, err := json.Marshal(upstream)
	if err != nil {
		return fmt.Errorf("序列化上游请求体失败: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, targetURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return fmt.Errorf("构建上游 HTTP 请求失败: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("请求上游服务商失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return &UpstreamHTTPError{
			StatusCode: resp.StatusCode,
			Message:    string(bodyBytes),
		}
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	reader := bufio.NewReader(resp.Body)
	var accumulatedContent strings.Builder
	var accumulatedReasoning strings.Builder

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			if _, writeErr := c.Writer.Write([]byte(line)); writeErr != nil {
				break
			}
			c.Writer.Flush()

			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "data:") {
				dataContent := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
				if dataContent != "[DONE]" && dataContent != "" {
					var chunk streamChunk
					if json.Unmarshal([]byte(dataContent), &chunk) == nil {
						for _, ch := range chunk.Choices {
							if ch.Delta.Content != "" {
								accumulatedContent.WriteString(ch.Delta.Content)
							}
							if ch.Delta.ReasoningContent != "" {
								accumulatedReasoning.WriteString(ch.Delta.ReasoningContent)
							}
						}
					}
				}
			}
		}
		if err != nil {
			break
		}
	}

	// If a valid session ID is specified, persist question and answer upon completion
	if req.SessionID != nil && *req.SessionID > 0 {
		var lastUserContent string
		for i := len(req.Messages) - 1; i >= 0; i-- {
			if req.Messages[i].Role == model.RoleUser {
				lastUserContent = req.Messages[i].Content
				break
			}
		}
		_ = p.chatSvc.SaveExchange(*req.SessionID, userID, lastUserContent, accumulatedContent.String(), accumulatedReasoning.String())
	}

	return nil
}
