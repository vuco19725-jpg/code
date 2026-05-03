package middleware

import (
	"fmt"
	"net/http"
	"time"

	"seckill/repository"
	"seckill/utils"

	"github.com/gin-gonic/gin"
)

// rateLimitBreaker 限流器的熔断器（单例）
// 当 Redis 连续失败时触发熔断，防止无限流保护地被刷爆
var rateLimitBreaker = utils.NewCircuitBreaker(utils.CircuitBreakerConfig{
	FailureThreshold: 3,        // 恢复：3次失败触发
	SuccessThreshold: 2,        // 恢复：2次成功恢复
	HalfMaxRequests:  5,        // 恢复：半开状态放行5个
	OpenTimeout:      10 * time.Second,
})

// RateLimitByIP 基于 IP 的限流
func RateLimitByIP(limit int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 熔断器检查
		if !rateLimitBreaker.Allow() {
			c.JSON(http.StatusServiceUnavailable, utils.Resp(5002, "系统繁忙，请稍后再试", nil))
			c.Abort()
			return
		}

		ip := c.ClientIP()
		key := repository.RateLimitIPKey(ip)

		// 使用原子操作：INCR + EXPIRE 一步完成
		count, err := repository.IncrWithExpire(c.Request.Context(), key, window)
		if err != nil {
			// Redis 出错时放行（压测模式：保证请求通过）
			utils.Warn("rate limit check failed, allowing request", utils.Err(err))
			c.Next()
			return
		}

		if count > int64(limit) {
			c.JSON(http.StatusTooManyRequests, utils.Resp(4001, "请求过于频繁", nil))
			c.Abort()
			return
		}

		c.Next()
	}
}

// RateLimitByUser 基于用户的限流
func RateLimitByUser(limit int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 熔断器检查
		if !rateLimitBreaker.Allow() {
			c.JSON(http.StatusServiceUnavailable, utils.Resp(5002, "系统繁忙，请稍后再试", nil))
			c.Abort()
			return
		}

		userID, exists := c.Get("user_id")
		if !exists {
			// 未登录用户不限制
			c.Next()
			return
		}

		uid, ok := userID.(uint)
		if !ok {
			utils.Error("rate limit: user_id type assertion failed", utils.Any("user_id", userID))
			c.Next()
			return
		}

		key := repository.RateLimitUserKey(fmt.Sprintf("%d", uid))

		// 使用原子操作：INCR + EXPIRE 一步完成
		count, err := repository.IncrWithExpire(c.Request.Context(), key, window)
		if err != nil {
			// Redis 出错时放行（压测模式：保证请求通过）
			utils.Warn("rate limit check failed, allowing request", utils.Err(err))
			c.Next()
			return
		}

		if count > int64(limit) {
			c.JSON(http.StatusTooManyRequests, utils.Resp(4001, "请求过于频繁", nil))
			c.Abort()
			return
		}

		c.Next()
	}
}

// GetRateLimitBreakerStatus 获取限流熔断器状态（供监控使用）
func GetRateLimitBreakerStatus() map[string]interface{} {
	stats := rateLimitBreaker.GetStats()
	return map[string]interface{}{
		"state":            stats.State.String(),
		"total_requests":   stats.TotalRequests,
		"total_failures":   stats.TotalFailures,
		"total_successes":  stats.TotalSuccesses,
		"failure_rate":     fmt.Sprintf("%.2f%%", stats.FailureRate*100),
		"last_change":      stats.LastStateChange.Format("2006-01-02 15:04:05"),
	}
}
