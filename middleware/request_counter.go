package middleware

import (
	"seckill/utils"

	"github.com/gin-gonic/gin"
)

// requestCounter 请求计数器单例
var requestCounter = utils.NewRequestCounter()

// GetRequestCounter 获取请求计数器
func GetRequestCounter() *utils.RequestCounter {
	return requestCounter
}

// RequestCounterMiddleware 请求计数中间件
// 追踪活跃请求数量，用于优雅关闭时判断请求是否处理完毕
func RequestCounterMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 检查是否正在关闭
		if requestCounter.IsShutting() {
			c.JSON(503, utils.Resp(5002, "服务正在关闭，请稍后重试", nil))
			c.Abort()
			return
		}

		requestCounter.Increment()
		defer requestCounter.Decrement()

		c.Next()
	}
}