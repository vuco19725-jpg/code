package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"seckill/config"

	"github.com/redis/go-redis/v9"
)

var Redis *redis.Client

// 限流 Lua 脚本：原子递增 + 设置过期时间
// 返回递增后的计数
const rateLimitScript = `
local key = KEYS[1]
local window = tonumber(ARGV[1])

local count = redis.call('INCR', key)
if count == 1 then
    redis.call('EXPIRE', key, window)
end

return count
`

// rateLimitScript SHA（预加载）
var rateLimitScriptSHA string

func InitRedis(cfg *config.RedisConfig) error {
	// 使用配置值或默认值
	poolCfg := cfg.Pool
	if poolCfg.PoolSize <= 0 {
		poolCfg.PoolSize = config.DefaultRedisPoolConfig.PoolSize
	}
	if poolCfg.MinIdleConns <= 0 {
		poolCfg.MinIdleConns = config.DefaultRedisPoolConfig.MinIdleConns
	}

	Redis = redis.NewClient(&redis.Options{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     poolCfg.PoolSize,
		MinIdleConns: poolCfg.MinIdleConns,
	})

	ctx := context.Background()
	if err := Redis.Ping(ctx).Err(); err != nil {
		return err
	}

	// 预加载限流脚本,预编译
	sha, err := Redis.ScriptLoad(ctx, rateLimitScript).Result()
	if err != nil {
		return fmt.Errorf("failed to load rate limit script: %w", err)
	}
	rateLimitScriptSHA = sha

	return nil
}

func GetRedis() *redis.Client {
	return Redis
}

// CloseRedis 关闭 Redis 连接
func CloseRedis() error {
	if Redis != nil {
		return Redis.Close()
	}
	return nil
}

// 库存 Key（单桶）
func StockKey(skuID string) string {
	return fmt.Sprintf("stock:%s", skuID)
}

// 库存分桶 Key
// 分桶策略：将库存拆分成多个桶，每个桶独立扣减，避免单 key 热点的竞争问题
func StockBucketKey(skuID string, bucketID int) string {
	return fmt.Sprintf("stock:%s:bucket:%d", skuID, bucketID)
}

// GetBucketStock 获取指定桶的库存
func GetBucketStock(ctx context.Context, skuID string, bucketID int) (int, error) {
	return Redis.Get(ctx, StockBucketKey(skuID, bucketID)).Int()
}

// SetBucketStock 设置指定桶的库存
func SetBucketStock(ctx context.Context, skuID string, bucketID int, stock int, expiration time.Duration) error {
	return Redis.Set(ctx, StockBucketKey(skuID, bucketID), stock, expiration).Err()
}

// SetBucketStockIfNotExists 仅当桶不存在时设置（用于初始化）
func SetBucketStockIfNotExists(ctx context.Context, skuID string, bucketID int, stock int, expiration time.Duration) (bool, error) {
	return Redis.SetNX(ctx, StockBucketKey(skuID, bucketID), stock, expiration).Result()
}

// DecrBucketStock 原子递减桶库存（返回递减后的值）
func DecrBucketStock(ctx context.Context, skuID string, bucketID int) (int64, error) {
	return Redis.Decr(ctx, StockBucketKey(skuID, bucketID)).Result()
}

// IncrBucketStock 原子递增桶库存（用于回滚）
func IncrBucketStock(ctx context.Context, skuID string, bucketID int) (int64, error) {
	return Redis.Incr(ctx, StockBucketKey(skuID, bucketID)).Result()
}

// GetTotalStockFromBuckets 获取所有桶的总库存
func GetTotalStockFromBuckets(ctx context.Context, skuID string, bucketCount int) (int, error) {
	var total int
	pipe := Redis.Pipeline()
	keys := make([]string, bucketCount)
	for i := 1; i <= bucketCount; i++ {
		keys[i-1] = StockBucketKey(skuID, i)
		pipe.Get(ctx, keys[i-1])
	}
	cmds, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		return 0, err
	}
	for _, cmd := range cmds {
		if cmd.Err() == nil {
			val, _ := cmd.(*redis.StringCmd).Int()
			total += val
		}
	}
	return total, nil
}

