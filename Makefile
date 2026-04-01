.PHONY: build run test lint clean

build:
	go build -o bin/valet ./cmd/valet

run:
	go run ./cmd/valet

test:
	go test ./...

test-cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

lint:
	golangci-lint run

clean:
	rm -rf bin/ coverage.out

dev-web:
	cd web && npm run dev

build-web:
	cd web && npm run build

build-all: build-web build
