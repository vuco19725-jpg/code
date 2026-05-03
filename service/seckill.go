package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"strconv"
	"sync/atomic"
	"time"

	"seckill/model"
	"seckill/repository"
	"seckill/utils"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// 错误定义
var (
	ErrSoldOut       = errors.New("库存不足")
	ErrAlreadyBought = errors.New("已购买过该商品")
	ErrGoodsNotExist = errors.New("商品不存在")
	ErrGoodsNotStart = errors.New("商品未开始")
	ErrGoodsEnded    = errors.New("商品已结束")
	ErrSystemError   = errors.New("系统错误")
)

// 回滚重试配置
const (
	maxRollbackRetries = 3         // 最大重试次数
	rollbackRetryDelay = 100        // 基础重试延迟（毫秒）
)

// 订单创建重试配置
const (
	maxOrderRetries = 3            // 最大重试次数
	orderRetryDelay = 100           // 基础重试延迟（毫秒）
)

// 分桶库存配置
const (
	// StockBucketCount 库存分桶数量
	// 分桶可以减少单个 key 的热点竞争，每个实例处理不同的桶
	StockBucketCount = 4
)

// getBucketForUser 根据用户 ID 获取对应的库存桶
// 使用 FNV 哈希保证均匀分布，同一用户同一商品路由到同一桶
func getBucketForUser(userID uint, skuID uint) int {
	h := fnv.New32a()
	h.Write([]byte(fmt.Sprintf("%d:%d", userID, skuID)))
	return int(h.Sum32()%uint32(StockBucketCount)) + 1
}

// SeckillService 秒杀服务
type SeckillService struct {
	db         *gorm.DB
	redis      *redis.Client
	orderChan  chan *model.Order  // 订单 channel
	stopChan   chan struct{}
	// 监控计数器
	asyncSuccess   int64  // 异步发送成功计数
	syncFallback   int64  // 同步Fallback计数
	deadLetterCount int64 // 死信订单计数（创建失败）
}

// NewSeckillService 创建秒杀服务
func NewSeckillService(db *gorm.DB, redis *redis.Client) *SeckillService {
	s := &SeckillService{
		db:        db,
		redis:     redis,
		orderChan: make(chan *model.Order, 1000), // 缓冲 1000 个订单
		stopChan:  make(chan struct{}),
	}

	// 启动后台订单处理 goroutine
	go s.processOrders()

	return s
}

// processOrders 后台处理订单创建（带重试机制）
func (s *SeckillService) processOrders() {
	for {
		select {
		case order := <-s.orderChan:
			if order == nil {
				continue
			}
			// 创建订单（带重试）
			if err := s.createOrderWithRetry(order); err != nil {
				// 创建失败，回滚库存
				skuStr := strconv.Itoa(int(order.SkuID))
				bucketID := getBucketForUser(order.UserID, order.SkuID)
				stockKey := repository.StockBucketKey(skuStr, bucketID)
				purchaseKey := repository.UserSeckillKey(strconv.Itoa(int(order.UserID)), skuStr)
				s.rollbackBucketWithRetry(context.Background(), stockKey, purchaseKey, "", skuStr, int(order.UserID), bucketID)

				utils.Error("异步创建订单失败，已回滚库存",
					utils.String("order_no", order.OrderNo),
					utils.Int("user_id", int(order.UserID)),
					utils.Int("sku_id", int(order.SkuID)),
					utils.Err(err),
				)
				atomic.AddInt64(&s.deadLetterCount, 1)
			}
		case <-s.stopChan:
			// 处理剩余订单
			remaining := 0
			for order := range s.orderChan {
				if order != nil {
					if err := s.createOrderWithRetry(order); err != nil {
						utils.Error("优雅退出时订单创建失败",
							utils.String("order_no", order.OrderNo),
							utils.Err(err),
						)
					} else {
						remaining++
					}
				}
			}
			utils.Info("订单处理完成",
				utils.Int("remaining_orders", remaining),
			)
			return
		}
	}
}

// createOrderWithRetry 带重试的订单创建
func (s *SeckillService) createOrderWithRetry(order *model.Order) error {
	var lastErr error
	for i := 0; i < maxOrderRetries; i++ {
		if err := s.db.Create(order).Error; err == nil {
			atomic.AddInt64(&s.asyncSuccess, 1)
			utils.Info("异步订单创建成功",
				utils.String("order_no", order.OrderNo),
			)
			return nil
		} else {
			lastErr = err
			utils.Warn("异步创建订单失败，重试",
				utils.String("order_no", order.OrderNo),
				utils.Int("retry", i+1),
				utils.Int("user_id", int(order.UserID)),
				utils.Err(err),
			)
			time.Sleep(time.Duration(i+1) * orderRetryDelay * time.Millisecond)
		}
	}
	return lastErr
}

