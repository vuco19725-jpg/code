-- 库存扣减 Lua 脚本
-- 参数：KEYS[1] = stock:{sku_id}
-- 返回值：
--   1: 扣减成功
--   -1: 库存不足
--   -2: 库存未初始化

local stock = redis.call('GET', KEYS[1])

if not stock then
    return -2
end

if tonumber(stock) <= 0 then
    return -1
end

redis.call('DECR', KEYS[1])
return 1