// PreHeatStockToBuckets 将库存预热到多个桶（Pipeline 批量操作）
// stockPerBucket: 每个桶的库存数量
// 返回实际分配的桶数
func PreHeatStockToBuckets(ctx context.Context, skuID string, totalStock int, bucketCount int, expiration time.Duration) (int, error) {
	if totalStock <= 0 || bucketCount <= 0 {
		return 0, errors.New("invalid stock or bucket count")
	}

	// 平均分配库存到各个桶
	stockPerBucket := totalStock / bucketCount
	remainder := totalStock % bucketCount

	pipe := Redis.Pipeline()
	for i := 1; i <= bucketCount; i++ {
		stock := stockPerBucket
		if i <= remainder {
			stock++ // 前面几个桶多分配 1 件
		}
		pipe.Set(ctx, StockBucketKey(skuID, i), stock, expiration)
	}
	_, err := pipe.Exec(ctx)
	if err != nil {
		return 0, err
	}
	return bucketCount, nil
}

// 用户购买记录 Key
func UserSeckillKey(userID, skuID string) string {
	return fmt.Sprintf("user:seckill:%s:%s", userID, skuID)
}

// 用户 Token Key（单设备登录）
func UserTokenKey(userID string) string {
	return fmt.Sprintf("user:token:%s", userID)
}

// 限流 Key
func RateLimitIPKey(ip string) string {
	return fmt.Sprintf("rate:ip:%s", ip)
}

func RateLimitUserKey(userID string) string {
	return fmt.Sprintf("rate:user:%s", userID)
}

// 商品缓存 Key
func GoodsKey(skuID string) string {
	return fmt.Sprintf("goods:%s", skuID)
}

// GetGoods 获取商品信息缓存
func GetGoods(ctx context.Context, skuID string) (string, error) {
	return Redis.Get(ctx, GoodsKey(skuID)).Result()
}

// SetGoods 设置商品信息缓存
func SetGoods(ctx context.Context, skuID string, goodsJSON string, expiration time.Duration) error {
	return Redis.Set(ctx, GoodsKey(skuID), goodsJSON, expiration).Err()
}

// SetWithExpire 设置带过期时间的 Key
func SetWithExpire(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return Redis.Set(ctx, key, value, expiration).Err()
}

// GetString 获取 String 值
func GetString(ctx context.Context, key string) (string, error) {
	return Redis.Get(ctx, key).Result()
}

// Delete 删除 Key
func Delete(ctx context.Context, key string) error {
	return Redis.Del(ctx, key).Err()
}

// Exists 检查 Key 是否存在
func Exists(ctx context.Context, key string) (int64, error) {
	return Redis.Exists(ctx, key).Result()
}

// Incr 原子递增
func Incr(ctx context.Context, key string) (int64, error) {
	return Redis.Incr(ctx, key).Result()
}

// Decr 原子递减
func Decr(ctx context.Context, key string) (int64, error) {
	return Redis.Decr(ctx, key).Result()
}

// SetNX 原子操作（key 不存在时设置）
func SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error) {
	return Redis.SetNX(ctx, key, value, expiration).Result()
}

// Expire 设置过期时间
func Expire(ctx context.Context, key string, expiration time.Duration) (bool, error) {
	return Redis.Expire(ctx, key, expiration).Result()
}

// IncrWithExpire 原子递增并设置过期时间（使用 Lua 脚本保证原子性）
// 返回递增后的计数
func IncrWithExpire(ctx context.Context, key string, expiration time.Duration) (int64, error) {
	if rateLimitScriptSHA == "" {
		// 兜底：使用 Eval（性能稍差但正确）
		result, err := Redis.Eval(ctx, rateLimitScript, []string{key}, int(expiration.Seconds())).Int64()
		return result, err
	}
	// 使用预加载的脚本
	result, err := Redis.EvalSha(ctx, rateLimitScriptSHA, []string{key}, int(expiration.Seconds())).Int64()
	if err != nil {
		// 如果 SHA 不存在，重新加载
		sha, reErr := Redis.ScriptLoad(ctx, rateLimitScript).Result()
		if reErr != nil {
			return 0, reErr
		}
		rateLimitScriptSHA = sha
		result, err = Redis.EvalSha(ctx, rateLimitScriptSHA, []string{key}, int(expiration.Seconds())).Int64()
	}
	return result, err
}
