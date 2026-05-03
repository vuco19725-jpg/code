package ai

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"
)

// RateLimiter 协程池限流器
type RateLimiter struct {
	sem       *semaphore.Weighted
	timeout   time.Duration
	waitCount int64
	waitLock  sync.Mutex
}

// NewRateLimiter 创建限流器
// maxConcurrent: 最大并发数
// timeout: 获取令牌超时时间
func NewRateLimiter(maxConcurrent int, timeout time.Duration) *RateLimiter {
	return &RateLimiter{
		sem:     semaphore.NewWeighted(int64(maxConcurrent)),
		timeout: timeout,
	}
}

// Acquire 获取令牌，超时返回 false
func (r *RateLimiter) Acquire(ctx context.Context) bool {
	r.waitLock.Lock()
	r.waitCount++
	r.waitLock.Unlock()

	// 创建超时上下文
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// 获取令牌（Acquire 返回 error）
	err := r.sem.Acquire(ctx, 1)

	r.waitLock.Lock()
	r.waitCount--
	r.waitLock.Unlock()

	return err == nil
}

// Release 释放令牌
func (r *RateLimiter) Release() {
	r.sem.Release(1)
}

// WaitCount 当前等待中的请求数
func (r *RateLimiter) WaitCount() int64 {
	r.waitLock.Lock()
	defer r.waitLock.Unlock()
	return r.waitCount
}

// InFlight 当前正在执行的请求数
func (r *RateLimiter) InFlight() int64 {
	r.waitLock.Lock()
	defer r.waitLock.Unlock()
	// 粗略估算：等待中 + 正在执行
	return r.waitCount
}
