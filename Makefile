GO ?= go
DLV ?= dlv
AIR ?= air
PLATFORMS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64
# 版本号：取最近 git tag（不含落后提交数/短哈希），无 tag 时回退 dev-日期；可 make release VERSION=v1.2.3 覆盖
VERSION ?= $(shell git describe --tags --abbrev=0 2>/dev/null || echo dev-$(shell date +%Y%m%d))
# 短 commit id：package 汇总包命名用（git rev-parse 取短哈希，如 d59e502）
SHORT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILDTIME = $(shell date '+%y%m%d%H%M')
# 编译标签：make build TAGS=opensource 或 make release TAGS=opensource 时启用开源构建（"关于"弹窗展示项目 Git 地址与联系方式）
TAGS ?=
LDFLAGS_REL := -s -w -X dqex/internal/cli.Version=$(VERSION) -X dqex/internal/cli.CommitID=$(SHORT_COMMIT) -X 'dqex/internal/cli.BuildTime=$(BUILDTIME)'

# 检测 air 是否已安装
HAS_AIR := $(shell command -v $(AIR) 2>/dev/null)
# dev 模式 air 配置：DEBUG=1 时使用 .air-debug.toml（后端加 --debug，输出全局 debug 日志）
AIR_CFG := $(if $(DEBUG),.air-debug.toml,.air.toml)

.PHONY: all build build-oss install install-oss uninstall web web-deps web-dist web-stub dev dev-debug stop release package clean air-install

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
	$(GO) build $(if $(TAGS),-tags $(TAGS),) -ldflags "-X dqex/internal/cli.Version=$(VERSION) -X dqex/internal/cli.CommitID=$(SHORT_COMMIT) -X 'dqex/internal/cli.BuildTime=$(BUILDTIME)'" -o dqex ./cmd
	@echo ">> 构建完成: ./dqex（版本 $(VERSION)，构建于 $(BUILDTIME)）"

# 构建并安装到本机（默认 /usr/local/bin，无写权限时自动 sudo；可 make install PREFIX=$HOME/bin 覆盖）
PREFIX ?= /usr/local/bin
install: build
	@if [ "$(PREFIX)" != "/usr/local/bin" ] && [ ! -d "$(PREFIX)" ]; then mkdir -p "$(PREFIX)"; fi
	@if [ "$$(id -u)" = "0" ] || [ -w "$(PREFIX)" ]; then \
		cp dqex "$(PREFIX)/dqex" && chmod +x "$(PREFIX)/dqex"; \
	else \
		echo ">> $(PREFIX) 无写权限，使用 sudo 安装"; \
		sudo cp dqex "$(PREFIX)/dqex" && sudo chmod +x "$(PREFIX)/dqex"; \
	fi
	@echo ">> 安装完成: $$($(PREFIX)/dqex version | head -1) -> $(PREFIX)/dqex"

# 开源构建：带项目 Git 地址与联系方式（等效 make build/install TAGS=opensource）
build-oss:
	@$(MAKE) build TAGS=opensource
install-oss:
	@$(MAKE) install TAGS=opensource

# 卸载本机安装
uninstall:
	@if [ -w "$(PREFIX)" ] || [ "$$(id -u)" = "0" ]; then rm -f "$(PREFIX)/dqex"; else sudo rm -f "$(PREFIX)/dqex"; fi
	@echo ">> 已卸载 $(PREFIX)/dqex"

# 清理开发端口残留进程（上次 dev 未正常退出导致 8181/5281 被占用时手动执行，dev 启动前会自动执行）
stop:
	@sh scripts/kill-dev-ports.sh

# 安装 air（Go 热重载工具）：后端代码变更时自动重新编译并重启
air-install:
	@echo ">> 安装 air (Go 热重载工具)..."
	@$(GO) install github.com/air-verse/air@latest

# 开发模式：后端用 air 热重载（Go 代码变更自动重启）+ 前端 Vite HMR（React 代码变更即时生效）
# 默认访问 5281；后端 8181 仅监听 127.0.0.1，仅供 Vite 代理转发 /api，不直接对外暴露
# OPEN_BACKEND=1 时后端启动后额外自动打开 8181 网页（带令牌，便于直接调试后端），默认仅开 5281
OPEN_BACKEND ?=
dev: web-deps web-stub stop
	@if [ -z "$(HAS_AIR)" ]; then \
		echo ">> 未检测到 air，正在安装..."; \
		$(GO) install github.com/air-verse/air@latest; \
		echo ">> air 安装完成，启动开发模式"; \
	fi
	@echo ">> air 热重载启动后端（Go 代码变更自动重启，仅本机回环 127.0.0.1:8181）"
	@echo ">> Vite HMR 启动前端 http://localhost:5281（React 代码变更即时生效）"
	@echo ">> Ctrl+C 同时停止前后端"
	@if [ -n "$(DEBUG)" ]; then echo ">> DEBUG=1：后端以 --debug 启动，输出全局 debug 及以上级别日志"; fi
	@if [ -z "$(OPEN_BACKEND)" ]; then echo ">> 默认不打开 8181 网页；如需同步打开请加 OPEN_BACKEND=1"; fi
	@echo ">> dev 代理自动注入令牌（读 ~/.dqex/web-access.json），5281 无需 ?token= 即可访问"
	@$(AIR) -c $(AIR_CFG) & AIR_PID=$$!; \
	trap "kill $$AIR_PID 2>/dev/null; sh scripts/kill-dev-ports.sh" EXIT; \
	cd web && yarn dev

