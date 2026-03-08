APP_NAME := femtoclaw
CMD_DIR := ./cmd/femtoclaw
BIN_DIR := ./bin

.PHONY: build test lint run podman-up podman-down logs watch-logs

build:
	go build -o $(BIN_DIR)/$(APP_NAME) $(CMD_DIR)

test:
	go test ./...

lint:
	go vet ./...

run:
	set -a; [ -f .env ] && . .env; set +a; go run $(CMD_DIR)

podman-up:
	podman compose up -d --build

podman-down:
	podman compose down

logs:
	podman compose logs -f

watch-logs: logs
