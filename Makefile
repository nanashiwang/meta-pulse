.PHONY: help fmt vet test build build-pulse build-forum build-blog build-community test-community \
        run-api run-worker migrate-up migrate-status up down deploy-install deploy-update deploy-test deploy-config-test test-integration test-forum-integration clean

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
	@echo "  make build        build pulse binaries + blog + METAR frontend"
	@echo "  make migrate-up   apply Pulse Goose migrations"
	@echo "  make migrate-status show Pulse migration status"
	@echo "  make up           docker compose up"
	@echo "  make deploy-install  服务器首次生产部署"
	@echo "  make deploy-update   服务器拉取并更新生产服务"
	@echo "  make deploy-test     离线验证部署脚本"
	@echo "  make test-community  验证 METAR 正式前端构建边界"
	@echo "  make deploy-config-test  生产 Compose 角色配置回归"
	@echo "  make test-integration    独立 MySQL 事务/并发回归（需测试 DSN）"
	@echo "  make test-forum-integration  Answer 绑定约束与内容映射回归（需测试 DSN）"
	@echo ""
	@echo "Tracks: services/pulse (A) | sites/blog (B) | services/forum* (C)"

fmt:
	gofmt -w $(GO_DIRS)

vet:
	go vet $(GO_MODULES)

test: deploy-test test-community
	go test $(GO_MODULES)

deploy-test:
	./deploy/test.sh

deploy-config-test:
	./deploy/config-test.sh

# Refuse a green run made entirely of skipped database tests.
test-integration:
	@test -n "$$PULSE_INTEGRATION_DSN" || { echo "请设置专用测试库 PULSE_INTEGRATION_DSN" >&2; exit 1; }
	go test -count=1 -race ./services/pulse/internal/service -run MySQL -timeout 5m

# Run sequentially because both packages intentionally recreate disposable
# Answer tables in the same isolated MySQL database.
test-forum-integration:
	@test -n "$$FORUM_INTEGRATION_DSN" || { echo "请设置专用测试库 FORUM_INTEGRATION_DSN" >&2; exit 1; }
	FORUM_BINDING_GUARD_INTEGRATION_DSN="$$FORUM_INTEGRATION_DSN" go test -count=1 -race ./services/forum-plugin/user-center-pulse -run TestMySQLBindingGuard -timeout 5m
	FORUM_CONTENT_READER_INTEGRATION_DSN="$$FORUM_INTEGRATION_DSN" go test -count=1 -race ./services/pulse/internal/adapter/forum -run TestMySQLFetch -timeout 5m

build: build-pulse build-blog build-community

build-pulse:
	mkdir -p bin
	go build -o bin/meta-pulse-api ./services/pulse/cmd/api
	go build -o bin/meta-pulse-worker ./services/pulse/cmd/worker
	go build -o bin/meta-pulse-tool ./services/pulse/cmd/tool

# Rebuilds Answer with the Pulse user center plugin compiled in.
build-forum:
	docker build -f services/forum/Dockerfile -t meta-pulse-forum:dev .

build-blog:
	cd sites/blog && npm ci --ignore-scripts && npm run build


build-community:
	./deploy/build-community.sh


test-community:
	python3 -m unittest discover -s metar-frontend/production/tests -v
	node --test metar-frontend/production/tests/*.test.js

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
