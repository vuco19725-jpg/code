# 秒杀系统项目规划(目标以底下的内容来go实习项目,最终找到实习)

> 请直接修改以下内容，我会根据你的改动重新整理方案

---

## 一、技术栈

| 技术 | 选型 | 版本建议 |
|------|------|----------|
| 语言 | Go 1.20+ |  |
| Web 框架 | Gin | v1.9+ |
| ORM | GORM | v2.0 |
| 数据库 | MySQL | 8.0 |
| 缓存 | Redis | 7.0 |
| 配置管理 | Viper | v1.18 |
| 认证 | JWT | - |
| 日志 | Zap | v1.26 |

**扩展能力（预留接口）：**
- 消息队列：Kafka（预留接口，后期可扩展）
- 分布式锁：RedLock（预留接口，多实例部署时启用）
- 链路追踪：OpenTelemetry（先用 trace_id + 结构化日志替代）
- 配置中心：etcd/consul（预留配置热更新接口）
- 监控：Prometheus（预留 metrics 接口）

---

## 二、项目结构（部署版）

```
seckill/
├── main.go
├── config/
│   ├── config.go          # 配置加载
│   └── config_dev.yaml    # 开发环境
│   └── config_prod.yaml   # 生产环境
├── handler/
│   ├── user.go
│   ├── goods.go
│   ├── seckill.go
│   └── health.go          # 健康检查
├── service/
│   ├── user.go
│   ├── goods.go
│   └── seckill.go
├── model/
│   └── model.go
├── repository/
│   ├── db.go
│   └── redis.go
├── middleware/
│   ├── auth.go
│   ├── ratelimit.go
│   ├── cors.go
│   └── trace.go
├── utils/
│   ├── response.go
│   ├── jwt.go
│   ├── trace.go
│   ├── captcha.go
│   └── logger.go
├── pkg/
│   ├── lock/              # 分布式锁（预留）
│   ├── mq/                # 消息队列接口（预留）
│   └── metrics/           # Prometheus metrics（预留）
├── scripts/
│   ├── init.sql
│   └── lua/
│       └── decr_stock.lua
├── deploy/
│   ├── docker-compose.yml # 本地开发环境
│   ├── Dockerfile         # 构建镜像
│   └── nginx.conf        # Nginx 配置
├── logs/                  # 日志目录
└── go.mod
```

---

## 三、部署预留内容

### 3.1 多环境配置

```
config/
├── config_dev.yaml    # 开发：本地 MySQL/Redis
├── config_test.yaml   # 测试：测试库
└── config_prod.yaml   # 生产：云数据库
```

**环境切换方式：**
```bash
# 运行指定环境
SECKILL_ENV=prod ./seckill

# 或启动参数
./seckill -env=prod
```

**配置内容差异：**
| 配置项 | 开发 | 生产 |
|--------|------|------|
| 数据库地址 | localhost | 云数据库内网地址 |
| Redis 地址 | localhost | 云 Redis |
| 日志级别 | debug | info |
| 限流阈值 | 宽松 | 严格 |
| CORS | 允许本地 | 限制域名 |

### 3.2 Docker 部署

**Dockerfile：**
```dockerfile
FROM golang:1.20-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o seckill main.go

FROM alpine
COPY --from=builder /app/seckill /usr/local/bin/
COPY --from=builder /app/config/config_prod.yaml /etc/seckill/config.yaml
EXPOSE 8080
CMD ["seckill"]
```

**docker-compose.yml（开发环境）：**
```yaml
version: '3.8'
services:
  seckill:
    build: .
    ports:
      - "8080:8080"
    depends_on:
      - mysql
      - redis
    environment:
      - SECKILL_ENV=dev

  mysql:
    image: mysql:8.0
    ports:
      - "3306:3306"
    environment:
      - MYSQL_ROOT_PASSWORD=123456

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
```

### 3.3 Nginx 配置预留

```nginx
upstream seckill_backend {
    least_conn;                    # 最少连接负载均衡
    server 127.0.0.1:8080;
    # 后期多实例：
    # server 127.0.0.1:8081;
    # server 127.0.0.1:8082;
}

server {
    listen 80;
    server_name seckill.example.com;

    # 静态资源缓存
    location /static/ {
        expires 7d;
    }

    # API 代理
    location /api/ {
        proxy_pass http://seckill_backend;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;

        # 超时配置
        proxy_connect_timeout 5s;
        proxy_send_timeout 30s;
        proxy_read_timeout 30s;
    }
}
```

