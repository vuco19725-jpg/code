# 高并发秒杀系统设计与优化

## 概述

面向电商场景的高并发秒杀系统，单实例压测达到 **32,288 QPS**（wrk -t4 -c200 -d30s），生产模式（含防重复购买）**28,931 QPS**，平均延迟 **6.3ms**。

从初始 63 QPS 起步，经过 7 轮迭代优化，性能提升 **428 倍**。

## 系统架构

```
用户 → Nginx（反向代理）
        ↓
    Go (Gin) 应用层
        ↓
    ┌── Redis（库存扣减 + 排队状态）
    │   - Lua 原子脚本（防重复 + 扣库存 + 排队）
    │   - 分桶策略（FNV 哈希 → 4 个 Key）
    │   - 本地售罄缓存（sync.Map，零网络开销）
    │
    └── Channel → Kafka（异步落单）
                  → MySQL（订单持久化）
```

## 优化历程

**第一阶段：TCP Redis 优化（WSL2）**

| 阶段 | 优化点 | QPS | 延迟 |
|------|--------|-----|------|
| 初始 | MySQL 直读库存 | 63 | 36ms |
| P1 | 商品缓存到 Redis | 3,169 | 63ms |
| P3 | 非阻塞 Kafka 异步落单 | 14,111 | 14ms |
| A1 | 请求排队 + 前端轮询 | 14,751 | 13.7ms |
| A2 | 移除多余 Redis EXISTS 检查 | 19,826 | 10.5ms |
| A4 | 本地售罄标记缓存 | ~20k（扣库存） | — |

> A4 混合场景均值为 29,662 QPS，其中售罄后本地缓存直接返回达 57k QPS 拉高了均值。
> 上表取扣库存阶段实际值约 20k。

**第二阶段：Unix Socket 优化（Redis 6380）**

| 阶段 | 优化点 | QPS | 延迟 |
|------|--------|-----|------|
| A5 | 切换 Unix Socket（新基准） | 22,928 | 2.15ms |
| A6 | 合并 Lua + SetEx 排队状态 | 27,015 | 7.71ms |
| **A7** | **合并防重复购买进 Lua** | **32,288** | **6.36ms** |

## 核心技术

### 1. 全链路 Lua 原子化

将三次 Redis 操作合并为一次 Lua 调用：

```
改前：SetNX（防重复）→ GET（查库存）→ DECR + SETEX（扣库存+排队）
改后：Lua EVAL（SET NX + GET + DECR + SETEX，一次往返）
```

生产模式 QPS 从 ~16,000 提升至 28,931（+80%）。

### 2. 分桶库存 + 本地缓存

- **FNV 哈希**：同一用户同一商品路由到固定桶，保证一致性
- **4 个桶**：分散 Redis 热点 Key 竞争
- **本地售罄缓存**：sync.Map + 3s TTL，售罄后直接返回，零 Redis 开销

### 3. 四级降级链路

```
Redis 分桶（首选）→ Lua 自动回填（冷启动）
                  → MySQL 直扣（Redis 故障，限流 200/s）
                  → 快速失败（限流超限，60ms 返回）
```

### 4. 异步管道

- Go channel（缓冲 100k）+ Kafka AsyncProducer 解耦 HTTP 与订单持久化
- channel 满时自动切换 Redis List 兜底队列，保证零丢单
- 自研熔断器：3 次失败 OPEN，60ms 快速失败，半开自动恢复

### 5. RAG 智能客服

- 商品文档 → Embedding 向量化 → Qdrant 向量库 → 语义检索
- DeepSeek LLM 问答 + SSE 流式输出
- 信号量限流（最大 3 并发）

## 压测数据

| 场景 | QPS | 平均延迟 | 备注 |
|------|-----|---------|------|
| 单实例（SkipPurchaseCheck） | **32,288** | 6.36ms | 压测模式 |
| 单实例（含防重复检查） | **28,931** | 6.93ms | 生产模式 |
| 3 实例直连（汇总） | 30,154 | ~19ms | 瓶颈在 Redis |
| 3 实例走 nginx | 16,358 | 12.38ms | WSL2 损耗 40% |

## 选型与技术栈

| 组件 | 选型 | 理由 |
|------|------|------|
| 语言 | Go | goroutine 轻量并发，编译型性能 |
| Web 框架 | Gin | 成熟稳定，RESTful 路由 |
| 缓存 | Redis + go-redis | Lua 原子脚本、Pipeline 批处理 |
| 消息队列 | Kafka + sarama | 高吞吐异步订单处理 |
| 数据库 | MySQL + GORM | 事务保障，单机部署简单 |
| 配置 | Viper | 多环境配置管理 |
| 日志 | Zap | 高性能结构化日志 |
| 部署 | Docker Compose | 单机一键部署 |

## 部署

```bash
# 启动依赖
docker compose -f deploy/docker-compose.yml up -d

# 初始化数据库
mysql -h 127.0.0.1 -u root -p < scripts/init.sql

# 启动服务
SECKILL_PORT=8080 go run main.go
```

详见 `deploy/deploy.sh` 一键部署脚本。

## 项目结构

```
├── main.go              # 入口
├── handler/             # HTTP 路由处理器
├── service/             # 核心业务逻辑
├── middleware/          # JWT 鉴权 / 限流 / 追踪
├── model/               # 数据模型
├── repository/          # Redis / MySQL 数据访问
├── ai/                  # RAG 智能客服管线
├── config/              # 配置管理
├── utils/               # 工具库（熔断器、限流器、日志）
├── scripts/             # 数据库初始化、Lua 脚本
└── deploy/              # Docker Compose / Nginx 配置
```
