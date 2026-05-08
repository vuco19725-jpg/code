# 秒杀系统 - 实习面试问题

---

## Q1: 请用2分钟介绍一下你这个项目的整体架构和核心流程。

### 整体架构

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   用户端     │     │   网关层     │     │  业务层      │
│  (HTTP)     │────▶│  (Gin)      │────▶│ (Service)   │
└─────────────┘     │  - JWT认证   │     └─────────────┘
                    │  - 限流      │            │
                    │  - 熔断      │            ▼
                    └─────────────┘     ┌─────────────┐
                                         │  数据层      │
                                         │ MySQL+Redis │
                                         └─────────────┘
```

### 核心流程（秒杀）

1. **预热阶段**：管理员创建商品 → 库存预热到Redis
2. **抢购阶段**：用户浏览商品列表 → 秒杀下单 → Redis原子扣减库存
3. **订单阶段**：库存扣减成功 → 异步创建订单 → 库存同步MySQL

### 核心亮点

| 特性 | 说明 |
|------|------|
| 库存分桶 | 减少Redis单key热点竞争 |
| 熔断器 | 失败率过高时自动熔断，保护系统 |
| 降级策略 | Redis故障时降级到MySQL |
| 异步下单 | 秒杀成功后异步创建订单 |

---

## Q2: 你的项目用到了哪些技术栈？为什么选择这些技术？

| 技术 | 用途 | 选型理由 |
|------|------|----------|
| **Gin** | HTTP框架 | 轻量、高性能、Go生态主流 |
| **GORM** | ORM | 简洁、支持自动迁移 |
| **Redis** | 缓存/库存 | 高并发下原子操作，适合扣减场景 |
| **MySQL** | 持久化 | 事务保证数据一致性 |
| **JWT** | 认证 | 无状态、适合分布式 |
| **熔断器** | 限流保护 | 已集成熔断器模式 |

**为什么选择Go？**
- 高并发支持好，适合秒杀场景
- 性能接近C，语言简洁效率高
- 丰富的Redis/MySQL客户端支持

---


---

## Q3: 你的分桶库存是如何设计的？为什么分成4个桶而不是更多？

### 分桶设计

```
总库存 10000 件，分 4 桶
├── 桶1: 2500 件
├── 桶2: 2500 件
├── 桶3: 2500 件
└── 桶4: 2500 件
```

**用户路由**：根据 `userID * skuID` 的 hash 值一致性路由，保证同一用户每次访问同一个桶。

```go
func getBucketForUser(userID uint, skuID uint) int {
    hash := int(userID*31 + skuID*17)
    bucket := (hash % StockBucketCount) + 1  // 1 ~ 4
    return bucket
}
```

### 为什么是 4 个桶？

**不是越多越好，要权衡：**

| 桶数 | 优点 | 缺点 |
|------|------|------|
| 4（当前） | 热点分散效果好，实现简单 | 并发度有限 |
| 100+ | 热点分散极强 | 管理复杂，库存不均匀 |

**当前选择 4 的理由**：
- 够用：4 个桶已经能有效分散热点（4倍并发能力）
- 简单：运维、调试、问题排查成本低
- 经验值：业界常见配置

### 追问：桶过少/过多的问题

**桶过少（1-2个）**：
- 单 key 热点竞争严重，性能瓶颈
- 相当于没用分桶

**桶过多（100+）**：
- 每个桶库存变少，可能出现"桶售罄但其他桶有货"的情况
- 库存分配不均，部分用户请求打到空桶直接失败
- 运维复杂度增加

---

## Q4: 你在扣库存时用了 Redis Lua 脚本，能解释一下原子性是如何保证的吗？

### Lua 脚本扣库存

```lua
local stock = redis.call('GET', KEYS[1])
if not stock then return -2 end           -- 桶不存在
if tonumber(stock) <= 0 then return -1 end -- 库存不足
redis.call('DECR', KEYS[1])                -- 扣减库存
return 1                                    -- 成功
```

### 原子性保证

**Redis 单线程执行 Lua 脚本**：
- Redis 执行 Lua 脚本时，整个脚本被当作**单个原子操作**执行
- 执行过程中不会执行其他客户端命令
- 不会出现竞态条件

**类比**：就像数据库的存储过程，整个逻辑一次性执行完，不会被其他请求"插队"。

### 追问：Lua 脚本失败怎么办？

**失败场景**：Redis 执行 Lua 时崩溃、网络中断

**重试机制**（代码中的 `rollbackWithRetry`）：

```go
for i := 0; i < maxRollbackRetries; i++ {
    _, err := s.redis.Eval(ctx, script, []string{stockKey, purchaseKey}).Result()
    if err == nil {
        return  // 成功
    }
    // 指数退避：100ms, 200ms, 300ms
    time.Sleep(time.Duration(i+1) * rollbackRetryDelay * time.Millisecond)
}
```

**回滚 Lua 脚本**（库存+1，删除购买标记）：
```lua
redis.call('INCR', KEYS[1])   -- 回滚库存
redis.call('DEL', KEYS[2])    -- 删除购买标记
return 1
```

**最坏情况**：3次重试都失败 → 记录严重错误，需要人工介入处理

---

## Q5: 你的异步下单 channel 缓冲了 1000 个请求，如果满了会怎样？

### Channel 满的处理

```go
select {
case s.orderChan <- order:
    atomic.AddInt64(&s.asyncSuccess, 1)  // 异步发送成功
default:
    atomic.AddInt64(&s.syncFallback, 1)   // channel满，同步处理
    s.db.Create(order)                     // 同步写入MySQL
}
```

**Channel 满时**：
- `select default` 触发，直接**同步创建订单到 MySQL**
- 不阻塞用户请求，返回成功
- 记录 `syncFallback` 计数器

### 追问：后果和更优雅的方案

**后果**：
- 少量请求降级为同步，性能下降
- 不影响用户体验（请求仍然成功）
- 属于可接受的降级方案

**更优雅的方案**：
1. **增加 channel 缓冲**：但不能无限增加，会积压太多
2. **使用消息队列**：Kafka/RabbitMQ，支持持久化、万级缓冲
3. **限流熔断**：超过阈值直接拒绝，保护系统

**当前方案的优势**：
- 实现简单，无需引入外部依赖
- 足够应对秒杀场景的瞬时高峰
- 有 fallback 机制，不完全阻塞

---

## Q6: 如何保证不超卖？

### 多层保证

| 层级 | 机制 | 说明 |
|------|------|------|
| **Redis 层** | Lua 原子扣减 | `stock > 0` 才 DECR，不会变成负数 |
| **MySQL 层** | 乐观锁 `WHERE stock > 0` | `UPDATE stock - 1 WHERE stock > 0` |
| **用户维度** | 购买标记 `SetNX` | 同一用户不能重复购买 |
| **最终兜底** | Redis/MySQL 双写 | 两边都保证一致性 |

### 核心代码

**Redis Lua 扣库存（第一道防线）**：
```lua
if tonumber(stock) <= 0 then return -1 end  -- 库存<=0 直接返回
redis.call('DECR', KEYS[1])                 -- 扣减
```

**MySQL 降级方案（第二道防线）**：
```go
result := tx.Model(&model.SeckillGoods{}).
    Where("id = ? AND stock > 0", skuID).    -- 乐观锁
    Update("stock", gorm.Expr("stock - 1"))
