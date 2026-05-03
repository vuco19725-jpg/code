package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"seckill/ai"
	"seckill/config"
	"seckill/utils"
)

// AIService AI 服务
type AIService struct {
	embedder    ai.Embedder
	vectorStore ai.VectorStore
	llm         ai.LLM
	splitter    *ai.MarkdownSplitter
	rateLimiter *ai.RateLimiter
	topK        int
}

// NewAIService 创建 AI 服务
func NewAIService(cfg *config.AIConfig) (*AIService, error) {
	// 创建 LLM (DeepSeek)
	llm := ai.NewDeepSeekLLM(cfg)

	// 创建 VectorStore
	vecSize := 4096  // Qwen/Qwen3-VL-Embedding-8B 维度
	vectorStore, err := ai.NewQdrantStore(cfg.QdrantAddr, "seckill_kb", vecSize)
	if err != nil {
		return nil, fmt.Errorf("create vector store failed: %w", err)
	}

	// 创建 Embedder (SiliconFlow)
	embedder := ai.NewSiliconFlowEmbedder(cfg)

	// 创建限流器（默认最大3并发，超时10秒）
	rateLimiter := ai.NewRateLimiter(cfg.MaxConcurrency, 10*time.Second)

	return &AIService{
		embedder:    embedder,
		vectorStore: vectorStore,
		llm:         llm,
		splitter:    ai.NewMarkdownSplitter(),
		rateLimiter: rateLimiter,
		topK:        cfg.TopK,
	}, nil
}

// ChatResponse 聊天响应
type ChatResponse struct {
	Answer     string
	References []ai.RetrievedChunk
}

// Chat 处理用户问题
func (s *AIService) Chat(ctx context.Context, question string) (*ChatResponse, error) {
	// 1. 向量化问题
	vectors, err := s.embedder.Embed(ctx, []string{question})
	if err != nil {
		return nil, fmt.Errorf("embed question failed: %w", err)
	}
	if len(vectors) == 0 {
		return nil, fmt.Errorf("empty embedding returned")
	}

	// 2. 检索相关知识
	chunks, err := s.vectorStore.Search(vectors[0], s.topK)
	if err != nil {
		return nil, fmt.Errorf("search knowledge failed: %w", err)
	}
	utils.Info("rag search result",
		utils.Int("chunk_count", len(chunks)),
		utils.String("question", question),
	)
	// 调试：打印每个 chunk 的内容
	for i, chunk := range chunks {
		contentPreview := chunk.Content
		if len(contentPreview) > 100 {
			contentPreview = contentPreview[:100] + "..."
		}
		utils.Info("rag_chunk",
			utils.Int("idx", i),
			utils.Any("score", chunk.Score),
			utils.String("content", contentPreview),
		)
	}

	// 3. 组装 Prompt
	prompt := ai.BuildPrompt(question, chunks)

	// 4. 调用 LLM
	answer, err := s.llm.Chat(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("llm chat failed: %w", err)
	}

	return &ChatResponse{
		Answer:     answer,
		References: chunks,
	}, nil
}

// ChatStreamResponse 流式聊天响应
type ChatStreamResponse struct {
	Answer     string
	References []ai.RetrievedChunk
	Release    func()  // 释放限流令牌
}

// ChatStream 流式处理用户问题
func (s *AIService) ChatStream(ctx context.Context, question string) (*ChatStreamResponse, <-chan string, error) {
	// 1. 向量化问题
	vectors, err := s.embedder.Embed(ctx, []string{question})
	if err != nil {
		return nil, nil, fmt.Errorf("embed question failed: %w", err)
	}
	if len(vectors) == 0 {
		return nil, nil, fmt.Errorf("empty embedding returned")
	}

	// 2. 检索相关知识
	chunks, err := s.vectorStore.Search(vectors[0], s.topK)
	if err != nil {
		return nil, nil, fmt.Errorf("search knowledge failed: %w", err)
	}
	utils.Info("rag search result",
		utils.Int("chunk_count", len(chunks)),
		utils.String("question", question),
	)

	// 3. 限流获取令牌
	if !s.rateLimiter.Acquire(ctx) {
		return nil, nil, fmt.Errorf("rate limit exceeded, please try again later")
	}

	// 4. 组装 Prompt
	prompt := ai.BuildPrompt(question, chunks)

	// 5. 流式调用 LLM
	streamCh, err := s.llm.ChatStream(ctx, prompt)
	if err != nil {
		s.rateLimiter.Release()
		return nil, nil, fmt.Errorf("llm stream failed: %w", err)
	}

	return &ChatStreamResponse{
		Answer:     "",
		References: chunks,
		Release:    s.rateLimiter.Release,
	}, streamCh, nil
}

// IngestKnowledge 更新知识库
func (s *AIService) IngestKnowledge(ctx context.Context, content string) error {
	// 1. 切分文档
	chunks := s.splitter.Split(content)
	if len(chunks) == 0 {
		return fmt.Errorf("no chunks generated")
	}

	// 2. 向量化
	texts := make([]string, len(chunks))
	for i := range chunks {
		texts[i] = chunks[i].Content
	}
	vectors, err := s.embedder.Embed(ctx, texts)
	if err != nil {
		return fmt.Errorf("embed chunks failed: %w", err)
	}

	// 3. 存入向量库（先清空再写入）
	if err := s.vectorStore.DeleteAll(); err != nil {
		return fmt.Errorf("delete old data failed: %w", err)
	}
	if err := s.vectorStore.Upsert(chunks, vectors); err != nil {
		return fmt.Errorf("upsert chunks failed: %w", err)
	}

	return nil
}

// IsValidQuestion 检查问题是否有效
func IsValidQuestion(question string) bool {
	question = strings.TrimSpace(question)
	return len(question) >= 2 && len(question) <= 500
}
