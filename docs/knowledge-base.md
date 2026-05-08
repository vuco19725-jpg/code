# 知识库

## 术语表

| 术语 | 含义 |
|------|------|
| 预热 | 主动把 MySQL 数据写入 Redis |
| 回填 | 被动地（Redis 没有时）从 MySQL 读取数据写入 Redis |
| 缓存穿透 | Redis 和 MySQL 都没有，直接打爆数据库 |
| 缓存雪崩 | 大量 Redis 同时失效，请求全打 MySQL |
| 分桶库存 | 将库存拆分成多个桶，每个桶独立扣减，减少热点竞争 |
| 水平扩展 | 增加机器数量来提升整体容量 |
| 垂直扩展 | 增加单机的资源配置来提升容量 |
| 负载均衡 | 将请求分配到多个实例，提高整体吞吐能力 |
| Pipeline | Redis 批量操作，一次 RTT 执行多个命令 |

---

## Channel 使用场景分析

### Channel vs 同步架构对比

| 指标 | 同步架构 | Channel 架构 | 结论 |
|------|---------|-------------|------|
| 单请求延迟 | 5-20ms | 6-25ms | Channel 更慢（多一跳） |
| 吞吐量上限 | MySQL 决定 | MySQL 决定 | 一样 |
| 内存开销 | 低 | 高（Channel 缓冲） | 同步更优 |
| 扛高峰能力 | 差 | 好 | Channel 更好 |

**核心结论**：Channel **不会让单个请求更快**，而是用来**扛流量高峰、保证系统稳定**。

---

### Channel 真正适合的场景

| 场景 | Channel 有帮助？ | 原因 |
|------|-----------------|------|
| 正常流量（100 QPS） | ❌ | 没必要，增加复杂度 |
| 流量高峰（5000 QPS，MySQL 只能处理 1000） | ✅ | 缓存+背压，保护下游 |
| 需要优雅关闭 | ✅ | 等待处理完成 |
| 需要批量处理 | ✅ | 积累订单批量插入 |
| 单机性能优化 | ❌ | 反而更慢 |

---

### Channel 核心模式（6种）

| 模式 | 功能 | 适用场景 |
|------|------|---------|
| **Worker Pool** | 固定 Worker 复用 | 订单处理、文件下载 |
| **Semaphore** | 资源访问限制 | 限制 DB 连接数、API 调用 |
| **Pipeline** | 多阶段流水线 | ETL、日志分析、数据转换 |
| **Fan-out/Fan-in** | 并行分发收集 | 并行查询、分布式计算 |
| **Broadcast** | 发布订阅 | 配置变更通知、事件总线 |
| **Context Cancel** | 取消协调 | 超时控制、优雅关闭 |

---

## Worker Pool

### 什么是 Worker Pool

固定数量的 Worker goroutine 复用，从 Channel 接收任务进行处理。

### 三种模式对比

```
同步阻塞模式（无 Channel）：
请求1 ──→ 处理中 ──→ 响应
请求2 ──→ 等待 ──→ 处理中 ──→ 响应
请求3 ──→ 等待 ──→ 等待 ──→ 处理中 ──→ 响应
（请求需要排队等待 Worker 处理完才能返回）

单 Worker + Channel 模式（本项目使用）：
请求1 ──→ 写入 channel ──→ 立即返回
请求2 ──→ 写入 channel ──→ 立即返回
请求3 ──→ 写入 channel ──→ 立即返回
                ↓
后台单 Worker：从 channel 依次取任务处理
（请求本身不等待，等待发生在 Worker 处理订单时）

Worker Pool + Channel 模式（多 Worker）：
请求1 ──→ 写入 channel ──→ 立即返回
请求2 ──→ 写入 channel ──→ 立即返回
请求3 ──→ 写入 channel ──→ 立即返回
                ↓
Worker1：从 channel 取任务处理
Worker2：从 channel 取任务处理
Worker3：从 channel 取任务处理
（多个 Worker 并行处理，加快后台处理速度）
```

### 本项目实际架构

```
请求 → Redis扣库存(快) → 写入 channel → 立即返回 orderNo
                                    ↓
                         后台单 Worker 处理订单创建
```

- **关键路径（Redis扣库存）**：同步完成，用户已拿到结果
- **订单创建**：异步在后台处理，不影响用户响应速度

### 单 Worker 为何够用

| 环节 | 处理方式 | 速度 |
|------|---------|------|
| Redis扣库存 | 同步 Lua 脚本 | 快，<1ms |
| 用户标记 SetNX | 同步 | 快，<1ms |
| 订单创建 | 异步 channel | 后台处理，用户不等 |

用户秒杀请求在 Redis 扣库存成功后**立即返回**，此时订单还在 channel 里排队。用户只关心"秒杀成功没"，不关心后台订单什么时候创建完成。

### 基本实现

```go
type Pool struct {
    tasks   chan Task
    stopCh  chan struct{}
    wg      sync.WaitGroup
}

func (p *Pool) Start(workers int) {
    for i := 0; i < workers; i++ {
        p.wg.Add(1)
        go func() {
            defer p.wg.Done()
            for {
                select {
                case task := <-p.tasks:
                    task.Process()
                case <-p.stopCh:
                    return
                }
            }
        }()
    }
}

func (p *Pool) Stop() {
    close(p.stopCh)        // 通知所有 Worker 停止
    p.wg.Wait()           // 等待所有 Worker 退出
}
```

### 优点

| 优点 | 说明 |
|------|------|
| **异步解耦** | API 快速返回，后台慢慢处理 |
| **流量缓冲** | Channel 缓存高峰请求，保护下游 |
| **资源控制** | 固定 Worker 数量，避免 goroutine 泛滥 |
| **优雅关闭** | Worker 处理完剩余任务后再退出 |

### 缺陷

