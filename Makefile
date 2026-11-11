NETWORK ?= $(or $(ORCHESTRATOR_NETWORK),mired)

.PHONY: build test tidy vet gen-fixtures network docker-build docker-up docker-down

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

# Crea la red compartida solo si no existe (si el orquestador ya la creó, no hace nada).
network:
	@docker network inspect $(NETWORK) >/dev/null 2>&1 || \
		(echo "Creando red $(NETWORK)..." && docker network create $(NETWORK))

docker-build:
	docker compose build

docker-up: network
	docker compose up -d --build

docker-down:
	docker compose down