if result.RowsAffected == 0 {
    return ErrSoldOut
}
```

### 追问：Redis 和 MySQL 都失败了，回滚逻辑？

**场景**：Lua 扣库存成功 → 订单创建失败

**回滚流程**：
```
1. 订单创建失败
2. 调用 rollbackBucketWithRetry()
3. Lua 脚本：INCR 库存 + DEL 购买标记（原子操作）
4. 最多重试 3 次（指数退避）
5. 全部失败 → 记录严重错误，需要人工介入
```

**回滚 Lua 脚本**：
```lua
redis.call('INCR', KEYS[1])   -- 库存 +1
redis.call('DEL', KEYS[2])    -- 删除购买标记
return 1
```

**最坏情况**：3次重试都失败 → 记录日志，人工处理（概率极低）

---

## Q7: 你的熔断降级策略是怎样的？什么时候会触发降级？降级后用户体验是什么？

### 熔断器配置

```go
breaker := utils.NewCircuitBreaker(utils.CircuitBreakerConfig{
    FailureThreshold: 5,        // 5次失败触发熔断
    SuccessThreshold: 3,         // 3次成功恢复
    HalfMaxRequests: 5,         // 半开状态放行5个请求试探
    OpenTimeout:     30 * time.Second,
})
```

### 三态转换

```
                    失败 >= 5 次
        ┌──────────────────────────────┐
        ▼                              │
    ┌────────┐  超时30s   ┌───────────┐  失败   ┌───────┐
    │ Closed │ ─────────▶ │ Half-Open │ ──────▶ │ Open  │
    └────────┘            └───────────┘         └───────┘
        ▲                      │                    │
        │                      │ 成功 >= 3 次        │
        └──────────────────────┘                    │
                                                     │ 超时30s
                                                     ▼
                                                  (回到Half-Open)