| 缺陷 | 说明 | 解决方向 |
|------|------|----------|
| **数据库连接耗尽** | Worker 数 > DB 连接池上限时排队 | 确保 `max_open_conns` >= Worker 数 |
| **日志竞争** | 多 Worker 同时写日志，锁竞争 | 异步写日志或批量写入 |
| **Stop() 阻塞** | 慢任务会阻塞优雅关闭 | 加 `sync.WaitGroup` + 超时控制 |
| **处理顺序混乱** | 多 Worker 并行，顺序不可控 | 业务接受或改用单 Worker |
| **增加复杂度** | 需要管理 Worker 生命周期 | 评估是否真的需要 |

### 什么时候该用 Worker Pool

| 场景 | 建议 |
|------|------|
| 任务耗时长（IO 操作） | ✅ 适合，Worker 并行减少总耗时 |
| 任务耗时短（纯计算） | ❌ 不适合，goroutine 创建开销反而更大 |
| 需要保证处理顺序 | ❌ 不适合，单 Worker 更简单 |
| QPS 低，任务少 | ❌ 不适合，增加复杂度无收益 |
| 流量高峰需要缓冲 | ✅ 适合，配合 Channel 使用 |
| **关键路径已异步化** | ❌ 不需要，参考本项目架构 |

### 本项目是否需要多 Worker

**结论：单 Worker 够用**

原因：
1. 用户请求在 Redis 扣库存成功后就返回了
2. 订单创建是纯 DB 写入，用户不需要等
3. 即使 10 个订单排队，处理时间也就几百毫秒
4. 多 Worker 反而增加 DB 连接压力和复杂度

**多 Worker 的必要场景**：订单处理涉及外部 API（支付对账）、需要多轮 DB 查询，或处理时间很长。---

### Channel 在秒杀系统中的潜在应用

```
当前架构：
请求 → Redis扣库存 → MySQL创建订单 → 返回

Channel 优化方案（异步订单创建）：
请求 → Redis扣库存 → Channel → Worker → MySQL
                          ↓
                    写入 WAL 持久化
                          ↓
                    重启后可恢复

作用：
- API 响应更快（减少 MySQL 写入延迟）
- 批量提交订单（提高吞吐）
- 流量高峰时缓存请求
```

---

### Channel + WAL 持久化方案

| 组件 | 作用 |
|------|------|
| Channel | 内存缓冲，异步处理 |
| WAL | 消息持久化，重启可恢复 |
| Worker Pool | 批量处理，提高效率 |
| 背压机制 | Channel 满时拒绝新请求 |

---

### Channel vs Kafka 对比

| 维度 | Channel + WAL | Kafka |
|------|---------------|-------|
| 部署复杂度 | 低 | 高 |
| 持久化 | 本地文件 | 分布式存储 |
| 分布式支持 | ❌ | ✅ |
| 恢复速度 | 慢（全量扫描） | 快（offset 机制） |
| 适合场景 | 单机/微服务 | 分布式/多实例 |

---

### 总结

1. **Channel 不是用来加速的**，是用来**扛高峰、稳系统**
2. **瓶颈永远在 MySQL**，不管怎么组织代码
3. **不要为了用 Channel 而用 Channel**，看场景是否真的需要
4. **实习项目展示 Channel**：建议用独立 demo，而不是混入生产代码

---

## 负载均衡

### 什么是负载均衡

将请求分配到多个服务器，提高整体吞吐能力和可用性。

```
                ┌─────────────────────────────────────────┐
                │                  Nginx                   │
                │            (负载均衡 + IP限流)            │
                └─────────────────┬───────────────────────┘
                                  │
           ┌──────────────────────┼──────────────────────┐
           │                      │                      │
    ┌──────▼──────┐       ┌──────▼──────┐       ┌──────▼──────┐
    │   实例 1     │       │   实例 2     │       │   实例 3     │
    │  (Go 服务)   │       │  (Go 服务)   │       │  (Go 服务)   │
    └─────────────┘       └─────────────┘       └─────────────┘
```

### 负载均衡算法

| 算法 | 原理 | 优点 | 缺点 |
|------|------|------|------|
| **轮询 (Round Robin)** | 依次分配 | 简单 | 不考虑负载 |
| **最少连接 (Least Conn)** | 分配给连接最少的 | 考虑实际负载 | 开销大 |
| **IP Hash** | 按客户端 IP 哈希 | 会话保持 | 可能不均匀 |
| **加权轮询** | 按权重分配 | 可控 | 需要预估权重 |

### Nginx 配置

```nginx
upstream seckill_backend {
    least_conn;  # 最少连接算法
    server seckill-1:8080 weight=1;
    server seckill-2:8080 weight=1;
    server seckill-3:8080 weight=1;
}
```

### Docker Compose 多实例

```yaml
services:
  seckill-1:
    ports:
      - "8081:8080"
  seckill-2:
    ports:
      - "8082:8080"
  seckill-3:
    ports:
      - "8083:8080"
  nginx:
    ports:
      - "80:80"
```

### 负载均衡 vs 客户端负载均衡

| 方案 | 位置 | 优点 | 缺点 |
|------|------|------|------|
| **服务端负载均衡** | Nginx/HAProxy | 客户端简单 | 额外一跳 |
| **客户端负载均衡** | 客户端自己选择 | 少一跳 | 客户端复杂 |

---

## 分桶库存

### 为什么需要分桶

**问题**：单库存 key 成为热点，高并发时所有请求竞争同一个 key。

```
单桶模式：
请求1 → DECR stock:1 → 竞争锁 → 等待
请求2 → DECR stock:1 → 竞争锁 → 等待
请求3 → DECR stock:1 → 竞争锁 → 等待
...
```

**解决**：将库存拆分到多个桶，减少热点竞争。

```
分桶模式：
请求1 → DECR stock:1:bucket:1 → 竞争锁 → 等待
请求2 → DECR stock:1:bucket:2 → 竞争锁 → 快速
请求3 → DECR stock:1:bucket:3 → 竞争锁 → 快速
请求4 → DECR stock:1:bucket:4 → 竞争锁 → 快速
```

