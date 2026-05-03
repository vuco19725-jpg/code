package ai

import (
	"fmt"
	"strings"
)

// MarkdownSplitter 按 Markdown 结构切分文档
type MarkdownSplitter struct {
	MaxChunkSize int
}

// NewMarkdownSplitter 创建 Markdown 切分器
func NewMarkdownSplitter() *MarkdownSplitter {
	return &MarkdownSplitter{
		MaxChunkSize: 500,
	}
}

// Split 按 Markdown 结构切分文本
func (s *MarkdownSplitter) Split(text string) []Chunk {
	var chunks []Chunk
	lines := strings.Split(text, "\n")

	var currentSection strings.Builder
	sectionCount := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// 跳过空行和代码块标记
		if trimmed == "" || trimmed == "```" {
			continue
		}

		// 检测标题（## 开头）
		if strings.HasPrefix(trimmed, "## ") {
			// 保存上一个 section
			if currentSection.Len() > 0 {
				content := strings.TrimSpace(currentSection.String())
				if content != "" {
					chunks = append(chunks, Chunk{
						ID:      fmt.Sprintf("chunk_%d", sectionCount),
						Content: content,
						Metadata: map[string]any{},
					})
					sectionCount++
				}
				currentSection.Reset()
			}
			currentSection.WriteString(trimmed + "\n")
			continue
		}

		// 检测问题（**Q: 开头）
		if strings.HasPrefix(trimmed, "**Q:") || strings.HasPrefix(trimmed, "Q:") {
			// 如果当前内容过长，先保存
			if currentSection.Len() > s.MaxChunkSize*2 {
				content := strings.TrimSpace(currentSection.String())
				chunks = append(chunks, Chunk{
					ID:      fmt.Sprintf("chunk_%d", sectionCount),
					Content: content,
					Metadata: map[string]any{},
				})
				sectionCount++
				currentSection.Reset()
			}
		}

		currentSection.WriteString(trimmed + "\n")
	}

	// 保存最后一个 section
	if currentSection.Len() > 0 {
		content := strings.TrimSpace(currentSection.String())
		if content != "" {
			chunks = append(chunks, Chunk{
				ID:      fmt.Sprintf("chunk_%d", sectionCount),
				Content: content,
				Metadata: map[string]any{},
			})
		}
	}

	return chunks
}
