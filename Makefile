GO ?= go
DLV ?= dlv
PLATFORMS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64

.PHONY: all build web web-deps web-dist web-stub dev dev-debug stop release clean

all: build

# 安装前端依赖（node_modules 缺失时）
web-deps:
	@if [ ! -d web/node_modules ]; then \
		echo ">> 安装前端依赖 (yarn)..."; \
		cd web && yarn install; \
	fi

# 无条件构建前端：每次都执行 yarn build，输出新的 web/dist
web: web-deps
	@echo ">> 构建前端 (web/dist)..."
	@cd web && yarn build

# 真实构建前端产物（build/release 使用）：dist 缺失时执行 yarn build
web-dist: web-deps
	@if [ ! -f web/dist/index.html ] || [ -f web/dist/.placeholder ]; then \
		echo ">> 构建前端 (web/dist)..."; \
		cd web && yarn build; \
	fi

# 开发占位：go:embed 要求 web/dist 存在，dev 模式访问 Vite :5281，
# 后端嵌入内容用不到，缺失时仅放占位文件让 Go 编译通过，不执行前端构建
web-stub:
	@if [ ! -f web/dist/index.html ]; then \
		echo ">> 生成 web/dist 占位文件（开发模式不构建前端）"; \
		mkdir -p web/dist && echo '<html><body>dev placeholder - use http://localhost:5281</body></html>' > web/dist/index.html && touch web/dist/.placeholder; \
	fi

# 构建单二进制（内嵌真实前端产物）
build: web-dist
	$(GO) build -o dbimpex ./cmd
	@echo ">> 构建完成: ./dbimpex"

# 清理开发端口残留进程（上次 dev 未正常退出导致 8181/5281 被占用时手动执行，dev 启动前会自动执行）
stop:
	@sh scripts/kill-dev-ports.sh

# 开发模式：不构建前端，go run 启动后端 :8181 + Vite :5281（代理 /api），Ctrl+C 同时停止
dev: web-deps web-stub stop
	@echo ">> go run 启动后端 http://localhost:8181"
	@echo ">> 启动前端 http://localhost:5281 (Ctrl+C 停止)"
	@$(GO) run ./cmd & BACKEND_PID=$$!; \
	trap "kill $$BACKEND_PID 2>/dev/null; sh scripts/kill-dev-ports.sh" EXIT; \
	cd web && yarn dev

# 调试模式：dlv headless 启动后端（:2345），VS Code 使用 launch.json 中的
# "Attach dbimpex" 配置 attach 调试；前端 Vite 同时启动
dev-debug: web-deps web-stub stop
	@echo ">> dlv 调试服务: 127.0.0.1:2345（VS Code F5 attach）"
	@echo ">> 启动前端 http://localhost:5281 (Ctrl+C 停止)"
	@$(DLV) debug --headless --listen=127.0.0.1:2345 --api-version=2 --accept-multiclient ./cmd & BACKEND_PID=$$!; \
	trap "kill $$BACKEND_PID 2>/dev/null; sh scripts/kill-dev-ports.sh" EXIT; \
	cd web && yarn dev

# 跨平台打包：输出到 release/（CGO 关闭，纯静态二进制）
release: web-dist
	@rm -rf release && mkdir -p release
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		out=release/dbimpex-$$os-$$arch; \
		if [ "$$os" = "windows" ]; then out=$$out.exe; fi; \
		echo ">> 构建 $$os/$$arch -> $$out"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags "-s -w" -o $$out ./cmd || exit 1; \
	done
	@echo ">> 打包完成:"
	@ls -lh release/

clean:
	rm -f dbimpex
	rm -rf release web/dist