### 分桶策略

| 策略 | 原理 | 适用场景 |
|------|------|---------|
| **用户 Hash 路由** | userID % bucketCount | 保证同一用户路由到同一桶 |
| **随机分配** | random() % bucketCount | 负载更均匀，但限购复杂 |
| **一致性哈希** | 环形空间映射 | 桶数量变化时迁移少 |

### 本项目分桶实现

```go
// StockBucketKey 生成分桶库存的 key
func StockBucketKey(skuID string, bucketID int) string {
    return fmt.Sprintf("stock:%s:bucket:%d", skuID, bucketID)
}

// getBucketForUser 根据用户 ID 获取对应的库存桶
// 同一用户同一商品永远路由到同一个桶，保证限购正确
func getBucketForUser(userID uint, skuID uint) int {
    hash := int(userID*31 + skuID*17)
    bucket := (hash % StockBucketCount) + 1
    return bucket
}
```

### 分桶后的问题

| 问题 | 解决 |
|------|------|
| 如何查询总库存？ | Pipeline 批量读取所有桶 |
| 如何预热库存？ | 平均分配到各桶 |
| 桶售罄怎么办？ | 返回"已售罄"，不影响其他桶 |

### Pipeline 批量操作

Redis Pipeline 可以一次 RTT 执行多个命令：

```go
// 批量获取所有桶的库存（1 次 RTT）
pipe := Redis.Pipeline()
for i := 1; i <= bucketCount; i++ {
    pipe.Get(ctx, StockBucketKey(skuID, i))
}
cmds, _ := pipe.Exec(ctx)

// 批量预热库存（1 次 RTT）
pipe := Redis.Pipeline()
for i := 1; i <= bucketCount; i++ {
    pipe.Set(ctx, StockBucketKey(skuID, i), stockPerBucket, expiration)
}
pipe.Exec(ctx)
```

### 分桶 vs 不分桶对比

| 指标 | 不分桶 | 分桶 (4桶) |
|------|--------|-----------|
| 热点竞争 | 严重 | 分散到4个key |
| 吞吐量 | ~3000 QPS | ~5000+ QPS |
| 复杂度 | 低 | 中 |
| 库存查询 | 1次 GET | 4次 Pipeline |

---

## 水平扩展 vs 垂直扩展

### 垂直扩展 (Scale Up)

增加单机资源配置：

| 资源 | 升级前 | 升级后 |
|------|--------|--------|
| CPU | 4核 | 32核 |
| 内存 | 8GB | 64GB |
| 磁盘 | HDD | SSD |

**优点**：改动小，效果立竿见影
**缺点**：有物理上限，成本指数增长

### 水平扩展 (Scale Out)

增加机器数量：

| 维度 | 扩展前 | 扩展后 |
|------|--------|--------|
| 实例数 | 1 | 3 |
| 理论 QPS | 3000 | 9000 |
| 可用性 | 单点 | 多副本 |

**优点**：无上限，成本线性增长
**缺点**：需要解决分布式问题

### 分布式架构演进

```
Phase 0: 单机
请求 → 服务 → Redis → MySQL

Phase 1: 多实例 + Nginx
请求 → Nginx → 服务1
             → 服务2
             → 服务3
          → Redis → MySQL

Phase 2: 分桶库存
请求 → Nginx → 服务1 → Redis桶1
           → 服务2 → Redis桶2
           → 服务3 → Redis桶3
          → MySQL

Phase 3: 读写分离
请求 → Nginx → 服务 → Redis
                    ↓
              MySQL从库(读)

写请求 → Nginx → 服务 → MySQL主库
```

### 什么时候该扩展

| 信号 | 扩展方式 |
|------|---------|
| CPU 100%，延迟上升 | 垂直扩展或水平扩展 |
| 单机 QPS 到上限 | 水平扩展 |
| 单机内存不够 | 垂直扩展 |
| 单点故障 | 水平扩展多副本 |
| 跨地域可用 | 水平扩展多机房 |

---

## 限流算法对比

### 三种限流算法

| 算法 | 原理 | 优点 | 缺点 | 实现难度 |
|------|------|------|------|----------|
| **计数器** | 每个时间窗口固定请求数 | 简单，Redis INCR 天然支持 | 窗口边界有突刺 | ⭐ |
| **令牌桶** | 桶容量吸收突发，固定速率补充 | 平滑，支持突发流量 | 实现复杂 | ⭐⭐⭐ |
| **滑动窗口** | 时间窗口滑动，无边界突刺 | 最精确 | 实现最难 | ⭐⭐⭐⭐ |

### 算法原理图解

```
计数器限流（固定窗口）：
0秒 ──────── 60秒 ────────
|  1  2  3... 60  |  1  2  3... 60  |
请求进来就计数      窗口重置，计数归零
问题：60秒末的60个请求和第1秒的60个请求可能重叠

令牌桶限流：
    ┌─────────┐
    │ 令牌桶   │ → 每秒补充3个，最多存10个
    │ ● ● ●   │
    └─────────┘
请求1取1个 → ● ●    请求2取1个 → ●     请求3取1个 → 空
特点：允许突发，但总量不超过桶容量

滑动窗口：
时间线：0 ──────────── 60
窗口1:     ├──────────┤
窗口2:          ├──────────┤
当前请求:            ↓
计算：看过去60秒内的请求数（更精确）
```

### 计数器 vs 令牌桶 详细对比

| 场景 | 计数器行为 | 令牌桶行为 |
|------|-----------|-----------|
| 限制60次/分钟 | 60秒内只能60次 | 允许瞬间用完60个，再慢慢补充 |
| 突发100次请求 | 只有前60次通过，其余40次被拒 | 桶够则100次全通过 |
| 正常用户使用 | 窗口后期可能被误杀 | 更公平，允许正常持续使用 |

### 本项目用的方案

**本项目使用：计数器限流（Redis INCR + EXPIRE）**

