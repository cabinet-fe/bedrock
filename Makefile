.PHONY: dev build build-frontend build-backend \
	build-linux build-linux-arm64 build-win \
	build-agent-linux build-agent-linux-arm64 build-agent-win \
	clean \
	smoke-fresh-install smoke-api-e2e smoke-three-db smoke-linux-package smoke-restart-recovery smoke \
	checksums \
	install-hooks check-api-contracts

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
FRONTEND_DIR ?= web
export VITE_APP_VERSION := $(VERSION)
# Dev encryption key for web password_cipher (must match config.yaml encryption.key)
export VITE_BEDROCK_ENCRYPTION_KEY ?= 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
LDFLAGS := -s -w -X main.version=$(VERSION)

dev:
	@trap 'kill 0' INT TERM; \
	(cd cmd/server && go run -tags dev . --config ../../config.yaml) & \
	(cd $(FRONTEND_DIR) && vp dev) & \
	wait

build-frontend:
	cd $(FRONTEND_DIR) && vp install && vp build

build-backend:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bedrock ./cmd/server

build: build-frontend
	rm -rf cmd/server/dist && cp -r $(FRONTEND_DIR)/dist cmd/server/dist
	$(MAKE) build-backend

build-linux: build-frontend
	rm -rf cmd/server/dist && cp -r $(FRONTEND_DIR)/dist cmd/server/dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bedrock-linux-amd64 ./cmd/server

build-linux-arm64: build-frontend
	rm -rf cmd/server/dist && cp -r $(FRONTEND_DIR)/dist cmd/server/dist
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bedrock-linux-arm64 ./cmd/server

build-win: build-frontend
	rm -rf cmd/server/dist && cp -r $(FRONTEND_DIR)/dist cmd/server/dist
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bedrock-windows-amd64.exe ./cmd/server

build-agent-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bedrock-agent-linux-amd64 ./cmd/agent

build-agent-linux-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bedrock-agent-linux-arm64 ./cmd/agent

build-agent-win:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bedrock-agent-windows-amd64.exe ./cmd/agent

checksums:
	@sha256sum bedrock-linux-amd64 bedrock-linux-arm64 bedrock-agent-linux-amd64 bedrock-agent-linux-arm64 2>/dev/null || \
		shasum -a 256 bedrock-linux-amd64 bedrock-linux-arm64 bedrock-agent-linux-amd64 bedrock-agent-linux-arm64 2>/dev/null || \
		(echo "build linux packages first" >&2; exit 1)

install-hooks:
	git config core.hooksPath .githooks
	@echo "git hooks 已安装（.githooks/pre-commit：API 契约先行检查）"

check-api-contracts:
	node .agents/scripts/api-sync.mjs --worktree

smoke-fresh-install:
	bash scripts/smoke/fresh-install.sh

smoke-api-e2e:
	bash scripts/smoke/api-e2e.sh

smoke-three-db:
	bash scripts/smoke/three-db.sh

smoke-linux-package:
	bash scripts/smoke/linux-package.sh

smoke-restart-recovery:
	bash scripts/smoke/restart-recovery.sh

smoke: smoke-fresh-install smoke-api-e2e smoke-restart-recovery smoke-three-db smoke-linux-package

clean:
	rm -rf bedrock* cmd/server/dist $(FRONTEND_DIR)/dist data/ .tmp/smoke
