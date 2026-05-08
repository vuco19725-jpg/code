# AI RAG 知识库问答系统计划

> 为秒杀系统添加 AI 智能客服能力

---

## 一、项目概述

### 1.1 目标

为秒杀系统添加 AI 客服功能，用户可以通过自然语言询问秒杀规则、订单问题、商品信息等。

### 1.2 核心价值

| 价值点 | 说明 |
|--------|------|
| **面试加分** | 展示 AI 落地能力、RAG 原理、向量检索理解 |
| **实际可用** | 替代部分人工客服，提升用户体验 |
| **技术积累** | 向量数据库、Embedding、Prompt Engineering |

### 1.3 RAG 原理

```
传统 LLM 的问题：
用户问："你们的退换货政策是什么？"
LLM 答："抱歉，我不知道你们公司的具体政策..."（幻觉/知识盲区）

RAG 的解决：
用户问："你们的退换货政策是什么？"
    ↓
[检索阶段] 从知识库找到相关内容
    → "秒杀商品不支持退换货"（相似度 0.95）
    → "质量问题可联系客服处理"（相似度 0.80）
    ↓
[生成阶段] 把检索结果注入 Prompt
    → "基于以下信息回答：1. 秒杀商品不支持退换货...
    → "根据我们的政策，秒杀商品不支持退换货..."
```

---

## 二、技术架构

### 2.1 整体架构图

```
┌─────────────────────────────────────────────────────────────────┐
│                        用户请求层                                 │
│              "秒杀什么时候开始？" / "怎么退款？"                    │
└─────────────────────────────┬───────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────┐
│                      Go API 层 (Gin)                             │
│  ┌─────────────────┐     ┌─────────────────┐                   │
│  │ POST /ai/chat   │     │ POST /ai/ingest │                   │
│  │   处理聊天请求   │     │  更新知识库      │                   │
│  └─────────────────┘     └─────────────────┘                   │
└─────────────────────────────┬───────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────┐
│                      AI 服务层 (Go)                              │
│                                                                 │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐      │
│  │  切分器   │  │ Embedder │  │  检索器   │  │  Prompt  │      │
│  │ Splitter │  │ 向量化    │  │ Retriever│  │  组装器  │      │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘      │
└─────────────────────────────┬───────────────────────────────────┘
                              ↓
         ┌────────────────────┼────────────────────┐
         ↓                    ↓                    ↓
   ┌──────────┐        ┌──────────┐       ┌──────────┐
   │ 向量数据库 │        │   LLM    │       │  知识库   │
   │ Qdrant   │        │ Claude/  │       │ Markdown │
   │          │        │ MiniMax  │       │  文件    │
   └──────────┘        └──────────┘       └──────────┘
```

### 2.2 数据流

```
知识库更新流程：
knowledge-base.md → 读取文件 → 文本切分 → Embedding → 存入 Qdrant

用户查询流程：
用户问题 → Embedding → Qdrant 检索 → 组装 Prompt → LLM 生成 → 返回回答
```

### 2.3 技术选型

| 组件 | 选型 | 理由 |
|------|------|------|
| **向量数据库** | Qdrant | 轻量、Docker 部署、Go SDK 好用 |
| **Embedding** | MiniMax embedding-2 | 用现有 MiniMax Key，无需额外申请 |
| **LLM** | MiniMax abab6.5s-chat | 用现有 MiniMax Key，支持中文 |
| **框架** | Gin | 复用现有技术栈 |
| **部署** | Docker | 与现有架构一致 |

> **成本控制**：接口限流（复用现有限流中间件）+ 答案缓存（Redis，相同问题 1 小时内返回缓存）

---

## 三、功能清单

### 3.1 API 接口

| 接口 | 方法 | 说明 | 权限 |
|------|------|------|------|
| `/ai/chat` | POST | 聊天接口 | 用户 |
| `/ai/capabilities` | GET | 支持的问题类型 | 用户 |
| `/ai/ingest` | POST | 更新知识库 | 管理员 |

### 3.2 聊天接口

**请求：**
```json
{
    "question": "秒杀什么时候开始？",
    "user_id": "12345"
}
```