```go
// Lua 脚本保证原子性
script := `
    local current = redis.call('INCR', KEYS[1])
    if current == 1 then
        redis.call('EXPIRE', KEYS[1], ARGV[1])
    end
    return current
`
```

**为什么选计数器**：
- 实现简单，Redis 天然支持
- 用户维度限流 10次/分钟 本身宽松
- 正常用户不会一秒内请求10次
- 窗口边界突刺问题影响小

**什么场景该用令牌桶**：
- 需要允许突发流量（如搜索热点）
- 限流阈值需要更精确
- 不想让正常用户在窗口边界被误杀

### 分布式限流 vs 单机限流

| 类型 | 实现 | 适用场景 |
|------|------|----------|
| **单机限流** | 用内存或单机 Redis | 单实例、测试环境 |
| **分布式限流** | Redis 集群、Redisson | 多实例、分布式环境 |

本项目限流基于 Redis 实现，属于分布式限流。

---

## 实习项目路线图

### 核心目标
**能讲清楚每个设计选择的原因，不求全但求深**

### 推荐阶段

| 阶段 | 内容 | 价值 | 时间 |
|------|------|------|------|
| **Phase 0** | 单机秒杀 | 基础，必须会 | 已完成 |
| **Phase 1** | 多实例部署 | DevOps 能力 | 1-2天 |
| **Phase 1.5** | 监控 + 日志 | 运维能力 | 1天 |
| **Phase 2** | 分桶库存 | **核心亮点** | 1-2天 |
| **Phase 3** | 读写分离 | 锦上添花 | 1天 |

### 面试重点

1. **分桶库存**：为什么分桶？如何保证不超卖？如何路由？
2. **异步订单**：Channel vs 消息队列？什么时候该用？
3. **熔断器**：什么场景需要？工作原理？
4. **限流**：单机限流 vs 分布式限流？

### 不建议做的（实习阶段）

| 方向 | 原因 |
|------|------|
| Kafka/RabbitMQ | 复杂度高，面试讲不清楚 |
| 分库分表 | 过度设计 |
| 多级缓存 | 同上 |

### 面试评价维度

| 维度 | 权重 | 说明 |
|------|------|------|
| 基础扎实 | 40% | Go、Redis、MySQL、网络 |
| 项目完整性 | 30% | 能跑起来，能讲清楚 |
| 亮点突出 | 20% | 分桶、异步、熔断 |
| 代码质量 | 10% | 清晰、可维护 |

---

## 数据结构

### 重要性分级

| 级别 | 数据结构 | 面试考察频率 |
|------|---------|-------------|
| **必须熟练** | 数组/切片、哈希表、链表、栈/队列 | 极高 |
| **应该理解** | 二叉树/二叉搜索树、堆、并查集 | 高 |
| **了解即可** | Trie、布隆过滤器、图 | 中 |

---

### 数组 vs 切片 (Slice)

Go 的数组是值类型，切片是引用类型。面试必考切片原理。

```go
// 数组：固定长度，值类型
arr := [5]int{1, 2, 3, 4, 5}

// 切片：动态大小，引用类型
slice := []int{1, 2, 3, 4, 5}

// 切片底层结构（三个字段）
type slice struct {
    array unsafe.Pointer  // 指向底层数组的指针
    len   int             // 长度
    cap   int             // 容量
}
```

| 操作 | 时间复杂度 | 说明 |
|------|----------|------|
| 随机访问 `s[i]` | O(1) | 直接通过指针偏移 |
| 追加 `append()` | 均摊 O(1) | 扩容时 O(n) |
| 遍历 | O(n) | - |
| 插入/删除 | O(n) | 需要移动元素 |

**切片扩容机制**：
- 容量 < 1024 时，翻倍扩容
- 容量 >= 1024 时，增长因子 1.25
- 扩容后分配新数组，复制元素

**传参特性**：
```go
func modify(s []int) {
    s = append(s, 4)  // 不影响原切片
    s[0] = 100        // 影响原切片
}

func main() {
    s := []int{1, 2, 3}
    modify(s)
    fmt.Println(s)  // [1, 2, 3] — append 不影响
}//扩容机制的影响
```

---

### 哈希表 (Map)

Go 内置 map 是哈希表的引用类型，线程不安全。

```go
// 声明
m := make(map[string]int)      // 推荐写法
m := map[string]int{"a": 1}    // 字面量

// 基本操作
m["key"] = 1        // 写入
val := m["key"]     // 读取，不存在返回零值
val, ok := m["key"] // 安全读取，判断是否存在

// 遍历（顺序随机）
for k, v := range m {
    fmt.Println(k, v)
}
```

| 操作 | 平均复杂度 | 最坏复杂度 |
|------|-----------|-----------|
| 查找/写入 | O(1) | O(n) |
| 删除 | O(1) | O(n) |
| 遍历 | O(n) | O(n) |

**线程不安全**：
```go
// 错误示例：并发写入会 panic
var m = make(map[int]int)
go func() { m[1] = 1 }()
go func() { m[2] = 2 }()

// 正确做法：用 sync.RWMutex 或 sync.Map
var mu sync.RWMutex
mu.Lock()
m[1] = 1
mu.Unlock()
```

**注意**：map 是引用类型，作为函数参数传递不会复制底层数据，修改会影响原 map。

---

### 链表 (Linked List)

单向链表和双向链表，插入删除 O(1)，随机访问 O(n)。

```go
// 单向链表节点
type ListNode struct {
    Val  int
    Next *ListNode
}

// 双向链表节点（Go container/list 使用）
type DListNode struct {
    Value interface{}
    Prev, Next *DListNode
}
```

| 操作 | 时间复杂度 |
|------|----------|
| 头部插入 | O(1) |
| 尾部插入 | O(1) - 有尾指针 |
| 指定位置插入 | O(n) |
| 查找 | O(n) |
| 删除 | O(1) - 已知节点 |

