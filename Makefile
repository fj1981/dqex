GO ?= go
DLV ?= dlv
PLATFORMS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64
# 版本号：优先 git tag（含落后提交数），无 tag 时用短哈希，非 git 环境回退 dev-日期；可 make release VERSION=v1.2.3 覆盖
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev-$(shell date +%Y%m%d))
BUILDTIME = $(shell date '+%y%m%d%H%M')
LDFLAGS_REL := -s -w -X dbimpex/internal/cli.Version=$(VERSION) -X 'dbimpex/internal/cli.BuildTime=$(BUILDTIME)'

.PHONY: all build install uninstall web web-deps web-dist web-stub dev dev-debug stop release clean

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

# 构建单二进制（内嵌真实前端产物，注入版本号与构建时间）
build: web-dist
	$(GO) build -ldflags "-X dbimpex/internal/cli.Version=$(VERSION) -X 'dbimpex/internal/cli.BuildTime=$(BUILDTIME)'" -o dbx ./cmd
	@echo ">> 构建完成: ./dbx（版本 $(VERSION)，构建于 $(BUILDTIME)）"

# 构建并安装到本机（默认 /usr/local/bin，无写权限时自动 sudo；可 make install PREFIX=$HOME/bin 覆盖）
PREFIX ?= /usr/local/bin
install: build
	@if [ "$(PREFIX)" != "/usr/local/bin" ] && [ ! -d "$(PREFIX)" ]; then mkdir -p "$(PREFIX)"; fi
	@if [ "$$(id -u)" = "0" ] || [ -w "$(PREFIX)" ]; then \
		cp dbx "$(PREFIX)/dbx" && chmod +x "$(PREFIX)/dbx"; \
	else \
		echo ">> $(PREFIX) 无写权限，使用 sudo 安装"; \
		sudo cp dbx "$(PREFIX)/dbx" && sudo chmod +x "$(PREFIX)/dbx"; \
	fi
	@echo ">> 安装完成: $$($(PREFIX)/dbx version | head -1) -> $(PREFIX)/dbx"

# 卸载本机安装
uninstall:
	@if [ -w "$(PREFIX)" ] || [ "$$(id -u)" = "0" ]; then rm -f "$(PREFIX)/dbx"; else sudo rm -f "$(PREFIX)/dbx"; fi
	@echo ">> 已卸载 $(PREFIX)/dbx"

# 清理开发端口残留进程（上次 dev 未正常退出导致 8181/5281 被占用时手动执行，dev 启动前会自动执行）
stop:
	@sh scripts/kill-dev-ports.sh

# 开发模式：不构建前端，go run 启动后端 :8181 + Vite :5281（代理 /api），Ctrl+C 同时停止
dev: web-deps web-stub stop
	@echo ">> go run 启动后端 http://localhost:8181"
	@echo ">> 启动前端 http://localhost:5281 (Ctrl+C 停止)"
	@echo ">> dev 代理自动注入令牌（读 ~/.dbimpex/web-access.json），5281 无需 ?token= 即可访问"
	@$(GO) run ./cmd & BACKEND_PID=$$!; \
	trap "kill $$BACKEND_PID 2>/dev/null; sh scripts/kill-dev-ports.sh" EXIT; \
	cd web && yarn dev

# 调试模式：dlv headless 启动后端（:2345），VS Code 使用 launch.json 中的
# "Attach dbx" 配置 attach 调试；前端 Vite 同时启动
dev-debug: web-deps web-stub stop
	@echo ">> dlv 调试服务: 127.0.0.1:2345（VS Code F5 attach）"
	@echo ">> 启动前端 http://localhost:5281 (Ctrl+C 停止)"
	@echo ">> dev 代理自动注入令牌（读 ~/.dbimpex/web-access.json），5281 无需 ?token= 即可访问"
	@$(DLV) debug --headless --listen=127.0.0.1:2345 --api-version=2 --accept-multiclient ./cmd & BACKEND_PID=$$!; \
	trap "kill $$BACKEND_PID 2>/dev/null; sh scripts/kill-dev-ports.sh" EXIT; \
	cd web && yarn dev

# 跨平台打包：输出 release/dbx-$(VERSION)-{os}-{arch}.zip（CGO 关闭，纯静态二进制）；
# zip 内为干净的 dbx（Windows 为 dbx.exe）+ 安装脚本（unix: install.sh / windows: install.bat），解压即用；
# install.bat 打包时转 CRLF + GBK，避免 Windows cmd（GBK 代码页）中文乱码与解析问题
release: web-dist
	@rm -rf release && mkdir -p release
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		bin=dbx; stage=release/$$os-$$arch; mkdir -p $$stage; \
		if [ "$$os" = "windows" ]; then bin=dbx.exe; fi; \
		echo ">> 构建 $$os/$$arch (版本 $(VERSION))"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags "$(LDFLAGS_REL)" -o $$stage/$$bin ./cmd || exit 1; \
		if [ "$$os" = "windows" ]; then \
			perl -pe 's/\r?\n/\r\n/' scripts/install.bat | iconv -f UTF-8 -t GBK > $$stage/install.bat || cp scripts/install.bat $$stage/install.bat; \
		else cp scripts/install.sh $$stage/ && chmod +x $$stage/install.sh; fi; \
		(cd $$stage && zip -q ../dbx-$(VERSION)-$$os-$$arch.zip *) || exit 1; \
		rm -rf $$stage; \
	done
	@echo ">> 打包完成:"
	@ls -lh release/

clean:
	rm -f dbx dbimpex
	rm -rf release web/dist
