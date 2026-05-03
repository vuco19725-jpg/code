package ai

// Chunk 文档块
type Chunk struct {
	ID       string
	Content  string
	Metadata map[string]any
}

// RetrievedChunk 检索到的文档块
type RetrievedChunk struct {
	ID      string
	Content string
	Score   float32
}

// VectorStore 向量存储接口
type VectorStore interface {
	// Upsert 插入或更新文档
	Upsert(chunks []Chunk, vectors [][]float32) error
	// Search 相似度搜索
	Search(queryVector []float32, topK int) ([]RetrievedChunk, error)
	// DeleteAll 删除所有文档
	DeleteAll() error
}
