package handler

import (
	"fmt"
	"net/http"
	"time"

	"seckill/model"
	"seckill/repository"
	"seckill/service"
	"seckill/utils"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type UserHandler struct {
	userService *service.UserService
}

func NewUserHandler(db *gorm.DB) *UserHandler {
	return &UserHandler{
		userService: service.NewUserService(db),
	}
}

// Register 注册
func (h *UserHandler) Register(c *gin.Context) {
	var req struct {
		Phone    string `json:"phone" binding:"required,len=11"`
		Password string `json:"password" binding:"required,min=6"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, utils.Resp(1001, "参数错误", nil))
		return
	}

	// 加密密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, utils.Resp(5001, "系统错误", nil))
		return
	}

	user := &model.User{
		Phone:    req.Phone,
		Password: string(hashedPassword),
		Role:     "user",
	}

	if err := h.userService.CreateUser(user); err != nil {
		c.JSON(http.StatusBadRequest, utils.Resp(1002, "用户已存在", nil))
		return
	}

	utils.Success(c, gin.H{"user_id": user.ID})
}

// Login 登录
func (h *UserHandler) Login(c *gin.Context) {
	var req struct {
		Phone    string `json:"phone" binding:"required,len=11"`
		Password string `json:"password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, utils.Resp(1001, "参数错误", nil))
		return
	}

	user, err := h.userService.GetUserByPhone(req.Phone)
	if err != nil {
		c.JSON(http.StatusBadRequest, utils.Resp(1003, "用户不存在", nil))
		return
	}

	// 验证密码
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		c.JSON(http.StatusBadRequest, utils.Resp(1004, "密码错误", nil))
		return
	}

	// 生成 Token（携带 role）
	token, err := utils.GenerateToken(user.ID, user.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, utils.Resp(5001, "系统错误", nil))
		return
	}

	// 单设备登录：删除旧 Token，设置新 Token
	userIDStr := fmt.Sprintf("%d", user.ID)
	ctx := c.Request.Context()
	repository.Delete(ctx, repository.UserTokenKey(userIDStr))
	repository.SetWithExpire(ctx, repository.UserTokenKey(userIDStr), token, 24*time.Hour)

	c.JSON(http.StatusOK, utils.Resp(0, "success", gin.H{
		"token":  token,
		"user_id": user.ID,
	}))
}

// GetUserInfo 获取用户信息
func (h *UserHandler) GetUserInfo(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, utils.Resp(401, "未登录", nil))
		return
	}
	uid, ok := userID.(uint)
	if !ok {
		c.JSON(http.StatusUnauthorized, utils.Resp(401, "登录状态异常", nil))
		return
	}

	user, err := h.userService.GetUserByID(uid)
	if err != nil {
		c.JSON(http.StatusBadRequest, utils.Resp(1003, "用户不存在", nil))
		return
	}

	c.JSON(http.StatusOK, utils.Resp(0, "success", gin.H{
		"id":    user.ID,
		"phone": user.Phone,
		"role":  user.Role,
	}))
}

// Logout 退出登录
func (h *UserHandler) Logout(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, utils.Resp(401, "未登录", nil))
		return
	}
	uid, ok := userID.(uint)
	if !ok {
		c.JSON(http.StatusUnauthorized, utils.Resp(401, "登录状态异常", nil))
		return
	}

	// 删除 Redis 中的用户 Token
	userIDStr := fmt.Sprintf("%d", uid)
	repository.Delete(c.Request.Context(), repository.UserTokenKey(userIDStr))

	utils.Info("user logout",
		utils.String("user_id", userIDStr),
		utils.String("trace_id", utils.GetTraceID(c)),
	)

	c.JSON(http.StatusOK, utils.Resp(0, "success", nil))
}
