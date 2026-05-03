package utils

import (
	"github.com/gin-gonic/gin"
)

type Response struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data interface{} `json:"data,omitempty"`
}

func Resp(code int, msg string, data interface{}) Response {
	return Response{
		Code: code,
		Msg:  msg,
		Data: data,
	}
}

func Success(c *gin.Context, data interface{}) {
	c.JSON(200, Resp(0, "success", data))
}

func ErrorResp(c *gin.Context, code int, msg string) {
	c.JSON(200, Resp(code, msg, nil))
}
