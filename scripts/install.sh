#!/bin/sh
# dqex 安装脚本：把与脚本同目录的 dqex 二进制安装到 bin 目录
# 用法：
#   ./install.sh                      # 安装到 /usr/local/bin（无写权限时自动 sudo）
#   ./install.sh ~/bin                # 安装到自定义目录（如 /usr/local/bin 无权限时）
set -e

DIR=$(cd "$(dirname "$0")" && pwd)
BIN="$DIR/dqex"
DEST_DIR=${1:-/usr/local/bin}
DEST="$DEST_DIR/dqex"

if [ ! -f "$BIN" ]; then
	echo "错误: 未在脚本同目录找到 dqex 二进制 ($DIR)" >&2
	exit 1
fi

install_as() { # $1 = 执行身份前缀（空或 sudo）
	$1 mkdir -p "$DEST_DIR" && $1 cp "$BIN" "$DEST" && $1 chmod +x "$DEST"
}

# 先确保目标目录存在（目录不存在时 -w 会误判），再按写权限选择安装身份
if [ "$(id -u)" != "0" ] && [ ! -w "$DEST_DIR" ]; then
	mkdir -p "$DEST_DIR" 2>/dev/null || true
fi
if [ "$(id -u)" = "0" ] || [ -w "$DEST_DIR" ]; then
	install_as ""
else
	echo ">> $DEST_DIR 无写权限，使用 sudo 安装"
	install_as "sudo"
fi

if [ ! -x "$DEST" ]; then
	echo "错误: 安装失败（$DEST 不存在或不可执行）" >&2
	exit 1
fi
# 执行校验：架构不符等问题在此暴露，避免误报安装成功
if OUT=$("$DEST" version 2>&1); then
	echo "安装完成: $OUT -> $DEST"
else
	echo "错误: 安装的二进制无法执行（$OUT），请确认 zip 平台与本机架构匹配" >&2
	exit 1
fi

# 自定义目录不在 PATH 中时给出提示
case ":$PATH:" in
	*":$DEST_DIR:"*) ;;
	*) echo "提示: $DEST_DIR 不在 PATH 中，可执行 export PATH=\"$DEST_DIR:\$PATH\" 或改用 ./dqex 直接运行" ;;
esac
