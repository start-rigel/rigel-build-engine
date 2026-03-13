APP_NAME := $(notdir $(CURDIR))

.PHONY: run build test fmt

run:
	go run ./cmd/server

build:
	mkdir -p bin
	go build -o bin/$(APP_NAME) ./cmd/server

test:
	go test ./...

fmt:
	gofmt -w $(shell find . -name '*.go' -type f)
