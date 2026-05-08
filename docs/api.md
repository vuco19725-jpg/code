# 秒杀系统 API 文档

## 基础信息

- **Base URL**: `http://localhost:8080`
- **认证方式**: JWT Token（部分接口需要）
- **Headers**: `Authorization: Bearer <token>`

---

## 目录

1. [健康检查](#1-健康检查)
2. [用户模块](#2-用户模块)
3. [商品模块](#3-商品模块)
4. [秒杀模块](#4-秒杀模块)
5. [订单模块](#5-订单模块)
6. [AI 模块](#6-ai-模块)
7. [管理后台](#7-管理后台)
8. [监控接口](#8-监控接口)

---

## 1. 健康检查

### 1.1 健康检查

`GET /health`

响应:
```json
{"code": 0, "msg": "success", "data": {"status": "ok"}}
```

### 1.2 就绪检查

`GET /health/ready`

检查服务是否就绪（数据库、Redis连接正常）。

---

## 2. 用户模块

### 2.1 用户注册

`POST /api/v1/user/register`

**Body:**
| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| phone | string | 是 | 手机号（11位） |
| password | string | 是 | 密码（至少6位） |

**响应:**
```json
{"code": 0, "msg": "success", "data": {"user_id": 1}}
```

**错误码:** 1001=参数错误, 1002=用户已存在, 5001=系统错误

### 2.2 用户登录

`POST /api/v1/user/login`

**Body:**
| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| phone | string | 是 | 手机号（11位） |
| password | string | 是 | 密码 |

**响应:**
```json
{"code": 0, "msg": "success", "data": {"token": "eyJ...", "user_id": 1}}
```

**错误码:** 1001=参数错误, 1003=用户不存在, 1004=密码错误

### 2.3 用户登出

`POST /api/v1/user/logout` (需要JWT)

**响应:**
```json
{"code": 0, "msg": "success", "data": null}
```

### 2.4 获取用户信息

`GET /api/v1/user/info` (需要JWT)

**响应:**
```json
{"code": 0, "msg": "success", "data": {"id": 1, "phone": "13800138000", "role": "user"}}
```

---

## 3. 商品模块

### 3.1 获取商品列表

`GET /api/v1/goods`

**Query参数:** page(页码默认1), page_size(每页数量默认10)

**响应:**
```json
{
    "code": 0, "msg": "success", "data": {
        "list": [{"id": 1, "name": "iPhone 16 Pro", "price": 8999.00, "stock": 100, "start_time": "...", "end_time": "..."}],
        "total": 50, "page": 1, "page_size": 10
    }
}
```

---

## 4. 秒杀模块

### 4.1 秒杀下单

`POST /api/v1/seckill/:sku_id` (需要JWT)

**响应（成功）:**
```json
{"code": 0, "msg": "success", "data": {"order_id": "SK...", "status": "pending"}}
```

**响应（失败）:**
```json
{"code": 4001, "msg": "库存不足", "data": null}
```

**错误码:** 1001=参数错误, 2001=商品不存在, 2002=活动未开始, 2003=活动已结束, 2004=库存不足, 2005=重复购买, 4001=系统繁忙, 5001=系统错误

---

## 5. 订单模块

### 5.1 查询订单

`GET /api/v1/order/:id` (需要JWT)

**响应:**
```json
{"code": 0, "msg": "success", "data": {"order_id": "SK...", "sku_id": 1, "status": 0, "created_at": "..."}}
```

**订单状态:** 0=待支付, 1=已支付, 2=已取消

**错误码:** 3001=订单不存在

### 5.2 我的订单列表

`GET /api/v1/orders` (需要JWT)

**Query参数:** page(页码默认1), page_size(每页数量默认10)

---

## 6. AI 模块

### 6.1 获取 AI 能力

`GET /ai/capabilities`

获取 AI 助手支持的问题分类和示例问题。

**响应:**
```json
{
    "code": 0,
    "msg": "success",
    "data": {
        "categories": [
            "秒杀规则",
            "商品咨询",
            "订单问题",
            "账户问题",
            "系统限制"
        ],
        "example_questions": [
            "秒杀什么时候开始？",
            "怎么查看我的订单？",
            "每个用户限购几件？",
            "秒杀失败会返回什么错误？",
            "怎么登录账号？"
        ]
    }
}
```

### 6.2 AI 聊天问答

`POST /ai/chat` (需要JWT)

基于知识库的 RAG 问答。

**Body:**
| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| question | string | 是 | 用户问题（2-500字） |

**响应:**
```json
{
    "code": 0,
    "msg": "success",
    "data": {
        "answer": "秒杀活动时间由管理员设置，请查看商品详情页。",
        "references": [
            {
                "content": "**Q: 秒杀时间是怎么安排的？**\nA: 秒杀活动时间由管理员在创建商品时设置...",
                "score": 0.92
            }
        ]
    }
}
```

**错误码:** 1001=参数错误, 5001=AI服务暂时不可用

**说明:**
- 基于 RAG（检索增强生成）技术
- 从知识库检索相关内容，结合 LLM 生成回答
- 返回 references 中的 score 表示相关度（0-1）

### 6.3 更新知识库

`POST /admin/ai/ingest` (需要JWT + Admin)

从 Markdown 文件读取内容，更新知识库向量数据。

**Body:**
| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| file_path | string | 是 | 知识库文件路径（相对项目根目录） |

**响应:**
```json
{
    "code": 0,
    "msg": "success",
    "data": {
        "file_path": "./docs/rag-knowledge.md",
        "status": "updated"
    }
}
```

**说明:**
- 会清空现有知识库，重新导入
- 支持 Markdown 格式，按 ## 标题分割 chunk
- 每个 chunk 约 500 字符

---

## 7. 管理后台

> **注意**: 以下接口需要管理员权限（JWT Token + role=admin）

### 7.1 创建秒杀商品

`POST /admin/goods` (需要JWT + Admin)

**Body:**
| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| name | string | 是 | 商品名称 |
| price | float64 | 是 | 价格（需大于0） |
| stock | int | 是 | 库存（需大于0） |
| start_time | string | 是 | 开始时间，格式：2006-01-02 15:04:05 |
| end_time | string | 是 | 结束时间，格式：2006-01-02 15:04:05 |

**响应:**
```json
{"code": 0, "msg": "success", "data": {"goods_id": 1}}
```

**说明:** 创建商品后会自动预热库存到Redis。

### 7.2 获取商品列表（管理后台）

`GET /admin/goods` (需要JWT + Admin)

### 7.3 获取商品详情

`GET /admin/goods/:id` (需要JWT + Admin)

### 7.4 修改商品库存

`PUT /admin/goods/:id/stock` (需要JWT + Admin)

**Body:** `{"stock": 200}`

### 7.5 更新知识库

`POST /admin/ai/ingest` (需要JWT + Admin)

详见 [AI 模块](#62-更新知识库)

---

## 8. 监控接口

### 8.1 熔断器状态

`GET /api/v1/seckill/breaker`

**响应:**
```json
{"code": 0, "msg": "success", "data": {"state": "closed", "total_requests": 1000, "total_failures": 3, "total_successes": 997, "failure_rate": "0.30%", "last_change": "..."}}
```

**熔断器状态:** closed=正常, open=熔断中, half_open=半开

### 8.2 异步处理统计

`GET /api/v1/seckill/async_stats`

---

## 通用响应格式

```json
{"code": 0, "msg": "success", "data": {}}
```

### 通用错误码

| code | 说明 |
|------|------|
| 0 | 成功 |
| 1001 | 参数错误 |
| 1002 | 用户已存在 |
| 1003 | 用户不存在 |
| 1004 | 密码错误 |
| 2001 | 商品不存在 |
| 2002 | 活动未开始 |
| 2003 | 活动已结束 |
| 2004 | 库存不足 |
| 2005 | 重复购买 |
| 3001 | 订单不存在 |
| 4001 | 系统繁忙（熔断触发） |
| 401 | 未登录或Token无效 |
| 403 | 权限不足（非管理员） |
| 5001 | 系统内部错误 |
| 5002 | 服务不可用（熔断中） |

---

## 测试示例

```bash
# 注册用户
curl -X POST http://localhost:8080/api/v1/user/register \
  -H "Content-Type: application/json" \
  -d '{"phone": "13800138000", "password": "123456"}'

# 登录
curl -X POST http://localhost:8080/api/v1/user/login \
  -H "Content-Type: application/json" \
  -d '{"phone": "13800138000", "password": "admin123"}'

# 获取商品列表
curl http://localhost:8080/api/v1/goods

# 秒杀下单
curl -X POST http://localhost:8080/api/v1/seckill/1 \
  -H "Authorization: Bearer <token>"

# 查询订单
curl http://localhost:8080/api/v1/order/SK... \
  -H "Authorization: Bearer <token>"

# 获取 AI 能力
curl http://localhost:8080/ai/capabilities

# AI 聊天问答
curl -X POST http://localhost:8080/ai/chat \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <token>" \
  -d '{"question": "秒杀规则是什么？"}'

# 更新知识库（管理员）
curl -X POST http://localhost:8080/admin/ai/ingest \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <admin_token>" \
  -d '{"file_path": "./docs/rag-knowledge.md"}'

# 创建商品（管理员）
curl -X POST http://localhost:8080/admin/goods \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <admin_token>" \
  -d '{"name": "iPhone 16 Pro", "price": 8999.00, "stock": 100, "start_time": "2026-04-01 00:00:00", "end_time": "2026-04-30 23:59:59"}'

# 修改库存（管理员）
curl -X PUT http://localhost:8080/admin/goods/1/stock \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <admin_token>" \
  -d '{"stock": 200}'

# 查看日志（服务器上）
sudo tail -f /root/code/seckill.log

# 查看 Docker 容器日志
sudo docker logs -f deploy-nginx-1
sudo docker logs -f deploy-mysql-1
sudo docker logs -f deploy-redis-1
sudo docker logs -f qdrant
```