```

### 状态说明

| 状态 | 行为 |
|------|------|
| **Closed（正常）** | 全部请求放行，失败累计 |
| **Open（熔断）** | 全部请求拒绝，返回"系统繁忙" |
| **Half-Open（半开）** | 放行5个试探请求，根据结果决定恢复或重新熔断 |

### 触发时机

```go
// 秒杀时检查熔断器
if !h.breaker.Allow() {
    c.JSON(http.StatusServiceUnavailable, utils.Resp(5002, "系统繁忙，请稍后再试", nil))
    return
}
```

**触发条件**：秒杀请求连续失败 5 次

### 用户体验

| 状态 | 用户看到 |
|------|----------|
| **正常** | 秒杀结果（成功/失败） |
| **熔断** | `"系统繁忙，请稍后再试"` (code: 5002) |
| **降级MySQL** | 仍可秒杀，但性能下降 |

### 降级方案

除了熔断器，代码还有**自动降级**：
- Redis 挂了 → 自动降级到 MySQL 直接扣减
- Channel 满了 → 同步创建订单

---

## Q8: 为什么设计了分桶库存而不是用分布式锁？两者各有什么优缺点？

### 分桶库存 vs 分布式锁

| 方案 | 核心思想 | 适用场景 |
|------|----------|----------|
| **分桶库存** | 分散热点，多桶独立扣减 | 高并发读多写多 |
| **分布式锁** | 互斥访问，串化处理 | 少量并发，需要强一致性 |

### 分桶库存

```go
// 用户路由到固定桶
bucket := (userID*31 + skuID*17) % 4 + 1
// 每个桶独立扣减，互不干扰
redis.call('DECR', KEYS[1])  // 只扣自己的桶
```

**优点**：
- 并发度高：4个桶可以同时处理4倍请求
- 无锁竞争：不需要等待锁，性能好
- 实现简单：普通 Redis 命令即可

**缺点**：
- 库存分配不均：可能桶1卖完但桶3还有
- 实现复杂：需要一致性hash保证用户路由

### 分布式锁（如 Redisson）

```go
// 伪代码
lock := redisson.Lock("seckill:sku:1")
if lock.tryLock()) {
    try {
        // 扣库存
    } finally {
        lock.unlock()
    }
}
```

**优点**：
- 强一致性：同一时间只有一个请求能扣库存
- 库存绝对准确：不会出现超卖

**缺点**：
- 性能差：所有请求串化等待锁
- 锁开销：获取/释放锁有网络开销
- 问题多：死锁、锁超时、锁竞争

### 为什么选择分桶？

**秒杀场景特点**：
- 库存充足（通常几百到几千）
- 请求量极大（万级QPS）
- 允许少量超卖/不均

**分桶刚好满足**：
- 高并发支持
- 足够的一致性（Lua脚本保证原子）
- 性能优秀

---

## Q9: 热卖商品的缓存预热是如何做的？如果 Redis 挂了会怎样？

### 预热流程

```
管理员创建商品
      │
      ▼
  预热库存到Redis（分桶模式）
      │
      ├── 桶1: 2500 件
      ├── 桶2: 2500 件
      ├── 桶3: 2500 件
      └── 桶4: 2500 件
      │
      ▼
  预热商品信息缓存
      │
      ▼
  用户可开始秒杀
```

### 预热代码

```go
// 创建商品时异步预热
go func() {
    h.goodsService.PreHeatStock(goods.ID)
}()

// PreHeatStock 分桶预热
func (s *GoodsService) PreHeatStock(skuID uint) error {
    // 分桶预热库存（Pipeline批量）
    repository.PreHeatStockToBuckets(ctx, skuStr, goods.Stock, 4, 24*time.Hour)
    
    // 预热商品信息
    repository.SetGoods(ctx, skuStr, goodsJSON, 24*time.Hour)
}
```

### Redis 挂了会怎样？

**4 层降级保护**：

| 层级 | Redis挂了 | 表现 |
|------|-----------|------|
| **1. 库存检查** | 无法预热 | 自动从 MySQL 加载并预热 |
| **2. 扣库存** | Lua执行失败 | 降级到 MySQL 直接扣减 |
| **3. 订单创建** | Channel 异步失败 | 同步创建 MySQL 订单 |
| **4. 库存同步** | 同步失败 | 日志记录，后续补偿 |

### 降级到 MySQL

```go
// Seckill 时检查 Redis 是否有预热
if !s.hasStockInRedis(skuID) {
    s.preheatFromMySQL(skuID)  // 自动从 MySQL 加载
}

