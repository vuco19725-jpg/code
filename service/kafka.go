package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"seckill/config"
	"seckill/model"
	"seckill/repository"
	"seckill/utils"

	"github.com/IBM/sarama"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// ---------- Producer ----------

// NewKafkaProducer 创建 Kafka SyncProducer
func NewKafkaProducer(cfg *config.KafkaConfig) (sarama.SyncProducer, error) {
	sc := sarama.NewConfig()
	sc.Producer.RequiredAcks = sarama.WaitForAll
	sc.Producer.Retry.Max = 3
	sc.Producer.Return.Successes = false
	sc.Producer.Compression = sarama.CompressionSnappy
	sc.Producer.Timeout = 10 * time.Second
	sc.Net.DialTimeout = 5 * time.Second
	sc.Net.ReadTimeout = 10 * time.Second
	sc.Net.WriteTimeout = 10 * time.Second

	producer, err := sarama.NewSyncProducer(cfg.Brokers, sc)
	if err != nil {
		return nil, fmt.Errorf("kafka sync producer init failed: %w", err)
	}
	return producer, nil
}

// NewAsyncKafkaProducer 创建 Kafka AsyncProducer
func NewAsyncKafkaProducer(cfg *config.KafkaConfig) (sarama.AsyncProducer, error) {
	sc := sarama.NewConfig()
	// 不等待全部 ACK，只等本地写入
	sc.Producer.RequiredAcks = sarama.WaitForLocal
	sc.Producer.Retry.Max = 3
	// 开启成功/失败回调
	sc.Producer.Return.Successes = false
	sc.Producer.Return.Errors = true
	// 压缩
	sc.Producer.Compression = sarama.CompressionSnappy
	// 缓冲配置（批量发送）
	sc.Producer.Flush.Bytes = 32 * 1024   // 32KB
	sc.Producer.Flush.Messages = 100       // 100条
	sc.Producer.Flush.Frequency = 3 * time.Millisecond

	producer, err := sarama.NewAsyncProducer(cfg.Brokers, sc)
	if err != nil {
		return nil, fmt.Errorf("kafka async producer init failed: %w", err)
	}
	return producer, nil
}

// StartProducerCallbacks 启动回调处理（监控告警）
func StartProducerCallbacks(producer sarama.AsyncProducer) {
	// 错误回调（必须，用于日志和告警）
	go func() {
		for err := range producer.Errors() {
			utils.Error("kafka async send failed",
				utils.String("topic", err.Msg.Topic),
				utils.Err(err.Err),
			)
		}
	}()
}

// ---------- Consumer ----------

// NewKafkaConsumerGroup 创建 Kafka ConsumerGroup
func NewKafkaConsumerGroup(cfg *config.KafkaConfig) (sarama.ConsumerGroup, error) {
	sc := sarama.NewConfig()
	sc.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{
		sarama.NewBalanceStrategyRoundRobin(),
	}
	sc.Consumer.Offsets.Initial = sarama.OffsetOldest
	sc.Consumer.Return.Errors = true
	sc.Consumer.Group.Session.Timeout = 30 * time.Second
	sc.Consumer.Group.Heartbeat.Interval = 10 * time.Second
	sc.Net.DialTimeout = 10 * time.Second
	sc.Net.ReadTimeout = 30 * time.Second
	sc.Net.WriteTimeout = 30 * time.Second

	cg, err := sarama.NewConsumerGroup(cfg.Brokers, cfg.ConsumerGroup, sc)
	if err != nil {
		return nil, fmt.Errorf("kafka consumer group init failed: %w", err)
	}
	return cg, nil
}

// ---------- OrderConsumer ----------

// OrderConsumer 订单异步落库消费者
type OrderConsumer struct {
	db       *gorm.DB
	redis    *redis.Client
	cg       sarama.ConsumerGroup
	topic    string
	cancel   context.CancelFunc
	deadLetterCount int64
}

// NewOrderConsumer 创建订单消费者
func NewOrderConsumer(db *gorm.DB, redis *redis.Client, cg sarama.ConsumerGroup, topic string) *OrderConsumer {
	return &OrderConsumer{
		db:    db,
		redis: redis,
		cg:    cg,
		topic: topic,
	}
}

