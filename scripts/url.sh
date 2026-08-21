#!/bin/sh
# dqex 获取 Web 访问链接
# 用法：./url.sh
set -e

DIR=$(cd "$(dirname "$0")" && pwd)
DBX=""

if [ -f "$DIR/dqex" ] && [ -x "$DIR/dqex" ]; then
	DBX="$DIR/dqex"
elif command -v dqex >/dev/null 2>&1; then
	DBX="dqex"
else
	echo "错误: 未找到 dqex" >&2
	exit 1
fi

exec "$DBX" url
