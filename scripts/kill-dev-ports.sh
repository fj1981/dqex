#!/bin/sh
# 清理开发端口（后端 8181 / Vite 5281）上的残留进程。
# 场景：上次 make dev 退出时后端未被带走（Ctrl+C 只停了前端），再次启动会 address already in use。
# 先 TERM 优雅退出，2 秒后仍存活则 KILL 强杀。

if ! command -v lsof >/dev/null 2>&1; then
	echo ">> 未找到 lsof，跳过端口残留清理"
	exit 0
fi

for port in 8181 5281; do
	pids=$(lsof -ti tcp:"$port" 2>/dev/null | sort -u)
	if [ -n "$pids" ]; then
		printf '>> 清理端口 %s 上的残留进程: %s\n' "$port" "$(echo $pids | tr '\n' ' ')"
		kill -TERM $pids 2>/dev/null
		sleep 2
		left=$(lsof -ti tcp:"$port" 2>/dev/null)
		if [ -n "$left" ]; then
			kill -KILL $left 2>/dev/null
		fi
	fi
done

exit 0
