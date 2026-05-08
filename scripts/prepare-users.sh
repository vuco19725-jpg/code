#!/bin/bash
# prepare-users.sh - 批量注册用户并生成 token 列表
# 使用方式: ./scripts/prepare-users.sh [用户数量] [起始手机号]

COUNT=${1:-200}
START_NUM=${2:-13800138001}
BASE_URL=${BASE_URL:-http://localhost:8080}
TOKENS_FILE=${TOKENS_FILE:-scripts/tokens.txt}

echo "=========================================="
echo "批量准备用户"
echo "=========================================="
echo "用户数量: $COUNT"
echo "起始手机号: $START_NUM"
echo "Token 文件: $TOKENS_FILE"
echo "=========================================="

# 清空 token 文件
> "$TOKENS_FILE"

success_count=0
fail_count=0

for i in $(seq 0 $((COUNT-1))); do
    phone_num=$((START_NUM + i))
    phone="$phone_num"
    password="test123456"

    # 注册
    register_resp=$(curl -s -X POST "$BASE_URL/api/v1/user/register" \
        -H "Content-Type: application/json" \
        -d "{\"phone\":\"$phone\",\"password\":\"$password\"}")

    code=$(echo "$register_resp" | grep -o '"code":[0-9]*' | cut -d':' -f2)

    # 无论注册成功还是用户已存在，都需要登录获取 token
    login_resp=$(curl -s -X POST "$BASE_URL/api/v1/user/login" \
        -H "Content-Type: application/json" \
        -d "{\"phone\":\"$phone\",\"password\":\"$password\"}")

    code=$(echo "$login_resp" | grep -o '"code":[0-9]*' | cut -d':' -f2)
    if [ "$code" != "0" ]; then
        echo "[-] 用户 $i ($phone): 登录失败"
        ((fail_count++))
        continue
    fi

    # 提取 token 和 user_id
    token=$(echo "$login_resp" | grep -o '"token":"[^"]*"' | cut -d'"' -f4)
    user_id=$(echo "$login_resp" | grep -o '"user_id":[0-9]*' | cut -d':' -f2)

    if [ -n "$token" ] && [ -n "$user_id" ]; then
        # 保存格式: user_id,token
        echo "$user_id,$token" >> "$TOKENS_FILE"
        ((success_count++))

        if [ $((success_count % 20)) -eq 0 ]; then
            echo "[+] 已完成 $success_count/$COUNT"
        fi
    else
        echo "[-] 用户 $i ($phone): 获取 token 失败"
        ((fail_count++))
    fi
done

echo ""
echo "=========================================="
echo "完成!"
echo "成功: $success_count"
echo "失败: $fail_count"
echo "Token 文件: $TOKENS_FILE"
echo "=========================================="
echo ""
echo "使用方式:"
echo "  wrk -t4 -c200 -d60s -s scripts/wrk-login.lua http://localhost:8080/api/v1/seckill/1"