// Stop 停止订单处理（优雅退出）
func (s *SeckillService) Stop() {
	close(s.stopChan)
}

// ErrorCode 将错误转换为错误码
func (s *SeckillService) ErrorCode(err error) (int, string) {
	switch err {
	case ErrSoldOut:
		return 2004, "库存不足"
	case ErrAlreadyBought:
		return 2005, "已购买过该商品"
	case ErrGoodsNotExist:
		return 2001, "商品不存在"
	case ErrGoodsNotStart:
		return 2002, "商品未开始"
	case ErrGoodsEnded:
		return 2003, "商品已结束"
	default:
		return 5001, "系统错误"
	}
}

// hasStockInRedis 检查Redis是否有预热库存
func (s *SeckillService) hasStockInRedis(skuID uint) bool {
	ctx := context.Background()
	skuStr := strconv.Itoa(int(skuID))

	// 检查任一桶是否有库存
	for i := 1; i <= StockBucketCount; i++ {
		stockKey := repository.StockBucketKey(skuStr, i)
		exists, _ := s.redis.Exists(ctx, stockKey).Result()
		if exists > 0 {
			return true
		}
	}
	return false
}

// preheatFromMySQL 从MySQL加载并预热到Redis
func (s *SeckillService) preheatFromMySQL(skuID uint) error {
	ctx := context.Background()
	skuStr := strconv.Itoa(int(skuID))

	var goods model.SeckillGoods
	if err := s.db.First(&goods, skuID).Error; err != nil {
		return err
	}

	// 预热分桶库存
	_, err := repository.PreHeatStockToBuckets(ctx, skuStr, goods.Stock, StockBucketCount, 24*time.Hour)
	if err != nil {
		return fmt.Errorf("预热分桶库存失败: %w", err)
	}

	// 预热商品信息缓存
	goodsJSON, _ := json.Marshal(goods)
	repository.SetGoods(ctx, skuStr, string(goodsJSON), 24*time.Hour)

	utils.Info("自动预热库存成功",
		utils.Int("sku_id", int(skuID)),
		utils.Int("stock", goods.Stock),
	)
	return nil
}

// syncStockToMySQL 将Redis总库存同步到MySQL
func (s *SeckillService) syncStockToMySQL(skuID uint) error {
	ctx := context.Background()
	skuStr := strconv.Itoa(int(skuID))

	// 获取Redis总库存
	totalStock, err := repository.GetTotalStockFromBuckets(ctx, skuStr, StockBucketCount)
	if err != nil {
		return err
	}

	// 更新MySQL
	err = s.db.Model(&model.SeckillGoods{}).Where("id = ?", skuID).Update("stock", totalStock).Error
	if err != nil {
		return err
	}

	utils.Info("库存同步到MySQL",
		utils.Int("sku_id", int(skuID)),
		utils.Int("total_stock", totalStock),
	)
	return nil
}