---

## 四、运维相关预留

### 4.1 健康检查接口

| 接口 | 方法 | 说明 |
|------|------|------|
| `/health` | GET | 存活检查（仅判断进程是否存活，不依赖下游） |
| `/health/ready` | GET | 就绪检查（检查 MySQL/Redis 等依赖是否可用） |

**响应：**
```json
{
  "status": "ok",
  "uptime": 3600,
  "mysql": "ok",
  "redis": "ok"
}
```

**实现（建议拆分存活与就绪）：**
```go
func LivenessHandler(c *gin.Context) {
    c.JSON(200, gin.H{"status": "ok"})
}

func ReadinessHandler(c *gin.Context) {
    // 检查 MySQL
    if err := db.DB().Ping(); err != nil {
        c.JSON(503, gin.H{"status": "mysql error"})
        return
    }

    // 检查 Redis
    if _, err := redis.Ping(ctx).Result(); err != nil {
        c.JSON(503, gin.H{"status": "redis error"})
        return
    }

    c.JSON(200, gin.H{"status": "ok"})
}
```

### 4.2 Prometheus Metrics 预留

```go
// pkg/metrics/metrics.go
var (
    SeckillRequests = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "seckill_requests_total",
            Help: "Total seckill requests",
        },
        []string{"sku_id", "status"},
    )

    SeckillDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "seckill_duration_seconds",
            Buckets: prometheus.DefBuckets,
        },
        []string{"sku_id"},
    )

    ActiveUsers = prometheus.NewGauge(
        prometheus.GaugeOpts{
            Name: "seckill_active_users",
            Help: "Number of active users",
        },
    )
)
```

**接口：**
| 接口 | 说明 |
|------|------|
| `/metrics` | Prometheus 拉取指标 |

### 4.3 Graceful Shutdown（优雅退出）

```go
// 收到信号后，先停止接收新请求，再处理完现有请求
srv := &http.Server{
    Addr:    ":8080",
    Handler: router,
}

go func() {
    if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
        log.Fatal(err)
    }
}()

// 等待中断信号
quit := make(chan os.Signal, 1)
signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
<-quit

// 优雅退出：等待 30 秒处理完现有请求
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
srv.Shutdown(ctx)
```

### 4.4 配置热更新预留

```go
// Viper 支持监听文件变化
viper.WatchConfig()
viper.OnConfigChange(func(e fsnotify.Event) {
    // 重新加载配置
    loadConfig()
    // 更新限流阈值等
})
```

---

## 五、权限管理预留

### 5.1 用户角色

| 角色 | 说明 |
|------|------|
| user | 普通用户，只能秒杀和查看自己的订单 |
| admin | 管理员，可以管理商品和库存 |

### 5.2 权限中间件预留

```go
// 角色校验
func RequireRole(roles ...string) gin.HandlerFunc {
    return func(c *gin.Context) {
        userRole := c.GetString("user_role")
        for _, role := range roles {
            if userRole == role {
                c.Next()
                return
            }
        }
        c.JSON(403, gin.H{"error": "权限不足"})
        c.Abort()
    }
}

// 使用
admin := router.Group("/admin")
admin.Use(jwt.Auth())
admin.Use(RequireRole("admin"))
admin.POST("/goods", CreateGoods)
```

### 5.3 数据库变更

**用户表增加角色字段：**
| 字段 | 类型 | 约束 | 说明 |
|------|------|------|------|
| role | VARCHAR(16) | DEFAULT 'user' | 角色：user/admin |

---

## 六、运维功能预留

### 6.1 操作日志

谁在什么时候做了什么操作，便于审计。

```go
// 操作日志表
type OperationLog struct {
    ID         uint      `gorm:"primaryKey"`
    UserID     uint      `gorm:"index"`
    Action     string    `gorm:"size:64"`    // create_goods, update_stock
    Target     string    `gorm:"size:64"`    // goods:5
    Detail     string    `gorm:"type:text"` // 变更详情 JSON
    IP         string    `gorm:"size:32"`
    CreatedAt  time.Time
}
```

### 6.2 定时任务预留

| 任务 | 说明 | 实现方式 |
|------|------|----------|
| 库存预热 | 活动开始前加载到 Redis | 定时任务 + 手动触发 |
| 超时订单关闭 | 30分钟未支付自动取消 | 定时任务 |
| 数据统计 | 秒杀数据汇总 | 定时任务 |