// Start 启动消费，阻塞直到 ctx 取消
func (c *OrderConsumer) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	handler := newOrderHandler(c.db, c.redis, &c.deadLetterCount)

	go func() {
		for {
			if err := c.cg.Consume(ctx, []string{c.topic}, handler); err != nil {
				if errors.Is(err, sarama.ErrClosedConsumerGroup) {
					return
				}
				utils.Error("kafka consume error", utils.Err(err))
			}
			if ctx.Err() != nil {
				utils.Info("kafka consumer stopped")
				return
			}
		}
	}()
}

// Stop 停止消费者
func (c *OrderConsumer) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
	if err := c.cg.Close(); err != nil {
		utils.Error("kafka consumer close error", utils.Err(err))
	}
}

// DeadLetterCount 死信计数
func (c *OrderConsumer) DeadLetterCount() int64 {
	return c.deadLetterCount
}

// ---------- ConsumerGroupHandler ----------

type orderHandler struct {
	db      *gorm.DB
	redis   *redis.Client
	dlCount *int64
	// 并发处理配置
	workerCount int           // 并发 worker 数
	batchSize   int           // 批量插入大小
}

func newOrderHandler(db *gorm.DB, redis *redis.Client, dlCount *int64) *orderHandler {
	return &orderHandler{
		db:         db,
		redis:      redis,
		dlCount:    dlCount,
		workerCount: 6,   // 6个并发 worker
		batchSize:  50,  // 每批50条
	}
}

func (h *orderHandler) Setup(sarama.ConsumerGroupSession) error  { return nil }
func (h *orderHandler) Cleanup(sarama.ConsumerGroupSession) error { return nil }

func (h *orderHandler) ConsumeClaim(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	// 使用 channel 收集消息
	msgCh := make(chan *sarama.ConsumerMessage, 1000)

	// 启动多个 worker 并发处理
	var wg sync.WaitGroup
	for i := 0; i < h.workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for msg := range msgCh {
				h.processMessage(msg)
				sess.MarkMessage(msg, "")
			}
		}()
	}

	// 生产者：将消息放入 channel
	for msg := range claim.Messages() {
		msgCh <- msg
	}

	// 关闭 channel，等待所有 worker 完成
	close(msgCh)
	wg.Wait()

	// 最后标记已消费
	return nil
}

func (h *orderHandler) processMessage(msg *sarama.ConsumerMessage) {
	var order model.Order
	if err := json.Unmarshal(msg.Value, &order); err != nil {
		utils.Warn("kafka: unmarshal order failed, skip",
			utils.String("data", string(msg.Value)),
			utils.Err(err),
		)
		return
	}

	// 幂等插入（忽略重复）
	var isNew bool
	if err := h.db.Create(&order).Error; err != nil {
		if isDuplicateKeyError(err) {
			return
		}
		// 插入失败，重试
		if retryErr := h.retryCreate(order); retryErr != nil {
			utils.Error("kafka: order create failed after retries",
				utils.String("order_no", order.OrderNo),
				utils.Err(retryErr),
			)
			return
		}
	} else {
		isNew = true
	}

	// 更新排队状态（新创建的订单才更新）
	if order.QueueToken != "" && isNew {
		ctx := context.Background()
		statusKey := repository.QueueStatusKey(order.QueueToken)
		if err := h.redis.Set(ctx, statusKey, order.OrderNo, 1*time.Hour).Err(); err != nil {
			utils.Warn("kafka: update queue status failed",
				utils.String("queue_token", order.QueueToken),
				utils.Err(err),
			)
		}
	}

	utils.Info("kafka: order consumed",
		utils.String("order_no", order.OrderNo),
		utils.Uint("user_id", order.UserID),
	)
}

func (h *orderHandler) retryCreate(order model.Order) error {
	baseDelay := 100 * time.Millisecond
	for i := 0; i < 3; i++ {
		if i > 0 {
			time.Sleep(time.Duration(i+1) * baseDelay)
		}
		err := h.db.Create(&order).Error
		if err == nil {
			return nil
		}
		if isDuplicateKeyError(err) {
			return nil
		}
	}

	return fmt.Errorf("order %s still failed after 3 retries", order.OrderNo)
}

// isDuplicateKeyError 判断是否为 MySQL 唯一索引冲突
func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// MySQL 1062: Duplicate entry
	return contains(errStr, "Duplicate entry") || contains(errStr, "1062")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
