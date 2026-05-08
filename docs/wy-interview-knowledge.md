# 高并发秒杀系统 - 面试知识体系

---

## 零、数据结构与算法

### 0.1 数据结构
**必须掌握：**
- 数组、链表（单向、双向）
- 栈（括号匹配、函数调用栈）
- 队列（循环队列、FIFO）
- 哈希表（哈希函数、冲突解决：拉链法/开放寻址）
- 二叉树、堆、二叉搜索树
- B+Tree（MySQL 索引结构）

**项目对应：**
- Redis 分桶 → 哈希思想（hash(key) % 4）
- Go channel → 队列实现（环形缓冲区）
- MySQL 索引 → B+Tree

---

### 0.2 常用算法
**必须掌握：**
- 二分查找（模板背熟）
- 滑动窗口（最长无重复子串、连续子数组和）
- 排序（快排、归排、堆排）
- BFS/DFS（图遍历、树的遍历）

**拓展：**
- 动态规划（爬楼梯、背包）
- 分治法（二分查找就是分治）
- 贪心算法

**面试常考：**
- 手撕代码：二分查找、滑动窗口、快速排序
- 算法题：括号匹配、LRU Cache、TOP K

---

## 一、Go 语言

### 1.1 基础语法
**必须掌握：**
- 变量声明、切片、map、结构体
- 函数、错误处理（error、defer、panic/recover）
- 接口、类型断言
- 包管理（import、go mod）

**拓展：**
- context 上下文传递
- unsafe 包（了解即可）

---

### 1.2 并发编程（核心）
**必须掌握：**
- goroutine 的创建与调度
- channel：创建、发送、接收、关闭、select
- sync.Mutex、sync.RWMutex
- sync.WaitGroup
- sync.Once（单例模式）
- context.WithCancel/WithTimeout

**拓展：**
- sync.Map（高效并发读写）
- atomic 包（原子操作）
- `golang.org/x/sync/semaphore`（信号量）
- GMP 调度模型原理

**项目对应：**
- `channel` → 异步下单、订单异步写库
- `semaphore` → AI 模块协程池限流
- `sync.Mutex` → 熔断器状态修改
- `atomic` → 请求计数器
- `WaitGroup` → 优雅关闭等待

---

### 1.3 网络编程
**必须掌握：**
- HTTP 协议（请求/响应、状态码、Header）
- Gin 框架（路由、中间件、绑定参数）
- JSON 编解码（json.Marshal/Unmarshal）

**项目对应：**
- Gin 框架 → HTTP 路由、中间件
- SSE 流式输出 → text/event-stream

---

## 二、Redis

### 2.1 数据结构
**必须掌握：**
- STRING（set/get/incr/del）
- HASH（hset/hget/hgetall）
- LIST（lpush/rpop/lrange）
- SET（sadd/smembers/sismember）
- ZSET（zadd/zscore/zrangebyscore）

**项目对应：**
- STRING → 库存桶、用户购买标记、熔断器计数

---

### 2.2 核心特性
**必须掌握：**
- TTL（过期时间）
- Lua 脚本（redis-cli eval）
- Pipeline（管道批量操作）
- 事务（MULTI/EXEC/WATCH）

**项目对应：**
- Lua 脚本 → 库存扣减+回滚原子操作
- Pipeline → 批量查询库存

---

### 2.3 高并发与安全
**必须掌握：**
- 内存淘汰策略（LRU、LFU）
- 缓存击穿、穿透、雪崩
- Redis 分布式锁

**项目对应：**
- 缓存击穿 → 库存桶预热
- 雪崩 → 桶 TTL 随机偏移
- 分布式锁 → 熔断器状态锁

---

### 2.4 应用场景
**必须掌握：**
- 计数器（限流）
- 缓存（热点数据）

**项目对应：**
- 限流 → 用户/IP 计数器
- 缓存 → 商品列表缓存、库存预热

---

## 三、MySQL

### 3.1 基础操作
**必须掌握：**
- DDL（CREATE/ALTER/DROP）
- DML（INSERT/SELECT/UPDATE/DELETE）
- 索引创建与管理
- 事务（BEGIN/COMMIT/ROLLBACK）

**项目对应：**
- 事务 → 订单创建、库存扣减

---

### 3.2 索引
**必须掌握：**
- B+Tree 结构原理
- 主键索引、唯一索引、普通索引
- 联合索引、最左前缀原则
- 索引失效情况（函数、运算、LIKE 开头）

**项目对应：**
- 联合索引 → `(status, start_time, end_time)` 商品列表
- 唯一索引 → `idx_user_sku` 防重复购买

---

### 3.3 锁与事务
**必须掌握：**
- 行锁、表锁
- 共享锁、排他锁
- 乐观锁（version）、悲观锁（for update）
- 死锁与避免

**项目对应：**
- 乐观锁 → 库存扣减（version 字段）
- 唯一索引 → 防重复购买