**实现：**
```go
// 使用 cron 库
c := cron.New(cron.WithSeconds())
c.AddFunc("0 */5 * * * *", preHeatActiveGoods)  // 每5分钟检查
c.AddFunc("0 */1 * * * *", closeTimeoutOrders)  // 每分钟检查超时订单
c.Start()
```

### 6.3 数据库迁移

```go
// 生产环境建议使用版本化迁移工具（如 goose/atlas），避免直接 AutoMigrate
db.AutoMigrate(
    &User{},
    &SeckillGoods{},
    &Order{},
    &OperationLog{},
)
```

---

## 七、安全相关预留

### 7.1 基础安全

| 防护项 | 实现 |
|--------|------|
| 密码加密 | bcrypt |
| SQL 注入 | GORM 参数化查询 |
| XSS | Gin 拒绝 HTML 响应 |
| CORS | 中间件限制允许的域名 |
| 请求签名 | 预留接口（后期加） |
| 敏感日志脱敏 | 手机号、密码打码 |

### 7.2 请求签名验证（预留）

```go
// 后期可加，防止请求被篡改
func VerifySignature(c *gin.Context) bool {
    signature := c.GetHeader("X-Signature")
    timestamp := c.GetHeader("X-Timestamp")

    // 签名 = HMAC-SHA256(timestamp + nonce + body, secret)
    // 校验时间窗口（如5分钟）+ nonce 防重放
}
```

---

## 八、API 版本管理预留

```
当前：/api/v1/xxx
后期：/api/v2/xxx（兼容 v1）
```

```go
v1 := router.Group("/api/v1")
v2 := router.Group("/api/v2")

// v2 新增接口可以改变行为
// v1 保持不变，保证向后兼容
```

---

## 九、实现顺序（完整版）

### 第一阶段：基础搭建
1. [ ] 项目结构初始化
2. [ ] 多环境配置文件
3. [ ] 数据库连接（GORM + 时区 + AutoMigrate）
4. [ ] Redis 连接封装
5. [ ] 日志封装（zap + 文件轮转）
6. [ ] 统一响应封装
7. [ ] 路由注册框架
8. [ ] trace_id 中间件
9. [ ] 健康检查接口（/health）

### 第二阶段：用户模块
10. [ ] 数据模型（User + role 字段）
11. [ ] 验证码接口
12. [ ] 注册接口（bcrypt 加密）
13. [ ] 登录接口（JWT + 单设备限制）
14. [ ] JWT 中间件
15. [ ] 用户信息接口
16. [ ] 角色权限中间件（预留）

### 第三阶段：商品模块
17. [ ] 数据模型（SeckillGoods）
18. [ ] 创建商品接口（需要 admin 权限）
19. [ ] 商品列表接口
20. [ ] 商品详情接口
21. [ ] 修改库存接口（需要 admin 权限）
22. [ ] 商品信息 Redis 缓存

### 第四阶段：秒杀核心
23. [ ] 库存预加载到 Redis
24. [ ] Redis Lua 脚本原子扣库存
25. [ ] 防重复购买（Redis Set）
26. [ ] 秒杀下单接口
27. [ ] 订单创建（MySQL + 事务）
28. [ ] 查询订单接口
29. [ ] 我的订单列表

### 第五阶段：Go 并发增强
30. [ ] 批量订单查询使用 goroutine
31. [ ] 库存预热使用 goroutine
32. [ ] 日志异步写入（goroutine + channel）
33. [ ] 连接池配置（Redis + MySQL）

### 第六阶段：运维支持
34. [ ] Docker + docker-compose 本地部署
35. [ ] Nginx 配置
36. [ ] Graceful Shutdown
37. [ ] Prometheus Metrics（预留 /metrics 接口）
38. [ ] 操作日志记录

### 第七阶段：定时任务
39. [ ] 活动开始前自动预热库存
40. [ ] 超时订单自动关闭
41. [ ] 数据库迁移脚本

### 第八阶段：防护与收尾
42. [ ] IP 限流中间件
43. [ ] 用户限流中间件
44. [ ] CORS 中间件
45. [ ] 结构化日志（带 trace_id）
46. [ ] 单元测试

---

## 十、失败场景与处理方案

### 10.1 失败场景总览

