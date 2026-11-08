.PHONY: help fmt vet verify-notes test build build-pulse build-intel build-forum build-blog build-community test-community \
        run-api run-worker migrate-up migrate-status up down deploy-install deploy-update deploy-test deploy-config-test test-integration test-forum-integration test-growth-integration test-chat-integration clean

# The repo root is not a Go module, so `./...` does not resolve across the
# workspace. Every Go target lists module paths explicitly.
GO_MODULES := ./services/dnsprobe/... ./services/pulse/... ./services/forum-plugin/user-center-pulse/... ./services/intel/...
GO_DIRS := services/dnsprobe services/pulse services/forum-plugin services/intel

help:
	@echo "Meta Pulse monorepo"
	@echo ""
	@echo "  make fmt          gofmt all Go sources"
	@echo "  make vet          go vet all modules"
	@echo "  make verify-notes 验证工具安装与决策笔记目录、格式、链接、归档完整性"
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

verify-notes:
	bash .agents/verify-notes.sh

vet:
	go vet $(GO_MODULES)

test: deploy-test test-community test-forum-session test-forum-optional-email
	go test $(GO_MODULES)

deploy-test:
	./deploy/test.sh

deploy-config-test:
	python3 -m unittest deploy/test_dnsprobe.py -v
	./deploy/config-test.sh

# Refuse a green run made entirely of skipped database tests.
test-integration:
	@test -n "$$PULSE_INTEGRATION_DSN" || { echo "请设置专用测试库 PULSE_INTEGRATION_DSN" >&2; exit 1; }
	go test -count=1 -race ./services/pulse/internal/service -run MySQL -timeout 5m
	go test -count=1 -race ./services/pulse/internal/store/mysql -run MySQL -timeout 5m

# Run sequentially because both packages intentionally recreate disposable
# Answer tables in the same isolated MySQL database.
test-forum-integration:
	@test -n "$$FORUM_INTEGRATION_DSN" || { echo "请设置专用测试库 FORUM_INTEGRATION_DSN" >&2; exit 1; }
	FORUM_BINDING_GUARD_INTEGRATION_DSN="$$FORUM_INTEGRATION_DSN" go test -count=1 -race ./services/forum-plugin/user-center-pulse -run 'TestMySQL(BindingGuard|AdminIdentity)' -timeout 5m
	FORUM_CONTENT_READER_INTEGRATION_DSN="$$FORUM_INTEGRATION_DSN" go test -count=1 -race ./services/pulse/internal/adapter/forum -run TestMySQLFetch -timeout 5m

# Dedicated disposable schema avoids touching Answer fixture tables in other suites.
test-growth-integration:
	@test -n "$$GROWTH_INTEGRATION_DSN" || { echo "请设置以 _test 结尾的专用经验测试库" >&2; exit 1; }
	go test -count=1 -race ./services/forum-plugin/user-center-pulse/growth -run TestMySQL -timeout 5m
	FORUM_GROWTH_INTEGRATION_DSN="$$GROWTH_INTEGRATION_DSN" go test -count=1 -race ./services/forum-plugin/user-center-pulse -run TestMySQLExperienceSync -timeout 5m

# Chat tests must use their own disposable Answer-like schema.
test-chat-integration:
	@test -n "$$CHAT_INTEGRATION_DSN" || { echo "请设置以 _chat_test 结尾的专用聊天测试库" >&2; exit 1; }
	go test -count=1 -race ./services/forum-plugin/user-center-pulse/chat -run TestMySQL -timeout 5m

build: build-pulse build-intel build-blog build-community

build-pulse:
	mkdir -p bin
	go build -o bin/meta-pulse-api ./services/pulse/cmd/api
	go build -o bin/meta-pulse-worker ./services/pulse/cmd/worker
	go build -o bin/meta-pulse-tool ./services/pulse/cmd/tool

build-intel:
	mkdir -p bin
	go build -o bin/metar-intel-api ./services/intel/cmd/api
	go build -o bin/metar-intel-worker ./services/intel/cmd/worker
	go build -o bin/metar-intel-tool ./services/intel/cmd/tool

# Rebuilds Answer with the Pulse user center plugin compiled in.
build-forum:
	docker build -f services/forum/Dockerfile -t meta-pulse-forum:dev .

build-blog:
	cd sites/blog && npm ci --ignore-scripts && npm run build


build-community:
	./deploy/build-community.sh


test-editor-images:
	npm ci --prefix services/forum-plugin/user-center-pulse/tests --ignore-scripts --no-audit --no-fund
	node --test services/forum-plugin/user-center-pulse/tests/*.test.mjs

.PHONY: test-editor-images

test-community: test-editor-images
	python3 metar-frontend/shared/generate-routes.py --check
	python3 metar-frontend/shared/generate-theme.py --check
	python3 -m unittest discover -s metar-frontend/production/tests -v
	node --test metar-frontend/production/tests/*.test.js
	node --test services/forum/customization/*.test.js
	node --test services/forum-plugin/user-center-pulse/*.test.js

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

.PHONY: test-events-integration
test-events-integration:
	@test -n "$$EVENTS_INTEGRATION_DSN" || { echo "请设置以 _events_test 结尾的专用活动测试库" >&2; exit 1; }
	go test -count=1 -race ./services/forum-plugin/user-center-pulse/events -run TestMySQL -timeout 5m

.PHONY: test-analytics-integration
test-analytics-integration:
	@test -n "$$ANALYTICS_INTEGRATION_DSN" || { echo "请设置以 _analytics_test 结尾的专用统计测试库" >&2; exit 1; }
	go test -count=1 -race ./services/forum-plugin/user-center-pulse/analytics -run TestMySQL -timeout 5m

.PHONY: test-community-tickets-integration
test-community-tickets-integration:
	@test -n "$$TICKETS_INTEGRATION_DSN" || { echo "请设置以 _tickets_test 结尾的专用赠券测试库" >&2; exit 1; }
	go test -count=1 -race ./services/forum-plugin/user-center-pulse/communitytickets -run TestMySQL -timeout 5m

.PHONY: test-account-switch-integration
test-account-switch-integration:
	@test -n "$$FORUM_SWITCH_INTEGRATION_DSN" || { echo "请设置专用账号切换测试库 FORUM_SWITCH_INTEGRATION_DSN" >&2; exit 1; }
	go test -count=1 -race ./services/forum-plugin/user-center-pulse -run TestMySQLAdminSwitch -timeout 2m

.PHONY: test-marketplace-integration
test-marketplace-integration:
	@test -n "$$MARKET_INTEGRATION_DSN" || { echo "请设置以 _market_test 结尾的专用集市测试库" >&2; exit 1; }
	go test -count=1 -race ./services/forum-plugin/user-center-pulse/marketplace -run TestMySQL -timeout 5m

.PHONY: test-forum-session
test-forum-session:
	bash services/forum/test-session.sh

.PHONY: test-forum-optional-email
test-forum-optional-email:
	bash services/forum/test-optional-admin-email.sh
