# 项目可能碰见的问题

## 一、数据一致性类问题

### 1.1 库存超卖

| 项目 | 内容 |
|------|------|
| 问题 | 卖出数量 > 实际库存 |
| 代码位置 | [seckill.go:308-366](service/seckill.go#L308-L366), [seckill.go:544-587](service/seckill.go#L544-L587) |

**分析：**

1. **Lua 脚本是原子的**：`GET + DECR` 在 Lua 中单线程执行，分桶内不会超卖
2. **但 Redis 扣减 vs MySQL 订单创建 不是原子的**：
   - 订单创建成功 → 返回订单号 → 异步同步库存到 MySQL
   - 如果在异步同步之前 Redis 数据丢失（如宕机），会导致超卖
3. **回滚机制**：最多 3 次重试（指数退避 100/200/300ms），3 次都失败只记录 ERROR 日志

**结论**：存在理论风险，回滚完全失败时库存永久丢失

---

### 1.2 库存数据不一致

| 项目 | 内容 |
|------|------|
| 问题 | Redis 库存 + MySQL 库存 != 总库存 |
| 代码位置 | [seckill.go:407-415](service/seckill.go#L407-L415) |

**分析：**
```go
go func() {
    if err := s.syncStockToMySQL(skuID); err != nil {
        utils.Error("库存同步MySQL失败", ...)  // 只记录日志，没有重试
    }
}()
```

- 同步失败只记录日志，没有重试
- 没有补偿机制

**结论**：存在问题

---

### 1.3 订单数据不一致

| 项目 | 内容 |
|------|------|
| 问题 | 用户拿到订单号，但订单没创建 |
| 代码位置 | [seckill.go:378-404](service/seckill.go#L378-L404) |

**分析：**
- Channel 满时走 `default` 同步 fallback：`s.db.Create(order)`
- 同步创建失败会回滚库存
- 但如果 db.Create 成功 → 返回 → 进程崩溃 → 事务可能未提交

**结论**：有风险，但相对较小

---

### 1.4 回滚失败导致数据不一致

| 项目 | 内容 |
|------|------|
| 问题 | 用户钱扣了，库存没回滚，订单没创建 |
| 代码位置 | [seckill.go:544-587](service/seckill.go#L544-L587) |

**分析：**
```go
for i := 0; i < maxRollbackRetries; i++ {  // 最多 3 次
    _, err := s.redis.Eval(ctx, script, []string{stockKey, purchaseKey}).Result()
    if err == nil { return }
    time.Sleep(time.Duration(i+1) * rollbackRetryDelay * time.Millisecond)
}
// 所有重试都失败，只记录 ERROR
utils.Error("分桶回滚完全失败，需要人工处理", ...)
```

- 3 次全失败只记录 ERROR，无告警、无补偿
- 库存永久丢失

**结论**：P0 风险

---

## 二、可靠性类问题

### 2.1 Redis 单点故障

| 项目 | 内容 |
|------|------|
| 降级方案 | 已实现 [seckill.go:320-329](service/seckill.go#L320-L329) |
| 性能下降 | Redis 10万 QPS → MySQL 约 1000 QPS |

**结论**：已处理

---

### 2.2 MySQL 单点故障

| 项目 | 内容 |
|------|------|
| 降级方案 | **无**，seckillWithMySQL 也依赖 MySQL |

**结论**：未处理

---

### 2.3 Channel 堆积导致 OOM

| 项目 | 内容 |
|------|------|
| 问题 | Channel 满了，请求堆积，内存爆炸 |
| 代码位置 | [seckill.go:80](service/seckill.go#L80) |
| 容量 | 1000 个订单 |

**分析：**
- Channel 满走同步 fallback
- 同步 fallback 失败会回滚并返回错误
- 但如果请求持续 > 处理速度，Channel 堆积可能导致内存压力

**结论**：存在风险，但有 fallback 保护

---

### 2.4 优雅退出丢单

| 项目 | 内容 |
|------|------|
| 问题 | 停止服务时 Channel 中订单丢失 |
| 代码位置 | [seckill.go:115-133](service/seckill.go#L115-L133) |

**分析：**
```go
case <-s.stopChan:
    remaining := 0
    for order := range s.orderChan {  // drain channel
        if err := s.createOrderWithRetry(order); err != nil {
            // 记录失败
        } else {
            remaining++
        }
    }
    return
```

- 设计上处理了优雅退出，会 drain channel
- **但需要配合外部 SIGTERM 信号处理**：Stop() 被调用后，新请求还会来吗？

**结论**：设计上处理了，但需配合服务层信号处理

---

### 2.5 异步任务没有补偿机制

| 项目 | 内容 |
|------|------|
| 问题 | 库存同步失败，没有重试，没有告警 |
| 代码位置 | [seckill.go:407-415](service/seckill.go#L407-L415) |

**结论**：存在问题，同 1.2

---

## 三、性能类问题

### 3.1 库存同步 MySQL 阻塞

| 项目 | 内容 |
|------|------|
| 问题 | 每个秒杀请求都触发异步同步 |
| 代码位置 | [seckill.go:407-415](service/seckill.go#L407-L415) |

**分析：** goroutine 异步执行，不阻塞请求

**结论**：已处理

---

### 3.2 热点商品分桶不均

| 项目 | 内容 |
|------|------|
| 问题 | 桶1 卖完了，桶2 还有 90% |
| 代码位置 | [seckill.go:50-61](service/seckill.go#L50-L61) |

**分析：**
```go
hash := int(userID*31 + skuID*17)
bucket := (hash % StockBucketCount) + 1  // 1 ~ 4
```

- 算法过于简单，哈希分布不均匀
- 预热分配：`stockPerBucket = totalStock / 4`，余数给前几个桶

**结论**：存在不均风险

---

### 3.3 数据库连接池瓶颈

| 项目 | 内容 |
|------|------|
| 问题 | 并发高时 MySQL 拒绝连接 |
| 配置 | MaxOpenConns = 100 |

**分析：**
- 降级路径：2-3 个 DB 操作/请求（检查订单、扣库存、创建订单）
- 异步同步库存：额外占用连接
- 10000 并发 → 连接池耗尽 → 排队等待 → 超时

**结论**：存在瓶颈

---

### 3.4 Redis 连接池瓶颈

| 项目 | 内容 |
|------|------|
| 问题 | Redis 连接耗尽，请求阻塞 |
| 配置 | PoolSize = 100 |

**结论**：存在瓶颈

---

## 四、安全类问题

### 4.1 重复购买防护依赖 Redis

| 项目 | 内容 |
|------|------|
| 代码位置 | [seckill.go:283-298](service/seckill.go#L283-L298) |

**分析：**
```go
success, err := repository.SetNX(ctx, purchaseKey, "1", 24*time.Hour)
if err != nil {
    return s.seckillWithMySQL(...)  // 降级到 MySQL 检查
}
if !success {
    return "", ErrAlreadyBought
}
```

- Redis 失败 → 降级 MySQL 检查
- 不会重复购买

**结论**：已处理

---

### 4.2 库存预热被绕过

| 项目 | 内容 |
|------|------|
| 代码位置 | [seckill.go:262-276](service/seckill.go#L262-L276) |

**分析：**
```go
if !s.hasStockInRedis(skuID) {
    if err := s.preheatFromMySQL(skuID); err != nil {
        // 预热失败不影响继续流程
    }
}
```

- 无预热 → 自动从 MySQL 加载

**结论**：已处理

---

### 4.3 没有限流防爬虫

| 项目 | 内容 |
|------|------|
| 问题 | 爬虫/脚本刷接口 |
| 当前防护 | 熔断器（5 次失败触发） |

**结论**：防护不足，只有熔断器没有限流

---

## 五、扩展性类问题

### 5.1 单实例瓶颈

| 项目 | 内容 |
|------|------|
| 问题 | 无法水平扩展 |
| 原因 | orderChan 在进程内存中 |

**结论**：确实无法水平扩展

---

### 5.2 分桶数量固定

| 项目 | 内容 |
|------|------|
| 问题 | 4 桶无法动态调整 |
| 代码位置 | [seckill.go:47](service/seckill.go#L47) |

**结论**：确实无法动态调整

---

## 六、运维类问题

### 6.1 监控指标不完善

| 项目 | 内容 |
|------|------|
| 缺少 | 请求延迟分布、慢请求统计 |
| 现有 | asyncSuccess, syncFallback, deadLetter |

**结论**：需要补充

---

### 6.2 日志采样可能漏掉关键错误

| 项目 | 内容 |
|------|------|
| 代码位置 | [logger.go:24-52](utils/logger.go#L24-L52) |
| 采样率 | 每 100 条采样 1 条 |

**分析：**
- `LogWithSampling()` 受采样影响
- 但 `Error()` 直接调用 `logger.Error()`，**不受采样限制**

**结论**：实际上没问题，采样只影响 Info/Warn

---

### 6.3 没有定时任务补偿

| 项目 | 内容 |
|------|------|
| 问题 | 库存同步失败，没有补偿 |

**结论**：存在问题

---

## 问题优先级汇总

| 优先级 | 问题 | 严重程度 | 代码位置 |
|--------|------|----------|----------|
| P0 | 回滚失败数据不一致 | 极高 | [seckill.go:544-587](service/seckill.go#L544-L587) |
| P0 | MySQL 单点故障无降级 | 极高 | [seckill.go:465-542](service/seckill.go#L465-L542) |
| P1 | 库存同步失败无重试/补偿 | 高 | [seckill.go:407-415](service/seckill.go#L407-L415) |
| P1 | MySQL 连接池瓶颈 | 高 | [config/config.go:37](config/config.go#L37) |
| P1 | Redis 连接池瓶颈 | 高 | [config/config.go:59](config/config.go#L59) |
| P2 | 分桶不均 | 中 | [seckill.go:50-61](service/seckill.go#L50-L61) |
| P2 | Channel 堆积 OOM 风险 | 中 | [seckill.go:80](service/seckill.go#L80) |
| P2 | 单实例无法水平扩展 | 中 | [seckill.go:67](service/seckill.go#L67) |
| P3 | 监控指标不完善 | 低 | [seckill.go:669-671](service/seckill.go#L669-L671) |
| P3 | 限流防爬虫不足 | 低 | [seckill.go:46-48](service/seckill.go#L46-L48) |
| P3 | 分桶数量固定 | 低 | [seckill.go:47](service/seckill.go#L47) |

---

## 你的代码已有保护措施

1. **熔断器** ([seckill.go:46-48](service/seckill.go#L46-L48))：防止雪崩
2. **降级到 MySQL** ([seckill.go:465-542](service/seckill.go#L465-L542))：Redis 故障时保底
3. **回滚机制带重试** ([seckill.go:544-587](service/seckill.go#L544-L587))：最多 3 次指数退避
4. **同步 Fallback** ([seckill.go:386-404](service/seckill.go#L386-L404))：Channel 满时同步处理
5. **优雅退出** ([seckill.go:115-133](service/seckill.go#L115-L133))：Drain Channel 处理剩余订单
6. **订单创建重试** ([seckill.go:138-160](service/seckill.go#L138-L160))：最多 3 次
7. **自动预热** ([seckill.go:262-276](service/seckill.go#L262-L276))：无预热时自动加载
8. **重复购买防护** ([seckill.go:283-298](service/seckill.go#L283-L298))：Redis + MySQL 双保险
