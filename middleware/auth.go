package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"seckill/repository"
	"seckill/utils"

	"github.com/gin-gonic/gin"
)

func JWTAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, utils.Resp(401, "缺少认证信息", nil))
			c.Abort()
			return
		}

		// Bearer token
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, utils.Resp(401, "格式错误", nil))
			c.Abort()
			return
		}

		tokenString := parts[1]
		claims, err := utils.ParseToken(tokenString)
		if err != nil {
			c.JSON(http.StatusUnauthorized, utils.Resp(401, "无效的token", nil))
			c.Abort()
			return
		}

		userID := claims.UserID

		// 检查是否被挤下线（单设备登录）
		storedToken, err := repository.GetString(c.Request.Context(), repository.UserTokenKey(strconv.Itoa(int(userID))))
		if err != nil || storedToken != tokenString {
			c.JSON(http.StatusUnauthorized, utils.Resp(401, "账号已在其他设备登录", nil))
			c.Abort()
			return
		}

		c.Set("user_id", userID)
		c.Set("user_role", claims.Role)
		c.Next()
	}
}

func RequireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userRole := c.GetString("user_role")
		for _, role := range roles {
			if userRole == role {
				c.Next()
				return
			}
		}
		c.JSON(http.StatusForbidden, utils.Resp(403, "权限不足", nil))
		c.Abort()
	}
}

// AdminAuth 管理员权限校验（需在 JWTAuth 之后使用）
func AdminAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetUint("user_id")
		if userID == 0 {
			c.JSON(http.StatusUnauthorized, utils.Resp(401, "未登录", nil))
			c.Abort()
			return
		}

		// 从 Redis 查 role（JWT 中未携带 role，需从 DB 或 context 获取）
		// 此处从 context 中取，需在 JWTAuth 阶段将 role 写入
		userRole := c.GetString("user_role")
		if userRole != "admin" {
			c.JSON(http.StatusForbidden, utils.Resp(403, "权限不足", nil))
			c.Abort()
			return
		}
		c.Next()
	}
}
