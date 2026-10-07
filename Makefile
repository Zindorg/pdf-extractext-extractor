.PHONY: build test docker-build docker-up docker-down

build:
	go build ./cmd/server

test:
	go test ./... -race -count=1

docker-build:
	docker compose build

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down