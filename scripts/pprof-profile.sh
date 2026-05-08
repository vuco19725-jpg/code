#!/bin/bash

# pprof 分析脚本
# 使用方式: ./scripts/pprof-profile.sh [PROFILE_TYPE] [SECONDS]
# PROFILE_TYPE: cpu, mem, goroutine, mutex, trace
# 默认 PROFILE_TYPE=cpu, SECONDS=30

set -e

PROFILE_TYPE=${1:-cpu}
SECONDS=${2:-30}
OUTPUT_DIR=${3:-./pprof-results}
PPROF_PORT=${4:-6060}

mkdir -p "$OUTPUT_DIR"

echo "开始 pprof $PROFILE_TYPE 分析..."
echo "采样时间: ${SECONDS}秒"
echo "pprof 端口: $PPROF_PORT"

case "$PROFILE_TYPE" in
    cpu)
        curl -s "http://localhost:$PPROF_PORT/debug/pprof/profile?seconds=$SECONDS" > "$OUTPUT_DIR/cpu.prof"
        echo "CPU profile 已保存到: $OUTPUT_DIR/cpu.prof"
        echo "分析命令: go tool pprof -http=:8081 $OUTPUT_DIR/cpu.prof"
        ;;

    mem)
        curl -s "http://localhost:$PPROF_PORT/debug/pprof/heap" > "$OUTPUT_DIR/mem.prof"
        echo "Memory profile 已保存到: $OUTPUT_DIR/mem.prof"
        echo "分析命令: go tool pprof -http=:8082 $OUTPUT_DIR/mem.prof"
        ;;

    goroutine)
        curl -s "http://localhost:$PPROF_PORT/debug/pprof/goroutine" > "$OUTPUT_DIR/goroutine.prof"
        echo "Goroutine profile 已保存到: $OUTPUT_DIR/goroutine.prof"
        echo "分析命令: go tool pprof -http=:8083 $OUTPUT_DIR/goroutine.prof"
        ;;

    mutex)
        curl -s "http://localhost:$PPROF_PORT/debug/pprof/mutex" > "$OUTPUT_DIR/mutex.prof"
        echo "Mutex profile 已保存到: $OUTPUT_DIR/mutex.prof"
        echo "分析命令: go tool pprof -http=:8084 $OUTPUT_DIR/mutex.prof"
        ;;

    trace)
        curl -s "http://localhost:$PPROF_PORT/debug/pprof/trace?seconds=$SECONDS" > "$OUTPUT_DIR/trace.out"
        echo "Trace 已保存到: $OUTPUT_DIR/trace.out"
        echo "分析命令: go tool trace $OUTPUT_DIR/trace.out"
        ;;

    *)
        echo "未知的 profile 类型: $PROFILE_TYPE"
        echo "支持的类型: cpu, mem, goroutine, mutex, trace"
        exit 1
        ;;
esac

echo "完成!"
