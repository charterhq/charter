.PHONY: build test lint db

build:
	go build -o bin/ ./cmd/...

test:
	go test -race -count=1 ./...

lint:
	gofmt -l . | (! grep .)
	go vet ./...

db:
	docker compose -f deploy/compose/dev.yml up -d --wait
