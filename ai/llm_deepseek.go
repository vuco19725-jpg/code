package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"seckill/config"
)

// DeepSeekLLM DeepSeek LLM 实现
type DeepSeekLLM struct {
	APIKey     string
	Model      string
	MaxTokens  int
	Temperature float32
}

// NewDeepSeekLLM 创建 DeepSeek LLM
func NewDeepSeekLLM(cfg *config.AIConfig) *DeepSeekLLM {
	return &DeepSeekLLM{
		APIKey:     cfg.APIKey,
		Model:      cfg.Model,
		MaxTokens:  cfg.MaxTokens,
		Temperature: cfg.Temp,
	}
}

type dsMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type dsRequest struct {
	Model       string      `json:"model"`
	MaxTokens   int         `json:"max_tokens"`
	Temperature float32    `json:"temperature"`
	Messages    []dsMessage `json:"messages"`
	Stream     bool        `json:"stream,omitempty"` // 流式标志
}

type dsResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int    `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

func (l *DeepSeekLLM) Chat(ctx context.Context, prompt string) (string, error) {
	reqBody := dsRequest{
		Model:      l.Model,
		MaxTokens:  l.MaxTokens,
		Temperature: l.Temperature,
		Messages: []dsMessage{
			{Role: "user", Content: prompt},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal chat request failed: %w", err)
	}

	url := "https://api.deepseek.com/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonData))
	if err != nil {
		return "", fmt.Errorf("create chat request failed: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", l.APIKey))

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("do chat request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("chat request failed: status=%d, body=%s", resp.StatusCode, string(body))
	}

	var result dsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode chat response failed: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("chat response empty")
	}

	return result.Choices[0].Message.Content, nil
}

// ChatStream 流式调用 DeepSeek API
func (l *DeepSeekLLM) ChatStream(ctx context.Context, prompt string) (<-chan string, error) {
	reqBody := dsRequest{
		Model:      l.Model,
		MaxTokens:  l.MaxTokens,
		Temperature: l.Temperature,
		Messages: []dsMessage{
			{Role: "user", Content: prompt},
		},
		Stream: true, // 启用流式
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal chat stream request failed: %w", err)
	}

	url := "https://api.deepseek.com/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("create chat stream request failed: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", l.APIKey))

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do chat stream request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("chat stream request failed: status=%d, body=%s", resp.StatusCode, string(body))
	}

	ch := make(chan string, 100) // 带缓冲，避免阻塞

	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		// 增大 scanner 缓冲区，处理大行
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)

		for scanner.Scan() {
			line := scanner.Text()

			// SSE 格式: data: {...}
			if !strings.HasPrefix(line, "data:") {
				continue
			}

			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))

			// 结束信号
			if data == "[DONE]" {
				return
			}

			// 解析增量内容
			var streamResp struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}

			if err := json.Unmarshal([]byte(data), &streamResp); err != nil {
				continue
			}

			if len(streamResp.Choices) > 0 && streamResp.Choices[0].Delta.Content != "" {
				select {
				case ch <- streamResp.Choices[0].Delta.Content:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return ch, nil
}