**响应：**
```json
{
    "code": 0,
    "data": {
        "answer": "秒杀活动每天10:00和20:00开始，请提前做好准备！",
        "references": [
            {"content": "秒杀时间为每天10:00、20:00", "score": 0.95}
        ]
    }
}
```

### 3.3 知识库更新

**请求：**
```json
{
    "file_path": "./knowledge-base.md"
}
```

**响应：**
```json
{
    "code": 0,
    "data": {
        "chunks_count": 50,
        "status": "success"
    }
}
```

---

## 四、项目结构

与现有项目分层风格保持一致（handler → service → 底层组件）：

```
seckill/
├── handler/
│   └── ai.go                  # HTTP Handler（路由 + 参数校验）
├── service/
│   └── ai.go                  # 业务逻辑（RAG 编排）
├── ai/                         # AI 核心组件（不涉及 HTTP）
│   ├── embedder.go            # Embedder 接口
│   ├── embedder_minimax.go   # MiniMax 实现
│   ├── vector_store.go        # 向量存储接口
│   ├── vector_qdrant.go      # Qdrant 实现
│   ├── llm.go                # LLM 接口
│   ├── llm_minimax.go        # MiniMax 实现
│   ├── splitter.go           # 文档切分
│   └── prompt.go             # Prompt 模板
```

**说明**：
- 知识库文档：`seckill-kb.md`（独立文档）
- Qdrant：在现有 `deploy/docker-compose.yml` 中添加服务

---

## 五、核心实现

### 5.1 文档切分

```go
// Chunk 文档块
type Chunk struct {
    ID       string
    Content  string
    Metadata map[string]any
}

// 按段落切分
func SplitByParagraph(text string, maxLen int) []Chunk
```

**切分策略**：一个完整段落为一个 chunk，保留语义完整性

### 5.2 Embedder 接口

```go
type Embedder interface {
    Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// OpenAI 实现
type OpenAIEmbedder struct {
    APIKey string
    Model  string  // "text-embedding-3-small"
}

func (e *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error)
```

### 5.3 向量存储接口

```go
type VectorStore interface {
    Upsert(ctx context.Context, chunks []Chunk, vectors [][]float32) error
    Search(ctx context.Context, queryVector []float32, topK int) ([]RetrievedChunk, error)
    DeleteAll(ctx context.Context) error
}

// Qdrant 实现
type QdrantStore struct {
    client     *qdrant.Client
    collection string
}
```

### 5.4 LLM 接口

```go
type LLM interface {
    Chat(ctx context.Context, prompt string) (string, error)
}

// Claude 实现
type ClaudeLLM struct {
    APIKey string
}

// MiniMax 实现
type MiniMaxLLM struct {
    APIKey string
}
```

### 5.5 AI Service 主逻辑

```go
type AIService struct {
    embedder    Embedder
    vectorStore VectorStore
    llm         LLM
}

func (s *AIService) Chat(ctx context.Context, question string) (*ChatResponse, error) {
    // 1. 向量化问题
    vectors, err := s.embedder.Embed(ctx, []string{question})

    // 2. 检索相关知识
    chunks, err := s.vectorStore.Search(ctx, vectors[0], 5)

    // 3. 组装 Prompt
    prompt := BuildPrompt(question, chunks)

    // 4. 调用 LLM
    answer, err := s.llm.Chat(ctx, prompt)

    return &ChatResponse{Answer: answer, References: chunks}, nil
}
```

---

## 六、Prompt 设计

### 6.1 系统提示词

```go
const SystemPrompt = `你是一个秒杀平台的智能客服助手。
你的职责是帮助用户解答关于秒杀活动、商品、订单等问题。

重要规则：
1. 只回答与秒杀相关的问题
2. 如果不知道答案，说"这个问题我暂时无法回答，请联系人工客服"
3. 回答要简洁、专业、友好
4. 引用知识库内容时要准确，不要编造

参考知识库：
{context}`
```

### 6.2 Prompt 组装

