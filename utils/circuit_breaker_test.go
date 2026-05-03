package utils

import (
	"testing"
	"time"
)

func TestNewCircuitBreaker(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		HalfMaxRequests:  5,
		OpenTimeout:      10 * time.Second,
	}

	cb := NewCircuitBreaker(config)

	if cb.failureThreshold != 3 {
		t.Errorf("Expected failureThreshold 3, got %d", cb.failureThreshold)
	}
	if cb.successThreshold != 2 {
		t.Errorf("Expected successThreshold 2, got %d", cb.successThreshold)
	}
	if cb.halfMaxRequests != 5 {
		t.Errorf("Expected halfMaxRequests 5, got %d", cb.halfMaxRequests)
	}
	if cb.state != StateClosed {
		t.Errorf("Expected initial state StateClosed, got %v", cb.state)
	}
}

func TestNewCircuitBreaker_DefaultValues(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{})

	if cb.failureThreshold != DefaultCircuitBreakerConfig.FailureThreshold {
		t.Errorf("Expected default failureThreshold %d, got %d", DefaultCircuitBreakerConfig.FailureThreshold, cb.failureThreshold)
	}
	if cb.successThreshold != DefaultCircuitBreakerConfig.SuccessThreshold {
		t.Errorf("Expected default successThreshold %d, got %d", DefaultCircuitBreakerConfig.SuccessThreshold, cb.successThreshold)
	}
}

func TestCircuitBreaker_Allow_ClosedState(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 3,
		OpenTimeout:      10 * time.Second,
	})

	// 正常状态下应该允许请求
	for i := 0; i < 10; i++ {
		if !cb.Allow() {
			t.Errorf("Expected Allow() to return true in closed state, got false at iteration %d", i)
		}
	}
}

func TestCircuitBreaker_Allow_OpenState(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 2,
		OpenTimeout:      10 * time.Second,
	})

	// 触发熔断
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.GetState() != StateOpen {
		t.Errorf("Expected state to be Open, got %v", cb.GetState())
	}

	// 熔断状态下应该拒绝请求
	if cb.Allow() {
		t.Error("Expected Allow() to return false in open state")
	}
}

func TestCircuitBreaker_Allow_HalfOpenState_Timeout(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 1,
		SuccessThreshold: 2,
		HalfMaxRequests:  1,
		OpenTimeout:      50 * time.Millisecond,
	})

	// 触发熔断
	cb.RecordFailure()

	if cb.GetState() != StateOpen {
		t.Errorf("Expected state to be Open, got %v", cb.GetState())
	}

	// 等待超时后应该进入半开状态
	time.Sleep(100 * time.Millisecond)

	// 半开状态应该允许请求
	if !cb.Allow() {
		t.Error("Expected Allow() to return true after timeout in open state")
	}

	if cb.GetState() != StateHalfOpen {
		t.Errorf("Expected state to be HalfOpen, got %v", cb.GetState())
	}
}

func TestCircuitBreaker_Allow_HalfOpenState_MaxRequests(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 1,
		SuccessThreshold: 2,
		HalfMaxRequests:  2,
		OpenTimeout:      10 * time.Millisecond,
	})

	// 触发熔断
	cb.RecordFailure()
	time.Sleep(20 * time.Millisecond)

	// 第一次请求进入半开
	cb.Allow()
	// 第二次请求
	cb.Allow()
	// 第三次请求应该被拒绝
	if cb.Allow() {
		t.Error("Expected Allow() to return false when halfMaxRequests reached")
	}
}

func TestCircuitBreaker_RecordSuccess(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 2,
		OpenTimeout:      50 * time.Millisecond, // 改为 50ms，与 Sleep 20ms 配合
	})

	// 触发熔断
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.GetState() != StateOpen {
		t.Errorf("Expected state to be Open, got %v", cb.GetState())
	}

	// 等待超时
	time.Sleep(60 * time.Millisecond) // 超过 OpenTimeout 50ms

	// 进入半开并成功
	cb.Allow()
	cb.RecordSuccess()

	if cb.GetState() != StateHalfOpen {
		t.Errorf("Expected state to still be HalfOpen after 1 success, got %v", cb.GetState())
	}

	// 达到成功阈值后应该恢复
	cb.Allow()
	cb.RecordSuccess()

	if cb.GetState() != StateClosed {
		t.Errorf("Expected state to be Closed after successThreshold, got %v", cb.GetState())
	}
}

func TestCircuitBreaker_RecordFailure(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 3,
		OpenTimeout:      10 * time.Second,
	})

	stats := cb.GetStats()
	if stats.TotalFailures != 0 {
		t.Errorf("Expected initial TotalFailures 0, got %d", stats.TotalFailures)
	}

	cb.RecordFailure()
	cb.RecordFailure()

	stats = cb.GetStats()
	if stats.TotalFailures != 2 {
		t.Errorf("Expected TotalFailures 2, got %d", stats.TotalFailures)
	}

	if cb.GetState() != StateClosed {
		t.Errorf("Expected state to be Closed before threshold, got %v", cb.GetState())
	}

	// 触发熔断
	cb.RecordFailure()

	if cb.GetState() != StateOpen {
		t.Errorf("Expected state to be Open after reaching failureThreshold, got %v", cb.GetState())
	}
}