// Seckill 秒杀处理（自动降级 + 数据同步）
// 流程：
//  1. 检查Redis是否有预热库存，没有则自动从MySQL加载
//  2. 尝试Redis分桶秒杀
//  3. Redis成功 后同步库存到MySQL
//  4. Redis失败（故障）时降级到MySQL直接扣减
func (s *SeckillService) Seckill(ctx context.Context, userID, skuID uint, traceID string) (string, error) {
	skuStr := strconv.Itoa(int(skuID))
	userStr := strconv.Itoa(int(userID))

	// ========== 步骤1：检查Redis是否有预热库存，没有则自动预热 ==========
	if !s.hasStockInRedis(skuID) {
		utils.Info("Redis无预热库存，自动从MySQL加载",
			utils.String("trace_id", traceID),
			utils.Int("sku_id", int(skuID)),
		)
		if err := s.preheatFromMySQL(skuID); err != nil {
			utils.Error("自动预热失败",
				utils.String("trace_id", traceID),
				utils.Int("sku_id", int(skuID)),
				utils.Err(err),
			)
			// 预热失败不影响继续流程，尝试直接用MySQL
		}
	}

	// ========== 步骤2：计算用户对应的库存桶 ==========
	bucketID := getBucketForUser(userID, skuID)

	// ========== 步骤3：原子标记用户 ==========
	purchaseKey := repository.UserSeckillKey(userStr, skuStr)
	success, err := repository.SetNX(ctx, purchaseKey, "1", 24*time.Hour)
	if err != nil {
		// Redis 出错了，降级到 MySQL 直接处理
		// MySQL 降级流程本身有购买检查，不会超卖
		utils.Warn("Redis标记用户失败，降级MySQL处理",
			utils.String("trace_id", traceID),
			utils.Int("user_id", int(userID)),
			utils.String("sku_id", skuStr),
			utils.Err(err),
		)
		return s.seckillWithMySQL(ctx, userID, skuID, traceID)
	}
	if !success {
		// key 已存在，说明买过了
		return "", ErrAlreadyBought
	}

	// ========== 步骤4：数据库重复购买检查 ==========
	var existingOrder model.Order
	if err := s.db.WithContext(ctx).Where("user_id = ? AND sku_id = ?", userID, skuID).First(&existingOrder).Error; err == nil {
		repository.Delete(ctx, purchaseKey)
		return "", ErrAlreadyBought
	}

	// ========== 步骤5：检查商品状态 ==========
	goods, err := s.getGoodsWithCheck(skuID)
	if err != nil {
		repository.Delete(ctx, purchaseKey)
		return "", err
	}

	// ========== 步骤6：尝试Redis分桶扣库存 ==========
	stockKey := repository.StockBucketKey(skuStr, bucketID)
	script := `
		local stock = redis.call('GET', KEYS[1])
		if not stock then return -2 end
		if tonumber(stock) <= 0 then return -1 end
		redis.call('DECR', KEYS[1])
		return 1
	`

	redisSuccess := false
	result, err := s.redis.Eval(ctx, script, []string{stockKey}).Int()

	if err != nil {
		// Redis执行出错，降级到MySQL
		utils.Warn("Redis扣库存失败，降级到MySQL",
			utils.String("trace_id", traceID),
			utils.String("sku_id", skuStr),
			utils.Int("bucket_id", bucketID),
			utils.Err(err),
		)
		repository.Delete(ctx, purchaseKey)
		return s.seckillWithMySQL(ctx, userID, skuID, traceID)
	}

	if result == -2 {
		// 库存未预热，尝试回填
		stockPerBucket := goods.Stock / StockBucketCount
		set, err := s.redis.SetNX(ctx, stockKey, stockPerBucket, 24*time.Hour).Result()
		if err != nil {
			repository.Delete(ctx, purchaseKey)
			return s.seckillWithMySQL(ctx, userID, skuID, traceID)
		}
		if !set {
			// 被其他请求回填了，重新执行Lua扣库存
			result, err = s.redis.Eval(ctx, script, []string{stockKey}).Int()
			if err != nil {
				repository.Delete(ctx, purchaseKey)
				return s.seckillWithMySQL(ctx, userID, skuID, traceID)
			}
			if result == -1 {
				repository.Delete(ctx, purchaseKey)
				return "", ErrSoldOut
			}
			redisSuccess = true
		} else {
			// 回填成功，检查是否还有库存
			if stockPerBucket <= 0 {
				repository.Delete(ctx, purchaseKey)
				return "", ErrSoldOut
			}
			redisSuccess = true
		}
	} else if result == -1 {
		// 该桶售罄
		repository.Delete(ctx, purchaseKey)
		return "", ErrSoldOut
	} else {
		redisSuccess = true
	}

	// ========== 步骤7：Redis扣减成功，创建订单 ==========
	if redisSuccess {
		order := &model.Order{
			OrderNo: s.generateOrderNo(),
			UserID:  userID,
			SkuID:   skuID,
			Status:  model.OrderStatusPending,
		}

		// 异步发送到 channel，如果 channel 满则同步处理
		select {
		case s.orderChan <- order:
			atomic.AddInt64(&s.asyncSuccess, 1)
			utils.Info("秒杀成功（订单异步创建）",
				utils.String("trace_id", traceID),
				utils.String("order_no", order.OrderNo),
				utils.Int("bucket_id", bucketID),
			)
		default:
			atomic.AddInt64(&s.syncFallback, 1)
			if err := s.db.Create(order).Error; err != nil {
				s.rollbackBucketWithRetry(ctx, stockKey, purchaseKey, traceID, skuStr, int(userID), bucketID)
				utils.Error("订单创建失败",
					utils.String("trace_id", traceID),
					utils.String("sku_id", skuStr),
					utils.Int("bucket_id", bucketID),
					utils.Int("user_id", int(userID)),
					utils.Err(err),
				)
				return "", ErrSystemError
			}
			utils.Info("秒杀成功",
				utils.String("trace_id", traceID),
				utils.String("order_no", order.OrderNo),
				utils.Int("bucket_id", bucketID),
			)
		}

		// ========== 步骤8：同步库存到MySQL（异步，不阻塞返回） ==========
	go func() {
		if err := s.syncStockToMySQL(skuID); err != nil {
			utils.Error("库存同步MySQL失败",
				utils.String("trace_id", traceID),
				utils.Int("sku_id", int(skuID)),
				utils.Err(err),
			)
		}
	}()

		return order.OrderNo, nil
	}

	return "", ErrSystemError
}