| 失败阶段 | 失败原因 | 影响 | 处理方案 |
|----------|----------|------|----------|
| Redis 扣库存 | Redis宕机/网络抖动 | 请求直接打到MySQL | 限流兜底，返回"系统繁忙" |
| 创建订单 | MySQL宕机/唯一键冲突 | 库存已扣但订单未创建 | 事务回滚 + 库存回退 |
| Redis 标记购买 | 网络中断 | 用户可重复购买 | MySQL 唯一索引兜底 |
| 用户重复请求 | 前端重复点击/网络重试 | 库存被多次扣减 | Redis Set + 幂等性保证 |
| 超时未支付 | 用户放弃/支付失败 | 库存被占用 | 定时任务回滚库存 |
| 库存预热失败 | 商品不存在 | 秒杀时直接查库 | 活动开始前检查预热状态 |

---

### 10.2 库存扣减失败

**场景：** Redis DECR 执行失败或返回错误

**失败后会怎样：**
```
1. 库存未扣减，用户看到"系统繁忙"
2. 用户可能重复点击，导致更多失败请求
3. 后续请求全部打到 MySQL
```

**解决代码：**
```go
func Seckill(c *gin.Context) {
    skuID := c.Param("sku_id")
    userID := c.GetInt("user_id")

    // 方案：Redis 扣库存用 Lua 脚本，失败有明确返回值
    script := `
        local stock = redis.call('GET', KEYS[1])
        if not stock then return -2 end  -- 库存未初始化
        if tonumber(stock) <= 0 then return -1 end  -- 已售罄
        redis.call('DECR', KEYS[1])
        return 1
    `

    result, err := redis.Eval(ctx, script, []string{"stock:" + skuID}).Int()
    if err != nil {
        // Redis 彻底挂了，走限流兜底
        logger.Error("Redis扣库存失败",
            zap.String("trace_id", traceID),
            zap.String("sku_id", skuID),
            zap.Error(err),
        )
        c.JSON(500, gin.H{"code": 5001, "msg": "系统繁忙"})
        return
    }

    if result == -2 {
        // 库存未预热，可能是服务刚启动
        c.JSON(500, gin.H{"code": 5001, "msg": "活动未开始"})
        return
    }

    if result == -1 {
        c.JSON(400, gin.H{"code": 2004, "msg": "已售罄"})
        return
    }
}
```

**限流兜底机制：**
```go
// 当 Redis 不可用时，限流器直接返回失败
func RateLimitMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        if !rateLimiter.Allow(c.ClientIP()) {
            c.JSON(429, gin.H{"code": 4001, "msg": "请求过于频繁"})
            c.Abort()
            return
        }
        c.Next()
    }
}
```

---

### 10.3 订单创建失败

**场景：** Redis 扣库存成功，但 MySQL 创建订单失败（宕机/唯一键冲突/事务超时）

**失败后会怎样：**
```
1. 库存已扣减（Redis DECR 成功）
2. 订单未创建
3. 用户付了钱但没有订单记录
4. 库存永久丢失（超卖方向：多扣了库存）
```

**解决代码：**
```go
func Seckill(c *gin.Context) {
    skuID := c.Param("sku_id")
    userID := c.GetInt("user_id")

    // 1. 检查用户是否已购买（防重复）
    purchaseKey := fmt.Sprintf("user:seckill:%d:%s", userID, skuID)
    if redis.Exists(ctx, purchaseKey).Val() > 0 {
        c.JSON(400, gin.H{"code": 2005, "msg": "已购买过"})
        return
    }

    // 2. Lua 脚本扣库存（原子操作）
    script := `
        local stock = redis.call('GET', KEYS[1])
        if not stock then return -2 end
        if tonumber(stock) <= 0 then return -1 end
        redis.call('DECR', KEYS[1])
        return 1
    `
    result, err := redis.Eval(ctx, script, []string{"stock:" + skuID}).Int()
    if err != nil || result <= 0 {
        c.JSON(400, gin.H{"code": 2004, "msg": "已售罄"})
        return
    }

    // 3. 扣库存成功，标记用户已购买（如果这里失败，后面订单也失败，会导致库存多扣）
    redis.SetEx(ctx, purchaseKey, "1", 24*time.Hour)

    // 4. 创建订单（MySQL）- 这里是可能失败的点
    order := &Order{
        OrderNo:  generateOrderNo(),
        UserID:   uint(userID),
        SkuID:    parseUint(skuID),
        Status:   OrderStatusPending,
    }

    // 事务保证：扣库存失败不会走到这里
    // 如果这里失败，需要回滚 Redis 库存
    if err := db.Create(order).Error; err != nil {
        // 回滚 Redis 库存（多扣了，需要加回来）
        redis.Incr(ctx, "stock:"+skuID)
        // 删除用户购买标记
        redis.Del(ctx, purchaseKey)

        logger.Error("订单创建失败，已回滚库存",
            zap.String("trace_id", traceID),
            zap.String("sku_id", skuID),
            zap.Int("user_id", userID),
            zap.Error(err),
        )
        c.JSON(500, gin.H{"code": 5001, "msg": "创建订单失败"})
        return
    }

    c.JSON(200, gin.H{"order_id": order.OrderNo, "status": "pending"})
}
```

