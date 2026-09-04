.PHONY: help fmt vet test build build-pulse build-forum build-blog \
        run-api run-worker migrate-up migrate-status up down deploy-install deploy-update deploy-test clean

# The repo root is not a Go module, so `./...` does not resolve across the
# workspace. Every Go target lists module paths explicitly.
GO_MODULES := ./services/pulse/... ./services/forum-plugin/user-center-pulse/...
GO_DIRS := services/pulse services/forum-plugin

help:
	@echo "Meta Pulse monorepo"
	@echo ""
	@echo "  make fmt          gofmt all Go sources"
	@echo "  make vet          go vet all modules"
	@echo "  make test         go test all modules"
	@echo "  make build        build pulse binaries + forum image + blog"
	@echo "  make migrate-up   apply Pulse Goose migrations"
	@echo "  make migrate-status show Pulse migration status"
	@echo "  make up           docker compose up"
	@echo "  make deploy-install  服务器首次生产部署"
	@echo "  make deploy-update   服务器拉取并更新生产服务"
	@echo "  make deploy-test     离线验证部署脚本"
	@echo ""
	@echo "Tracks: services/pulse (A) | sites/blog (B) | services/forum* (C)"

fmt:
	gofmt -w $(GO_DIRS)

vet:
	go vet $(GO_MODULES)

test: deploy-test
	go test $(GO_MODULES)

deploy-test:
	./deploy/test.sh

build: build-pulse build-blog

build-pulse:
	mkdir -p bin
	go build -o bin/meta-pulse-api ./services/pulse/cmd/api
	go build -o bin/meta-pulse-worker ./services/pulse/cmd/worker
	go build -o bin/meta-pulse-tool ./services/pulse/cmd/tool

# Rebuilds Answer with the Pulse user center plugin compiled in.
build-forum:
	docker build -f services/forum/Dockerfile -t meta-pulse-forum:dev .

build-blog:
	cd sites/blog && pnpm install --frozen-lockfile && pnpm build

run-api:
	go run ./services/pulse/cmd/api

run-worker:
	go run ./services/pulse/cmd/worker

migrate-up:
	go run ./services/pulse/cmd/tool migrate-up

migrate-status:
	go run ./services/pulse/cmd/tool migrate-status

up:
	docker compose up -d

down:
	docker compose down

deploy-install:
	./deploy/install.sh

deploy-update:
	./deploy/update.sh

clean:
	rm -rf bin sites/blog/docs/.vitepress/dist