// Redis 扣库存失败
if err != nil {
    return s.seckillWithMySQL(ctx, userID, skuID, traceID)  // 降级 MySQL
}
```

### 用户体验

| 场景 | 用户体验 |
|------|----------|
| **Redis 正常** | 极快（<10ms） |
| **Redis 挂了** | 降级 MySQL，稍慢（<100ms）但仍能下单 |
| **MySQL 也挂了** | 服务不可用 |

**总结**：即使 Redis 完全挂了，系统仍能通过 MySQL 降级方案正常工作，只是性能下降。

---

## Q10: Redis 的 Pipeline 和 Transaction (MULTI/EXEC) 有什么区别？你在项目里用到了吗？

### 两者对比

| 特性 | Pipeline | Transaction (MULTI/EXEC)mysql |
|------|----------|-------------------------|
| **执行方式** | 批量发送命令，最后一次性获取结果 | 开启事务，排队执行，结果原子提交 |
| **原子性** | 非原子，各自执行 | 原子，所有命令要么全成功要么全回滚 |
| **回滚** | 不支持 | 支持（DISCARD） |
| **监控(WATCH)** | 不支持 | 支持（乐观锁） |
| **网络开销** | 1次RTT（批量） | 1次RTT（批量） |
| **用途** | 批量操作，提升性能 | 需要原子性的场景 |

### Pipeline（项目中有用到）

```go
// 预热库存时分桶批量设置
pipe := Redis.Pipeline()
for i := 1; i <= bucketCount; i++ {
    pipe.Set(ctx, StockBucketKey(skuID, i), stock, expiration)
}
pipe.Exec(ctx)  // 一次RTT发送所有命令
```

**用途**：减少网络 RTT，提升批量操作性能（如预热、批量查询）

### Transaction（项目中有用到）

```go
// MySQL 事务（不是 Redis，但项目用的是 GORM Transaction）
err = s.db.Transaction(func(tx *gorm.DB) error {
    // 扣库存 + 创建订单
    if result := tx.Model(&Goods{}).Where("stock > 0").Update("stock", stock-1); result.RowsAffected == 0 {
        return ErrSoldOut
    }
    return tx.Create(order).Error
})
```

### 为什么不用 Redis Transaction？

**Redis Transaction 局限性**：
- 不支持回滚（只能 DISCARD 放弃整个事务）
- 不支持条件执行（如 Lua 的 `if stock > 0`）
- WATCH 在高并发下性能差

**Lua 脚本替代了 Redis Transaction**：
```lua
-- Lua 脚本实现原子判断+扣减（Redis 单线程保证原子）
if tonumber(stock) <= 0 then return -1 end
redis.call('DECR', KEYS[1])
```

---

## Q11: 你们用到了哪些 Redis 数据结构？订单的幂等性是如何用 Redis 保证的？

### Redis 数据结构使用

| 数据结构 | Key Pattern | 用途 |
|----------|-------------|------|
| **String** | `stock:sku:bucket:N` | 库存数量 |
| **String** | `goods:sku` | 商品信息JSON |
| **String** | `user:seckill:userID:skuID` | 用户购买标记 |
| **String** | `user:token:userID` | 单设备登录Token |
| **String** | `rate:ip:xxx` / `rate:user:xxx` | 限流计数 |
| **String** | `captcha:xxx` | 验证码 |
| **Lua脚本** | - | 原子扣减库存 |

### 订单幂等性保证

**防止重复购买**：使用 `SetNX` 原子操作

```go
// 用户购买标记 Key
func UserSeckillKey(userID, skuID string) string {
    return fmt.Sprintf("user:seckill:%s:%s", userID, skuID)
}

// 秒杀时原子检查+设置
purchaseKey := repository.UserSeckillKey(userStr, skuStr)
success, err := repository.SetNX(ctx, purchaseKey, "1", 24*time.Hour)
// success=true 表示设置成功（未购买过）
// success=false 表示已存在（购买过）
if !success || err != nil {
    return "", ErrAlreadyBought
}
```

**原理**：
- `SetNX` = SET if Not eXists
- Redis 保证原子性：检查和写入是同一个操作
- 设置 24 小时过期，防止用户无法再次购买

### 幂等性多层保证

| 层级 | 机制 | 说明 |
|------|------|------|
| **Redis** | SetNX 用户购买标记 | 第一道防线 |
| **MySQL** | 唯一索引 `UNIQUE(user_id, sku_id)` | 最终兜底 |

```go
// MySQL 订单表有联合唯一索引
Order struct {
    UserID uint `gorm:"uniqueIndex:idx_user_sku"`
    SkuID  uint `gorm:"uniqueIndex:idx_user_sku"`
}
```

---

## Q12: Redis 集群/主从环境下，你们的库存数据一致性如何保证？

### 当前架构

**单机 Redis**（当前），没有主从/集群

```yaml
Redis:
  Host: "localhost"
  Port: 6379
```

### 如果要上 Redis 主从/集群，问题是什么？

| 问题 | 说明 |
|------|------|
| **库存不一致** | 主从延迟期间，主库扣减成功但从库看不到 |
| **超卖风险** | 读写分离时，从库库存不准 |
| **下单失败** | 主库挂了但从库有库存 |

### 一致性保证方案

**方案1：Redis Cluster + 槽迁移（不推荐库存场景）**
- 不同商品不同槽，跨槽操作复杂
- 库存扣减需要强一致性

**方案2：Codis（不推荐）**
- 代理模式，但官方已停止维护

**方案3：主从 + 读始终从主库写（推荐）**

```
库存扣减：始终走主库（强一致）
库存查询：
  - 正常：走主库
  - 降级：走从库（可能短暂不一致）