**流程图：**
```
扣库存成功 → 标记购买 → 创建订单
                      ↓ 失败
               回滚库存 → 回滚购买标记 → 返回错误
```

---

### 10.4 用户重复购买

**场景：** 用户手抖多点了一次，或者前端重复请求

**失败后会怎样：**
```
1. 第一次请求：库存100 → 99，订单创建成功
2. 第二次请求：库存99 → 98，又创建了一个订单（超买）
```

**解决代码（两阶段检查）：**

```go
// 第一阶段：Redis 检查（快速拦截 99% 重复请求）
purchaseKey := fmt.Sprintf("user:seckill:%d:%s", userID, skuID)
success, err := redis.SetNX(ctx, purchaseKey, "1", 24*time.Hour).Result()
if err != nil {
    // Redis 挂了，不阻塞，继续尝试
}
// SetNX 返回 false 说明已存在，快速返回
if !success {
    c.JSON(400, gin.H{"code": 2005, "msg": "已购买过"})
    return
}
```

```go
// 第二阶段：MySQL 唯一索引兜底（保证最终一致性）
type Order struct {
    UserID uint `gorm:"uniqueIndex:idx_user_sku"`  // 联合唯一索引
    SkuID  uint `gorm:"uniqueIndex:idx_user_sku"`
}

// 创建订单时如果冲突，GORM 会返回错误
if err := db.Create(order).Error; err != nil {
    if errors.Is(err, gorm.ErrDuplicatedKey) {
        // 回滚 Redis 库存（因为第一阶段 SetNX 成功了）
        redis.Incr(ctx, "stock:"+skuID)
        redis.Del(ctx, purchaseKey)
        c.JSON(400, gin.H{"code": 2005, "msg": "已购买过"})
        return
    }
    // 其他错误
    c.JSON(500, gin.H{"code": 5001, "msg": "系统错误"})
    return
}
```

**MySQL 唯一索引建表语句：**
```sql
CREATE TABLE orders (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    order_no VARCHAR(64) UNIQUE NOT NULL,
    user_id BIGINT NOT NULL,
    sku_id BIGINT NOT NULL,
    status TINYINT DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY idx_user_sku (user_id, sku_id)  -- 联合唯一索引
);
```

---

### 10.5 超时未支付

**场景：** 用户秒杀成功，但 30 分钟内没支付

**失败后会怎样：**
```
1. 库存已被扣（用户占着库存）
2. 订单状态是"待支付"
3. 库存无法释放，其他用户买不到
4. 超时后需要自动取消订单、释放库存
```

**解决代码（定时任务）：**

```go
// 定时任务：每分钟检查超时订单
func CloseTimeoutOrders() {
    // 查找 30 分钟前创建且状态为"待支付"的订单
    timeout := time.Now().Add(-30 * time.Minute)
    var orders []Order
    db.Where("status = ? AND created_at < ?", OrderStatusPending, timeout).Find(&orders)

    for _, order := range orders {
        // 开启事务
        tx := db.Begin()

        // 1. 更新订单状态为"已取消"
        if err := tx.Model(&order).Update("status", OrderStatusCancelled).Error; err != nil {
            tx.Rollback()
            continue
        }

        // 2. 回滚 Redis 库存
        stockKey := "stock:" + strconv.Itoa(int(order.SkuID))
        if _, err := redis.Incr(ctx, stockKey).Result(); err != nil {
            tx.Rollback()
            continue
        }

        // 3. 删除用户购买标记
        purchaseKey := fmt.Sprintf("user:seckill:%d:%d", order.UserID, order.SkuID)
        redis.Del(ctx, purchaseKey)

        tx.Commit()

        logger.Info("超时订单已关闭",
            zap.String("order_no", order.OrderNo),
            zap.Int64("sku_id", order.SkuID),
        )
    }
}
```