// rollbackWithRetry 带重试的回滚
// 库存+1 和 删除购买标记 使用 Lua 脚本保证原子性
// 如果回滚失败，最多重试 maxRollbackRetries 次（指数退避）
func (s *SeckillService) rollbackWithRetry(ctx context.Context, stockKey, purchaseKey, traceID string, skuStr string, userID int) {
	script := `
		redis.call('INCR', KEYS[1])
		redis.call('DEL', KEYS[2])
		return 1
	`

	for i := 0; i < maxRollbackRetries; i++ {
		_, err := s.redis.Eval(ctx, script, []string{stockKey, purchaseKey}).Result()
		if err == nil {
			utils.Info("回滚成功",
				utils.String("trace_id", traceID),
				utils.Int("retry", i+1),
			)
			return
		}

		utils.Warn("回滚失败，重试",
			utils.String("trace_id", traceID),
			utils.Int("retry", i+1),
			utils.Int("user_id", userID),
			utils.String("sku_id", skuStr),
			utils.Err(err),
		)

		// 指数退避：100ms, 200ms, 300ms
		time.Sleep(time.Duration(i+1) * rollbackRetryDelay * time.Millisecond)
	}

	// 所有重试都失败，记录严重错误（需要人工介入）
	utils.Error("回滚完全失败，需要人工处理",
		utils.String("trace_id", traceID),
		utils.String("stock_key", stockKey),
		utils.String("purchase_key", purchaseKey),
		utils.Int("user_id", userID),
		utils.String("sku_id", skuStr),
	)
}

// seckillWithMySQL MySQL直接扣减（降级方案）
// 当Redis不可用时，使用MySQL直接扣减
func (s *SeckillService) seckillWithMySQL(ctx context.Context, userID, skuID uint, traceID string) (string, error) {
	// 1. 检查是否已购买（MySQL查询）
	var existingOrder model.Order
	err := s.db.WithContext(ctx).Where("user_id = ? AND sku_id = ?", userID, skuID).First(&existingOrder).Error
	if err == nil {
		return "", ErrAlreadyBought
	}
	if err != gorm.ErrRecordNotFound {
		return "", ErrSystemError
	}

	// 2. 使用事务扣减库存
	var orderNo string
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 查询商品
		var goods model.SeckillGoods
		if err := tx.First(&goods, skuID).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return ErrGoodsNotExist
			}
			return err
		}

		// 检查时间状态
		if err := checkGoodsTimeStatus(&goods); err != nil {
			return err
		}

		// 检查库存
		if goods.Stock <= 0 {
			return ErrSoldOut
		}

		// 扣减库存（乐观锁方式：通过 WHERE stock > 0 保证原子性）
		result := tx.Model(&model.SeckillGoods{}).Where("id = ? AND stock > 0", skuID).
			Update("stock", gorm.Expr("stock - 1"))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrSoldOut
		}

		// 生成订单号
		orderNo = s.generateOrderNo()

		// 创建订单
		order := &model.Order{
			OrderNo: orderNo,
			UserID:  userID,
			SkuID:   skuID,
			Status:  model.OrderStatusPending,
		}
		if err := tx.Create(order).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		utils.Error("MySQL降级秒杀失败",
			utils.String("trace_id", traceID),
			utils.Int("user_id", int(userID)),
			utils.Int("sku_id", int(skuID)),
			utils.Err(err),
		)
		return "", err
	}

	utils.Info("MySQL降级秒杀成功",
		utils.String("trace_id", traceID),
		utils.String("order_no", orderNo),
	)
	return orderNo, nil
}