```

### 项目中的保障

**1. 单线程原子操作**
```lua
-- Lua 脚本在主库执行，保证原子性
redis.call('DECR', KEYS[1])
```

**2. 异步同步 MySQL**
```go
// 秒杀成功后异步同步到 MySQL
go func() {
    s.syncStockToMySQL(skuID)  // 不阻塞用户请求
}()
```

**3. 降级到 MySQL**
```
Redis 不可用 → 降级 MySQL 直接扣减
MySQL 是最终数据源
```

### 结论

**当前方案**：单机 Redis + MySQL 持久化
- Redis 作为高性能缓存
- MySQL 作为最终数据源
- 异步同步保证最终一致

**如果要上集群**：
- 库存操作走主库
- 接受短暂主从延迟（<1s）
- MySQL 作为最终兜底

---

## Q16: JWT Token 过期时间为什么设置 24 小时？有没有考虑过更短的有效期？

### 当前配置

```go
// 生成 Token
ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour))
```

### 为什么是 24 小时？

| 考量 | 说明 |
|------|------|
| **用户体验** | 用户一天内无需重新登录，体验好 |
| **安全性平衡** | 24小时相对合理，既不太长也不太短 |
| **移动端场景** | 手机用户不希望频繁登录 |
| **会话管理** | 配合单设备登录，24小时强制刷新 |

### 更短有效期的问题

| 有效期 | 问题 |
|--------|------|
| **1小时** | 用户体验差，秒杀中途需要重新登录 |
| **15分钟** | 无法接受，需要频繁刷新Token |

### Token 刷新机制

**当前方案**：单设备登录 + Token 持久化

```go
// 登录时：删除旧 Token，设置新 Token
repository.SetWithExpire(ctx, repository.UserTokenKey(userIDStr), token, 24*time.Hour)

// 验证时：检查 Token 是否与 Redis 中的一致
storedToken, err := repository.GetString(c.Request.Context(), repository.UserTokenKey(userIDStr))
if storedToken != tokenString {
    c.JSON(401, "账号已在其他设备登录")  // 被挤下线
}
```

**密钥轮换**：
```go
// 支持双密钥验证（新密钥 + 旧密钥）
keys := [][]byte{manager.current, manager.previous}
```

**更完善的刷新方案**（可扩展）：
```
Access Token: 15分钟有效期
Refresh Token: 7天有效期

用户请求 → Access过期 → 用Refresh换新的Access Token
```

---

## Q17: 你的用户购买限制是基于什么的？如何防止刷单？

### 购买限制机制

**核心**：基于 `userID + skuID` 的 SetNX 原子标记

```go
// 用户购买标记 Key
purchaseKey := fmt.Sprintf("user:seckill:%d:%d", userID, skuID)

// 原子检查+设置
success, err := repository.SetNX(ctx, purchaseKey, "1", 24*time.Hour)
if !success {
    return "", ErrAlreadyBought  // 已购买过
}
```

### 多层防护

| 层级 | 防护 | 说明 |
|------|------|------|
| **1. 购买标记** | SetNX 原子操作 | 同一用户同一商品只能购买一次 |
| **2. MySQL唯一索引** | `UNIQUE(user_id, sku_id)` | 数据库层面兜底 |
| **3. IP限流** | 60次/分钟 | 防止同一IP刷单 |
| **4. 用户限流** | 10次/分钟 | 防止同一用户频繁请求 |

### 追问：如何防止换账号刷单？

**结合 IP + 用户ID双重限制**：

```go
// IP 限流
RateLimitByIP(60, time.Minute)  // 每IP每分钟最多60次

// 用户限流
RateLimitByUser(10, time.Minute)  // 每用户每分钟最多10次秒杀请求
```

**限制逻辑**：
- IP限制：防止同一IP下多个账号
- 用户限制：防止同一用户频繁请求
- 组合：即使换账号，IP限流也能拦住

---

## Q18: 如何防止刷接口的恶意请求？你的限流方案是怎样的？

### 多层限流架构

```
请求进来
    │
    ▼
┌─────────────────┐
│  1. IP 限流     │  60次/分钟/IP
│  60 req/min    │
└────────┬────────┘
         │ 通过
         ▼
┌─────────────────┐
│  2. 用户限流     │  10次/分钟/用户
│  10 req/min    │
└────────┬────────┘
         │ 通过
         ▼
┌─────────────────┐
│  3. 熔断器       │  5次失败触发
│  CircuitBreaker │
└────────┬────────┘
         │ 通过
         ▼
     秒杀逻辑
```

### IP 限流

```go
// IP 限流 中间件
RateLimitByIP(60, time.Minute)

func RateLimitByIP(limit int, window time.Duration) gin.HandlerFunc {
    ip := c.ClientIP()
    key := repository.RateLimitIPKey(ip)
    
    // 原子 INCR + 设置过期
    count, err := repository.IncrWithExpire(ctx, key, window)
    if count > int64(limit) {
        return 429 Too Many Requests
    }
}
```

### 用户限流

```go
// 用户限流 中间件
RateLimitByUser(10, time.Minute)

// 只对已登录用户生效
userID := c.Get("user_id")  // 未登录不限制
```

### 限流熔断保护

**限流器自身也有熔断**（防止 Redis 挂了我们自己被刷爆）：

```go
var rateLimitBreaker = utils.NewCircuitBreaker(utils.CircuitBreakerConfig{
    FailureThreshold: 3,     // Redis 连续3次失败触发熔断
    SuccessThreshold: 2,     // 2次成功恢复
    OpenTimeout:      10 * time.Second,
})
```

### Redis 挂了怎么办？

```go
count, err := repository.IncrWithExpire(ctx, key, window)
if err != nil {
    // Redis 出错时放行（保证请求通过，不阻塞服务）
    c.Next()
    return
}
```

**原则**：限流是为了保护系统，不是为了阻塞用户。Redis 挂了就暂时不限流，保证服务可用。

### 限流效果

| 攻击类型 | 防护效果 |
|----------|----------|
| 单IP高频请求 | IP限流拦住 |
| 多账号刷单 | IP+用户双重限流 |
| 分布式请求 | 只能靠用户限流（IP不同） |
| Redis挂 | 暂时不限流，服务继续 |

---

## Q19: 如果现在有 10 万用户同时抢购 100 件商品，你的系统会怎么处理？最大 QPS 能到多少？

### 10万用户抢购100件商品

**问题分析**：
- 10万用户同时发起请求
- 只有100件库存
- 目标是让100个用户成功买到

### 系统处理流程

```
10万请求同时到达
       │
       ▼
