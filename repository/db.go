package repository

import (
	"context"
	"fmt"
	"time"

	"seckill/config"
	"seckill/model"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func InitDB(cfg *config.DatabaseConfig, env string) error {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Name)

	var err error
	// 根据环境设置日志级别：dev 打印慢查询，prod 只打印错误
	gormLogger := logger.Default.LogMode(logger.Warn)
	DB, err = gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: gormLogger,
	})
	if err != nil {
		return err
	}

	sqlDB, err := DB.DB()
	if err != nil {
		return err
	}

	// 使用配置或默认值
	poolCfg := cfg.Pool
	if poolCfg.MaxOpenConns <= 0 {
		poolCfg.MaxOpenConns = config.DefaultDatabasePoolConfig.MaxOpenConns
	}
	if poolCfg.MaxIdleConns <= 0 {
		poolCfg.MaxIdleConns = config.DefaultDatabasePoolConfig.MaxIdleConns
	}
	if poolCfg.ConnMaxLifetime <= 0 {
		poolCfg.ConnMaxLifetime = config.DefaultDatabasePoolConfig.ConnMaxLifetime
	}
	if poolCfg.ConnMaxIdleTime <= 0 {
		poolCfg.ConnMaxIdleTime = config.DefaultDatabasePoolConfig.ConnMaxIdleTime
	}

	// 连接池配置
	sqlDB.SetMaxOpenConns(poolCfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(poolCfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(poolCfg.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(poolCfg.ConnMaxIdleTime)

	// 启动连接健康检查（后台定期 Ping）
	if poolCfg.HealthCheck {
		go startDBHealthCheck(sqlDB, poolCfg)
	}

	// 仅在开发/测试环境执行自动迁移
	// 生产环境应使用 SQL 脚本或专业 Migration 工具（如 golang-migrate、goose）
	if env == "dev" || env == "test" {
		if err := DB.AutoMigrate(&model.User{}, &model.SeckillGoods{}, &model.Order{}, &model.OperationLog{}); err != nil {
			return err
		}
	}

	return nil
}

// startDBHealthCheck 定期检查数据库连接健康
func startDBHealthCheck(sqlDB interface {
	Ping() error
	Close() error
}, poolCfg config.DatabasePoolConfig) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if err := sqlDB.Ping(); err != nil {
			// 连接不健康，记录错误（实际生产中应告警）
			fmt.Printf("database health check failed: %v\n", err)
			// 不自动关闭，让 GORM 自动处理重连
		}
	}
}

func GetDB(ctx context.Context) *gorm.DB {
	return DB.WithContext(ctx)
}

// CloseDB 关闭数据库连接
func CloseDB() error {
	if DB != nil {
		sqlDB, err := DB.DB()
		if err != nil {
			return err
		}
		return sqlDB.Close()
	}
	return nil
}
