#!/bin/sh
# dqex 停止脚本：终止正在运行的 dqex Web 服务
# 用法：./stop.sh
set -e

PID=$(pgrep -f 'dqex$' | head -1 2>/dev/null || true)

if [ -z "$PID" ]; then
	echo "未找到正在运行的 dqex 进程"
	exit 0
fi

echo ">> 终止 dqex 进程 (PID: $PID)..."
kill "$PID" 2>/dev/null || true

# 等待进程退出，最多 5 秒
for i in $(seq 1 50); do
	if ! kill -0 "$PID" 2>/dev/null; then
		echo ">> dqex 已停止"
		exit 0
	fi
	sleep 0.1
done

# 超时强制终止
echo ">> 超时，强制终止..."
kill -9 "$PID" 2>/dev/null || true
echo ">> dqex 已强制停止"
