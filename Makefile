.PHONY: build test lint db web web-dev

build:
	go build -o bin/ ./cmd/...

test:
	go test -race -count=1 ./...

lint:
	gofmt -l . | (! grep .)
	go vet ./...

db:
	docker compose -f deploy/compose/dev.yml up -d --wait

# Web UI: npm run build puts it into internal/webui/dist, the next go build embeds it.
web:
	cd web && npm ci --no-audit --no-fund && npm run build

# UI with hot reload; the API goes to agentd on CHARTER_DEV_API (default http://localhost:8080).
web-dev:
	cd web && npm run dev
