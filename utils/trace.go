package utils

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func GetTraceID(c *gin.Context) string {
	traceID, exists := c.Get("trace_id")
	if !exists {
		return ""
	}
	return traceID.(string)
}

func GenerateTraceID() string {
	return uuid.New().String()
}
