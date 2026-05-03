package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"seckill/config"
	"seckill/utils"
)

// SiliconFlowEmbedder SiliconFlow embedding 实现
type SiliconFlowEmbedder struct {
	APIKey string
	Model  string
}

// NewSiliconFlowEmbedder 创建 SiliconFlow Embedder
func NewSiliconFlowEmbedder(cfg *config.AIConfig) *SiliconFlowEmbedder {
	return &SiliconFlowEmbedder{
		APIKey: cfg.EmbedKey,
		Model:  cfg.EmbedModel,
	}
}

func (e *SiliconFlowEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, 0, len(texts))

	for _, text := range texts {
		// SiliconFlow API: input 是单个字符串
		reqBody := map[string]string{
			"input": text,
			"model": e.Model,
		}

		jsonData, err := json.Marshal(reqBody)
		if err != nil {
			return nil, fmt.Errorf("marshal embed request failed: %w", err)
		}

		url := "https://api.siliconflow.cn/v1/embeddings"
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonData))
		if err != nil {
			return nil, fmt.Errorf("create embed request failed: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", e.APIKey))

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("do embed request failed: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("embed request failed: status=%d, body=%s", resp.StatusCode, string(body))
		}

		// SiliconFlow 返回格式: {"object":"list","data":[{"embedding":[...]}]}
		var result struct {
			Data []struct {
				Embedding []float32 `json:"embedding"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("decode embed response failed: %w", err)
		}

		if len(result.Data) > 0 {
			vectors = append(vectors, result.Data[0].Embedding)
			utils.Info("embedding_result",
				utils.Int("vec_dim", len(result.Data[0].Embedding)),
				utils.String("model", e.Model),
			)
		}
	}

	return vectors, nil
}
