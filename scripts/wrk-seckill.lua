-- wrk-seckill.lua - 秒杀压测脚本
-- 使用方式: wrk -t4 -c100 -d30s -s scripts/wrk-seckill.lua http://localhost:8080/api/v1/seckill/1

-- 替换为真实登录获取的 token
TOKEN = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VyX2lkIjo5LCJyb2xlIjoidXNlciIsImV4cCI6MTc3NTU0ODcxNCwiaWF0IjoxNzc1NDYyMzE0fQ.C0TZ32sUzvipBnxNufIo7JmwXGCXw1l3Jc25wnYcRTg"

wrk.method = "POST"
wrk.headers["Content-Type"] = "application/json"
wrk.headers["Authorization"] = "Bearer " .. TOKEN

counter = 0

request = function()
    counter = counter + 1
    -- 生成唯一 user_id 避免同一用户重复购买检查
    local user_id = 1000 + (counter % 10000)
    local body = string.format('{"sku_id":1,"user_id":%d}', user_id)
    return wrk.format(nil, nil, nil, body)
end

response = function(status, headers, body)
    if status ~= 200 then
        print("Error: " .. status .. " - " .. body)
    end
end