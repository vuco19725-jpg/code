package utils

import (
	"sync/atomic"
	"time"
)

// RequestCounter 请求计数器，用于追踪正在处理的请求数量
type RequestCounter struct {
	active   int64  // 当前活跃请求数
	peak     int64  // 峰值请求数
	total    int64  // 累计请求数（重启后重置）
	shutting int64  // 是否正在关闭 (0=否, 1=是)
}

// NewRequestCounter 创建请求计数器
func NewRequestCounter() *RequestCounter {
	return &RequestCounter{}
}

// Increment 增加活跃请求计数
func (rc *RequestCounter) Increment() {
	if atomic.LoadInt64(&rc.shutting) == 1 {
		return // 关闭时不再接受新请求
	}
	count := atomic.AddInt64(&rc.active, 1)
	atomic.AddInt64(&rc.total, 1)

	// 更新峰值
	for {
		peak := atomic.LoadInt64(&rc.peak)
		if count <= peak {
			break
		}
		if atomic.CompareAndSwapInt64(&rc.peak, peak, count) {
			break
		}
	}
}

// Decrement 减少活跃请求计数
func (rc *RequestCounter) Decrement() {
	atomic.AddInt64(&rc.active, -1)
}

// BeginShutdown 开始关闭，禁止新请求进入
func (rc *RequestCounter) BeginShutdown() {
	atomic.StoreInt64(&rc.shutting, 1)
	Info("request counter: shutdown initiated")
}

// ActiveCount 获取当前活跃请求数
func (rc *RequestCounter) ActiveCount() int64 {
	return atomic.LoadInt64(&rc.active)
}

// PeakCount 获取峰值请求数
func (rc *RequestCounter) PeakCount() int64 {
	return atomic.LoadInt64(&rc.peak)
}

// TotalCount 获取累计请求数
func (rc *RequestCounter) TotalCount() int64 {
	return atomic.LoadInt64(&rc.total)
}

// IsShutting 判断是否正在关闭
func (rc *RequestCounter) IsShutting() bool {
	return atomic.LoadInt64(&rc.shutting) == 1
}

// WaitForZero 等待所有请求完成（带超时）
func (rc *RequestCounter) WaitForZero(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		active := atomic.LoadInt64(&rc.active)
		if active == 0 {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		// 短暂休眠避免过度占用 CPU
		time.Sleep(100 * time.Millisecond)
	}
}

// Stats 获取统计信息
func (rc *RequestCounter) Stats() map[string]interface{} {
	return map[string]interface{}{
		"active":   rc.ActiveCount(),
		"peak":     rc.PeakCount(),
		"total":    rc.TotalCount(),
		"shutting": rc.IsShutting(),
	}
}