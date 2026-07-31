## Defaults (overridden by the implantbuilder or command-line)
OUTPUT    ?= implant
LHOST     ?= 127.0.0.1:4444
OS        ?= linux
ARCH      ?= amd64
URI       ?= /
UA        ?= Mozilla PurpCMD
TYPE      ?= impl
PUBLICKEY ?=

GOOS      ?= $(OS)
GOARCH    ?= $(ARCH)

TMP_SRC   := main_build.go

.DEFAULT_GOAL := build

## Build
.PHONY: build
build: ## Build implant binary (used by implantbuilder and manually)
	@echo "[*] Generating $(TMP_SRC) ..."
	@sed \
		-e 's|"LHOST"|"$(LHOST)"|g' \
		-e 's|"IMPLANT_TYPE"|"$(TYPE)"|g' \
		-e 's|uri  := "/"|uri  := "$(URI)"|g' \
		-e 's|"Mozilla PurpCMD"|"$(UA)"|g' \
		main.go > $(TMP_SRC)
	@if [ -n "$(PUBLICKEY)" ]; then \
		echo "[*] Embedding public key from $(PUBLICKEY) ..."; \
		python3 -c "\
import sys, base64; \
pem = open(sys.argv[1]).read(); \
b64 = ''.join(l for l in pem.splitlines() if not l.startswith('-----')); \
der = base64.b64decode(b64); \
arr = ','.join('0x{:02x}'.format(b) for b in der); \
decl = 'var publicKeyDER = []byte{' + arr + '}'; \
src = open(sys.argv[2]).read().replace('var publicKeyDER []byte', decl, 1); \
open(sys.argv[2], 'w').write(src) \
" $(PUBLICKEY) $(TMP_SRC); \
	fi
	@echo "[*] Building $(OUTPUT) ($(GOOS)/$(GOARCH)) ..."
	@GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=0 \
		go build -ldflags "-s -w" -o $(OUTPUT) $(TMP_SRC)
	@rm -f $(TMP_SRC)
	@echo "[+] Done: $(OUTPUT)"

.PHONY: build32
build32: ## Build 32-bit Linux binary
	@$(MAKE) build ARCH=386 GOARCH=386

.PHONY: compress
compress: build ## Build and compress with UPX
	upx -9 -o $(OUTPUT).upx $(OUTPUT)

.PHONY: compress32
compress32: build32 ## Build 32-bit and compress with UPX
	upx -9 -o $(OUTPUT).upx $(OUTPUT)

.PHONY: clean
clean: ## Remove build artifacts
	@rm -f $(TMP_SRC) _pubkey_snippet.go.tmp

## Help
.PHONY: help
help: ## Show this help
	@echo ''
	@echo 'Usage:'
	@echo '  make <target> [OPTION=value ...]'
	@echo ''
	@echo 'Options (with defaults):'
	@echo '  OUTPUT=$(OUTPUT)'
	@echo '  LHOST=$(LHOST)'
	@echo '  OS=$(OS)  ARCH=$(ARCH)'
	@echo '  URI=$(URI)'
	@echo '  UA=$(UA)'
	@echo '  TYPE=$(TYPE)'
	@echo '  PUBLICKEY=$(PUBLICKEY)'
	@echo ''
	@echo 'Targets:'
	@awk 'BEGIN {FS = ":.*?## "} \
	/^[a-zA-Z_-]+:.*?##/ { printf "  %-14s %s\n", $$1, $$2 } \
	/^## / { printf "\n%s\n", substr($$0,4) }' $(MAKEFILE_LIST)
