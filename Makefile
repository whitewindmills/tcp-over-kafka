GO ?= go
CGO_ENABLED ?= 0
BIN_DIR ?= bin
BIN := $(BIN_DIR)/tcp-over-kafka
GO_FILES := $(shell rg --files cmd pkg -g '*.go')
VENDOR_MODE := -mod=vendor
GIT_COMMIT := $(shell git rev-parse "HEAD^{commit}")
VERSION := $(shell git describe --tags --abbrev=14 "$(GIT_COMMIT)^{commit}" --always 2>/dev/null || git rev-parse --short HEAD)
LDFLAGS := -s -w -X main.version=$(VERSION)
DEPLOY_MODE ?= systemd
BROKER_DEPLOY_MODE ?= $(DEPLOY_MODE)
CLIENT_DEPLOY_MODE ?= $(DEPLOY_MODE)
SERVER_DEPLOY_MODE ?= $(DEPLOY_MODE)

.PHONY: all fmt vet test build vendor clean docker-build deploy-broker deploy-client deploy-server deploy-all deploy-systemd deploy-docker deploy-kubernetes

all: test build

fmt:
	gofmt -w $(GO_FILES)

vet:
	$(GO) vet $(VENDOR_MODE) ./...

test:
	CGO_ENABLED=$(CGO_ENABLED) $(GO) test $(VENDOR_MODE) -count=1 ./...

build: $(BIN)

$(BIN): $(GO_FILES) go.mod go.sum vendor
	mkdir -p $(BIN_DIR)
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(VENDOR_MODE) -trimpath -ldflags='$(LDFLAGS)' -o $(BIN) ./cmd/tcp-over-kafka

vendor: go.mod go.sum
	$(GO) mod vendor

docker-build:
	docker build --build-arg VERSION="$(VERSION)" -t tcp-over-kafka:$(VERSION) .

deploy-broker:
	BROKER_DEPLOY_MODE=$(BROKER_DEPLOY_MODE) ./hack/deploy-remote.sh broker

deploy-client: $(BIN)
	CLIENT_DEPLOY_MODE=$(CLIENT_DEPLOY_MODE) TCP_OVER_KAFKA_BINARY=$(BIN) ./hack/deploy-remote.sh client

deploy-server: $(BIN)
	SERVER_DEPLOY_MODE=$(SERVER_DEPLOY_MODE) TCP_OVER_KAFKA_BINARY=$(BIN) ./hack/deploy-remote.sh server

deploy-all: $(BIN)
	DEPLOY_MODE=$(DEPLOY_MODE) \
	BROKER_DEPLOY_MODE=$(BROKER_DEPLOY_MODE) \
	CLIENT_DEPLOY_MODE=$(CLIENT_DEPLOY_MODE) \
	SERVER_DEPLOY_MODE=$(SERVER_DEPLOY_MODE) \
	TCP_OVER_KAFKA_BINARY=$(BIN) ./hack/deploy-remote.sh all

deploy-systemd:
	$(MAKE) deploy-all DEPLOY_MODE=systemd

deploy-docker:
	$(MAKE) deploy-all DEPLOY_MODE=docker

deploy-kubernetes:
	$(MAKE) deploy-all DEPLOY_MODE=kubernetes

clean:
	rm -rf $(BIN_DIR) tcp-over-kafka
