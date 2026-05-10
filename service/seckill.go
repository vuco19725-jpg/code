package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"strconv"
	"sync"
	"time"

	"seckill/model"
	"seckill/repository"
	"seckill/utils"

	"github.com/IBM/sarama"
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

// 压测开关：true 时跳过防重复购买检查，允许同一用户多次秒杀（仅用于压测）
const SkipPurchaseCheck = true

// PendingWorkerCount 待处理订单 Worker 数量
// 每个 worker 向 Kafka 发送订单，多 worker 并行提高吞吐
const PendingWorkerCount = 8

// 回滚重试配置
const (
	maxRollbackRetries = 3          // 最大重试次数
	rollbackRetryDelay = 100        // 基础重试延迟（毫秒）
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

// pendingOrder 待处理订单（排队后可靠发送到 Kafka）
type pendingOrder struct {
	order      *model.Order
	queueToken string
}

// SeckillService 秒杀服务
type SeckillService struct {
	db             *gorm.DB
	redis          *redis.Client
	producer       sarama.AsyncProducer
	topic          string
	pendingChan    chan *pendingOrder
	localGoodsCache sync.Map    // key: skuStr, value: *localGoodsEntry, 3s TTL
	soldOutCache   sync.Map    // key: "skuStr:bucketID", value: true
	degradeLimiter *utils.RateLimiter // 降级限流器：Redis 故障时保护 MySQL
}

// localGoodsEntry 本地缓存条目
type localGoodsEntry struct {
	goods   *model.SeckillGoods
	expires time.Time
}

const goodsCacheTTL = 3 * time.Second

// NewSeckillService 创建秒杀服务
func NewSeckillService(db *gorm.DB, redis *redis.Client, producer sarama.AsyncProducer, topic string) *SeckillService {
	return &SeckillService{
		db:             db,
		redis:          redis,
		producer:       producer,
		topic:          topic,
		pendingChan:    make(chan *pendingOrder, 100000),
		degradeLimiter: utils.NewRateLimiter(200),
	}
}

// StartPendingWorker 启动后台待处理订单 Worker
// 从 pendingChan 读取订单，可靠发送到 Kafka（带重试）
func (s *SeckillService) StartPendingWorker(ctx context.Context) {
	for i := 0; i < PendingWorkerCount; i++ {
		go func(id int) {
			for {
				select {
				case <-ctx.Done():
					// 退出前排空 pendingChan
					s.drainPending()
					return
				case po := <-s.pendingChan:
					s.sendOrderToKafka(po)
				}
			}
		}(i)
	}
	utils.Info("pending workers started", utils.Int("count", PendingWorkerCount))
}

// StartFallbackWorker 启动兜底队列消费者
// 从 Redis fallback list 读取订单，重新发送到 Kafka
func (s *SeckillService) StartFallbackWorker(ctx context.Context) {
	go func() {
		fallbackKey := repository.FallbackQueueKey()
		for {
			// BRPOP 阻塞读取，超时 3s 后重试（避免空轮询）
			result, err := s.redis.BRPop(ctx, 3*time.Second, fallbackKey).Result()
			if err != nil || len(result) < 2 {
				if ctx.Err() != nil {
					return
				}
				continue
			}

			var order model.Order
			if err := json.Unmarshal([]byte(result[1]), &order); err != nil {
				utils.Warn("fallback: unmarshal order failed",
					utils.Err(err),
				)
				continue
			}

			// 重新包装为 pendingOrder 走 Kafka 发送
			po := &pendingOrder{
				order:      &order,
				queueToken: order.QueueToken,
			}
			s.sendOrderToKafka(po)
		}
	}()
	utils.Info("fallback worker started")
}

// drainPending 排空待处理队列（优雅关闭时调用）
func (s *SeckillService) drainPending() {
	for {
		select {
		case po := <-s.pendingChan:
			s.sendOrderToKafka(po)
		default:
			return
		}
	}
}

// sendOrderToKafka 可靠发送订单到 Kafka
// 排队状态已在 HTTP 路径的 Lua 脚本中写入 Redis
func (s *SeckillService) sendOrderToKafka(po *pendingOrder) {
	orderJSON, err := json.Marshal(po.order)
	if err != nil {
		utils.Error("marshal pending order failed",
			utils.String("queue_token", po.queueToken),
			utils.Err(err),
		)
		return
	}

	msg := &sarama.ProducerMessage{
		Topic: s.topic,
		Key:   sarama.StringEncoder(fmt.Sprintf("%d:%d", po.order.UserID, po.order.SkuID)),
		Value: sarama.ByteEncoder(orderJSON),
	}

	// 阻塞发送到 Kafka（AsyncProducer 有内部缓冲，阻塞即反压）
	// 此处不设超时重试：宁可慢不能丢，由多 worker 保吞吐
	s.producer.Input() <- msg
	return
}

// PollOrderStatus 轮询排队状态
// 返回 (orderNo, status, err)，status 为 "queuing" / "success" / "not_found"
func (s *SeckillService) PollOrderStatus(queueToken string) (string, string, error) {
	ctx := context.Background()
	val, err := s.redis.Get(ctx, repository.QueueStatusKey(queueToken)).Result()
	if err == redis.Nil {
		return "", "not_found", nil
	}
	if err != nil {
		return "", "error", err
	}
	if val == "queuing" {
		return "", "queuing", nil
	}
	return val, "success", nil
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


// StartPeriodicStockSync 启动后台周期性库存同步
// interval: 每次同步的间隔时间
// 在秒杀活动期间，定时将 Redis 库存同步到 MySQL，替代每次请求都同步
func (s *SeckillService) StartPeriodicStockSync(skuIDs []uint, interval time.Duration, stop <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// 立即同步一次
		for _, skuID := range skuIDs {
			if err := s.syncStockToMySQL(skuID); err != nil {
				utils.Error("初始库存同步失败", utils.Int("sku_id", int(skuID)), utils.Err(err))
			}
		}

		for {
			select {
			case <-ticker.C:
				for _, skuID := range skuIDs {
					if err := s.syncStockToMySQL(skuID); err != nil {
						utils.Error("周期性库存同步失败", utils.Int("sku_id", int(skuID)), utils.Err(err))
					}
				}
			case <-stop:
				// 停止前再同步一次，保证数据尽可能一致
				for _, skuID := range skuIDs {
					if err := s.syncStockToMySQL(skuID); err != nil {
						utils.Error("最终库存同步失败", utils.Int("sku_id", int(skuID)), utils.Err(err))
					}
				}
				utils.Info("周期性库存同步已停止")
				return
			}
		}
	}()
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
//  3. Redis成功 后发送到Kafka异步落单
//  4. Redis失败（故障）时降级到MySQL直接扣减
func (s *SeckillService) Seckill(ctx context.Context, userID, skuID uint, traceID string) (string, error) {
	skuStr := strconv.Itoa(int(skuID))
	userStr := strconv.Itoa(int(userID))

	// ========== 步骤2：计算用户对应的库存桶 ==========
	bucketID := getBucketForUser(userID, skuID)

	// ========== 步骤3：检查商品状态 ==========
	goods, err := s.getGoodsWithCheck(skuID)
	if err != nil {
		return "", err
	}

	// ========== 步骤4：检查本地售罄标记 ==========
	soldOutKey := skuStr + ":" + strconv.Itoa(bucketID)
	if _, ok := s.soldOutCache.Load(soldOutKey); ok {
		return "", ErrSoldOut
	}

	// ========== 步骤5：Lua 原子操作（防重复购买 + 扣库存 + 排队状态） ==========
	purchaseKey := repository.UserSeckillKey(userStr, skuStr)
	stockKey := repository.StockBucketKey(skuStr, bucketID)
	queueToken := traceID
	statusKey := repository.QueueStatusKey(queueToken)

	checkFlag := "0"
	if !SkipPurchaseCheck {
		checkFlag = "1"
	}

	script := `
		if ARGV[1] == "1" then
			if not redis.call('SET', KEYS[1], '1', 'NX', 'EX', 86400) then
				return -3
			end
		end
		local stock = redis.call('GET', KEYS[2])
		if not stock then
			if ARGV[1] == "1" then redis.call('DEL', KEYS[1]) end
			return -2
		end
		if tonumber(stock) <= 0 then
			if ARGV[1] == "1" then redis.call('DEL', KEYS[1]) end
			return -1
		end
		redis.call('DECR', KEYS[2])
		redis.call('SETEX', KEYS[3], 3600, 'queuing')
		return 1
	`

	result, err := s.redis.Eval(ctx, script, []string{purchaseKey, stockKey, statusKey}, checkFlag).Int()
	if err != nil {
		// Redis 执行出错，降级到 MySQL
		utils.Warn("Redis执行失败，降级到MySQL",
			utils.String("trace_id", traceID),
			utils.Int("user_id", int(userID)),
			utils.String("sku_id", skuStr),
			utils.Err(err),
		)
		return s.degradeToMySQL(ctx, userID, skuID, traceID)
	}

	switch result {
	case -3:
		return "", ErrAlreadyBought
	case -2:
		// 库存未预热，尝试回填
		stockPerBucket := goods.Stock / StockBucketCount
		if stockPerBucket <= 0 {
			s.soldOutCache.Store(soldOutKey, true)
			return "", ErrSoldOut
		}
		set, err := s.redis.SetNX(ctx, stockKey, stockPerBucket, 24*time.Hour).Result()
		if err != nil {
			utils.Warn("Redis回填库存失败，降级到MySQL",
				utils.String("trace_id", traceID),
				utils.Err(err),
			)
			return s.degradeToMySQL(ctx, userID, skuID, traceID)
		}
		if !set {
			utils.Info("库存桶已被其他请求回填", utils.String("trace_id", traceID))
		}
		// 重新执行 Lua（SetNX 成功时自己回填了桶，失败时别人回填了桶）
		result, err = s.redis.Eval(ctx, script, []string{purchaseKey, stockKey, statusKey}, checkFlag).Int()
		if err != nil {
			return s.degradeToMySQL(ctx, userID, skuID, traceID)
		}
		if result == -1 {
			s.soldOutCache.Store(soldOutKey, true)
			return "", ErrSoldOut
		}
		// result 为其他值（1 或 -3）：Lua 脚本会正确处理，继续下单
	case -1:
		s.soldOutCache.Store(soldOutKey, true)
		return "", ErrSoldOut
	}

	// ========== 步骤6：排队到后台处理 ==========
	orderNo := s.generateOrderNo(traceID)

	order := &model.Order{
		OrderNo:    orderNo,
		UserID:     userID,
		SkuID:      skuID,
		Status:     model.OrderStatusPending,
		QueueToken: queueToken,
	}

	po := &pendingOrder{
		order:      order,
		queueToken: queueToken,
	}
	select {
	case s.pendingChan <- po:
	default:
		orderJSON, _ := json.Marshal(order)
		fallbackKey := repository.FallbackQueueKey()
		s.redis.LPush(ctx, fallbackKey, string(orderJSON))
		utils.Warn("pending channel full, order written to redis fallback",
			utils.String("trace_id", traceID),
			utils.String("order_no", orderNo),
		)
	}

	utils.Info("秒杀排队成功",
		utils.String("trace_id", traceID),
		utils.String("queue_token", queueToken),
		utils.Int("bucket_id", bucketID),
	)

	return queueToken, nil
}

// rollbackBucketWithRetry 带重试的分桶库存回滚
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

// degradeToMySQL 降级到 MySQL（带限流保护）
// Redis 故障时，控制降级流量，防止 MySQL 被打垮
func (s *SeckillService) degradeToMySQL(ctx context.Context, userID, skuID uint, traceID string) (string, error) {
	if !s.degradeLimiter.Allow() {
		utils.Warn("降级限流触发，请求被拒绝",
			utils.String("trace_id", traceID),
			utils.Int("user_id", int(userID)),
			utils.Int("sku_id", int(skuID)),
		)
		return "", ErrSystemError
	}
	return s.degradeToMySQL(ctx, userID, skuID, traceID)
}

// seckillWithMySQL MySQL直接扣减（降级方案）
func (s *SeckillService) seckillWithMySQL(ctx context.Context, userID, skuID uint, traceID string) (string, error) {
	var existingOrder model.Order
	err := s.db.WithContext(ctx).Where("user_id = ? AND sku_id = ?", userID, skuID).First(&existingOrder).Error
	if err == nil {
		return "", ErrAlreadyBought
	}
	if err != gorm.ErrRecordNotFound {
		return "", ErrSystemError
	}

	var orderNo string
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var goods model.SeckillGoods
		if err := tx.First(&goods, skuID).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return ErrGoodsNotExist
			}
			return err
		}

		if err := checkGoodsTimeStatus(&goods); err != nil {
			return err
		}

		if goods.Stock <= 0 {
			return ErrSoldOut
		}

		result := tx.Model(&model.SeckillGoods{}).Where("id = ? AND stock > 0", skuID).
			Update("stock", gorm.Expr("stock - 1"))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrSoldOut
		}

		orderNo = s.generateOrderNo(traceID)

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