┌─────────────┐
│  1. IP限流   │  60次/分钟/IP
│  过滤大量无效 │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  2. 用户限流  │  10次/分钟/用户
│  过滤频繁请求 │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  3. 熔断器   │  连续失败5次打开
│  故障保护    │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  4. 库存扣减  │  Redis Lua 原子操作
│  100件卖完   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  5. 订单创建  │  异步 Channel
│  100个成功   │
└─────────────┘
```

### 实际压测数据（来自 docs/pressure-test-report.md）

| 指标 | 值 |
|------|-----|
| **QPS** | 3,169 req/s |
| **平均延迟** | 63ms |
| **最大延迟** | 291ms |
| **成功率** | ~5.4%（库存充足时） |

### 10万用户的处理估算

```
10万请求 / 3169 QPS ≈ 31秒 内处理完成
```

| 阶段 | 说明 |
|------|------|
| 限流过滤 | IP限流 + 用户限流拦截大部分 |
| 库存扣减 | Redis Lua 原子扣减，无超卖 |
| 订单创建 | 异步 Channel，1000缓冲 |

### 最大 QPS 评估

| 环境 | QPS |
|------|-----|
| **当前压测** | 3,169 |
| **理论上限** | 5,000 ~ 10,000（受限于 Redis 单线程） |

**Redis 单线程是瓶颈**：
- Redis QPS 理论 10万+
- 但 Lua 脚本执行有开销
- 网络 + Go 的开销

### 100件库存会怎样？

```
库存100件
    ↓
前100个请求抢到
    ↓
后续请求全部返回"库存不足"
    ↓
总请求 = 10万，实际成交 = 100
```

**限流保护了后端**：
- 10万请求不会同时打到 Redis
- 分批处理，每批几千请求

---

## Q20: 如果Redis突然挂了，你的系统还能正常工作吗？会降级到什么状态？

### 4 层降级保护

| 层级 | Redis挂了 | 降级到 | 用户体验 |
|------|---------|--------|----------|
| **1. 库存查询** | 无预热 | 自动从MySQL加载预热 | 稍慢，不中断 |
| **2. 库存扣减** | Lua失败 | MySQL直接扣减 | 降级MySQL，性能下降 |
| **3. 订单创建** | Channel异步失败 | 同步创建MySQL | 稍慢，不中断 |
| **4. 限流** | Redis限流失效 | fail-open，放行请求 | 无保护，但服务可用 |

### 降级流程代码

```go
// 步骤1：检查Redis是否有预热库存
if !s.hasStockInRedis(skuID) {
    s.preheatFromMySQL(skuID)  // 自动从MySQL加载
}

// 步骤2：Redis扣库存失败
result, err := s.redis.Eval(ctx, script, []string{stockKey}).Int()
if err != nil {
    return s.seckillWithMySQL(ctx, userID, skuID, traceID)  // 降级MySQL
}

// 步骤3：Channel满
select {
case s.orderChan <- order:
    // 异步成功
default:
    s.db.Create(order)  // 同步创建
}
```

### 压测验证（来自文档）

**Redis 故障注入测试结果**：
```
响应码: 503 "系统繁忙" (熔断器打开)
平均延迟: 61.68ms
```

**熔断器工作流程**：
```
Redis故障 → 连续失败5次 → 熔断器打开
    ↓
所有请求直接返回 503
    ↓
等待30秒超时
    ↓
熔断器半开，放行试探请求
    ↓
成功3次 → 恢复正常
```

### 降级后的用户体验

| 状态 | 用户体验 |
|------|----------|
| **Redis正常** | 极快 <10ms，秒杀成功/失败立即返回 |
| **Redis挂了（熔断打开前）** | 降级MySQL，稍慢 <100ms，仍能下单 |
| **Redis挂了（熔断打开后）** | 快速返回"系统繁忙"，不阻塞 |

### 总结

**Redis挂了，系统能继续工作**：
- 熔断器保护：快速失败，不堆积请求
- 降级MySQL：保证核心功能可用
- fail-open：限流失效但服务不中断

---

## Q21: 如果让你设计一个防秒杀薅羊毛的机制，你会怎么做？（结合压测文档）

### 薅羊毛的常见手法

| 手法 | 特征 |
|------|------|
| **多账号刷单** | 同一IP多个账号 |
| **机器脚本** | 请求规律，高频率 |
| **黄牛转售** | 秒杀成功后立即转让 |
| **内部作弊** | 员工提前知道活动时间 |

### 结合压测文档的防护方案

**当前系统已有的防护**（来自压测报告）：
```go
// 1. IP限流
RateLimitByIP(60, time.Minute)

