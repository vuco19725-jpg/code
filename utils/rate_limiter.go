package utils

import (
	"sync"
	"time"
)

// RateLimiter 简单的固定窗口限流器
// 用于降级模式下保护 MySQL，防止 Redis 故障时全量流量打垮数据库
type RateLimiter struct {
	mu        sync.Mutex
	rate      int
	counter   int
	lastReset time.Time
}

// NewRateLimiter 创建限流器，rate 为每秒允许的最大请求数
func NewRateLimiter(rate int) *RateLimiter {
	return &RateLimiter{
		rate:      rate,
		lastReset: time.Now(),
	}
}

// Allow 判断当前请求是否允许通过
func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	if now.Second() != rl.lastReset.Second() {
		rl.counter = 0
		rl.lastReset = now
	}

	if rl.counter >= rl.rate {
		return false
	}

	rl.counter++
	return true
}

// SetRate 动态调整限流速率
func (rl *RateLimiter) SetRate(rate int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.rate = rate
}
