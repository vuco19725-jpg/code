package utils

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims JWT Claims
type Claims struct {
	UserID uint   `json:"user_id"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

// SecretManager 密钥管理器，支持密钥轮换
type SecretManager struct {
	mu          sync.RWMutex
	current     []byte
	previous    []byte
	expireTime  time.Time
	rotateAfter time.Duration
}

var manager = &SecretManager{
	rotateAfter: 90 * 24 * time.Hour, // 默认90天轮换
}

// InitSecret 初始化密钥（启动时调用）
func InitSecret(secret string) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.current = []byte(secret)
	manager.expireTime = time.Now().Add(manager.rotateAfter)
	return nil
}

// SetRotateAfter 设置轮换周期
func SetRotateAfter(duration time.Duration) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.rotateAfter = duration
}

// RotateSecret 手动轮换密钥
func RotateSecret(newSecret string) error {
	if newSecret == "" {
		return errors.New("new secret cannot be empty")
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.previous = manager.current
	manager.current = []byte(newSecret)
	manager.expireTime = time.Now().Add(manager.rotateAfter)
	return nil
}

// GenerateRandomSecret 生成随机密钥
func GenerateRandomSecret(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// getKey 获取当前密钥（用于 token 生成）
func getKey() []byte {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return manager.current
}

// getKeys 获取所有可用密钥（用于 token 验证）
func getKeys() [][]byte {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	keys := [][]byte{manager.current}
	if manager.previous != nil {
		keys = append(keys, manager.previous)
	}
	return keys
}

// NeedRotate 检查是否需要轮换
func NeedRotate() bool {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return time.Now().After(manager.expireTime)
}

// GetRotateTime 获取下次轮换时间
func GetRotateTime() time.Time {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return manager.expireTime
}

// GenerateToken 生成 Token
func GenerateToken(userID uint, role string) (string, error) {
	claims := Claims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(getKey())
}

// GenerateTokenWithSecret 使用指定密钥生成 Token（兼容旧接口）
func GenerateTokenWithSecret(userID uint, role, secret string) (string, error) {
	claims := Claims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ParseToken 验证 Token（支持多密钥）
func ParseToken(tokenString string) (*Claims, error) {
	keys := getKeys()

	var lastErr error
	for _, key := range keys {
		token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
			return key, nil
		})
		if err == nil {
			if claims, ok := token.Claims.(*Claims); ok && token.Valid {
				return claims, nil
			}
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("invalid token")
	}
	return nil, lastErr
}