// getGoodsWithCheck 检查商品状态（本地缓存 -> Redis -> MySQL）
func (s *SeckillService) getGoodsWithCheck(skuID uint) (*model.SeckillGoods, error) {
	ctx := context.Background()
	skuStr := strconv.Itoa(int(skuID))

	// 0. 先查本地缓存（0 网络开销）
	if entry, ok := s.localGoodsCache.Load(skuStr); ok {
		e := entry.(*localGoodsEntry)
		if time.Now().Before(e.expires) {
			if err := checkGoodsTimeStatus(e.goods); err != nil {
				return nil, err
			}
			return e.goods, nil
		}
	}

	// 1. 查 Redis 缓存
	goodsJSON, err := repository.GetGoods(ctx, skuStr)
	if err == nil && goodsJSON != "" {
		var goods model.SeckillGoods
		if jsonErr := json.Unmarshal([]byte(goodsJSON), &goods); jsonErr == nil {
			if err := checkGoodsTimeStatus(&goods); err != nil {
				return nil, err
			}
			s.localGoodsCache.Store(skuStr, &localGoodsEntry{goods: &goods, expires: time.Now().Add(goodsCacheTTL)})
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

	// 写入本地缓存
	s.localGoodsCache.Store(skuStr, &localGoodsEntry{goods: &goods, expires: time.Now().Add(goodsCacheTTL)})

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

// generateOrderNo 生成订单号
func (s *SeckillService) generateOrderNo(traceID string) string {
	return fmt.Sprintf("SK%s%s", time.Now().Format("20060102150405"), traceID)
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