**定时任务注册：**
```go
c := cron.New(cron.WithSeconds())
c.AddFunc("0 */1 * * * *", CloseTimeoutOrders)  // 每分钟执行
c.Start()
```

---

### 10.6 库存预热失败

**场景：** 活动开始了，但 Redis 库存没预热

**失败后会怎样：**
```
1. 用户访问"库存key"不存在（Redis GET 返回 nil）
2. Lua 脚本返回 -2（未初始化）
3. 请求直接打到 MySQL
4. MySQL 承受不住高并发
```

**解决代码：**

```go
// 查询商品时，检查 Redis 是否有库存
func GetGoodsStock(skuID string) (int, error) {
    stockKey := "stock:" + skuID

    // 先查 Redis
    stock, err := redis.Get(ctx, stockKey).Int()
    if err == nil {
        return stock, nil
    }

    // Redis 没有，查 MySQL
    goods, err := db.GetGoods(skuID)
    if err != nil {
        return 0, err
    }

    // 回填 Redis（设置较短过期时间）
    redis.SetEx(ctx, stockKey, goods.Stock, 1*time.Hour)

    return goods.Stock, nil
}
```

**预热状态检查：**
```go
// 活动开始前 5 分钟自动预热
func PreHeatGoods(skuID uint) error {
    goods, err := db.GetGoods(skuID)
    if err != nil {
        return err
    }

    stockKey := "stock:" + strconv.Itoa(int(skuID))
    err = redis.SetEx(ctx, stockKey, goods.Stock, 24*time.Hour).Err()
    if err != nil {
        logger.Error("库存预热失败",
            zap.Uint("sku_id", skuID),
            zap.Error(err),
        )
        return err
    }

    logger.Info("库存预热成功",
        zap.Uint("sku_id", skuID),
        zap.Int("stock", goods.Stock),
    )
    return nil
}
```

**活动开始前检查：**
```go
// 秒杀接口入口检查
func Seckill(c *gin.Context) {
    skuID := c.Param("sku_id")

    // 检查 Redis 库存是否存在
    stock, err := redis.Get(ctx, "stock:"+skuID).Int()
    if err != nil {
        // 库存未预热，尝试回填
        stock, err = GetGoodsStock(skuID)
        if err != nil || stock <= 0 {
            c.JSON(400, gin.H{"code": 2001, "msg": "商品不存在"})
            return
        }
    }

    // 继续正常流程...
}
```

---

### 10.7 网络超时/服务崩溃

**场景：** 请求处理过程中，服务突然崩溃

**失败后会怎样：**
```
1. 用户请求处理到一半
2. Redis 扣了库存，但订单没创建
3. 用户看到超时，但库存已经少了
4. 定时任务也无法清理（订单不存在）
```

**解决代码（Graceful Shutdown）：**

```go
// 启动服务
srv := &http.Server{
    Addr:    ":8080",
    Handler: router,
}

go func() {
    if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
        log.Fatal(err)
    }
}()

// 等待中断信号
quit := make(chan os.Signal, 1)
signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
<-quit

// 收到信号后：
// 1. 停止接收新请求（让现有请求处理完）
// 2. 等待最多 30 秒
// 3. 然后强制退出
logger.Info("收到退出信号，开始优雅关闭...")

ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

if err := srv.Shutdown(ctx); err != nil {
    logger.Error("优雅关闭失败", zap.Error(err))
}
```

**goroutine panic 恢复：**
```go
// 异步任务需要 recover，防止 panic 导致崩溃
go func() {
    defer func() {
        if r := recover(); r != nil {
            logger.Error("goroutine panic",
                zap.Any("error", r),
                zap.String("trace", string(debug.Stack())),
            )
        }
    }()
    // 业务逻辑
}()
```

---

### 10.8 失败处理总结

| 场景 | 发现方式 | 处理方式 |
|------|----------|----------|
| Redis 扣库存失败 | Lua 脚本返回 error | 限流兜底，返回系统繁忙 |
| 订单创建失败 | MySQL 错误/事务回滚 | 回滚 Redis 库存 |
| 重复购买 | Redis SetNX + MySQL 唯一索引 | 两层拦截，已购买返回错误 |
| 超时未支付 | 定时任务扫描 | 自动关闭订单 + 回滚库存 |
| 库存未预热 | Redis GET 返回 nil | 自动回填 + 记录日志 |
| 服务崩溃 | OS 信号捕获 | Graceful Shutdown 等待处理完成 |

