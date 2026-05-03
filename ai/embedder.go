package ai

import "context"

// Embedder 文本向量化接口
type Embedder interface {
	// Embed 将文本列表转为向量列表
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}
