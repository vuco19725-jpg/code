package handler

import (
	"encoding/json"
	"net/http"
	"os"

	"seckill/service"
	"seckill/utils"

	"github.com/gin-gonic/gin"
)

// AIHandler AI 处理器
type AIHandler struct {
	aiService *service.AIService
}

// NewAIHandler 创建 AI Handler
func NewAIHandler(aiService *service.AIService) *AIHandler {
	return &AIHandler{
		aiService: aiService,
	}
}

// ChatRequest 聊天请求
type ChatRequest struct {
	Question string `json:"question" binding:"required,min=2,max=500"`
	UserID   string `json:"user_id"`
}

// Chat 聊天接口
func (h *AIHandler) Chat(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, utils.Resp(4001, "参数错误", nil))
		return
	}

	// 校验问题
	if !service.IsValidQuestion(req.Question) {
		c.JSON(http.StatusBadRequest, utils.Resp(4002, "问题长度需在2-500字之间", nil))
		return
	}

	// 调用 AI 服务
	resp, err := h.aiService.Chat(c.Request.Context(), req.Question)
	if err != nil {
		utils.Error("ai chat failed",
			utils.String("error", err.Error()),
			utils.String("question", req.Question),
		)
		c.JSON(http.StatusOK, utils.Resp(5001, "AI 服务暂时不可用，请稍后再试", nil))
		return
	}

	c.JSON(http.StatusOK, utils.Resp(0, "success", gin.H{
		"answer": resp.Answer,
	}))
}

// ChatSSE 流式聊天接口
func (h *AIHandler) ChatSSE(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, utils.Resp(4001, "参数错误", nil))
		return
	}

	// 校验问题
	if !service.IsValidQuestion(req.Question) {
		c.JSON(http.StatusBadRequest, utils.Resp(4002, "问题长度需在2-500字之间", nil))
		return
	}

	// 调用 AI 流式服务
	resp, streamCh, err := h.aiService.ChatStream(c.Request.Context(), req.Question)
	if err != nil {
		utils.Error("ai chat stream failed",
			utils.String("error", err.Error()),
			utils.String("question", req.Question),
		)
		c.JSON(http.StatusOK, utils.Resp(5001, err.Error(), nil))
		return
	}
	defer resp.Release()

	// 设置 SSE 响应头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	// 流式发送 LLM 输出
	for token := range streamCh {
		data, _ := json.Marshal(token)
		c.Writer.Write([]byte("data: " + string(data) + "\n\n"))
		c.Writer.Flush()
	}

	// 结束信号
	c.Writer.WriteString("data: \"[DONE]\"\n\n")
	c.Writer.Flush()
}

// IngestRequest 知识库更新请求
type IngestRequest struct {
	FilePath string `json:"file_path" binding:"required"`
}

// Ingest 更新知识库
func (h *AIHandler) Ingest(c *gin.Context) {
	var req IngestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, utils.Resp(4001, "参数错误", nil))
		return
	}

	// 读取文件
	content, err := os.ReadFile(req.FilePath)
	if err != nil {
		c.JSON(http.StatusBadRequest, utils.Resp(4003, "读取文件失败: "+err.Error(), nil))
		return
	}

	// 更新知识库
	if err := h.aiService.IngestKnowledge(c.Request.Context(), string(content)); err != nil {
		utils.Error("ingest knowledge failed",
			utils.String("error", err.Error()),
			utils.String("file", req.FilePath),
		)
		c.JSON(http.StatusInternalServerError, utils.Resp(5001, "更新知识库失败", nil))
		return
	}

	utils.Info("knowledge ingested",
		utils.String("file", req.FilePath),
	)

	c.JSON(http.StatusOK, utils.Resp(0, "success", gin.H{
		"file_path": req.FilePath,
		"status":    "updated",
	}))
}

// Capabilities 获取 AI 支持的能力
func (h *AIHandler) Capabilities(c *gin.Context) {
	c.JSON(http.StatusOK, utils.Resp(0, "success", gin.H{
		"categories": []string{
			"秒杀规则",
			"商品咨询",
			"订单问题",
			"账户问题",
			"系统限制",
		},
		"example_questions": []string{
			"秒杀什么时候开始？",
			"怎么查看我的订单？",
			"每个用户限购几件？",
			"秒杀失败会返回什么错误？",
			"怎么登录账号？",
		},
	}))
}
