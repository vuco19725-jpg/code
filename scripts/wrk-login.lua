-- wrk-login.lua - 多用户秒杀压测
-- 使用方式:
--   1. 先运行 ./scripts/prepare-users.sh 生成 tokens.txt
--   2. wrk -t4 -c200 -d60s -s scripts/wrk-login.lua http://localhost:8080/api/v1/seckill/1
--
--   格式: tokens.txt 每行 "user_id,token"

local tokens_file = "scripts/tokens.txt"
local user_tokens = {}  -- user_id 到 token 的映射
local user_ids = {}     -- 按顺序存储 user_id

-- 加载 token 列表
local file = io.open(tokens_file, "r")
if file then
    for line in file:lines() do
        if line and #line > 0 then
            -- 格式: user_id,token
            local user_id, token = line:match("([^,]+),(.+)")
            if user_id and token then
                table.insert(user_ids, user_id)
                user_tokens[user_id] = token
            end
        end
    end
    file:close()
    print("Loaded " .. #user_ids .. " users from " .. tokens_file)
else
    print("Error: " .. tokens_file .. " not found")
end

if #user_ids == 0 then
    print("Error: no users loaded")
    return
end

wrk.method = "POST"
wrk.headers["Content-Type"] = "application/json"

counter = 0

request = function()
    counter = counter + 1
    -- 轮询选择用户
    local idx = (counter % #user_ids) + 1
    local user_id = user_ids[idx]
    local token = user_tokens[user_id]

    -- 构建请求体，使用真实 user_id
    local body = string.format('{"user_id":%s,"sku_id":1}', user_id)

    wrk.headers["Authorization"] = "Bearer " .. token
    return wrk.format(nil, nil, nil, body)
end

response = function(status, headers, body)
    if status ~= 200 and status ~= 400 and status ~= 409 and status ~= 410 and status ~= 429 then
        print("Error: " .. status .. " - " .. body)
    end
end
