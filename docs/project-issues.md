# 项目缺点分析

---

## 已解决的问题

### 1. JWT Secret 管理不当
- **问题**：开发环境密钥明文存储在配置文件中
- **解决**：使用环境变量 `${JWT_SECRET}`，启动时校验密钥有效性，添加密钥管理器支持轮换

### 2. 全局 Context 使用风险
- **问题**：`repository/redis.go` 使用全局 `context.Background()`，无法实现请求级超时控制
- **解决**：所有 Redis 函数添加 `ctx context.Context` 参数，从 handler 传递 `c.Request.Context()`

### 3. 库存扣减竞态条件
- **问题**：Lua 脚本扣库存，但回滚操作非原子
- **解决**：方案 D+I（乐观锁 + 熔断保护），使用 Lua 脚本实现回滚的原子性

### 4. 用户购买检查非原子性
- **问题**：`Exists` 检查 + `SetNX` 标记之间存在竞态窗口
- **解决**：删除多余的 `Exists` 预检，直接使用 `SetNX` 作为原子检查+标记

### 5. 依赖注入不规范
- **问题**：`NewGoodsService` 内部直接使用 `repository.DB`，无法单元测试
- **解决**：通过构造函数注入 `*gorm.DB` 依赖

### 6. PreHeatStock 无 recover 保护
- **问题**：`go h.goodsService.PreHeatStock(goods.ID)`  goroutine  panic 会导致崩溃
- **解决**：添加 recover 捕获

### 7. 日志封装问题
- **问题**：日志没有真正的等级区分，内容冗余
- **解决**：使用 Uber Zap 结构化日志

### 8. 缺少退出登录功能
- **问题**：没有提供用户主动登出的接口
- **解决**：实现退出登录，删除 Redis 中的用户 Token

### 9. 数据库查询效率低
- **问题**：分页查询每次都执行 COUNT 操作，无缓存，缺少索引
- **解决**：
  - 添加 Redis 缓存（列表 30 秒，total 30 秒）
  - 添加联合索引 `(status, start_time, end_time)` 加速查询
  - 新增 `GetGoodsListSimple` 用于管理后台（无缓存）

### 10. 限流实现非原子性
- **问题**：`Incr` + `Expire` 两步操作非原子，高并发时 key 可能未设置过期时间
- **解决**：使用 Lua 脚本实现 `IncrWithExpire` 原子操作，预加载脚本提升性能

### 11. 连接池配置不合理
- **问题**：固定连接池大小（100/50），连接生命周期过长（1小时），缺少健康检查
- **解决**：
  - 连接池配置化（`max_open_conns`、`max_idle_conns`、`conn_max_lifetime`）
  - 缩短连接生命周期（5分钟，小于 MySQL `wait_timeout`）
  - 添加空闲连接存活时间（`conn_max_idle_time`：3分钟）
  - 启动后台健康检查（每30秒 Ping）

### 12. 错误处理不完善
- **问题**：回滚操作本身可能失败，无重试机制，缺少补偿事务设计
- **解决**：
  - Lua 脚本保证回滚原子性（已有）
  - 添加重试机制（最大 3 次，指数退避：100ms, 200ms, 300ms）
  - 回滚完全失败时记录严重错误日志（需人工介入）

### 13. 日志系统不完善
- **问题**：缺少日志级别动态调整、缺少采样机制、敏感信息未脱敏
- **解决**：
  - 添加 `SetLogLevel()` 动态调整日志级别（支持 debug/info/warn/error）
  - 添加 `LogWithSampling()` 采样日志（减少高频日志刷屏）
  - 添加 `SensitiveField()` 敏感信息脱敏字段
  - 添加 `MaskPhone()`、`MaskEmail()`、`MaskIDCard()` 脱敏工具函数

### 14. Handler 中分页逻辑重复
- **问题**：多个 Handler 中存在重复的分页解析代码
- **解决**：提取公共方法 `ParsePagination()` 到 `utils/pagination.go`，统一分页参数校验

### 15. 库存回填竞态风险
- **问题**：`result == -2` 时直接使用 `SetEx` 回填 Redis 库存，未加锁保护。高并发下多个请求同时回填可能导致库存不一致
- **解决**：使用 `SetNX` 代替 `SetEx`，只有 key 不存在时才能回填。回填失败时重新执行 Lua 扣库存

