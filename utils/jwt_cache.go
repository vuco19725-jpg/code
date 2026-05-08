package utils

import (
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type cacheEntry struct {
	claims  *Claims
	expires time.Time
}

// jwtCache 本地 JWT 缓存，避免重复解析和 Redis 单设备检查
var jwtCache sync.Map

// GetCachedClaims 从缓存获取 claims，已过期则删除并返回 nil
func GetCachedClaims(tokenString string) *Claims {
	val, ok := jwtCache.Load(tokenString)
	if !ok {
		return nil
	}
	entry := val.(*cacheEntry)
	if time.Now().After(entry.expires) {
		jwtCache.Delete(tokenString)
		return nil
	}
	return entry.claims
}

// SetCachedClaims 缓存 JWT 解析结果，过期时间取 token 自身 expiry
func SetCachedClaims(tokenString string, claims *Claims) {
	jwtCache.Store(tokenString, &cacheEntry{
		claims:  claims,
		expires: claims.ExpiresAt.Time,
	})
}

// InvalidateCachedClaims 主动使缓存失效（如密钥轮换时）
func InvalidateCachedClaims(tokenString string) {
	jwtCache.Delete(tokenString)
}

// GetClaimsFromCacheOrParse 优先读缓存，缓存未命中时解析 JWT 并缓存
// 返回 (claims, wasCached, error)
func GetClaimsFromCacheOrParse(tokenString string) (*Claims, bool, error) {
	if claims := GetCachedClaims(tokenString); claims != nil {
		return claims, true, nil
	}

	keys := getKeys()
	var lastErr error
	for _, key := range keys {
		token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
			return key, nil
		})
		if err == nil {
			if claims, ok := token.Claims.(*Claims); ok && token.Valid {
				SetCachedClaims(tokenString, claims)
				return claims, false, nil
			}
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = jwt.ErrSignatureInvalid
	}
	return nil, false, lastErr
}