// 2. 用户限流
RateLimitByUser(10, time.Minute)

// 3. 熔断器
CircuitBreaker(失败5次触发)

// 4. 购买标记
SetNX(user:seckill:userID:skuID, 1)
```

### 防薅羊毛新增机制

**1. 设备指纹识别**
```go
// 识别模拟请求
fingerprint := c.GetHeader("X-Device-ID")
if isBot(fingerprint) {
    c.JSON(403, "请求被拦截")
}
```

**2. 行为分析**
```
正常用户: 点击 → 犹豫 → 下单 (3-30秒)
机器脚本: 直接秒杀 (0.1秒内)
```
```go
if requestInterval < 100ms {
    captchaRequired = true  // 需要验证码
}
```

**3. 验证码（图形/短信）**
```go
// 秒杀前需通过验证码
if requireCaptcha(c) {
    if !verifyCaptcha(c, captchaID, userInput) {
        return ErrCaptchaFailed
    }
}
```

**4. 实名认证 + 购买限制**
```go
// 实名认证用户才能秒杀
if !user.IsVerified {
    return ErrNeedRealName
}

// 限制每用户购买数量（不仅限1次）
if user.GetPurchaseCount(skuID) >= 2 {
    return ErrPurchaseLimitReached
}
```

**5. 风控系统（进阶）**
```
秒杀前 → 风控引擎评分 → 高风险直接拒绝
              ↓
         低风险 → 继续秒杀
```

| 风控维度 | 说明 |
|----------|------|
| 账号等级 | 新注册账号扣分 |
| 设备指纹 | 模拟器扣分 |
| 行为特征 | 速度异常扣分 |
| 购买历史 | 频繁退货扣分 |

**6. 订单冷却期**
```go
// 秒杀成功后 N 分钟内不可转让
order.CoolingPeriod = 30 * time.Minute
if order.IsInCoolingPeriod() {
    return ErrCannotTransferYet
}
```

### 防护层次总结

| 层次 | 机制 | 效果 |
|------|------|------|
| **入口限流** | IP 60/min + 用户 10/min | 拦住高频机器 |
| **身份验证** | JWT Token + 设备指纹 | 识别真实用户 |
| **行为检测** | 请求间隔 + 验证码 | 识别脚本 |
| **购买限制** | 每用户N件 + 实名认证 | 防止囤货 |
| **熔断兜底** | CircuitBreaker | 故障时快速失败 |
| **订单冷却** | 30分钟不可转让 | 防止黄牛转售 |

### 压测文档中的教训

文档提到的问题：
> **问题**：Redis清理key格式错误导致测试失败
> **教训**：key设计要规范，容易排查

**薅羊毛防护也需要规范**：
- 埋点记录，便于溯源
- 日志要包含：userID、IP、设备指纹、购买结果

---

## Q22: 你的优雅关闭流程是怎样的？为什么这样设计？

### 优雅关闭流程

```
收到 SIGINT/SIGTERM 信号
         │
         ▼
1. 通知请求计数器开始关闭
   └─ 拒绝新请求进入（返回503）
         │
         ▼
2. 等待活跃请求完成
   └─ 基础超时30秒 + 每请求额外2秒（最高60秒）
         │
         ▼
3. 关闭 HTTP 服务
   └─ 停止接收新请求，等待处理中请求完成
         │
         ▼
4. 关闭数据库连接
         │
         ▼
5. 关闭 Redis 连接
         │
         ▼
6. 刷新日志缓冲区
```

### 核心代码

```go
// 1. 通知计数器开始关闭（禁止新请求进入）
requestCounter.BeginShutdown()

// 2. 关闭 HTTP 服务，等待活跃请求完成
ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
srv.Shutdown(ctx)  // 停止接收新请求

// 3. 关闭数据库
repository.CloseDB()

// 4. 关闭 Redis
repository.CloseRedis()

// 5. 刷新日志
utils.Sync()
```

### 为什么这样设计？

| 设计 | 原因 |
|------|------|
| **信号缓冲2** | 避免信号丢失，防止重复关闭 |
| **拒绝新请求** | 不接受新请求，但处理中的请求继续完成 |
| **动态超时** | 30秒基础 + 每请求2秒，不会太短中断请求，也不会无限等待 |
| **按顺序关闭** | HTTP → DB → Redis → Log，避免正在使用的资源被关闭 |
| **日志刷新** | 确保日志写入磁盘，不丢失 |

### 请求计数器

```go
// 新请求进来
if requestCounter.IsShutting() {
    c.JSON(503, "服务正在关闭")  // 拒绝新请求
    return
}
requestCounter.Increment()
defer requestCounter.Decrement()
```

### 设计原则

1. **不中断正在处理的请求**：让它们正常完成
2. **有上限的等待**：不会无限等待，最多60秒
3. **资源按依赖顺序关闭**：后启动的先关闭
4. **记录统计信息**：关闭前记录 total_requests、peak_requests

---

## Q23: 有没有做过性能测试？用什么工具？最大能支持多少并发？

### 压测工具

| 工具 | 用途 |
|------|------|
| **wrk** | HTTP 压测工具 |
| **Redis** | 库存、缓存 |
| **MySQL** | 订单持久化 |

### 压测命令（来自压测文档）

```bash
# 压测配置
wrk -t4 -c200 -d60s -s scripts/wrk-login.lua http://localhost:8080/api/v1/seckill/1

