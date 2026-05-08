package handler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"seckill/service"
	"seckill/utils"

	"github.com/gin-gonic/gin"
)

// SeckillHandler 秒杀处理器
type SeckillHandler struct {
	seckillService *service.SeckillService
	breaker        *utils.CircuitBreaker
}

// NewSeckillHandler 创建秒杀处理器
func NewSeckillHandler(seckillService *service.SeckillService) *SeckillHandler {
	return &SeckillHandler{
		seckillService: seckillService,
		breaker: utils.NewCircuitBreaker(utils.CircuitBreakerConfig{
			FailureThreshold: 5,        // 5次失败触发熔断
			SuccessThreshold: 3,        // 3次成功恢复
			HalfMaxRequests:  5,        // 半开状态放行5个请求试探
			OpenTimeout:      30 * time.Second,
		}),
	}
}

// Seckill 秒杀下单
func (h *SeckillHandler) Seckill(c *gin.Context) {
	// ========== 步骤1：熔断器检查 ==========
	if !h.breaker.Allow() {
		c.JSON(http.StatusServiceUnavailable, utils.Resp(5002, "系统繁忙，请稍后再试", nil))
		return
	}

	// ========== 步骤2：解析参数 ==========
	skuID, err := strconv.ParseUint(c.Param("sku_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, utils.Resp(1001, "参数错误", nil))
		return
	}

	userID := c.GetUint("user_id")
	traceID := utils.GetTraceID(c)

	// ========== 步骤3：设置超时 ==========
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	// ========== 步骤4：执行秒杀 ==========
	queueToken, err := h.seckillService.Seckill(ctx, userID, uint(skuID), traceID)
	if err != nil {
		code, msg := h.seckillService.ErrorCode(err)
		if code == 5001 {
			h.breaker.RecordFailure()
		}
		c.JSON(http.StatusOK, utils.Resp(code, msg, nil))
		return
	}

	h.breaker.RecordSuccess()

	c.JSON(http.StatusOK, utils.Resp(0, "排队中", gin.H{
		"queue_token": queueToken,
		"status":      "queuing",
	}))
}

// PollOrder 轮询秒杀排队结果  GET /api/v1/seckill/queue/:token
func (h *SeckillHandler) PollOrder(c *gin.Context) {
	token := c.Param("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, utils.Resp(1001, "参数错误", nil))
		return
	}

	orderNo, status, err := h.seckillService.PollOrderStatus(token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, utils.Resp(5001, "系统错误", nil))
		return
	}

	c.JSON(http.StatusOK, utils.Resp(0, "success", gin.H{
		"status":   status,
		"order_no": orderNo,
	}))
}

// GetOrder 查询订单
func (h *SeckillHandler) GetOrder(c *gin.Context) {
	orderID := c.Param("id")
	userID := c.GetUint("user_id")

	order, err := h.seckillService.GetOrderByNo(orderID)
	if err != nil {
		c.JSON(http.StatusOK, utils.Resp(3001, "订单不存在", nil))
		return
	}

	// 校验订单所属用户
	if order.UserID != userID {
		c.JSON(http.StatusOK, utils.Resp(3001, "订单不存在", nil))
		return
	}

	c.JSON(http.StatusOK, utils.Resp(0, "success", gin.H{
		"order_id":   order.OrderNo,
		"sku_id":     order.SkuID,
		"status":     order.Status,
		"created_at": order.CreatedAt,
	}))
}

// GetMyOrders 我的订单列表
func (h *SeckillHandler) GetMyOrders(c *gin.Context) {
	userID := c.GetUint("user_id")

	p := utils.ParsePagination(c)

	orders, total, err := h.seckillService.GetOrdersByUserID(userID, p.Page, p.PageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, utils.Resp(5001, "系统错误", nil))
		return
	}

	c.JSON(http.StatusOK, utils.Resp(0, "success", gin.H{
		"list":      orders,
		"total":     total,
		"page":      p.Page,
		"page_size": p.PageSize,
	}))

}

// GetBreakerStatus 获取熔断器状态（监控用）
func (h *SeckillHandler) GetBreakerStatus(c *gin.Context) {
	stats := h.breaker.GetStats()

	c.JSON(http.StatusOK, utils.Resp(0, "success", gin.H{
		"state":            stats.State.String(),
		"total_requests":   stats.TotalRequests,
		"total_failures":   stats.TotalFailures,
		"total_successes":  stats.TotalSuccesses,
		"failure_rate":     fmt.Sprintf("%.2f%%", stats.FailureRate*100),
		"last_change":      stats.LastStateChange.Format("2006-01-02 15:04:05"),
	}))
}