---

### 3.4 性能优化
**必须掌握：**
- 慢查询日志
- 连接池配置（max_open_conns、max_idle_conns）
- 分页优化

**项目对应：**
- 连接池 → 100 max_open_conns，5分钟生命周期

---

## 四、高并发与分布式

### 4.1 并发基础
**必须掌握：**
- 进程、线程、协程区别
- 并发与并行
- 线程安全、竞态条件
- 同步原语（mutex、semaphore、channel）

---

### 4.2 限流
**必须掌握：**
- 固定窗口限流
- 滑动窗口限流
- 令牌桶算法
- 漏桶算法
- 分布式限流（Redis Lua 实现）

**项目对应：**
- 用户维度 → 每分钟10次
- IP 维度 → 每分钟60次
- Lua 脚本保证原子性

---

### 4.3 熔断与降级
**必须掌握：**
- 熔断器原理（开启/关闭/半开状态机）
- 降级策略（熔断返回默认值）
- 限流 vs 熔断区别

**项目对应：**
- 熔断器 → Redis 连续3次失败触发，10秒后半开
- 降级 → Redis 挂了降级 MySQL 直接扣

---

### 4.4 分布式系统
**了解：**
- CAP 定理（一致性、可用性、分区容忍）
- BASE 理论（基本可用、软状态、最终一致）
- 负载均衡（轮询、随机、哈希）
- 分布式 ID（雪花算法、UUID）

**项目对应：**
- 最终一致 → 订单异步创建，秒杀成功即返回

---

## 五、系统设计

### 5.1 架构原则
**必须掌握：**
- 高可用（无单点）
- 高并发（水平扩展）
- 可扩展（解耦）

---

### 5.2 缓存设计
**必须掌握：**
- Cache Aside（旁路缓存）
- 缓存与数据库一致性

**项目对应：**
- Cache Aside → 商品列表缓存

---

### 5.3 消息队列
**了解：**
- 异步 vs 同步
- 队列削峰
- 消息可靠性

**项目对应：**
- Go channel → 异步下单（轻量级队列）

---

## 六、压测与性能优化

### 6.1 压测基础
**必须掌握：**
- 压测工具（wrk、ab、JMeter）
- QPS（每秒请求数）
- 延迟（P50、P95、P99）
- 吞吐量

**项目对应：**
- wrk 压测 → 3500+ QPS
- P99 延迟 < 100ms

---

### 6.2 压测方法
**必须掌握：**
- 单一链路压测（Redis 扣库存）
- 完整链路压测（秒杀接口）
- 瓶颈定位（top 命令、火焰图、pprof）

**项目对应：**
- 先压 Redis 分桶，验证 3500 QPS
- 再压完整链路，验证端到端性能

---

### 6.3 性能优化
**必须掌握：**
- 连接池调优
- Redis 管道批量操作
- 异步处理减少阻塞

**项目对应：**
- MySQL 连接池 → 100 max_open_conns
- Redis Lua → 减少网络往返

---

## 七、可观测性与稳定性

### 7.1 结构化日志
**必须掌握：**
- trace_id 请求串联
- 日志级别（debug/info/warn/error）
- 采样日志（减少刷屏）

**项目对应：**
- Zap 结构化日志 → trace_id、method、path、status、duration

---

### 7.2 监控与告警
**了解：**
- 请求计数（活跃请求、峰值请求）
- 响应延迟监控
- 熔断器状态监控

**项目对应：**
- RequestCounter → 追踪活跃请求、峰值

---

### 7.3 优雅关闭
**必须掌握：**
- 信号处理（SIGTERM、SIGINT）
- 分层关闭（HTTP → MySQL → Redis）
- 活跃请求等待

**项目对应：**
- 信号通道缓冲 2
- 每层关闭记录日志
- CloseDB()、CloseRedis() 显式关闭

---

### 7.4 健康检查
**必须掌握：**
- `/health` 就绪检查
- `/health/ready` 深度检查（Redis、MySQL）

**项目对应：**
- 启动时检查依赖
- K8s 存活探针/就绪探针

---

### 7.5 自动恢复
**必须掌握：**
- 连接断开重连
- 熔断器自动恢复
- 库存桶过期自动预热

**项目对应：**
- MySQL 健康检查 → 每30秒 Ping
- Redis 熔断 → 10秒后半开试探

---

## 八、DevOps 与部署

### 8.1 Docker
**必须掌握：**
- docker-compose（MySQL、Redis、Qdrant、服务）
- 数据卷挂载
- 容器管理（run/start/stop/exec）

**项目对应：**
- docker-compose.yml → 一键启动全链路

---

### 8.2 Linux 基础
**必须掌握：**
- 进程管理（ps/pkill）
- 网络诊断（curl/netstat）
- 日志查看（tail/grep）
- nohup 后台运行

