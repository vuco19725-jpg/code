# Seckill System

高并发秒杀系统，Go + Redis + Kafka + MySQL。

单实例压测 **32,000+ QPS**，生产模式（含防重复检查）**28,900 QPS**，平均延迟 **6.3ms**。

## 核心特性

- **Lua 原子操作**：防重复购买 + 库存扣减 + 排队状态，一次 Redis 往返
- **分桶库存**：FNV 哈希分散到 4 个 Redis key，降低热点竞争
- **本地售罄缓存**：sync.Map，售罄后零 Redis 开销
- **异步落单**：Go channel + Kafka 解耦，channel 满时 Redis 兜底
- **自动降级**：Redis 故障时限流降级到 MySQL（每秒 ≤ 200 请求）
- **熔断器**：自研滑动窗口，3 次失败 OPEN，60ms 快速失败
- **RAG 智能客服**：Embedding → Qdrant 检索 → DeepSeek LLM 问答

## 快速开始

```bash
docker compose -f deploy/docker-compose.yml up -d
mysql -h 127.0.0.1 -u root -p < scripts/init.sql
SECKILL_PORT=8080 go run main.go
```

## 架构

```
Client → Nginx → Go (Gin) → Redis Lua（库存扣减）
                             → Channel → Kafka → MySQL（订单持久化）
```

## 压测

```bash
wrk -t4 -c200 -d30s -s scripts/wrk-login.lua http://localhost:8080/api/v1/seckill/2
```

## 部署

```bash
bash deploy/deploy.sh
```

## 技术栈

| 组件 | 选型 |
|------|------|
| 语言 | Go |
| Web | Gin |
| 缓存 | Redis + go-redis |
| 消息 | Kafka + sarama |
| 数据库 | MySQL + GORM |
| AI | DeepSeek / Qdrant |
| 部署 | Docker Compose |