---

## 十一、测试框架预留

### 11.1 测试技术选型

| 测试类型 | 工具 | 说明 |
|----------|------|------|
| 单元测试 | Go 标准库 `testing` + `testify` | 核心逻辑验证 |
| 集成测试 | `httptest` + `miniredis` | HTTP 接口 + 模拟 Redis |
| 压测 | `wrk` | 高并发压测，验证系统瓶颈 |
| 基准测试 | Go `testing.B` | 性能测试 |

### 11.2 单元测试（testify）

**安装：**
```bash
go get github.com/stretchr/testify
```

**测试示例：**
```go
// service/seckill_test.go
package service

import (
    "testing"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/mock"
)

// 模拟 repository
type MockRedisRepo struct {
    mock.Mock
}

func (m *MockRedisRepo) GetStock(skuID string) (int, error) {
    args := m.Called(skuID)
    return args.Int(0), args.Error(1)
}

func (m *MockRedisRepo) DecrStock(skuID string) (int64, error) {
    args := m.Called(skuID)
    return args.Get(0).(int64), args.Error(1)
}

func TestSeckillService_DecrementStock(t *testing.T) {
    mockRepo := new(MockRedisRepo)
    svc := NewSeckillService(mockRepo)

    // 测试库存充足
    mockRepo.On("GetStock", "1001").Return(10, nil)
    stock, err := svc.GetStock("1001")
    assert.NoError(t, err)
    assert.Equal(t, 10, stock)
    mockRepo.AssertExpectations(t)
}
```

### 11.3 集成测试（httptest + miniredis）

**安装：**
```bash
go get github.com/alicebob/miniredis/v2
```

**集成测试示例：**
```go
// handler/seckill_test.go
package handler

import (
    "bytes"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"
    "github.com/alicebob/miniredis/v2"
    "github.com/stretchr/testify/assert"
)

func setupTestRouter(t *testing.T) (*gin.Engine, *miniredis.MinRedis) {
    // 启动模拟 Redis
    mr, _ := miniredis.Run()

    // 设置 Redis 地址
    redisAddr = mr.Addr()

    // 创建测试路由
    router := gin.New()
    RegisterRoutes(router)

    return router, mr
}

func TestSeckillHandler_Success(t *testing.T) {
    router, mr := setupTestRouter(t)
    defer mr.Close()

    // 预设置库存
    mr.Set("stock:1001", "10")

    // 构造请求
    req := httptest.NewRequest("POST", "/api/v1/seckill/1001", nil)
    req.Header.Set("Authorization", "Bearer test-token")

    // 执行请求
    w := httptest.NewRecorder()
    router.ServeHTTP(w, req)

    // 验证结果
    assert.Equal(t, http.StatusOK, w.Code)

    var resp map[string]interface{}
    json.Unmarshal(w.Body.Bytes(), &resp)
    assert.NotEmpty(t, resp["order_id"])
}
```

### 11.4 压测工具 wrk

**安装：**
```bash
# Ubuntu/Debian
sudo apt install wrk

# macOS
brew install wrk

# 源码编译
git clone https://github.com/wg/wrk.git
cd wrk && make
```

**wrk 常用参数：**
| 参数 | 说明 | 示例 |
|------|------|------|
| `-t` | 线程数 | `-t 4` |
| `-c` | 并发连接数 | `-c 100` |
| `-d` | 持续时间 | `-d 30s` |
| `-s` | Lua 脚本 | `-s post.lua` |
| `-H` | 请求头 | `-H "Authorization: Bearer xxx"` |

### 11.5 秒杀接口压测

**基础压测（秒杀为 POST，需 Lua 脚本携带 body）：**
```bash
# 建议使用 Lua 脚本压测（POST 秒杀接口），wrk 默认为 GET，结果会失真
wrk -t 4 -c 500 -d 30s -s scripts/wrk/seckill_with_auth.lua \
    http://localhost:8080/api/v1/seckill
```

**预期输出：**
```
Running 30s test @ http://localhost:8080/api/v1/seckill/1001
  4 threads and 500 connections
  Thread Stats   Avg      Stdev     Max   +/- Stdev
    Latency     45.23ms    12.45ms   198ms   85.32%
    Req/Sec    12,456.85    1,234.56  15,000  75.32%
  1,234,567 requests in 30.05s, 45.67MB read
Requests/sec:  41,067.23
Transfer/sec:    1.52MB
```

