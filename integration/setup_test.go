package integration

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
)

var (
	testDB    *sql.DB
	testRedis *redis.Client
)

// TestMain 集成测试入口
func TestMain(m *testing.M) {
	// 初始化测试数据库
	if err := setupTestDB(); err != nil {
		log.Fatalf("Failed to setup test DB: %v", err)
	}
	defer testDB.Close()

	// 初始化测试 Redis
	if err := setupTestRedis(); err != nil {
		log.Fatalf("Failed to setup test Redis: %v", err)
	}
	defer testRedis.Close()

	// 运行测试
	os.Exit(m.Run())
}

func setupTestDB() error {
	// TODO: 根据环境变量获取数据库配置
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		getEnv("TEST_DB_USER", "root"),
		getEnv("TEST_DB_PASS", "password"),
		getEnv("TEST_DB_HOST", "localhost"),
		getEnv("TEST_DB_PORT", "3306"),
		getEnv("TEST_DB_NAME", "seckill_test"),
	)

	var err error
	testDB, err = sql.Open("mysql", dsn)
	if err != nil {
		return err
	}

	// 设置连接池
	testDB.SetMaxOpenConns(10)
	testDB.SetMaxIdleConns(5)
	testDB.SetConnMaxLifetime(time.Hour)

	// 等待数据库就绪
	for i := 0; i < 30; i++ {
		if err := testDB.Ping(); err == nil {
			return nil
		}
		time.Sleep(time.Second)
	}

	return fmt.Errorf("db not ready after 30s")
}

func setupTestRedis() error {
	testRedis = redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("%s:%s",
			getEnv("TEST_REDIS_HOST", "localhost"),
			getEnv("TEST_REDIS_PORT", "6379"),
		),
		Password: getEnv("TEST_REDIS_PASS", ""),
		DB:      1, // 使用独立的 DB
	})

	// 等待 Redis 就绪
	ctx := testRedis.Context()
	for i := 0; i < 30; i++ {
		if _, err := testRedis.Ping(ctx).Result(); err == nil {
			return nil
		}
		time.Sleep(time.Second)
	}

	return fmt.Errorf("redis not ready after 30s")
}

// cleanupTestData 清理测试数据
func cleanupTestData(t *testing.T) {
	// TODO: 清理测试数据
}

// setupTestGoods 创建测试商品
func setupTestGoods(t *testing.T) uint {
	// TODO: 创建测试商品并返回 ID
	return 0
}

// teardownTestGoods 清理测试商品
func teardownTestGoods(t *testing.T, goodsID uint) {
	// TODO: 清理测试商品
}