### 16. 退出机制不够优雅
- **问题**：信号处理单一、超时后强制退出、资源未清理、后台 goroutine 未通知
- **解决**：
  - 信号通道缓冲增加到 2（避免丢失信号）
  - 记录收到的退出信号类型
  - 分层关闭：HTTP → MySQL → Redis
  - 每层关闭后记录日志
  - 添加 `CloseDB()` 和 `CloseRedis()` 显式关闭连接

### 17. 订单号可能重复
- **问题**：`generateOrderNo()` 使用 `time.Now().UnixNano()%1000000`，同一毫秒内并发请求可能产生相同订单号
- **解决**：使用 `uuid.New().String()[:8]` 替代纳秒取模，保证全局唯一性

### 18. context.WithTimeout 使用错误
- **问题**：`cancel == nil` 判断永远为 `false`（`cancel` 总是非 `nil`），导致代码永不执行且 context 泄漏
- **解决**：改为 `defer cancel()` 正确释放资源

### 19. 类型断言可能 panic
- **问题**：`userID.(uint)` 直接类型断言，在类型不匹配时会导致 panic
- **解决**：使用 comma-ok 模式进行安全类型断言，返回 401 状态码

### 20. JWT Secret 缺少最小长度检查
- **问题**：`InitSecret` 只检查空字符串，dev 环境无长度限制，弱密钥可能绕过校验
- **解决**：在 `InitSecret` 中添加最小 16 字符长度检查

### 21. 限流器无熔断保护
- **问题**：限流中间件 Redis 出错时直接放行所有请求，高并发场景下可能导致服务雪崩
- **解决**：添加限流专用熔断器，连续失败3次触发熔断，10秒后半开试探恢复

### 22. Redis 连接池配置硬编码
- **问题**：`PoolSize` 和 `MinIdleConns` 硬编码为 100/10，不适合动态调整
- **解决**：将连接池配置加入 `RedisConfig`，支持配置文件管理

### 23. 优雅关闭缺少请求等待机制
- **问题**：Graceful Shutdown 只等待 30 秒，没有追踪正在处理的请求数量，无法主动通知正在进行的请求
- **解决**：添加请求计数器追踪活跃请求，动态计算超时时间，记录峰值和总请求数

### 24. 缺少 API 请求/响应日志中间件
- **问题**：无法追踪每个请求的处理时间、参数和响应状态，排查问题时缺乏依据
- **解决**：添加 `RequestLoggerMiddleware`，记录 trace_id、method、path、params、status、duration、client_ip

### 25. RequestCounter.shutting 字段类型错误
- **问题**：`shutting` 声明为 `bool` 类型，但使用了 `atomic.StoreInt64/LoadInt64` 进行原子操作，类型不匹配导致编译错误
- **解决**：将 `shutting bool` 改为 `shutting int64`（0=否, 1=是），并修改相关原子操作

### 26. 秒杀成功但订单列表显示"暂无订单"
- **问题**：后端异步 channel 模式，订单通过 channel 异步写库。秒杀接口扣完 Redis 库存后立即返回成功，但订单还没写入数据库，前端轮询查询订单列表时查不到
- **解决（前端）**：秒杀成功后立即调用 `renderOptimisticOrder()` 渲染订单（乐观更新），同时在后台轮询订单落库（最多 15 秒）后刷新为真实数据

### 27. 商品突然显示"已售罄"，刷新后可能恢复
- **问题**：Redis 库存桶有 24 小时 TTL，桶过期后 `GetTotalStockFromBuckets` 返回 0。商品列表的缓存命中路径没有做 MySQL 降级，直接用 0 作为库存显示"已售罄"
- **解决（后端）**：`service/goods.go` 缓存命中路径增加 MySQL 降级：Redis 故障或桶不存在时使用 MySQL 原始库存

### 28. 秒杀重复购买导致死循环（数据库唯一索引冲突）
- **问题**：Redis 购买标记过期（24h TTL）后，同一用户可以再次发起秒杀。Redis 检查通过后返回成功，但异步 worker 写库时触发 `Duplicate entry '3-2' for key 'orders.idx_user_sku'` 唯一索引冲突，3 次重试失败后回滚 Redis。用户再次尝试，形成死循环
- **解决（后端）**：`service/seckill.go` 在 Redis 购买标记检查之后，增加数据库级别的重复购买检查，发现订单已存在时立即返回 `ErrAlreadyBought` 并清理 Redis 标记

### 29. AI 问答 SSE 流式输出内容截断
- **问题**：AI 流式回答被截断，内容不完整（如 "每个购1件" 而非 "每个用户每种商品限购1件"）
- **根因**：
  1. Service 层有两个 goroutine 同时读 `streamCh`，每个 token 只被其中一个收到，导致内容丢失
  2. 服务端每次循环内 `Flush()` 导致 TCP 包分割，完整行可能被拆到不同包
  3. 客户端解析时用 `split('\n')` 无法处理截断的行
