package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"seckill/config"
	"seckill/handler"
	"seckill/middleware"
	"seckill/repository"
	"seckill/service"
	"seckill/utils"

	"github.com/gin-gonic/gin"
)

var (
	buildTime = "unknown"
	gitCommit = "unknown"
	version   = "unknown"
)

func main() {
	// 加载配置
	env := os.Getenv("SECKILL_ENV")
	if env == "" {
		env = "dev"
	}

	if err := config.LoadConfig(env); err != nil {
		panic(fmt.Sprintf("load config failed: %v", err))
	}

	cfg := config.GlobalConfig

	// 端口覆盖（多实例测试用）
	if portStr := os.Getenv("SECKILL_PORT"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			cfg.App.Port = port
		}
	}

	// 初始化日志
	utils.InitLogger(cfg.App.Env)
	defer utils.Sync()

	// 初始化数据库
	if err := repository.InitDB(&cfg.Database, env); err != nil {
		utils.Error("init database failed", utils.Err(err))
		panic(err)
	}

	// 初始化 Redis
	if err := repository.InitRedis(&cfg.Redis); err != nil {
		utils.Error("init redis failed", utils.Err(err))
		panic(err)
	}

	// 初始化 JWT 密钥管理器
	if err := utils.InitSecret(cfg.App.JwtSecret); err != nil {
		utils.Error("init jwt secret failed", utils.Err(err))
		panic(err)
	}

	// 初始化 AI 服务
	aiService, err := service.NewAIService(&cfg.AI)
	if err != nil {
		utils.Error("init ai service failed", utils.Err(err))
		panic(err)
	}

	// 初始化 Kafka Producer
	kafkaProducer, err := service.NewAsyncKafkaProducer(&cfg.Kafka)
	if err != nil {
		utils.Error("init kafka producer failed", utils.Err(err))
		panic(err)
	}
	service.StartProducerCallbacks(kafkaProducer)
	defer kafkaProducer.AsyncClose()

	// 初始化 Kafka Consumer Group
	kafkaConsumerGroup, err := service.NewKafkaConsumerGroup(&cfg.Kafka)
	if err != nil {
		utils.Error("init kafka consumer failed", utils.Err(err))
		panic(err)
	}
	defer kafkaConsumerGroup.Close()

	// 初始化业务服务
	seckillSvc := service.NewSeckillService(repository.DB, repository.Redis, kafkaProducer, cfg.Kafka.Topic)
	// 启动后台周期性库存同步（每30秒将Redis库存同步到MySQL）
	stockSyncStop := make(chan struct{})
	seckillSvc.StartPeriodicStockSync([]uint{2, 3}, 30*time.Second, stockSyncStop)

	// 启动待处理订单 Worker（可靠发送到 Kafka）
	seckillSvc.StartPendingWorker(context.Background())
		// 启动兜底队列消费者（从 Redis fallback 读取，重新发送到 Kafka）
		seckillSvc.StartFallbackWorker(context.Background())

	// 启动 Kafka 订单消费者
	orderConsumer := service.NewOrderConsumer(repository.DB, repository.Redis, kafkaConsumerGroup, cfg.Kafka.Topic)
	orderConsumer.Start(context.Background())

	// 创建 Gin 引擎
	if cfg.App.Env == "prod" {
		gin.SetMode(gin.ReleaseMode)
	}

	// 初始化 Handler
	userHandler := handler.NewUserHandler(repository.DB)
	goodsHandler := handler.NewGoodsHandler(repository.DB)
	seckillHandler := handler.NewSeckillHandler(seckillSvc)
	healthHandler := handler.NewHealthHandler()
	aiHandler := handler.NewAIHandler(aiService)

	router := gin.New()
	router.Use(gin.Recovery())

	// 全局限间件
	router.Use(middleware.CORS())
	router.Use(middleware.TraceID())
	router.Use(middleware.RequestLoggerMiddleware())  // 请求日志
	router.Use(middleware.RequestCounterMiddleware()) // 请求计数

	// 健康检查
	router.GET("/health", healthHandler.Health)
	router.GET("/health/ready", healthHandler.Ready)

	// AI 接口
	router.GET("/ai/capabilities", aiHandler.Capabilities)
	router.POST("/ai/chat", aiHandler.Chat)
	router.POST("/ai/chatSSE", aiHandler.ChatSSE)

	// 熔断器状态监控（生产环境可移除）
	router.GET("/api/v1/seckill/breaker", seckillHandler.GetBreakerStatus)	// API v1
	v1 := router.Group("/api/v1")
	{
		// 用户模块
		user := v1.Group("/user")
		{
			user.POST("/register", userHandler.Register)
			user.POST("/login", userHandler.Login)
			user.POST("/logout", middleware.JWTAuth(), userHandler.Logout)
			user.GET("/info", middleware.JWTAuth(), userHandler.GetUserInfo)
		}

		// 秒杀模块（需要登录）
		seckill := v1.Group("/seckill")
		seckill.Use(middleware.JWTAuth())
		seckill.Use(middleware.RateLimitByIP(60, time.Minute))
		seckill.Use(middleware.RateLimitByUser(10, time.Minute))
		{
			seckill.POST("/:sku_id", seckillHandler.Seckill)
			seckill.GET("/queue/:token", seckillHandler.PollOrder)
		}

		// 订单模块（需要登录）
		order := v1.Group("/order")
		order.Use(middleware.JWTAuth())
		{
			order.GET("/:id", seckillHandler.GetOrder)
		}

		// 我的订单（需要登录）
		v1.GET("/orders", middleware.JWTAuth(), seckillHandler.GetMyOrders)

		// 商品列表（公开接口）
		v1.GET("/goods", goodsHandler.GetGoodsList)
	}

	// 管理后台
	admin := router.Group("/admin")
	admin.Use(middleware.JWTAuth())
	admin.Use(middleware.AdminAuth())
	{
		admin.POST("/goods", goodsHandler.CreateGoods)
		admin.GET("/goods", goodsHandler.GetGoodsList)
		admin.GET("/goods/:id", goodsHandler.GetGoodsDetail)
		admin.PUT("/goods/:id/stock", goodsHandler.UpdateStock)
		admin.POST("/ai/ingest", aiHandler.Ingest)
	}

	// 启动服务
	srv := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", cfg.App.Host, cfg.App.Port),
		Handler: router,
	}

	go func() {
		utils.Info("server starting",
			utils.String("addr", srv.Addr),
			utils.String("env", cfg.App.Env),
			utils.String("version", version),
			utils.String("build_time", buildTime),
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			utils.Error("server failed", utils.Err(err))
			panic(err)
		}
	}()

	// 优雅退出
	quit := make(chan os.Signal, 2) // 缓冲2个信号，避免丢失
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit

	utils.Info("shutting down server...",
		utils.String("signal", sig.String()),
	)

	// 1. 通知请求计数器开始关闭（禁止新请求进入）
	requestCounter := middleware.GetRequestCounter()
	requestCounter.BeginShutdown()

	// 记录关闭前的活跃请求数
	activeBeforeShutdown := requestCounter.ActiveCount()
	utils.Info("waiting for in-flight requests to complete",
		utils.Int64("active_requests", activeBeforeShutdown),
		utils.Int64("peak_requests", requestCounter.PeakCount()),
	)

	// 2. 关闭 HTTP 服务（停止接收新请求，等待现有请求完成）
	// 基础超时 30 秒，每增加一个活跃请求额外增加 2 秒，最高 60 秒
	baseTimeout := 30 * time.Second
	extraTimeout := time.Duration(activeBeforeShutdown) * 2 * time.Second
	maxTimeout := 60 * time.Second
	shutdownTimeout := baseTimeout + extraTimeout
	if shutdownTimeout > maxTimeout {
		shutdownTimeout = maxTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		utils.Error("server shutdown error", utils.Err(err))
	} else {
		utils.Info("http server stopped")
	}

	// 3. 停止 Kafka 消费者（确保正在消费的订单提交偏移量）
	orderConsumer.Stop()
	utils.Info("kafka consumer stopped")

	// 停止后台库存同步（触发最终同步）
	close(stockSyncStop)
	utils.Info("stock sync stopped")
	// 4. 再次检查是否还有未完成的请求
	if remaining := requestCounter.ActiveCount(); remaining > 0 {
		utils.Warn("some requests did not complete",
			utils.Int64("remaining_requests", remaining),
		)
	}

	// 5. 关闭数据库连接
	if err := repository.CloseDB(); err != nil {
		utils.Error("database close error", utils.Err(err))
	} else {
		utils.Info("database connection closed")
	}

	// 6. 关闭 Redis 连接
	if err := repository.CloseRedis(); err != nil {
		utils.Error("redis close error", utils.Err(err))
	} else {
		utils.Info("redis connection closed")
	}

	// 6. 刷新日志缓冲区
	utils.Sync()

	utils.Info("server stopped",
		utils.String("signal", sig.String()),
		utils.Int64("total_requests", requestCounter.TotalCount()),
		utils.Int64("peak_requests", requestCounter.PeakCount()),
	)
}