# 参数说明
# -t4: 4个线程
# -c200: 200个连接
# -d60s: 60秒压测
# -s: 使用 Lua 脚本
```

### 压测结果

| 指标 | 值 |
|------|-----|
| **QPS** | 3,169 req/s |
| **平均延迟** | 63ms |
| **最大延迟** | 291ms |
| **总请求数** | ~190,000（60秒） |

### P2优化前后对比

| 指标 | 优化前 | P2优化后 |
|------|--------|----------|
| QPS | ~2,400 | **3,169** |
| 平均延迟 | 85-87ms | **63ms** |
| 最大延迟 | 386ms | 291ms |

### Channel 容量测试

| Channel容量 | QPS | 平均延迟 |
|------------|-----|----------|
| 100 | 1,460 | 145ms |
| **1000** | ~3,500 | ~60ms |

### 最大并发评估

| 场景 | 值 |
|------|-----|
| **wrk 最大并发** | 200 连接 × 4 线程 |
| **理论支持** | 5,000 ~ 10,000 QPS |
| **瓶颈** | Redis 单线程 + MySQL 连接池 |

### 压测配置（限流临时调高）

```go
// 压测时调高限流阈值
seckill.Use(middleware.RateLimitByIP(10000, time.Minute))
seckill.Use(middleware.RateLimitByUser(10000, time.Minute))

// 生产恢复
seckill.Use(middleware.RateLimitByIP(60, time.Minute))
seckill.Use(middleware.RateLimitByUser(10, time.Minute))
```

---

## Q24: 项目中有哪些你觉得设计得不够好的地方？如果重新做你会怎么改进？

### 设计不够好的地方

**1. 库存分桶数量写死**

```go
const StockBucketCount = 4  // 写死了
```

**问题**：不能动态调整，不同商品可能需要不同的桶数

**改进**：
```go
// 改为配置项
type Goods struct {
    BucketCount int `json:"bucket_count"` // 每个商品独立配置
}
```

---

**2. 订单异步 Channel 容量固定**

```go
orderChan: make(chan *model.Order, 1000)  // 固定1000
```

**问题**：高峰期可能不够，低峰期浪费资源

**改进**：
```go
// 动态调整或使用消息队列
type SeckillService struct {
    orderChan chan *model.Order  // 改为接口，支持 Kafka
}
```

---

**3. 缺少消息队列**

```go
// 当前：内存 Channel
orderChan: make(chan *model.Order, 1000)
```

**问题**：
- Channel 满时降级同步，性能下降
- 服务重启未处理订单会丢失

**改进**：
```go
// 使用 Kafka/RabbitMQ
type SeckillService struct {
    orderProducer *kafka.Producer  // 持久化，不丢失
}
```

---

**4. 熔断器是单体，非分布式**

```go
// 当前：单机熔断器
breaker := utils.NewCircuitBreaker(...)
```

**问题**：多实例部署时，各实例熔断状态独立，不能协同

**改进**：
```go
// 使用分布式熔断（如 Sentinel）
// 或 Redis 存储熔断状态
```

---

**5. 缺少完整的监控体系**

**问题**：
- 没有 Prometheus/Grafana
- 没有请求追踪（虽然有 traceID）
- 没有业务指标告警

**改进**：
```go
// 接入监控
prometheus.MustRegister(httpRequestsTotal)
prometheus.MustRegister(httpRequestDuration)

// 添加业务指标
OrderCreatedTotal.Inc()
StockDepletedCounter.Inc()
```

---

**6. 缺少自动化测试**

```go
// 测试都是 TODO
func TestSeckill_Concurrent(t *testing.T) {
    // TODO: 填写测试用例
}
```

**改进**：
```go
// 补充集成测试
func TestSeckill_Concurrent(t *testing.T) {
    // 1000用户并发抢100件商品
    // 验证：100单成功，900单失败，无超卖
}
```

---

**7. 配置管理分散**

```go
// 散落在各处
const StockBucketCount = 4
const maxRollbackRetries = 3
```

**改进**：
```go
// 统一配置中心
type Config struct {
    Stock   StockConfig
    RateLimit RateLimitConfig
    CircuitBreaker CircuitBreakerConfig
}
```

---

### 总结：核心改进方向

| 优先级 | 改进项 | 价值 |
|--------|--------|------|
| **P0** | 引入消息队列 | 解决异步订单丢失问题 |
| **P1** | 动态分桶数量 | 更灵活的资源利用 |
| **P1** | 完善监控告警 | 可观测性 |
| **P2** | 补充集成测试 | 质量保证 |
| **P2** | 分布式熔断 | 多实例协同 |