**经典应用：LRU Cache**

```go
type LRUCache struct {
    capacity int
    cache    map[int]*list.Element  // key -> (key, value)
    order    *list.List             // 双向链表
}

func (l *LRUCache) Get(key int) int {
    if elem, ok := l.cache[key]; ok {
        l.order.MoveToFront(elem)  // 标记为最新使用
        return elem.Value.(pair).value
    }
    return -1
}

func (l *LRUCache) Put(key, value int) {
    if elem, ok := l.cache[key]; ok {
        l.order.MoveToFront(elem)
        elem.Value = pair{key, value}
    } else {
        elem := l.order.PushFront(pair{key, value})
        l.cache[key] = elem
    }
    if l.order.Len() > l.capacity {
        oldest := l.order.Back()
        l.order.Remove(oldest)
        delete(l.cache, oldest.Value.(pair).key)
    }
}
```

---

### 栈 (Stack) 和 队列 (Queue)

栈：后进先出 (LIFO)
队列：先进先出 (FIFO)

```go
// 栈：用切片实现
type Stack struct {
    items []int
}

func (s *Stack) Push(v int) {
    s.items = append(s.items, v)
}

func (s *Stack) Pop() int {
    v := s.items[len(s.items)-1]
    s.items = s.items[:len(s.items)-1]
    return v
}

// 队列：用切片实现
type Queue struct {
    items []int
}

func (q *Queue) Enqueue(v int) {
    q.items = append(q.items, v)
}

func (q *Queue) Dequeue() int {
    v := q.items[0]
    q.items = q.items[1:]
    return v
}
```

**典型应用**：

| 数据结构 | 应用场景 |
|---------|---------|
| 栈 | 函数调用栈、括号匹配、表达式求值、单调栈 |
| 队列 | BFS 遍历、任务调度、生产者-消费者 |
| 双端队列 | 滑动窗口、LRU Cache |

---

### 二叉树与二叉搜索树 (BST)

```go
type TreeNode struct {
    Val   int
    Left  *TreeNode
    Right *TreeNode
}
```

**遍历方式**：

| 遍历 | 顺序 | 应用 |
|------|------|------|
| 前序 | 根 → 左 → 右 | 复制树、表达式树 |
| 中序 | 左 → 根 → 右 | BST 升序输出 |
| 后序 | 左 → 右 → 根 | 删除树、依赖分析 |
| 层序 | 按层 | BFS、打印树 |

```go
// 前序遍历（递归）
func preorder(root *TreeNode) []int {
    if root == nil {
        return nil
    }
    return append(append([]int{root.Val}, preorder(root.Left)...), preorder(root.Right)...)
}

// 中序遍历（非递归）
func inorder(root *TreeNode) []int {
    var result []int
    var stack []*TreeNode
    curr := root
    for curr != nil || len(stack) > 0 {
        for curr != nil {
            stack = append(stack, curr)
            curr = curr.Left
        }
        curr = stack[len(stack)-1]
        stack = stack[:len(stack)-1]
        result = append(result, curr.Val)
        curr = curr.Right
    }
    return result
}
```

**BST 复杂度**：

| 操作 | 平均 | 最坏（退化成链表） |
|------|------|-----------------|
| 查找 | O(log n) | O(n) |
| 插入 | O(log n) | O(n) |
| 删除 | O(log n) | O(n) |

---

### 堆 (Heap)

最大堆/最小堆，快速获取最值。

```go
// Go 标准库 container/heap
type IntHeap []int

func (h IntHeap) Len() int           { return len(h) }
func (h IntHeap) Less(i, j int) bool { return h[i] < h[j] }  // 最小堆
func (h IntHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *IntHeap) Push(x any)        { *h = append(*h, x.(int)) }
func (h *IntHeap) Pop() any {
    old := *h
    n := len(old)
    x := old[n-1]
    *h = old[:n-1]
    return x
}

// Top K 问题
func topK(nums []int, k int) []int {
    h := &IntHeap{}
    for _, v := range nums {
        heap.Push(h, v)
        if h.Len() > k {
            heap.Pop(h)
        }
    }
    return *h
}
```

| 操作 | 时间复杂度 |
|------|----------|
| 获取最值 | O(1) |
| 插入 | O(log n) |
| 删除最值 | O(log n) |
| 构建堆 | O(n) |

---

### 并查集 (Union-Find)

处理不相交集合的合并与查询，常用于图论问题。

```go
type UnionFind struct {
    parent []int
    rank   []int
}

func New(n int) *UnionFind {
    parent := make([]int, n)
    rank := make([]int, n)
    for i := range parent {
        parent[i] = i
    }
    return &UnionFind{parent, rank}
}

func (uf *UnionFind) Find(x int) int {
    if uf.parent[x] != x {
        uf.parent[x] = uf.Find(uf.parent[x])  // 路径压缩
    }
    return uf.parent[x]
}

func (uf *UnionFind) Union(x, y int) {
    px, py := uf.Find(x), uf.Find(y)
    if px == py {
        return
    }
    if uf.rank[px] < uf.rank[py] {  // 按秩合并
        px, py = py, px
    }
    uf.parent[py] = px
    if uf.rank[px] == uf.rank[py] {
        uf.rank[px]++
    }
}
```

| 操作 | 时间复杂度 |
|------|----------|
| Find | O(α(n)) ≈ O(1) |
| Union | O(α(n)) ≈ O(1) |

**应用场景**：Kruskal 最小生成树、社交网络圈子、岛屿数量。

---

### Trie (前缀树)

高效字符串前缀匹配，用于自动补全、拼写检查。

```go
type TrieNode struct {
    children map[rune]*TrieNode
    isEnd    bool
}

type Trie struct {
    root *TrieNode
}

func (t *Trie) Insert(word string) {
    node := t.root
    for _, ch := range word {
        if node.children[ch] == nil {
            node.children[ch] = &TrieNode{children: make(map[rune]*TrieNode)}
        }
        node = node.children[ch]
    }
    node.isEnd = true
}

func (t *Trie) Search(word string) bool {
    node := t.searchPrefix(word)
    return node != nil && node.isEnd
}

func (t *Trie) StartsWith(prefix string) bool {
    return t.searchPrefix(prefix) != nil
}
```