func TestCircuitBreaker_RecordFailure_InHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 1,
		SuccessThreshold: 2,
		HalfMaxRequests:  1,
		OpenTimeout:      10 * time.Millisecond,
	})

	// 触发熔断
	cb.RecordFailure()
	time.Sleep(20 * time.Millisecond)

	// 进入半开
	cb.Allow()
	if cb.GetState() != StateHalfOpen {
		t.Errorf("Expected state to be HalfOpen, got %v", cb.GetState())
	}

	// 半开状态下失败，应该立即熔断
	cb.RecordFailure()

	if cb.GetState() != StateOpen {
		t.Errorf("Expected state to be Open after failure in half-open, got %v", cb.GetState())
	}
}

func TestCircuitBreaker_GetStats(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		OpenTimeout:      10 * time.Second,
	})

	stats := cb.GetStats()

	if stats.State != StateClosed {
		t.Errorf("Expected initial state StateClosed, got %v", stats.State)
	}
	if stats.TotalRequests != 0 {
		t.Errorf("Expected initial TotalRequests 0, got %d", stats.TotalRequests)
	}
	if stats.TotalSuccesses != 0 {
		t.Errorf("Expected initial TotalSuccesses 0, got %d", stats.TotalSuccesses)
	}
	if stats.TotalFailures != 0 {
		t.Errorf("Expected initial TotalFailures 0, got %d", stats.TotalFailures)
	}
	if stats.FailureRate != 0 {
		t.Errorf("Expected initial FailureRate 0, got %f", stats.FailureRate)
	}
}

func TestCircuitBreaker_FailureRate(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 10,
		OpenTimeout:      10 * time.Second,
	})

	// 模拟一些请求: Allow 增加 totalRequests, RecordFailure 只增加 totalFailures
	for i := 0; i < 5; i++ {
		cb.Allow()
	}
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}

	stats := cb.GetStats()
	// totalRequests=5, totalFailures=3, FailureRate = 3/5 = 0.6
	expectedRate := float64(3) / float64(5)
	if stats.FailureRate != expectedRate {
		t.Errorf("Expected FailureRate %f, got %f", expectedRate, stats.FailureRate)
	}
}

func TestCircuitBreaker_FailureRate_ZeroRequests(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{})

	stats := cb.GetStats()
	if stats.FailureRate != 0 {
		t.Errorf("Expected FailureRate 0 when no requests, got %f", stats.FailureRate)
	}
}

func TestCircuitState_String(t *testing.T) {
	tests := []struct {
		state    CircuitState
		expected string
	}{
		{StateClosed, "closed"},
		{StateHalfOpen, "half-open"},
		{StateOpen, "open"},
		{CircuitState(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.expected {
			t.Errorf("CircuitState.String() = %v, want %v", got, tt.expected)
		}
	}
}

func TestCircuitBreaker_ConcurrentAccess(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 100,
		SuccessThreshold: 100,
		OpenTimeout:      10 * time.Second,
	})

	done := make(chan bool)

	// 并发写入测试
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				cb.Allow()
				cb.RecordSuccess()
			}
			done <- true
		}()
	}

	// 并发读取测试
	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = cb.GetState()
				_ = cb.GetStats()
			}
			done <- true
		}()
	}

	// 等待所有 goroutine 完成
	for i := 0; i < 15; i++ {
		<-done
	}
}

func TestCircuitBreaker_StateTransitions(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 1,
		SuccessThreshold: 1,
		HalfMaxRequests:  1,
		OpenTimeout:      50 * time.Millisecond,
	})

	// Closed -> Open
	cb.RecordFailure()
	if cb.GetState() != StateOpen {
		t.Errorf("Expected StateOpen, got %v", cb.GetState())
	}

	// Open -> HalfOpen (after timeout)
	time.Sleep(100 * time.Millisecond)
	cb.Allow()
	if cb.GetState() != StateHalfOpen {
		t.Errorf("Expected StateHalfOpen, got %v", cb.GetState())
	}

	// HalfOpen -> Closed (success)
	cb.RecordSuccess()
	if cb.GetState() != StateClosed {
		t.Errorf("Expected StateClosed, got %v", cb.GetState())
	}
}

func BenchmarkCircuitBreaker_Allow(b *testing.B) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 3,
		OpenTimeout:      30 * time.Second,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cb.Allow()
	}
}

func BenchmarkCircuitBreaker_RecordSuccess(b *testing.B) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 3,
		OpenTimeout:      30 * time.Second,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cb.RecordSuccess()
	}
}

func BenchmarkCircuitBreaker_RecordFailure(b *testing.B) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 3,
		OpenTimeout:      30 * time.Second,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cb.RecordFailure()
	}
}
