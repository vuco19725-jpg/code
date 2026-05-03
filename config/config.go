package config

import (
	"errors"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Database DatabaseConfig `mapstructure:"database"`
	Redis    RedisConfig    `mapstructure:"redis"`
	App      AppConfig      `mapstructure:"app"`
	AI       AIConfig       `mapstructure:"ai"`
}

type AIConfig struct {
	APIKey         string  `mapstructure:"api_key"`      // DeepSeek API Key
	Model          string  `mapstructure:"model"`        // LLM 模型
	EmbedKey       string  `mapstructure:"embed_key"`    // SiliconFlow API Key
	EmbedModel     string  `mapstructure:"embed_model"` // Embedding 模型
	QdrantAddr     string  `mapstructure:"qdrant_addr"` // localhost:6333
	MaxTokens      int     `mapstructure:"max_tokens"`  // 最大生成 token 数
	Temp           float32 `mapstructure:"temperature"` // 温度参数
	TopK           int     `mapstructure:"top_k"`        // 检索返回数量
	MaxConcurrency int     `mapstructure:"max_concurrency"` // LLM 最大并发数
}

type DatabaseConfig struct {
	Host     string              `mapstructure:"host"`
	Port     int                 `mapstructure:"port"`
	User     string              `mapstructure:"user"`
	Password string              `mapstructure:"password"`
	Name     string              `mapstructure:"name"`
	Pool     DatabasePoolConfig  `mapstructure:"pool"`
}

// DatabasePoolConfig 数据库连接池配置
type DatabasePoolConfig struct {
	MaxOpenConns    int           `mapstructure:"max_open_conns"`     // 最大打开连接数（默认：100）
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`     // 最大空闲连接数（默认：20）
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`   // 连接最大生命周期（默认：5分钟）
	ConnMaxIdleTime time.Duration `mapstructure:"conn_max_idle_time"`   // 空闲连接最大存活时间（默认：3分钟）
	HealthCheck     bool          `mapstructure:"health_check"`        // 是否启用健康检查（默认：true）
}

// DefaultDatabasePoolConfig 默认连接池配置
var DefaultDatabasePoolConfig = DatabasePoolConfig{
	MaxOpenConns:    100,
	MaxIdleConns:    20,              // 低于 MaxOpenConns
	ConnMaxLifetime: 5 * time.Minute, // 小于 MySQL wait_timeout（默认8小时）
	ConnMaxIdleTime: 3 * time.Minute,
	HealthCheck:     true,
}

type RedisConfig struct {
	Host     string       `mapstructure:"host"`
	Port     int          `mapstructure:"port"`
	Password string       `mapstructure:"password"`
	DB       int          `mapstructure:"db"`
	Pool     RedisPoolConfig `mapstructure:"pool"`
}

// RedisPoolConfig Redis 连接池配置
type RedisPoolConfig struct {
	PoolSize     int `mapstructure:"pool_size"`      // 最大连接数（默认：100）
	MinIdleConns int `mapstructure:"min_idle_conns"` // 最小空闲连接数（默认：10）
}

// DefaultRedisPoolConfig 默认连接池配置
var DefaultRedisPoolConfig = RedisPoolConfig{
	PoolSize:     100,
	MinIdleConns: 10,
}

type AppConfig struct {
	Host      string `mapstructure:"host"`
	Port      int    `mapstructure:"port"`
	JwtSecret string `mapstructure:"jwt_secret"`
	Env       string `mapstructure:"env"`
}

var GlobalConfig *Config

func LoadConfig(env string) error {
	viper.SetConfigName("config_" + env)
	viper.SetConfigType("yaml")
	viper.AddConfigPath("./config")
	viper.AddConfigPath(".")

	if err := viper.ReadInConfig(); err != nil {
		return err
	}

	GlobalConfig = &Config{}
	if err := viper.Unmarshal(GlobalConfig); err != nil {
		return err
	}

	// JWT Secret 校验
	if err := validateJWTSecret(GlobalConfig.App.JwtSecret, GlobalConfig.App.Env); err != nil {
		return err
	}

	return nil
}

// validateJWTSecret 校验 JWT Secret 配置
func validateJWTSecret(secret, env string) error {
	// 检查是否为空或包含占位符
	invalidSecrets := []string{
		"",
		"your-secret-key",
		"dev_secret_change_in_production",
		"${JWT_SECRET}",
	}
	for _, invalid := range invalidSecrets {
		if secret == invalid {
			return errors.New("jwt_secret must be set and cannot be a default/placeholder value")
		}
	}

	// 生产环境检查密钥长度
	if env == "prod" && len(secret) < 32 {
		return errors.New("jwt_secret must be at least 32 characters in production")
	}

	// 检查是否包含环境变量格式（未替换）
	if strings.Contains(secret, "${") {
		return errors.New("jwt_secret contains unresolved environment variable")
	}

	return nil
}
