package middleware

import (
	"time"

	"seckill/utils"

	"github.com/gin-gonic/gin"
)

// RequestLoggerMiddleware 请求日志中间件
// 记录每个请求的处理时间、参数、响应状态
func RequestLoggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 记录开始时间
		start := time.Now()

		// 获取 trace_id（如果没有则生成）
		traceID, _ := c.Get("trace_id")
		traceIDStr, _ := traceID.(string)

		// 请求路径和方法
		path := c.Request.URL.Path
		method := c.Request.Method

		// 获取请求参数（根据方法不同获取不同参数）
		var reqParams map[string]interface{}
		if method == "GET" {
			reqParams = map[string]interface{}{
				"query": c.Request.URL.Query(),
			}
		} else {
			// POST/PUT/PATCH 等，从 form 或 body 获取
			reqParams = map[string]interface{}{
				"form": c.Request.PostForm,
			}
		}

		// 继续处理请求
		c.Next()

		// 计算处理耗时
		duration := time.Since(start)

		// 获取响应状态码
		statusCode := c.Writer.Status()

		// 根据状态码选择日志级别
		if statusCode >= 500 {
			utils.Error("request completed",
				utils.String("trace_id", traceIDStr),
				utils.String("method", method),
				utils.String("path", path),
				utils.Any("params", reqParams),
				utils.Int("status", statusCode),
				utils.Duration("duration", duration),
				utils.String("client_ip", c.ClientIP()),
			)
		} else if statusCode >= 400 {
			utils.Warn("request completed",
				utils.String("trace_id", traceIDStr),
				utils.String("method", method),
				utils.String("path", path),
				utils.Any("params", reqParams),
				utils.Int("status", statusCode),
				utils.Duration("duration", duration),
				utils.String("client_ip", c.ClientIP()),
			)
		} else {
			utils.Info("request completed",
				utils.String("trace_id", traceIDStr),
				utils.String("method", method),
				utils.String("path", path),
				utils.Any("params", reqParams),
				utils.Int("status", statusCode),
				utils.Duration("duration", duration),
				utils.String("client_ip", c.ClientIP()),
			)
		}
	}
}