SHELL := /bin/bash

.PHONY: help test lint fmt build tidy examples

help: ## Show available commands
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-12s %s\n", $$1, $$2}'

test: ## Run all tests
	go test ./...

lint: ## Run go vet
	go vet ./...

fmt: ## Format all Go source
	gofmt -w .

build: ## Build everything (library + examples)
	go build ./...

tidy: ## Tidy module deps
	go mod tidy

examples: ## Build the example programs
	go build ./examples/...
