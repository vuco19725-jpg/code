package utils

import (
	"errors"
	"sync"
	"time"
)

// CircuitState 熔断器状态
type CircuitState int

const (
	StateClosed   CircuitState = 0 // 正常状态
	StateHalfOpen CircuitState = 1 // 半开状态（试探恢复）
	StateOpen     CircuitState = 2 // 熔断状态（拒绝请求）
)

func (s CircuitState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateHalfOpen:
		return "half-open"
	case StateOpen:
		return "open"
	default:
		return "unknown"
	}
}

// CircuitBreaker 熔断器
type CircuitBreaker struct {
	mu sync.RWMutex

	// 配置
	failureThreshold int           // 失败次数阈值，达到后触发熔断
	successThreshold int           // 成功次数阈值，半开状态下达到后恢复
	halfMaxRequests  int           // 半开状态下最大放行请求数
	openTimeout      time.Duration // 熔断持续时间

	// 状态
	state           CircuitState
	failureCount    int           // 当前失败计数
	successCount    int           // 当前成功计数
	requestInHalf   int32         // 半开状态当前请求数
	lastFailureTime time.Time
	lastStateChange time.Time

	// 统计
	totalRequests   int64
	totalFailures   int64
	totalSuccesses  int64
}

// CircuitBreakerConfig 熔断器配置
type CircuitBreakerConfig struct {
	FailureThreshold int           // 失败次数阈值（默认：5）
	SuccessThreshold int           // 成功次数阈值（默认：3）
	HalfMaxRequests  int           // 半开状态最大放行（默认：3）
	OpenTimeout      time.Duration // 熔断持续时间（默认：30秒）
}

// DefaultCircuitBreakerConfig 默认配置
var DefaultCircuitBreakerConfig = CircuitBreakerConfig{
	FailureThreshold: 5,
	SuccessThreshold: 3,
	HalfMaxRequests:  3,
	OpenTimeout:      30 * time.Second,
}

// NewCircuitBreaker 创建熔断器
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	if config.FailureThreshold <= 0 {
		config.FailureThreshold = DefaultCircuitBreakerConfig.FailureThreshold
	}
	if config.SuccessThreshold <= 0 {
		config.SuccessThreshold = DefaultCircuitBreakerConfig.SuccessThreshold
	}
	if config.HalfMaxRequests <= 0 {
		config.HalfMaxRequests = DefaultCircuitBreakerConfig.HalfMaxRequests
	}
	if config.OpenTimeout <= 0 {
		config.OpenTimeout = DefaultCircuitBreakerConfig.OpenTimeout
	}

	return &CircuitBreaker{
		failureThreshold: config.FailureThreshold,
		successThreshold: config.SuccessThreshold,
		halfMaxRequests:  config.HalfMaxRequests,
		openTimeout:      config.OpenTimeout,
		state:            StateClosed,
		lastStateChange:  time.Now(),
	}
}

// Allow 检查是否允许请求通过
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.totalRequests++

	switch cb.state {
	case StateClosed:
		// 正常状态：全部放行
		return true

	case StateHalfOpen:
		// 半开状态：限制并发请求数
		if cb.requestInHalf >= int32(cb.halfMaxRequests) {
			return false
		}
		cb.requestInHalf++
		return true

	case StateOpen:
		// 检查超时，是否可以转为半开
		if time.Since(cb.lastFailureTime) > cb.openTimeout {
			cb.toState(StateHalfOpen)
			cb.requestInHalf = 1
			return true
		}
		return false

	default:
		return false
	}
}

// RecordSuccess 记录成功
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.totalSuccesses++
	cb.failureCount = 0 // 重置失败计数

	if cb.state == StateHalfOpen {
		cb.successCount++
		cb.requestInHalf--
		if cb.successCount >= cb.successThreshold {
			cb.toState(StateClosed)
		}
	}
}

// RecordFailure 记录失败
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.totalFailures++
	cb.lastFailureTime = time.Now()

	if cb.state == StateHalfOpen {
		// 半开状态下失败，立即熔断
		cb.toState(StateOpen)
		return
	}

	cb.failureCount++
	if cb.failureCount >= cb.failureThreshold {
		cb.toState(StateOpen)
	}
}

// toState 转换状态
func (cb *CircuitBreaker) toState(state CircuitState) {
	if cb.state == state {
		return
	}

	cb.state = state
	cb.lastStateChange = time.Now()

	// 重置计数
	cb.failureCount = 0
	cb.successCount = 0
	cb.requestInHalf = 0
}

// GetState 获取当前状态
func (cb *CircuitBreaker) GetState() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// GetStats 获取统计信息
func (cb *CircuitBreaker) GetStats() CircuitBreakerStats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return CircuitBreakerStats{
		State:           cb.state,
		FailureCount:    cb.failureCount,
		SuccessCount:    cb.successCount,
		TotalRequests:   cb.totalRequests,
		TotalFailures:   cb.totalFailures,
		TotalSuccesses:  cb.totalSuccesses,
		FailureRate:     cb.calculateFailureRate(),
		LastStateChange: cb.lastStateChange,
	}
}

// calculateFailureRate 计算失败率
func (cb *CircuitBreaker) calculateFailureRate() float64 {
	total := cb.totalRequests
	if total == 0 {
		return 0
	}
	return float64(cb.totalFailures) / float64(total)
}

// CircuitBreakerStats 熔断器统计
type CircuitBreakerStats struct {
	State           CircuitState
	FailureCount    int
	SuccessCount    int
	TotalRequests   int64
	TotalFailures   int64
	TotalSuccesses  int64
	FailureRate     float64
	LastStateChange time.Time
}

// ErrCircuitOpen 熔断器开启错误
var ErrCircuitOpen = errors.New("circuit breaker is open")
