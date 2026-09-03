.PHONY: help fmt vet test build build-pulse build-forum build-blog \
        run-api run-worker up down clean

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
	@echo "  make up           docker compose up"
	@echo ""
	@echo "Tracks: services/pulse (A) | sites/blog (B) | services/forum* (C)"

fmt:
	gofmt -w $(GO_DIRS)

vet:
	go vet $(GO_MODULES)

test:
	go test $(GO_MODULES)

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

up:
	docker compose up -d

down:
	docker compose down

clean:
	rm -rf bin sites/blog/docs/.vitepress/dist