# 调试模式：dlv headless 启动后端（:2345），VS Code 使用 launch.json 中的
# "Attach dqex" 配置 attach 调试；前端 Vite HMR 同时启动
# 注意：dlv 模式下后端不支持热重载（需手动重启 dlv），前端仍然 HMR
dev-debug: web-deps web-stub stop
	@echo ">> dlv 调试服务: 127.0.0.1:2345（VS Code F5 attach，不支持热重载，需手动重启）"
	@echo ">> Vite HMR 启动前端 http://localhost:5281（React 代码变更即时生效）"
	@echo ">> Ctrl+C 同时停止"
	@echo ">> dev 代理自动注入令牌（读 ~/.dqex/web-access.json），5281 无需 ?token= 即可访问"
	@$(DLV) debug --headless --listen=127.0.0.1:2345 --api-version=2 --accept-multiclient ./cmd & BACKEND_PID=$$!; \
	trap "kill $$BACKEND_PID 2>/dev/null; sh scripts/kill-dev-ports.sh" EXIT; \
	cd web && yarn dev

# 跨平台打包：输出 release/dqex-$(VERSION)-{os}-{arch}.zip（CGO 关闭，纯静态二进制）；
# zip 内包含 dqex（Windows 为 dqex.exe）+ 安装/启动/停止/查看链接脚本 + 中文安装使用说明 README.md + CLI.md 使用手册，解压即用；
# docs/README.release.md 为发行包专用中文说明（仅安装使用方法，不含开源/GitHub 信息），复制进包时改名为 README.md；
# install.bat/start.bat/stop.bat 打包时转 CRLF + GBK，避免 Windows cmd（GBK 代码页）中文乱码与解析问题
release: web-dist
	@rm -rf release && mkdir -p release
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		bin=dqex; stage=release/$$os-$$arch; mkdir -p $$stage; \
		if [ "$$os" = "windows" ]; then bin=dqex.exe; fi; \
		echo ">> 构建 $$os/$$arch (版本 $(VERSION))"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath $(if $(TAGS),-tags $(TAGS),) -ldflags "$(LDFLAGS_REL)" -o $$stage/$$bin ./cmd || exit 1; \
		if [ "$$os" = "windows" ]; then \
			perl -pe 's/\r?\n/\r\n/' scripts/install.bat | iconv -f UTF-8 -t GBK > $$stage/install.bat || cp scripts/install.bat $$stage/install.bat; \
			perl -pe 's/\r?\n/\r\n/' scripts/start.bat | iconv -f UTF-8 -t GBK > $$stage/start.bat || cp scripts/start.bat $$stage/start.bat; \
			perl -pe 's/\r?\n/\r\n/' scripts/stop.bat | iconv -f UTF-8 -t GBK > $$stage/stop.bat || cp scripts/stop.bat $$stage/stop.bat; \
		else \
			cp scripts/install.sh $$stage/ && chmod +x $$stage/install.sh; \
			cp scripts/start.sh $$stage/ && chmod +x $$stage/start.sh; \
			cp scripts/stop.sh $$stage/ && chmod +x $$stage/stop.sh; \
			cp scripts/url.sh $$stage/ && chmod +x $$stage/url.sh; \
		fi; \
		cp CLI.md $$stage/; \
		cp docs/README.release.md $$stage/README.md; \
		(cd $$stage && zip -q ../dqex-$(VERSION)-$$os-$$arch.zip *) || exit 1; \
		rm -rf $$stage; \
	done
	@echo ">> 打包完成:"
	@ls -lh release/

# 汇总打包：将 release 下各平台 zip（make release 产物，dqex-$(VERSION)-{os}-{arch}.zip）统一压缩为
# release/dqex-$(SHORT_COMMIT).zip，便于一次分发全部平台；只匹配平台包，避免嵌套历史汇总包；
# release 目录无匹配产物时提示先执行 make release
package:
	@if [ -z "$$(ls release/dqex-$(VERSION)-*.zip 2>/dev/null)" ]; then \
		echo ">> release 目录没有 $(VERSION) 平台 zip 产物，请先执行 make release"; \
		exit 1; \
	fi
	@cd release && rm -f dqex-$(SHORT_COMMIT).zip && zip -q dqex-$(SHORT_COMMIT).zip dqex-$(VERSION)-*.zip
	@echo ">> 汇总完成: release/dqex-$(SHORT_COMMIT).zip"
	@ls -lh release/dqex-$(SHORT_COMMIT).zip

clean:
	rm -f dqex dqex
	rm -rf release web/dist