**项目对应：**
- nohup ./seckill > seckill.log 2>&1 &

---

## 九、安全

### 9.1 接口安全
**必须掌握：**
- JWT 认证（Token 校验、过期时间）
- 限流防刷（用户/IP 维度）
- 敏感信息脱敏（手机号、邮箱）

**项目对应：**
- JWT → 24小时有效期
- 限流 → 用户每分钟10次、IP每分钟60次
- Zap 脱敏 → MaskPhone/MaskEmail

---

## 十、AI 知识库（RAG）

### 10.1 向量数据库
**必须掌握：**
- 向量检索原理（余弦相似度）
- Embedding（文本转向量）
- Qdrant（向量存储+检索）

**项目对应：**
- Qdrant → 存储知识库向量，topK=5 检索
- SiliconFlow Embedding → Qwen3-VL-Embedding-8B（4096维）

---

### 10.2 RAG 知识库
**必须掌握：**
- RAG 流程：文档切分 → Embedding → 向量检索 → 拼接 Context → LLM 回答
- 文档切分策略（按标题、按段落）
- Prompt 工程（system prompt + context + question）

**项目对应：**
- MarkdownSplitter → 按 `##` 标题切分
- BuildPrompt → 组装 system prompt + chunks + question
- topK=5 → 取最相关的 5 个 chunk

---

### 10.3 流式输出（SSE）
**必须掌握：**
- Server-Sent Events（SSE）协议
- 流式响应 vs 非流式（全量 vs 增量）
- Go channel 读取 LLM 增量输出

**项目对应：**
- `/ai/chatSSE` → SSE 流式输出
- `ChatStream` → 返回 `<-chan string`
- Handler 读取 channel 逐字写入 response

---

### 10.4 流量保护
**必须掌握：**
- 信号量控制并发（semaphore）
- 超时控制
- 降级策略

**项目对应：**
- max_concurrency=3 → 最多3个并发调用 LLM
- timeout=10s → 获取令牌超时
- SSE 失败 → 降级到普通接口 `/ai/chat`

---

## 十一、项目核心问题（必须会答）

### Q1：你们怎么防超卖？
> 库存扣减用 Lua 脚本实现原子操作。脚本逻辑：检查库存 > 0 → 减库存 → 设置购买标记。Redis 单线程保证原子，Lua 脚本把多步操作打包执行，避免并发问题。

### Q2：Redis 挂了怎么办？
> 三层降级：1）限流先拦截；2）熔断器检测 Redis 连续失败3次，触发熔断；3）熔断后降级到 MySQL 直接扣库存。保证核心链路不中断。

### Q3：为什么用 Redis 分桶？
> 把库存分成4个不同 key，不同用户扣不同桶，减少单个 key 的并发竞争。配合 Lua 脚本实现原子扣减，支撑 3500+ QPS。

### Q4：怎么测的性能？
> 用 wrk 压测工具，先压 Redis 分桶验证 3500 QPS，再压完整链路验证端到端 P99 < 100ms。用 top 命令定位瓶颈，火焰图分析 CPU 热点。

### Q5：熔断器怎么工作的？
> 连续失败3次触发熔断（开启状态），快速返回失败不访问 Redis。10秒后进入半开状态，试探性放行1个请求。如果成功则关闭熔断器，恢复正常；失败则继续熔断。

### Q6：异步下单怎么实现的？
> 秒杀成功后把订单信息写进 Go channel，主流程立即返回成功。后台 goroutine 从 channel 消费订单，异步写入 MySQL。防止秒杀接口阻塞，快速响应用户。

### Q7：AI 模块怎么做的流式输出？
> LLM 返回增量文本通过 Go channel 接收，Handler 读取 channel 逐字写入 SSE 响应。用 semaphore 控制 LLM 并发数（max_concurrency=3），避免触发限流。前端 SSE 失败自动降级到普通接口。

---

## 十二、面试答题技巧

### 结构化回答
1. **是什么**（知识点定义）
2. **为什么**（解决什么问题）
3. **怎么用**（你项目里怎么实现的）
4. **效果**（性能指标、线上效果）

### 示例
> "我实现了 Redis 分桶库存（是什么），把库存分成4个 key，减少单 key 并发竞争（为什么），配合 Lua 脚本原子扣减（怎么用），压测支撑 3500+ QPS（效果）。"

### 准备画图
- 秒杀链路流程图
- 熔断器状态机
- 限流器算法图
- 系统架构图

---

## 十三、知识优先级（必须 > 拓展）

| 优先级 | 内容 |
|--------|------|
| ★★★ 必须 | Redis Lua、限流、熔断、Go channel、MySQL 索引、事务、库存超卖、异步、压测 |
| ★★ 建议 | 高并发思想、缓存策略、连接池、结构化日志、优雅关闭 |
| ★ 了解 | CAP/BASE、消息队列、设计模式、容器编排 |

---

*最后更新：2026-04-28*