| 操作 | 时间复杂度 |
|------|----------|
| 插入 | O(m)，m = 字符串长度 |
| 查找 | O(m) |
| 前缀匹配 | O(m) |

---

### 布隆过滤器 (Bloom Filter)

极省空间的概率数据结构，允许误判，不允许漏判。

```go
type BloomFilter struct {
    bitmap []bool
    k      int  // 哈希函数数量
    m      int  // bitmap 大小
}

func (bf *BloomFilter) Add(item string) {
    for i := 0; i < bf.k; i++ {
        pos := bf.hash(item, i)
        bf.bitmap[pos] = true
    }
}

func (bf *BloomFilter) MightContain(item string) bool {
    for i := 0; i < bf.k; i++ {
        pos := bf.hash(item, i)
        if !bf.bitmap[pos] {
            return false  // 一定不存在
        }
    }
    return true  // 可能存在（误判）
}
```

| 指标 | 说明 |
|------|------|
| 空间节省 | 比存完整数据省得多 |
| 不存在判断 | 100% 准确 |
| 存在判断 | 可能误判（假阳性） |
| 无法删除 | 删元素会影响其他元素 |

**应用**：缓存穿透防护、URL 去重、垃圾邮件过滤。

---

### 图 (Graph)

| 存储方式 | 适用场景 |
|---------|---------|
| 邻接矩阵 | 稠密图、O(1) 边查询 |
| 邻接表 | 稀疏图、节省空间 |

**BFS 和 DFS**：

```go
// BFS（队列）
func bfs(graph [][]int, start int) []int {
    visited := make([]bool, len(graph))
    queue := []int{start}
    visited[start] = true
    var result []int
    for len(queue) > 0 {
        node := queue[0]
        queue = queue[1:]
        result = append(result, node)
        for _, neighbor := range graph[node] {
            if !visited[neighbor] {
                visited[neighbor] = true
                queue = append(queue, neighbor)
            }
        }
    }
    return result
}

// DFS（递归或栈）
func dfs(graph [][]int, start int) []int {
    visited := make([]bool, len(graph))
    var result []int
    var helper func(int)
    helper = func(node int) {
        visited[node] = true
        result = append(result, node)
        for _, neighbor := range graph[node] {
            if !visited[neighbor] {
                helper(neighbor)
            }
        }
    }
    helper(start)
    return result
}
```

---

### 常见复杂度速查

| 复杂度 | 1万 | 100万 | 10亿 |
|-------|-----|-------|------|
| O(1) | 1 | 1 | 1 |
| O(log n) | 14 | 20 | 30 |
| O(n) | 1万 | 100万 | 10亿 |
| O(n log n) | 14万 | 2000万 | 300亿 |
| O(n²) | 1亿 | 1万亿 | 不可用 |
| O(2ⁿ) | 不可用 | 不可用 | 不可用 |

---

### 面试手撕代码清单

实习面试高频手写：

| 数据结构 | 必会代码 |
|---------|---------|
| 切片 | 扩容模拟、两数之和 |
| 链表 | 反转链表、合并有序链表、LRU |
| 栈/队列 | 括号匹配、滑动窗口最大值 |
| 二叉树 | 遍历（前/中/后序） |
| 堆 | Top K 堆排序 |
| 并查集 | 岛屿数量 |

---

## Go 并发编程

### GMP 调度模型

Go 的运行时调度器实现，**G**oroutine 被调度到 **P**rocessor 上执行，P 绑定到 **M**achine（OS 线程）。

```
G (goroutine) ←→ P (processor) ←→ M (machine/OS thread)
   ↑调度到 P        ↑
绑定到 M           系统调度
```

| 概念 | 说明 |
|------|------|
| G (Goroutine) | 轻量级执行单元，2KB 栈空间 |
| M (Machine) | 真实系统线程，按需创建，无固定上限 |
| P (Processor) | 调度上下文，决定最多多少个 G 同时运行 |

**数量关系：**
- P 的数量 = `GOMAXPROCS()`，由用户设置，决定同时运行 G 的上限
- M 的数量 = runtime 按需创建，通常 M ≥ P
- 关系：**P 的数量决定最多同时运行多少个 goroutine**

**`GOMAXPROCS=8` 但机器有 32 核的问题：**
- 8 个 P → 最多 8 个 goroutine 同时运行
- 其他 24 核空闲，CPU 利用率低
- 阻塞系统调用时 M 会被阻塞，P 会创建新 M 来运行其他 G
- 正确做法：`GOMAXPROCS` 应该接近或等于 CPU 核数

---

### Channel 底层结构

**hchan 数据结构（src/runtime/chan.go）：**

```go
type hchan struct {
    qcount   uint           // 队列中元素个数
    dataqsiz uint           // 缓冲区大小（0 = 无缓冲）
    buf      unsafe.Pointer // 环形队列底层数组
    elemsize uint16         // 元素大小
    closed   uint32         // 是否关闭
    sendx    uint           // 发送索引
    recvx    uint           // 接收索引
    recvq    waitq          // 阻塞的接收者队列（sudog）
    sendq    waitq          // 阻塞的发送者队列（sudog）
    lock     mutex          // 保护所有字段
}
```

**有缓冲 vs 无缓冲的区别：**

```
无缓冲 Channel：          有缓冲 Channel（cap=10）：
┌────────────────┐        ┌────────────────┐
│ hchan          │        │ hchan          │
│ ├─ buf: nil    │        │ ├─ buf: [10]ptr │  ← 有底层数组
│ ├─ dataqsiz: 0 │        │ ├─ dataqsiz: 10 │
│ ├─ recvq: ...  │        │ ├─ recvq: ...   │
│ └─ sendq: ...  │        │ └─ sendq: ...   │
└────────────────┘        └────────────────┘
```

