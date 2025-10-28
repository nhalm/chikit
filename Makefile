.PHONY: test test-race test-cover test-redis test-all lint fmt vet clean docker-up docker-down

test:
	go test -v ./...

test-race:
	go test -race -v ./...

test-cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

test-redis: docker-up
	@echo "Waiting for Redis to be ready..."
	@sleep 2
	REDIS_URL=localhost:6379 go test -v ./ratelimit/store -run TestRedis
	$(MAKE) docker-down

test-all: docker-up
	@echo "Waiting for Redis to be ready..."
	@sleep 2
	REDIS_URL=localhost:6379 go test -v ./...
	$(MAKE) docker-down

docker-up:
	docker-compose up -d
	@echo "Redis started on localhost:6379"

docker-down:
	docker-compose down

lint:
	@which golangci-lint > /dev/null || (echo "golangci-lint not installed. Install: https://golangci-lint.run/usage/install/" && exit 1)
	golangci-lint run ./...

fmt:
	gofmt -w -s .
	go mod tidy

vet:
	go vet ./...

check: fmt vet lint test-race
	@echo "All checks passed!"

clean:
	rm -f coverage.out coverage.html
	go clean -testcache
