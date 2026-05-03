package handler

import (
	"net/http"

	"seckill/repository"
	"seckill/utils"

	"github.com/gin-gonic/gin"
)

type HealthHandler struct{}

func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

// Health 基础健康检查
func (h *HealthHandler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
	})
}

// Ready 就绪检查（依赖服务检查）
func (h *HealthHandler) Ready(c *gin.Context) {
	// 检查 MySQL
	db, err := repository.DB.DB()
	if err != nil || db.Ping() != nil {
		c.JSON(http.StatusServiceUnavailable, utils.Resp(5001, "mysql error", nil))
		return
	}

	// 检查 Redis
	if _, err := repository.Redis.Ping(c.Request.Context()).Result(); err != nil {
		c.JSON(http.StatusServiceUnavailable, utils.Resp(5001, "redis error", nil))
		return
	}

	c.JSON(http.StatusOK, utils.Resp(0, "ok", nil))
}