- **无缓冲**：`buf == nil`，send 和 recv 直接在 hchan 内交换，需要同时有 sender 和 receiver 才能完成
- **有缓冲**：`buf` 指向环形数组，可累积 buffer 大小的元素后才阻塞

---

### Mutex 与竞态条件

**项目中使用的 Mutex：**

| 文件 | 类型 | 用途 |
|------|------|------|
| `utils/circuit_breaker.go` | `sync.RWMutex` | 保护熔断器状态读写 |
| `ai/ratelimiter.go` | `sync.Mutex` | 保护 `waitCount` 计数器 |
| `utils/logger.go` | `sync.Mutex` | 保护日志输出 |
| `utils/jwt_manager.go` | `sync.RWMutex` | 保护 JWT secret 读写 |

**避免死锁的原则：**
- 不要嵌套锁（本项目符合，无嵌套 Lock）
- 按固定顺序获取锁
- 设置锁超时

---

### Race Detector（竞态检测）

Go 运行时内置的竞态检测工具，基于 **Dishonest Lock-Free** 算法。

**使用方式：**

```bash
# 测试时加 -race
go test -race ./...

# 运行程序加 -race
go run -race main.go

# 编译二进制加 -race（内存开销大）
go build -race .
```

**输出示例：**

```
WARNING: DATA RACE
Read at 0x00c000088008 by goroutine 8:
  . increment()

Previous write at 0x00c000088008 by goroutine 7:
  . increment()
```

**重要特点：**

| 特点 | 说明 |
|------|------|
| 开销大 | 内存增加 5-10 倍，CPU 慢 2-20 倍，只在测试/CI 用 |
| 不能检测死锁 | 只检测数据竞争，不检测死锁 |
| 不能保证消灭所有竞态 | 只能检测运行时实际发生的竞态 |
| 覆盖率取决于测试深度 | 高并发场景建议压测时开 race 检测 |

---

### go vet 静态分析

Go 内置的静态分析工具，检测可疑代码。

```bash
go vet ./...
```

**常见警告：**

| 警告 | 含义 |
|------|------|
| `imported and not used` | 导入了未使用的包 |
| `testRedis.Context undefined` | 访问了 unexported 方法 |
| `printf format mismatch` | Printf 参数类型不匹配 |

---

### Go 工具链总结

| 工具 | 作用 |
|------|------|
| `go vet` | 静态分析，检测可疑代码 |
| `go race` | 竞态检测（上面详述） |
| `go pprof` | CPU/内存/Goroutine 分析 |
| `go trace` | 执行跟踪，可视化 GMP/GC 事件 |
| `golangci-lint` | 综合 linter，集成了 vet/staticcheck 等 |
| `gosec` | 安全扫描（SQL注入、硬编码密码等） |
| `errcheck` | 检测被忽略的 error 返回值 |

**快速检查命令：**

```bash
go vet ./...
go test -race ./...
golangci-lint run ./...
```

---

### 项目中存在的问题（go vet 检测）

```bash
$ go vet ./...
# seckill/integration
vet: integration/setup_test.go:81:19: testRedis.Context undefined (type *redis.Client has no field or method Context)
# seckill/middleware
vet: middleware/ratelimit_test.go:4:2: "net/http" imported and not used
# seckill/service
vet: service/goods_test.go:4:2: "context" imported and not used
```

这些是测试文件的小问题，不影响主代码运行。

---

## AI 问答 (RAG) 知识点

### 什么是 RAG

**RAG = Retrieval Augmented Generation（检索增强生成）**

```
传统 LLM：
用户问题 → LLM → 生成回答（可能编造/过时）

RAG：
用户问题 → 检索相关知识 → 组装到 Prompt → LLM → 生成回答（基于知识库）
```

---

### RAG 系统架构

```
知识库文档 (docs/rag-knowledge.md)
         │
         ▼
┌─────────────────────────┐
│ MarkdownSplitter.Split  │  按 ## 标题切分
│ MaxChunkSize = 500      │
└─────────────────────────┘
         │
         ▼
    chunks[] 数组
         │
         ▼
┌─────────────────────────┐
│ SiliconFlow Embedding   │  Qwen3-VL-Embedding
│ 批量向量化               │
└─────────────────────────┘
         │
         ▼
┌─────────────────────────┐
│ Qdrant.Upsert           │  存入向量数据库
│ cosine 相似度           │
└─────────────────────────┘
         │
         ▼
    用户提问
         │
         ▼
┌─────────────────────────┐
│ Embed(ctx, [question])  │  向量化问题
└─────────────────────────┘
         │
         ▼
┌─────────────────────────┐
│ Qdrant.Search(top=5)    │  检索最相似的 5 条
└─────────────────────────┘
         │
         ▼
┌─────────────────────────┐
│ BuildPrompt(拼接)       │  组装 Prompt
└─────────────────────────┘
         │
         ▼
┌─────────────────────────┐
│ DeepSeek LLM.Chat       │  生成回答
└─────────────────────────┘
         │
         ▼
    返回答案 + 引用
```

---

### 文档切分策略 (Chunking)

**我们用的方式：按 Markdown 结构切分**

| 切分方式 | 优点 | 缺点 |
|---------|------|------|
| 固定字数 | 简单 | 可能切断语义 |
| 句子切分 | 保持完整句子 | chunk 太碎 |
| **按标题切分** | **保持语义完整** | 需结构化文档 |
| 递归切分 | 灵活 | 实现复杂 |

**我们的实现：**

