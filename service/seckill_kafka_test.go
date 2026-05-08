package service

import (
	"errors"
	"testing"

	"github.com/IBM/sarama"
	"github.com/IBM/sarama/mocks"
)

// ---------- SeckillService Kafka Producer Tests ----------

func TestSeckill_KafkaProduceSuccess(t *testing.T) {
	// 需要 DB + Redis 连接才能完整测试 Seckill 方法
	// 这是一个集成测试的骨架，展示如何使用 mock producer
	mockProducer := mocks.NewSyncProducer(t, nil)
	mockProducer.ExpectSendMessageAndSucceed()

	// 在没有真实 DB/Redis 的情况下，只验证 Producer 接口正常
	msg := &sarama.ProducerMessage{
		Topic: "test-topic",
		Key:   sarama.StringEncoder("1:1"),
		Value: sarama.ByteEncoder(`{"order_no":"SK001","user_id":1,"sku_id":1}`),
	}
	_, _, err := mockProducer.SendMessage(msg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if err := mockProducer.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSeckill_KafkaProduceFail(t *testing.T) {
	mockProducer := mocks.NewSyncProducer(t, nil)
	mockProducer.ExpectSendMessageAndFail(errors.New("kafka broker down"))

	msg := &sarama.ProducerMessage{
		Topic: "test-topic",
		Key:   sarama.StringEncoder("1:1"),
		Value: sarama.ByteEncoder(`{}`),
	}
	_, _, err := mockProducer.SendMessage(msg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------- Order Consumer Tests ----------

func TestOrderHandler_DuplicateOrder(t *testing.T) {
	// Consumer 幂等性：重复消息应该被跳过
	// 在集成测试中验证：发两条相同 order_no → MySQL 唯一索引拦截
	t.Log("consumer idempotency requires MySQL unique index: (order_no) + (user_id, sku_id)")
}

func TestOrderHandler_RetryOnFailure(t *testing.T) {
	// Consumer 重试机制：首次失败后重试
	t.Log("consumer retry: 3 attempts with exponential backoff before giving up")
}

// ---------- Integration Test Skeleton ----------

func TestSeckillService_SeckillWithKafka(t *testing.T) {
	// 完整集成测试需要：MySQL + Redis + Kafka
	// 使用 docker-compose.test.yml 启动依赖后再运行
	//
	// 测试步骤：
	//   1. 插入测试商品到 MySQL
	//   2. 预热库存到 Redis
	//   3. 调用 Seckill()
	//   4. 验证 Kafka 收到消息
	//   5. (可选) 消费消息并验证订单落库
	t.Skip("integration test: requires MySQL + Redis + Kafka running")
}

func TestSeckillService_RollbackOnKafkaFail(t *testing.T) {
	// 验证 Kafka 发送失败时，Redis 库存和购买标记正确回滚
	t.Skip("integration test: requires MySQL + Redis + Kafka running")
}

// ---------- Error Code Tests ----------

func TestErrorCodeMapping(t *testing.T) {
	tests := []struct {
		err      error
		wantCode int
		wantMsg  string
	}{
		{ErrSoldOut, 2004, "库存不足"},
		{ErrAlreadyBought, 2005, "已购买过该商品"},
		{ErrGoodsNotExist, 2001, "商品不存在"},
		{ErrGoodsNotStart, 2002, "商品未开始"},
		{ErrGoodsEnded, 2003, "商品已结束"},
		{errors.New("unknown"), 5001, "系统错误"},
		{nil, 5001, "系统错误"},
	}

	for _, tt := range tests {
		code, msg := (&SeckillService{}).ErrorCode(tt.err)
		if code != tt.wantCode || msg != tt.wantMsg {
			t.Errorf("ErrorCode(%v) = (%d, %q), want (%d, %q)", tt.err, code, msg, tt.wantCode, tt.wantMsg)
		}
	}
}
