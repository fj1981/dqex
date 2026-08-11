#!/bin/sh
# dbx 获取 Web 访问链接
# 用法：./url.sh
set -e

DIR=$(cd "$(dirname "$0")" && pwd)
DBX=""

if [ -f "$DIR/dbx" ] && [ -x "$DIR/dbx" ]; then
	DBX="$DIR/dbx"
elif command -v dbx >/dev/null 2>&1; then
	DBX="dbx"
else
	echo "错误: 未找到 dbx" >&2
	exit 1
fi

exec "$DBX" url
