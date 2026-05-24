.PHONY: all build deploy test local typecheck
all: local

APP_NAME    := karaoke
DEPLOY_HOST := vps

BUILD_DIR   := build
BINARY_NAME := karaoke_site

GOOS        := linux
GOARCH      := amd64
CGO_ENABLED := 0

build:
	@echo "Building frontend..."
	go run release/frontend.go
	@echo "Building $(BINARY_NAME)..."
	mkdir -p $(BUILD_DIR)
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) \
	  go build -tags=release -ldflags="-s -w" \
	    -o $(BUILD_DIR)/$(BINARY_NAME) release/release.go

deploy: build
	deploy $(APP_NAME) $(DEPLOY_HOST) $(BUILD_DIR)/$(BINARY_NAME)

test:
	go test ./backend/

typecheck:
	npx tsc --noEmit

local:
	go run karaoke/local