**Lua 脚本自定义请求（携带 token）：**
```lua
-- scripts/wrk_seckill.lua
wrk.headers["Authorization"] = "Bearer test-token-token"

-- 请求前执行的函数
wrk.setup = function(thread)
    -- 每个线程初始化，可以生成不同用户 token
end

-- 请求生成函数
wrk.body = '{"sku_id":"1001"}'

-- 响应处理函数
wrk.response = function(status, headers, body)
    -- 可以统计成功率等
end
```

**使用 Lua 脚本压测：**
```bash
wrk -t 4 -c 500 -d 30s -s scripts/wrk_seckill.lua \
    http://localhost:8080/api/v1/seckill
```

### 11.6 压测验收标准

> 注意：以下指标以本地 4 核 8G 单机部署（Redis 本地）为参考基线，实际值因机器规格、库存数量、并发模型不同而变化。

| 指标 | 目标值 | 说明 |
|------|--------|------|
| QPS | > 5000 | 4核单机，Redis 本地，库存充足场景 |
| P99 延迟 | < 100ms | 99% 请求延迟 |
| 成功率 | > 99.9% | 成功请求占比（含已售罄返回为正常） |
| 错误率 | < 0.1% | 系统 5xx 错误占比 |

**成功率验证脚本：**
```bash
#!/bin/bash
# test_success_rate.sh

TOKEN="Bearer test-token"
URL="http://localhost:8080/api/v1/seckill/1001"

success=0
fail=0

for i in {1..1000}; do
    resp=$(curl -s -o /dev/null -w "%{http_code}" \
        -X POST "$URL" \
        -H "Authorization: $TOKEN")

    if [ "$resp" == "200" ]; then
        success=$((success + 1))
    else
        fail=$((fail + 1))
    fi
done

echo "Success: $success, Fail: $fail"
echo "Success Rate: $(echo "scale=2; $success*100/1000" | bc)%"
```

### 11.7 测试脚本预留

```bash
# scripts/
├── unit_test.sh        # 运行单元测试
├── integration_test.sh # 运行集成测试
├── benchmark.sh        # 运行基准测试
└── wrk/
    ├── seckill_basic.lua      # 基础秒杀压测
    ├── seckill_with_auth.lua  # 带认证秒杀压测
    └── analyze.lua            # 结果分析
```

**unit_test.sh：**
```bash
#!/bin/bash
go test -v -cover ./service/...
go test -v -cover ./handler/...
```

**benchmark.sh：**
```bash
#!/bin/bash
echo "Running benchmark..."
go test -bench=. -benchmem -count=5 ./service/...
```

### 11.8 项目结构测试目录

```
seckill/
├── service/
│   ├── seckill.go
│   └── seckill_test.go     # 单元测试
├── handler/
│   ├── seckill.go
│   └── seckill_test.go     # 集成测试
├── repository/
│   ├── redis.go
│   └── redis_test.go       # 依赖模拟
├── scripts/
│   ├── wrk/
│   │   ├── seckill.lua
│   │   └── analyze.lua
│   └── test_*.sh
└── test/
    ├── integration/        # 集成测试
    └── benchmark/         # 性能测试
```

---

## 十二、面试能讲清楚的点

### 12.1 库存超卖问题
- Redis Lua 脚本保证原子性
- MySQL 乐观锁兜底

### 12.2 高并发处理
- Redis 连接池 + MySQL 连接池
- 限流防止刷请求
- goroutine 异步处理非核心流程
- Nginx 负载均衡（预留多实例）

### 12.3 防重复购买
- Redis Set 快速判断
- MySQL 唯一索引兜底

### 12.4 日志和链路追踪
- trace_id 贯穿整个请求生命周期
- 结构化日志（JSON 格式）
- 异步日志写入
- 操作审计日志

### 12.5 Go 并发体现
- sync.WaitGroup：批量预热、批量查询
- sync.Mutex：保护共享变量
- sync/atomic：计数器并发安全
- context：超时控制和请求取消
- 连接池：Redis + MySQL 高并发

### 12.6 运维能力
- Docker 容器化部署
- Graceful Shutdown 优雅退出
- 健康检查接口
- Prometheus Metrics 预留

### 12.7 扩展能力预留
- 分布式锁：预留 Lock 接口
- MQ：预留消息发送接口
- 配置中心：预留热更新接口
- API 版本：v1/v2 向后兼容

---

**请直接修改上述内容，我会根据你的改动重新整理。**
