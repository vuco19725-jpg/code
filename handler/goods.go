package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"seckill/model"
	"seckill/service"
	"seckill/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type GoodsHandler struct {
	goodsService *service.GoodsService
}

func NewGoodsHandler(db *gorm.DB) *GoodsHandler {
	return &GoodsHandler{
		goodsService: service.NewGoodsService(db),
	}
}

// CreateGoods 创建秒杀商品（admin）
func (h *GoodsHandler) CreateGoods(c *gin.Context) {
	var req struct {
		Name      string  `json:"name" binding:"required"`
		Price     float64 `json:"price" binding:"required,gt=0"`
		Stock     int     `json:"stock" binding:"required,gt=0"`
		StartTime string  `json:"start_time" binding:"required"`
		EndTime   string  `json:"end_time" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, utils.Resp(1001, "参数错误", nil))
		return
	}

	startTime, err := time.Parse("2006-01-02 15:04:05", req.StartTime)
	if err != nil {
		c.JSON(http.StatusBadRequest, utils.Resp(1001, "开始时间格式错误", nil))
		return
	}

	endTime, err := time.Parse("2006-01-02 15:04:05", req.EndTime)
	if err != nil {
		c.JSON(http.StatusBadRequest, utils.Resp(1001, "结束时间格式错误", nil))
		return
	}

	goods := &model.SeckillGoods{
		Name:      req.Name,
		Price:     req.Price,
		Stock:     req.Stock,
		StartTime: startTime,
		EndTime:   endTime,
	}

	if err := h.goodsService.CreateGoods(goods); err != nil {
		c.JSON(http.StatusInternalServerError, utils.Resp(5001, "系统错误", nil))
		return
	}

	// 预热库存到 Redis
	go func() {
		defer func() {
			if r := recover(); r != nil {
				utils.Error("PreHeatStock panic", utils.String("error", fmt.Sprintf("%v", r)))
			}
		}()
		h.goodsService.PreHeatStock(goods.ID)
	}()

	utils.Success(c, gin.H{"goods_id": goods.ID})
}

// GetGoodsList 商品列表
func (h *GoodsHandler) GetGoodsList(c *gin.Context) {
	p := utils.ParsePagination(c)

	goodsList, total, err := h.goodsService.GetGoodsList(c.Request.Context(), p.Page, p.PageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, utils.Resp(5001, "系统错误", nil))
		return
	}

	c.JSON(http.StatusOK, utils.Resp(0, "success", gin.H{
		"list":      goodsList,
		"total":     total,
		"page":      p.Page,
		"page_size": p.PageSize,
	}))
}

// GetGoodsDetail 商品详情
func (h *GoodsHandler) GetGoodsDetail(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, utils.Resp(1001, "参数错误", nil))
		return
	}

	goods, err := h.goodsService.GetGoodsByID(uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, utils.Resp(2001, "商品不存在", nil))
		return
	}

	// 获取实时库存
	stock, _ := h.goodsService.GetStock(c.Request.Context(), uint(id))

	c.JSON(http.StatusOK, utils.Resp(0, "success", gin.H{
		"id":          goods.ID,
		"name":        goods.Name,
		"price":       goods.Price,
		"stock":       stock,
		"start_time":  goods.StartTime,
		"end_time":    goods.EndTime,
	}))
}

// UpdateStock 修改库存（admin）
func (h *GoodsHandler) UpdateStock(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, utils.Resp(1001, "参数错误", nil))
		return
	}

	var req struct {
		Stock int `json:"stock" binding:"required,gt=0"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, utils.Resp(1001, "参数错误", nil))
		return
	}

	if err := h.goodsService.UpdateStock(uint(id), req.Stock); err != nil {
		c.JSON(http.StatusInternalServerError, utils.Resp(5001, "系统错误", nil))
		return
	}

	utils.Success(c, nil)
}
