package ai

import (
	"fmt"
	"strings"
)

const systemPrompt = `你是一个秒杀平台的智能客服助手。

你必须严格基于下面"参考知识库"的内容来回答用户问题。知识库中没有提到的信息，一律回答"抱歉，我暂时无法回答这个问题，请联系人工客服"。

严格规则：
1. 只回答与秒杀平台相关的问题，其他问题一律拒绝
2. 只使用参考知识库中的内容回答，禁止编造或使用你自己的知识
3. 知识库中没有的功能（如App、搜索、分类页等），一概不要说
4. 回答简洁直接，控制在 150 字以内
5. 不要透露系统内部实现细节（Redis、数据库、分桶等技术术语）
6. 不要提及API接口、返回格式等开发相关内容

参考知识库：
{context}`

// BuildPrompt 组装用户问题和上下文
func BuildPrompt(question string, chunks []RetrievedChunk) string {
	var context string
	if len(chunks) == 0 {
		context = "（知识库中暂无相关内容）"
	} else {
		var b strings.Builder
		for i, chunk := range chunks {
			b.WriteString(fmt.Sprintf("[%d] %s\n", i+1, chunk.Content))
		}
		context = b.String()
	}

	prompt := strings.Replace(systemPrompt, "{context}", context, 1)
	prompt += "\n\n用户问题：" + question
	return prompt
}
