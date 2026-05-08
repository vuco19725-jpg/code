#!/bin/bash

# Vegeta 压测脚本
# 使用方式: ./scripts/vegeta-test.sh [RATE] [DURATION]
# 默认 RATE=100, DURATION=30s

set -e

RATE=${1:-100}
DURATION=${2:-30s}
TARGET_URL=${3:-http://localhost:8080/api/v1/seckill/1}
OUTPUT_DIR=${4:-./results}

mkdir -p "$OUTPUT_DIR"

echo "开始 Vegeta 压测..."
echo "目标: $TARGET_URL"
echo "速率: $RATE 请求/秒"
echo "持续: $DURATION"

# 生成请求
echo "POST $TARGET_URL" | vegeta encode > "$OUTPUT_DIR/target.bin"

# 执行攻击
vegeta attack \
    -rate="$RATE" \
    -duration="$DURATION" \
    -target="$OUTPUT_DIR/target.bin" \
    > "$OUTPUT_DIR/results.bin"

# 生成报告
vegeta report "$OUTPUT_DIR/results.bin"

# 生成 HTML 报告
vegeta report -type=html "$OUTPUT_DIR/results.bin" > "$OUTPUT_DIR/results.html"

echo "结果已保存到: $OUTPUT_DIR/"
