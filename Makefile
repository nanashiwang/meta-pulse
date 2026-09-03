.PHONY: fmt test build build-api build-worker build-tool run-api run-worker clean

fmt:
	find cmd internal -name '*.go' -print0 | xargs -0 gofmt -w

test:
	go test ./...

build: build-api build-worker build-tool

build-api:
	mkdir -p bin
	go build -o bin/meta-pulse-api ./cmd/api

build-worker:
	mkdir -p bin
	go build -o bin/meta-pulse-worker ./cmd/worker

build-tool:
	mkdir -p bin
	go build -o bin/meta-pulse-tool ./cmd/tool

run-api:
	go run ./cmd/api

run-worker:
	go run ./cmd/worker

clean:
	rm -rf bin
