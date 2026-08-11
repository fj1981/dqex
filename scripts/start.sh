#!/bin/sh
# dbx 启动脚本：检查安装状态并启动 Web 服务
# 用法：
#   ./start.sh                         # 前台运行（Ctrl+C 停止）
#   ./start.sh -d                      # 后台运行（关闭终端不中断）
#   ./start.sh -d --port 9000          # 后台 + 指定端口
set -e

# 查找 dbx 二进制：优先同目录 > PATH
DIR=$(cd "$(dirname "$0")" && pwd)
DBX=""

if [ -f "$DIR/dbx" ] && [ -x "$DIR/dbx" ]; then
	DBX="$DIR/dbx"
elif command -v dbx >/dev/null 2>&1; then
	DBX="dbx"
else
	echo "错误: 未找到 dbx，请先执行 ./install.sh 安装" >&2
	exit 1
fi

# 解析参数，提取 -d
DAEMON=false
ARGS=""
for a in "$@"; do
	case "$a" in
		-d|--daemon) DAEMON=true ;;
		*) ARGS="$ARGS $a" ;;
	esac
done

echo ">> 使用: $DBX"

if [ "$DAEMON" = true ]; then
	nohup "$DBX" $ARGS > /dev/null 2>&1 &
	PID=$!
	sleep 0.5
	if kill -0 "$PID" 2>/dev/null; then
		echo ">> dbx 已在后台启动 (PID: $PID)"
		echo ">> 查看访问链接: $DIR/url.sh"
	else
		echo "错误: dbx 启动失败" >&2
		exit 1
	fi
else
	echo ">> 启动 Web 服务（Ctrl+C 停止）..."
	exec "$DBX" $ARGS
fi