// rollbackBucketWithRetry 带重试的分桶库存回滚
// 库存+1 和 删除购买标记 使用 Lua 脚本保证原子性
// 如果回滚失败，最多重试 maxRollbackRetries 次（指数退避）
func (s *SeckillService) rollbackBucketWithRetry(ctx context.Context, stockKey, purchaseKey, traceID string, skuStr string, userID int, bucketID int) {
	script := `
		redis.call('INCR', KEYS[1])
		redis.call('DEL', KEYS[2])
		return 1
	`

	for i := 0; i < maxRollbackRetries; i++ {
		_, err := s.redis.Eval(ctx, script, []string{stockKey, purchaseKey}).Result()
		if err == nil {
			utils.Info("分桶回滚成功",
				utils.String("trace_id", traceID),
				utils.Int("retry", i+1),
				utils.Int("bucket_id", bucketID),
			)
			return
		}

		utils.Warn("分桶回滚失败，重试",
			utils.String("trace_id", traceID),
			utils.Int("retry", i+1),
			utils.Int("user_id", userID),
			utils.String("sku_id", skuStr),
			utils.Int("bucket_id", bucketID),
			utils.Err(err),
		)

		// 指数退避：100ms, 200ms, 300ms
		time.Sleep(time.Duration(i+1) * rollbackRetryDelay * time.Millisecond)
	}

	// 所有重试都失败，记录严重错误（需要人工介入）
	utils.Error("分桶回滚完全失败，需要人工处理",
		utils.String("trace_id", traceID),
		utils.String("stock_key", stockKey),
		utils.String("purchase_key", purchaseKey),
		utils.Int("user_id", userID),
		utils.String("sku_id", skuStr),
		utils.Int("bucket_id", bucketID),
	)
}

// getGoodsWithCheck 检查商品状态（带 Redis 缓存）
func (s *SeckillService) getGoodsWithCheck(skuID uint) (*model.SeckillGoods, error) {
	ctx := context.Background()
	skuStr := strconv.Itoa(int(skuID))

	// 1. 先查 Redis 缓存
	goodsJSON, err := repository.GetGoods(ctx, skuStr)
	if err == nil && goodsJSON != "" {
		// 缓存命中，反序列化
		var goods model.SeckillGoods
		if jsonErr := json.Unmarshal([]byte(goodsJSON), &goods); jsonErr == nil {
			// 检查时间状态
			if err := checkGoodsTimeStatus(&goods); err != nil {
				return nil, err
			}
			return &goods, nil
		}
	}

	// 2. 缓存未命中，查 MySQL
	var goods model.SeckillGoods
	if err := s.db.First(&goods, skuID).Error; err != nil {
		return nil, ErrGoodsNotExist
	}

	// 3. 写入 Redis 缓存（24小时过期）
	if goodsJSONBytes, jsonErr := json.Marshal(&goods); jsonErr == nil {
		repository.SetGoods(ctx, skuStr, string(goodsJSONBytes), 24*time.Hour)
	}

	// 4. 检查时间状态
	if err := checkGoodsTimeStatus(&goods); err != nil {
		return nil, err
	}

	return &goods, nil
}

// checkGoodsTimeStatus 检查商品时间状态
func checkGoodsTimeStatus(goods *model.SeckillGoods) error {
	now := time.Now()
	if now.Before(goods.StartTime) {
		return ErrGoodsNotStart
	}
	if now.After(goods.EndTime) {
		return ErrGoodsEnded
	}
	return nil
}

// generateOrderNo 生成订单号（使用 UUID 保证唯一性）
func (s *SeckillService) generateOrderNo() string {
	return fmt.Sprintf("SK%s%s", time.Now().Format("20060102150405"), uuid.New().String()[:8])
}

// GetOrderByNo 根据订单号查询订单
func (s *SeckillService) GetOrderByNo(orderNo string) (*model.Order, error) {
	var order model.Order
	if err := s.db.Where("order_no = ?", orderNo).First(&order).Error; err != nil {
		return nil, err
	}
	return &order, nil
}

// GetOrdersByUserID 获取用户订单列表
func (s *SeckillService) GetOrdersByUserID(userID uint, page, pageSize int) ([]model.Order, int64, error) {
	var orders []model.Order
	var total int64

	offset := (page - 1) * pageSize

	s.db.Model(&model.Order{}).Where("user_id = ?", userID).Count(&total)
	if err := s.db.Where("user_id = ?", userID).Offset(offset).Limit(pageSize).Order("created_at desc").Find(&orders).Error; err != nil {
		return nil, 0, err
	}

	return orders, total, nil
}

// GetAsyncStats 获取异步处理统计
func (s *SeckillService) GetAsyncStats() (asyncSuccess, syncFallback, deadLetter int64) {
	return atomic.LoadInt64(&s.asyncSuccess), atomic.LoadInt64(&s.syncFallback), atomic.LoadInt64(&s.deadLetterCount)
}
