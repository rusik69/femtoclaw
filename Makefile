APP_NAME := nanoclaw
CMD_DIR := ./cmd/nanoclaw
BIN_DIR := ./bin

.PHONY: build test lint run docker-up docker-down

build:
	go build -o $(BIN_DIR)/$(APP_NAME) $(CMD_DIR)

test:
	go test ./...

lint:
	go vet ./...

run:
	set -a; [ -f .env ] && . .env; set +a; go run $(CMD_DIR)

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down
