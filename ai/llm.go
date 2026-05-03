package ai

import "context"

// LLM 大语言模型接口
type LLM interface {
	// Chat 生成对话回复（同步）
	Chat(ctx context.Context, prompt string) (string, error)

	// ChatStream 流式生成对话回复，返回增量文本的 channel
	// 调用方负责关闭 channel
	ChatStream(ctx context.Context, prompt string) (<-chan string, error)
}
