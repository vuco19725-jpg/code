package utils

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// 日志级别（原子操作，支持运行时调整）
var logLevel = zap.NewAtomicLevel()

// 采样器配置
var (
	samplerEnabled = true
	sampleRate     = 100 // 每100条采样1条
)

// samplerWrapper 采样器
type samplerWrapper struct {
	mu       sync.Mutex
	counts   map[string]int64
	lastReset time.Time
}

var sampler = &samplerWrapper{
	counts:    make(map[string]int64),
	lastReset: time.Now(),
}

// ShouldSample 判断是否应该记录日志
func (s *samplerWrapper) ShouldSample(key string) bool {
	if !samplerEnabled || sampleRate <= 1 {
		return true
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 每秒重置计数
	if time.Since(s.lastReset) > time.Second {
		s.counts = make(map[string]int64)
		s.lastReset = time.Now()
	}

	s.counts[key]++
	return s.counts[key]%int64(sampleRate) == 1
}

// Sensitive 敏感信息包装器
type Sensitive struct {
	key   string
	value string
}

// MarshalLogObject 实现 zap.ObjectMarshaler
func (s Sensitive) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("value", "***REDACTED***")
	return nil
}

// StringS 创建一个脱敏的字符串字段
func StringS(key, value string) zap.Field {
	return zap.String(key, value)
}

// SensitiveField 创建一个敏感字段（脱敏显示）
func SensitiveField(key, value string) zap.Field {
	return zap.Object(key, Sensitive{key: key, value: value})
}

var (
	logger *zap.Logger
	once   sync.Once
)

// InitLogger 初始化日志
func InitLogger(env string) {
	once.Do(func() {
		var cfg zap.Config
		if env == "prod" {
			cfg = zap.NewProductionConfig()
			cfg.OutputPaths = []string{"logs/seckill.log", "stdout"}
			cfg.ErrorOutputPaths = []string{"logs/error.log", "stderr"}
			cfg.EncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
			cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
		} else {
			cfg = zap.NewDevelopmentConfig()
			cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
			cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
		}

		// 设置日志级别
		cfg.Level = logLevel

		// 创建日志目录
		if env == "prod" {
			os.MkdirAll("logs", 0755)
		}

		var err error
		logger, err = cfg.Build()
		if err != nil {
			panic(fmt.Sprintf("init logger failed: %v", err))
		}
	})
}

// SetLogLevel 动态设置日志级别
// 支持: debug, info, warn, error
func SetLogLevel(level string) error {
	var zapLevel zapcore.Level
	switch strings.ToLower(level) {
	case "debug":
		zapLevel = zapcore.DebugLevel
	case "info":
		zapLevel = zapcore.InfoLevel
	case "warn":
		zapLevel = zapcore.WarnLevel
	case "error":
		zapLevel = zapcore.ErrorLevel
	case "dpanic":
		zapLevel = zapcore.DPanicLevel
	case "panic":
		zapLevel = zapcore.PanicLevel
	case "fatal":
		zapLevel = zapcore.FatalLevel
	default:
		return fmt.Errorf("invalid log level: %s", level)
	}
	logLevel.SetLevel(zapLevel)
	Info("日志级别已调整", String("level", level))
	return nil
}

// GetLogLevel 获取当前日志级别
func GetLogLevel() string {
	return logLevel.String()
}

// SetSampleRate 设置采样率
// rate=100 表示每100条采样1条，rate<=1 表示不采样
func SetSampleRate(rate int) {
	if rate <= 1 {
		samplerEnabled = false
		sampleRate = 1
	} else {
		samplerEnabled = true
		sampleRate = rate
	}
	Info("日志采样率已调整", Int("sample_rate", rate))
}

// GetSampleRate 获取当前采样率
func GetSampleRate() int {
	return sampleRate
}

// LogWithSampling 带采样的日志记录
func LogWithSampling(level, key, msg string, fields ...zap.Field) {
	if !sampler.ShouldSample(key) {
		return
	}

	switch strings.ToLower(level) {
	case "debug":
		logger.Debug(msg, fields...)
	case "info":
		logger.Info(msg, fields...)
	case "warn":
		logger.Warn(msg, fields...)
	case "error":
		logger.Error(msg, fields...)
	default:
		logger.Info(msg, fields...)
	}
}

// Debug 调试日志
func Debug(msg string, fields ...zap.Field) {
	logger.Debug(msg, fields...)
}

// Info 信息日志
func Info(msg string, fields ...zap.Field) {
	logger.Info(msg, fields...)
}

// Warn 警告日志
func Warn(msg string, fields ...zap.Field) {
	logger.Warn(msg, fields...)
}

// Error 错误日志
func Error(msg string, fields ...zap.Field) {
	logger.Error(msg, fields...)
}

// Fatal 致命错误日志
func Fatal(msg string, fields ...zap.Field) {
	logger.Fatal(msg, fields...)
}

// Sync 刷新日志缓冲区
func Sync() {
	if logger != nil {
		logger.Sync()
	}
}

// String 创建字符串字段
func String(key, val string) zap.Field {
	return zap.String(key, val)
}

// Int 创建整数字段
func Int(key string, val int) zap.Field {
	return zap.Int(key, val)
}

// Int64 创建 int64 字段
func Int64(key string, val int64) zap.Field {
	return zap.Int64(key, val)
}

// Uint 创建无符号整数字段
func Uint(key string, val uint) zap.Field {
	return zap.Uint(key, val)
}

// Err 创建错误字段
func Err(err error) zap.Field {
	return zap.Error(err)
}

// Any 创建任意类型字段
func Any(key string, val interface{}) zap.Field {
	return zap.Any(key, val)
}

// Bool 创建布尔字段
func Bool(key string, val bool) zap.Field {
	return zap.Bool(key, val)
}

// Duration 创建时间间隔字段
func Duration(key string, val time.Duration) zap.Field {
	return zap.Duration(key, val)
}

// Time 创建时间字段
func Time(key string, val time.Time) zap.Field {
	return zap.Time(key, val)
}

// GetLogger 获取原生 zap.Logger
func GetLogger() *zap.Logger {
	return logger
}

// MaskPhone 手机号脱敏
func MaskPhone(phone string) string {
	if len(phone) != 11 {
		return phone
	}
	return phone[:3] + "****" + phone[7:]
}

// MaskEmail 邮箱脱敏
func MaskEmail(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return "***"
	}
	username := parts[0]
	domain := parts[1]
	if len(username) <= 2 {
		return "***@" + domain
	}
	return username[:1] + "***@" + domain
}

// MaskIDCard 身份证号脱敏
func MaskIDCard(idCard string) string {
	if len(idCard) < 8 {
		return "***"
	}
	return idCard[:4] + "**********" + idCard[len(idCard)-4:]
}
