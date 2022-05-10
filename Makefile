
FIRST_GOPATH := $(firstword $(subst :, ,$(shell go env GOPATH)))
OS ?= $(shell uname -s | tr '[A-Z]' '[a-z]')
ARCH ?= $(shell uname -m)
GOARCH ?= $(shell go env GOARCH)
BIN_NAME ?= warp-speed-debugging-demo

TMP_DIR := $(shell pwd)/tmp
BIN_DIR ?= $(TMP_DIR)/bin

$(TMP_DIR):
	mkdir -p $(TMP_DIR)

$(BIN_DIR):
	mkdir -p $(BIN_DIR)

$(BIN_NAME): deps main.go $(wildcard *.go) $(wildcard */*.go)
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(GOARCH) GO111MODULE=on GOPROXY=https://proxy.golang.org go build -a -ldflags '-s -w' -o $(BIN_NAME) .

.PHONY: deps
deps: go.mod go.sum
	go mod tidy
	go mod download all
	go mod verify

.PHONY: build
build: $(BIN_NAME)

.PHONY: container-test
container-test: build
	@docker build \
		-f Dockerfile \
		-t warp-speed-debugging .

.PHONY: test-interactive
test-interactive: container-test
	@rm -rf e2e_*
	CGO_ENABLED=1 GO111MODULE=on go test -v -test.timeout=9999m ./test

.PHONY: clean
clean:
	-rm -rf tmp/bin
	-rm -rf tmp/src
	-rm $(BIN_NAME)