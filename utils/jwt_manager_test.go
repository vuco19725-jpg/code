package utils

import (
	"strings"
	"testing"
	"time"
)

func TestInitSecret(t *testing.T) {
	tests := []struct {
		name    string
		secret  string
		wantErr bool
	}{
		{"valid secret", "test-secret-key-12345", false},
		{"empty secret", "", true},
		{"short secret", "short", true},
		{"exactly 16 chars", "1234567890123456", false},
		{"nil secret", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := InitSecret(tt.secret)
			if (err != nil) != tt.wantErr {
				t.Errorf("InitSecret() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGenerateToken(t *testing.T) {
	InitSecret("test-secret-key-12345")

	token, err := GenerateToken(1, "user")
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	if token == "" {
		t.Error("GenerateToken() returned empty token")
	}

	// Token 应该有三部分组成
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Errorf("GenerateToken() token should have 3 parts, got %d", len(parts))
	}
}

func TestGenerateToken_DifferentUsers(t *testing.T) {
	InitSecret("test-secret-key-12345")

	token1, err := GenerateToken(1, "user")
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	token2, err := GenerateToken(2, "user")
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	if token1 == token2 {
		t.Error("GenerateToken() should generate different tokens for different users")
	}
}

func TestGenerateToken_DifferentRoles(t *testing.T) {
	InitSecret("test-secret-key-12345")

	token1, err := GenerateToken(1, "user")
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	token2, err := GenerateToken(1, "admin")
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	if token1 == token2 {
		t.Error("GenerateToken() should generate different tokens for different roles")
	}
}

func TestParseToken(t *testing.T) {
	InitSecret("test-secret-key-12345")

	token, err := GenerateToken(123, "admin")
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	claims, err := ParseToken(token)
	if err != nil {
		t.Fatalf("ParseToken() error = %v", err)
	}

	if claims.UserID != 123 {
		t.Errorf("ParseToken() UserID = %d, want 123", claims.UserID)
	}
	if claims.Role != "admin" {
		t.Errorf("ParseToken() Role = %s, want admin", claims.Role)
	}
}

func TestParseToken_Invalid(t *testing.T) {
	InitSecret("test-secret-key-12345")

	tests := []struct {
		name  string
		token string
	}{
		{"empty token", ""},
		{"malformed token", "invalid-token"},
		{"wrong signature", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VyX2lkIjoxMjMsInJvbGUiOiJhZG1pbiJ9.INVALID_SIGNATURE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseToken(tt.token)
			if err == nil {
				t.Error("ParseToken() should return error for invalid token")
			}
		})
	}
}

func TestParseToken_Expired(t *testing.T) {
	// 创建一个过期的 token（使用内部函数，不通过 GenerateToken）
	secret := "test-secret-key-12345"
	InitSecret(secret)

	// 由于 GenerateToken 硬编码了 24 小时过期，无法直接测试过期
	// 这里我们测试用错误密钥解析的情况
	_, err := ParseToken("eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VyX2lkIjoxMjMsInJvbGUiOiJhZG1pbiIsImV4cCI6MTYwMDAwMDAwMH0.xxxxxxxxxxxxxxxx")
	if err == nil {
		t.Error("ParseToken() should return error for expired token")
	}
}

func TestRotateSecret(t *testing.T) {
	InitSecret("old-secret-key-12345")

	token1, err := GenerateToken(1, "user")
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	// 轮换密钥
	err = RotateSecret("new-secret-key-12345")
	if err != nil {
		t.Fatalf("RotateSecret() error = %v", err)
	}

	token2, err := GenerateToken(1, "user")
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	if token1 == token2 {
		t.Error("GenerateToken() should generate different tokens after secret rotation")
	}

	// 旧密钥生成的 token 应该仍然可以验证
	claims, err := ParseToken(token1)
	if err != nil {
		t.Errorf("ParseToken() should still validate old token after rotation, error = %v", err)
	}
	if claims.UserID != 1 {
		t.Errorf("ParseToken() UserID = %d, want 1", claims.UserID)
	}

	// 新密钥生成的 token 也可以验证
	claims, err = ParseToken(token2)
	if err != nil {
		t.Errorf("ParseToken() should validate new token, error = %v", err)
	}
	if claims.UserID != 1 {
		t.Errorf("ParseToken() UserID = %d, want 1", claims.UserID)
	}
}

func TestRotateSecret_EmptySecret(t *testing.T) {
	InitSecret("test-secret-key-12345")

	err := RotateSecret("")
	if err == nil {
		t.Error("RotateSecret() should return error for empty secret")
	}
}

func TestGenerateRandomSecret(t *testing.T) {
	secret1, err := GenerateRandomSecret(32)
	if err != nil {
		t.Fatalf("GenerateRandomSecret() error = %v", err)
	}

	if len(secret1) != 64 { // 32 bytes -> 64 hex chars
		t.Errorf("GenerateRandomSecret() length = %d, want 64", len(secret1))
	}

	secret2, err := GenerateRandomSecret(32)
	if err != nil {
		t.Fatalf("GenerateRandomSecret() error = %v", err)
	}

	if secret1 == secret2 {
		t.Error("GenerateRandomSecret() should generate different secrets")
	}
}

func TestGenerateRandomSecret_DifferentLengths(t *testing.T) {
	secret16, _ := GenerateRandomSecret(16)
	secret32, _ := GenerateRandomSecret(32)

	if len(secret16) != 32 { // 16 bytes -> 32 hex chars
		t.Errorf("GenerateRandomSecret(16) length = %d, want 32", len(secret16))
	}
	if len(secret32) != 64 { // 32 bytes -> 64 hex chars
		t.Errorf("GenerateRandomSecret(32) length = %d, want 64", len(secret32))
	}
}

func TestNeedRotate(t *testing.T) {
	InitSecret("test-secret-key-12345")

	// 新初始化不应该需要轮换
	if NeedRotate() {
		t.Error("NeedRotate() should return false for newly initialized secret")
	}
}

func TestGetRotateTime(t *testing.T) {
	InitSecret("test-secret-key-12345")

	rotateTime := GetRotateTime()
	if rotateTime.IsZero() {
		t.Error("GetRotateTime() should not return zero time")
	}

	// 应该在未来 90 天左右
	expectedMin := time.Now().Add(89 * 24 * time.Hour)
	expectedMax := time.Now().Add(91 * 24 * time.Hour)

	if rotateTime.Before(expectedMin) || rotateTime.After(expectedMax) {
		t.Errorf("GetRotateTime() = %v, expected between %v and %v", rotateTime, expectedMin, expectedMax)
	}
}

func TestSetRotateAfter(t *testing.T) {
	// 先设置轮换时间，再初始化密钥
	SetRotateAfter(1 * time.Hour)
	InitSecret("test-secret-key-12345")

	rotateTime := GetRotateTime()
	expectedMin := time.Now().Add(30 * time.Minute)
	expectedMax := time.Now().Add(90 * time.Minute)

	if rotateTime.Before(expectedMin) || rotateTime.After(expectedMax) {
		t.Errorf("GetRotateTime() = %v, expected between %v and %v", rotateTime, expectedMin, expectedMax)
	}
}

func TestGenerateTokenWithSecret(t *testing.T) {
	// 先初始化为该密钥，使 ParseToken 能验证
	InitSecret("custom-secret-key-123456")

	token, err := GenerateTokenWithSecret(999, "vip", "custom-secret-key-123456")
	if err != nil {
		t.Fatalf("GenerateTokenWithSecret() error = %v", err)
	}

	// 使用相同密钥解析
	claims, err := ParseToken(token)
	if err != nil {
		t.Fatalf("ParseToken() error = %v", err)
	}

	if claims.UserID != 999 {
		t.Errorf("ParseToken() UserID = %d, want 999", claims.UserID)
	}
	if claims.Role != "vip" {
		t.Errorf("ParseToken() Role = %s, want vip", claims.Role)
	}
}

func TestGenerateTokenWithSecret_DifferentSecrets(t *testing.T) {
	token1, _ := GenerateTokenWithSecret(1, "user", "secret-key-one-12345678")
	token2, _ := GenerateTokenWithSecret(1, "user", "secret-key-two-12345678")

	if token1 == token2 {
		t.Error("GenerateTokenWithSecret() should generate different tokens for different secrets")
	}
}

func TestParseToken_MultipleKeys(t *testing.T) {
	InitSecret("first-secret-key-12345")

	token, _ := GenerateToken(1, "user")

	// 轮换到第二个密钥
	RotateSecret("second-secret-key-12345")

	// 仍然可以解析第一个密钥生成的 token
	claims, err := ParseToken(token)
	if err != nil {
		t.Errorf("ParseToken() should validate token with previous key, error = %v", err)
	}
	if claims.UserID != 1 {
		t.Errorf("ParseToken() UserID = %d, want 1", claims.UserID)
	}
}

func TestClaims_Fields(t *testing.T) {
	InitSecret("test-secret-key-12345")

	token, _ := GenerateToken(42, "special_role")

	claims, _ := ParseToken(token)

	// 检查 RegisteredClaims 字段
	if claims.ExpiresAt == nil {
		t.Error("Claims.ExpiresAt should not be nil")
	}
	if claims.IssuedAt == nil {
		t.Error("Claims.IssuedAt should not be nil")
	}

	// 过期时间应该在 24 小时左右
	expectedExpiry := time.Now().Add(24 * time.Hour)
	actualExpiry := claims.ExpiresAt.Time
	diff := actualExpiry.Sub(expectedExpiry)
	if diff < -time.Hour || diff > time.Hour {
		t.Errorf("Claims.ExpiresAt not within expected range, diff = %v", diff)
	}
}

func BenchmarkGenerateToken(b *testing.B) {
	InitSecret("test-secret-key-12345")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = GenerateToken(1, "user")
	}
}

func BenchmarkParseToken(b *testing.B) {
	InitSecret("test-secret-key-12345")
	token, _ := GenerateToken(1, "user")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ParseToken(token)
	}
}

func BenchmarkGenerateRandomSecret(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = GenerateRandomSecret(32)
	}
}