- **解决**：
  1. 删除 Service 层的 goroutine 空消费逻辑，改为 `defer resp.Release()` 在 SSE 发完后手动释放限流令牌
  2. 服务端改为循环结束后再 `Flush`，避免 TCP 包分割
  3. 客户端改用 `indexOf('\n')` 逐行解析，累积 buffer 直到 JSON 完整

### 30. AI 回答内容不完整（max_tokens 过小）
- **问题**：LLM 生成回答被截断
- **根因**：`max_tokens` 设置为 512，token 预算不足
- **解决**：调大到 2048

### 31. 普通接口返回 references 浪费带宽
- **问题**：前端不显示引用，但普通接口 `/ai/chat` 仍返回完整的 chunk 内容，数据量过大
- **解决**：普通接口 response 只返回 answer 字段，去掉 references

### 32. SSE 接口传输 refs 浪费资源
- **问题**：前端已隐藏"查看引用"按钮，但 SSE 接口仍先发送 refs 数据
- **解决**：SSE 接口去掉 `refs` 传输，只发送 LLM 增量文本

### 33. 前端 SSE 失败无降级
- **问题**：如果 SSE 接口失败，前端无备用方案
- **解决**：前端 SSE 失败时自动降级到普通接口 `/ai/chat`

---

## 待解决的问题

### 1. 库存同步与容灾问题

**问题描述：**

当前秒杀系统存在以下三个核心问题：

#### 1.1 库存同步问题
- **现象**：秒杀成功后 Redis 库存减少，但 MySQL 库存没有更新
- **影响**：如果 Redis 重启或预热失败，系统会回退到 MySQL 的旧库存数据，导致数据不一致
- **根因**：秒杀成功后只扣减了 Redis 库存，没有同步更新 MySQL

#### 1.2 熔断器缺乏降级方案
- **现象**：当 Redis 故障或连续失败时，熔断器触发后直接拒绝请求
- **影响**：用户看到"系统繁忙，请稍后再试"，无法完成秒杀
- **根因**：当前熔断器只有"快速失败"策略，没有降级到备用方案的逻辑

#### 1.3 必须预热才能秒杀
- **现象**：如果商品没有预热到 Redis，无法进行秒杀
- **影响**：新商品或 Redis 重启后，需要手动触发预热才能开始秒杀
- **根因**：系统没有实现"懒加载"预热逻辑

**解决方案：**

改造秒杀核心逻辑，实现**自动降级 + 数据同步**：

```
Seckill(ctx, userID, skuID)
  │
  ├─► hasStockInRedis(skuID)?
  │     ├─ NO  → preheatFromMySQL(skuID)  // 自动从MySQL加载到Redis
  │     └─ YES → 继续
  │
  ├─► seckillWithRedisBucket(ctx, userID, skuID)  // 尝试Redis分桶秒杀
  │     ├─ 成功 → syncToMySQL(skuID)  // 同步MySQL
  │     └─ 失败(Redis挂了) → seckillWithMySQL(ctx, userID, skuID)  // 降级MySQL秒杀
  │
  └─► syncToRedis(skuID)  // 确保Redis和MySQL一致
```

**需要添加/修改的方法：**

| 方法 | 功能 |
|------|------|
| `hasStockInRedis(skuID)` | 检查Redis是否有库存 |
| `preheatFromMySQL(skuID)` | 从MySQL加载并预热到Redis |
| `seckillWithMySQL(...)` | MySQL直接扣减（降级方案） |
| `syncToMySQL(skuID)` | Redis库存同步到MySQL |
| `syncToRedis(skuID)` | MySQL库存同步到Redis |

**效果对比：**

| 场景 | 修改前 | 修改后 |
|------|--------|--------|
| 正常预热 + Redis正常 | Redis分桶秒杀 | Redis分桶秒杀 → 同步MySQL |
| 未预热 | 无法秒杀 | 自动从MySQL加载 → 继续秒杀 |
| Redis挂了 | 返回"系统繁忙" | 降级MySQL直接扣减 → 继续服务 |
| MySQL也挂了 | - | 返回"系统繁忙" |

**影响分析：**
- 对秒杀性能：**无负面影响**（正常路径不变）
- 对数据一致性：**大幅提升**（双向同步）
- 对系统可用性：**大幅提升**（自动降级）