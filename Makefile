.PHONY: build test tidy vet gen-fixtures docker-build docker-up docker-down

build:
	go build ./cmd/server

test:
	go test ./... -race -count=1

tidy:
	go mod tidy

vet:
	go vet ./...

gen-fixtures:
	go run ./tools/genfixtures

docker-build:
	docker compose build

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down