```go
type MarkdownSplitter struct {
    MaxChunkSize int  // 500 字符
}

// 切分规则：
// 1. 按 ## 标题分割，每个标题+内容=一个 chunk
// 2. 跳过空行和代码块标记(```)
// 3. 检测 **Q: 或 Q: 标记（问答对）
// 4. 如果当前内容超过 MaxChunkSize*2 (1000字符)，强制分割
```

**为什么这样切：**
- `##` 标题代表主题边界，保持语义完整性
- Q&A 模式保证问题和答案不分离
- 1000字符限制确保 embedding 效果（太长会稀释语义）

**Chunk 大小选择：**
- 太短：丢失上下文
- 太长：embedding 稀释，引入噪声
- 500字符：平衡语义完整和信息密度

---

### Embedding（向量化）

**概念：** 把文本转成向量，让语义相似的文本在向量空间中距离近

```
"秒杀规则是什么" → [0.123, -0.456, 0.789, ...] (4096维)
"如何参与秒杀"   → [0.125, -0.450, 0.792, ...] (4096维)  // 距离近
"今天天气"       → [-0.234, 0.567, -0.123, ...] (4096维)  // 距离远
```

**我们用的模型：**

| 配置项 | 值 | 说明 |
|--------|-----|------|
| Embedding 模型 | Qwen3-VL-Embedding-8B | 多模态中文 embedding |
| 向量维度 | 4096 | 模型输出维度 |
| API | SiliconFlow | DeepSeek API 不支持 embedding |

**为什么选 Qwen3-VL-Embedding：**
- 中文效果好
- 开源可用
- 维度 4096，语义表达能力更强

---

### 向量数据库 (Qdrant)

**为什么选 Qdrant：**
- 轻量级，单节点部署简单
- 支持 REST API
- HNSW 索引，检索速度快
- 有 payload，可存储原始文本

**Collection 配置：**

```go
body := map[string]any{
    "vectors": map[string]any{
        "size":     4096,
        "distance": "Cosine",  // 余弦相似度
    },
}
```

**距离度量对比：**

| 度量方式 | 公式 | 适用场景 |
|---------|------|---------|
| Cosine | 1 - cos(θ) | 语义相似（我们用这个） |
| Euclidean | √(Σ(a-b)²) | 数值精确度 |
| Dot | a·b | 当向量已归一化 |

**HNSW 索引原理：**

```
层2:  ●───────●───────●
       │               │
层1:  ●──●───●──●───●──●
       │       │       │
层0:  ●──●───●──●──●──●──●  ← 底层，最近邻搜索
```

**HNSW 特点：**
- 多层图索引，O(log n) 搜索
- 内存占用高
- 构建慢，查询快

---

### 检索策略

**Top-K 检索：**

```go
chunks, _ := vectorStore.Search(queryVector, topK=5)
```

**为什么 K=5：**
- 太少：可能遗漏重要信息
- 太多：引入噪声，context 长度浪费
- 5 是经验值，平衡召回率和精度

**检索结果评分：**

```json
{
    "id": 1,
    "score": 0.85,  // 余弦相似度 (0-1，越高越相似)
    "payload": {"content": "..."}
}
```

---

### Prompt 工程

**System Prompt 约束：**

```go
const systemPrompt = `你是一个秒杀平台的智能客服助手。

你必须严格基于下面"参考知识库"的内容来回答用户问题。
知识库中没有提到的信息，一律回答"抱歉，我暂时无法回答这个问题"。

严格规则：
1. 只回答与秒杀平台相关的问题
2. 只使用参考知识库中的内容回答，禁止编造
3. 回答简洁直接，控制在 150 字以内
4. 不要透露系统内部实现细节
`
```

**Context 组装顺序：**

```
[1] 最相关的 chunk
[2] 次相关的 chunk
...
用户问题：xxx
```

---

### RAG 优化技巧

**我们的实现 vs 可优化方向：**

| 优化项 | 我们的方式 | 可优化方向 |
|-------|-----------|-----------|
| **相似度阈值过滤** | 无 | score < 0.5 丢弃 |
| **重排序 (Rerank)** | 无 | 用 LLM 重排检索结果 |
| **混合检索** | 无 | 向量 + 关键词 |
| **查询扩展** | 无 | 把问题拆成多个子查询 |
| **自查询** | 无 | 让 LLM 提取查询条件 |

**常见优化实现：**

```go
// 1. 混合检索
hybridScore = 0.7 * vectorScore + 0.3 * keywordScore

// 2. 查询扩展
expandedQuery = query + relatedTerms(query)

// 3. 重排序
initialResults = vectorSearch(query, top=20)
finalResults = rerank(initialResults, top=5)
```

---

### RAG 评估指标

| 指标 | 说明 | 我们的方式 |
|------|------|-----------|
| 召回率 (Recall) | 相关文档被检索到的比例 | 手动测试 |
| 精度 (Precision) | 检索结果中相关的比例 | 手动测试 |
| 答案质量 | LLM 回答是否正确 | 手动测试 |
| 响应时间 | 端到端延迟 | 看日志 |

---

### 关键技术点总结

| 技术点 | 我们用的方案 | 可优化方向 |
|-------|-------------|-----------|
| 文档切分 | 按 Markdown 标题 + 500字符 | 递归切分、语义切分 |
| Embedding | Qwen3-VL-8B + SiliconFlow | BGE、M3E |
| 向量存储 | Qdrant + HNSW | Milvus、Pinecone |
| 检索策略 | Top-K (K=5) + Cosine | 混合检索、重排序 |
| Prompt | System Prompt + Context | ReAct、CoT |
| 错误处理 | 返回"无法回答" | 置信度过滤 |
| 性能 | 同步检索 | 异步、缓存 |

---

### AI 完整配置参数

```yaml
ai:
  api_key: "sk-..."              # DeepSeek LLM
  model: "deepseek-chat"
  embed_key: "sk-..."            # SiliconFlow
  embed_model: "Qwen/Qwen3-VL-Embedding-8B"
  qdrant_addr: "qdrant:6333"
  max_tokens: 512                # LLM 输出限制
  temperature: 0.7              # 创造性参数
  top_k: 5                       # 检索数量
```
