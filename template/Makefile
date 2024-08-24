all: help

## Build
build: ## Build binary
	@cat main.go | sed -e 's/LHOST/$(LHOST)/g' > main_b.go
	@go build -o purpc -ldflags "-s -w" main_b.go
	@rm main_b.go

build32: ## Build 32 bits binary
	@cat main.go | sed -e 's/LHOST/$(LHOST)/g' > main_b.go
	@GOARCH=386 go build -o purpc -ldflags "-s -w" main_b.go
	@rm main_b.go

## Help
help: ## Show this help
	@echo ''
	@echo 'Usage:'
	@echo '  ${YELLOW}make${RESET} ${GREEN}<target>${RESET}'
	@echo ''
	@echo 'Targets:'
	@awk 'BEGIN {FS = ":.*?## "} { \
		if (/^[a-zA-Z_-]+:.*?##.*$$/) {printf "    ${YELLOW}%-20s${GREEN}%s${RESET}\n", $$1, $$2} \
		else if (/^## .*$$/) {printf "  ${CYAN}%s${RESET}\n", substr($$1,4)} \
		}' $(MAKEFILE_LIST)