```go
func BuildPrompt(query string, chunks []RetrievedChunk) string {
    var context strings.Builder
    for i, chunk := range chunks {
        context.WriteString(fmt.Sprintf("[%d] %s\n", i+1, chunk.Content))
    }

    prompt := strings.Replace(SystemPrompt, "{context}", context.String(), 1)
    prompt += fmt.Sprintf("\n\n用户问题：%s", query)
    return prompt
}
```

---

## 七、Docker 部署

### 7.1 Qdrant 部署

```yaml
# docker-compose.yml 新增
services:
  qdrant:
    image: qdrant/qdrant:latest
    ports:
      - "6333:6333"      # REST API
      - "6334:6334"      # gRPC
    volumes:
      - qdrant_data:/qdrant/storage

volumes:
  qdrant_data:
```

### 7.2 环境变量

```bash
# .env
OPENAI_API_KEY=sk-xxx
# 或
ANTHROPIC_API_KEY=sk-ant-xxx
MINIMAX_API_KEY=xxx
QDRANT_ADDR=localhost:6333
```

---

## 八、知识库内容

### 8.1 知识库来源

以现有的 `knowledge-base.md` 为核心知识源，补充客服常见问答。

### 8.2 知识库结构

```markdown
# 秒杀平台知识库

## 秒杀规则
- 秒杀时间为每天 10:00、20:00
- 每个用户每件商品限购 1 件
- 秒杀商品不支持退换货
- ...

## 常见问题
Q: 秒杀没抢到怎么办？
A: 可以关注下一场秒杀...

Q: 如何取消订单？
A: 秒杀订单支付后不可取消...

Q: 秒杀商品是正品吗？
A: 秒杀商品均为正品...
```

---

## 九、实施计划

| 阶段 | 内容 | 时间 | 产出 |
|------|------|------|------|
| **Phase 1** | Qdrant 部署 + 基础框架 | 1天 | Docker 运行起来 |
| **Phase 2** | Embedder + LLM 接口封装 | 1天 | 可调用 OpenAI/Claude |
| **Phase 3** | 向量存储 + 检索逻辑 | 1天 | 可检索知识库 |
| **Phase 4** | HTTP API + 集成知识库 | 1天 | 完整聊天功能 |
| **Phase 5** | 优化 + Prompt 调优 | 1天 | 回答质量优化 |

**总计：约 5 天**

---

## 十、里程碑

### M1：完成日期 + 3 天
- [ ] Qdrant Docker 部署成功
- [ ] Go 项目结构创建
- [ ] Embedder / LLM / VectorStore 接口定义

### M2：完成日期 + 5 天
- [ ] 完整聊天流程跑通
- [ ] `knowledge-base.md` 内容向量化
- [ ] 基本回答质量可用

### M3：完成日期 + 7 天
- [ ] API 接入现有系统
- [ ] 管理员更新知识库功能
- [ ] 回答质量优化

---

## 十一、面试能讲清楚的点

| 技术点 | 面试可讲内容 |
|--------|-------------|
| **RAG 原理** | 为什么需要 RAG？解决 LLM 什么问题？ |
| **向量检索** | 余弦相似度 vs 点积、ANN 索引原理 |
| **切分策略** | 不同切分方式对效果的影响 |
| **Prompt Engineering** | 怎么让 AI 回答更准确 |
| **Embedding** | 什么是向量嵌入、维度选择 |
| **架构设计** | 接口抽象、可替换实现 |

---

## 十二、风险与应对

| 风险 | 影响 | 应对 |
|------|------|------|
| LLM API 调用失败 | 回答不出来 | 返回默认回复，知识库兜底 |
| 向量检索效果差 | 答非所问 | 调优 chunk size + 混合检索 |
| API 成本超支 | 费用太高 | 限制调用频率 + 缓存向量 |
| 知识库更新延迟 | 内容过时 | 手动触发更新流程 |

---

## 十三、扩展方向（可选）

| 方向 | 说明 |
|------|------|
| 混合检索 | 向量检索 + 关键词检索（BM25）组合 |
| 重排序 | 用 BGE-Reranker 优化排序 |
| 多模态 | 支持图片输入 |
| 个性化 | 基于用户历史优化回答 |

---

**请审阅以上方案，有任何调整需求请直接提出。**
