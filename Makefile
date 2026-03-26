GO ?= go
CGO_ENABLED ?= 0
BIN_DIR ?= bin
BIN := $(BIN_DIR)/tcp-over-kafka
GO_FILES := $(shell rg --files cmd pkg -g '*.go')
VENDOR_MODE := -mod=vendor
GIT_COMMIT := $(shell git rev-parse "HEAD^{commit}")
GIT_COMMIT_SHORT := $(shell git rev-parse --short=12 "HEAD^{commit}" 2>/dev/null || printf unknown)
BUILD_DATE := $(shell date -u +%y%m%d)
VERSION := v$(BUILD_DATE)-$(GIT_COMMIT_SHORT)
LDFLAGS := -s -w -X main.version=$(VERSION)
DEPLOY_MODE ?= systemd
BROKER_DEPLOY_MODE ?= $(DEPLOY_MODE)
NODE_A_DEPLOY_MODE ?= $(DEPLOY_MODE)
NODE_B_DEPLOY_MODE ?= $(DEPLOY_MODE)

.PHONY: all fmt vet test build vendor clean docker-build deploy-broker deploy-node-a deploy-node-b deploy-all deploy-systemd deploy-docker deploy-kubernetes e2e-test e2e-test-ssh e2e-test-https e2e-test-file

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
	BROKER_DEPLOY_MODE=$(BROKER_DEPLOY_MODE) bash ./hack/deploy-remote.sh broker

deploy-node-a: $(BIN)
	NODE_A_DEPLOY_MODE=$(NODE_A_DEPLOY_MODE) TCP_OVER_KAFKA_BINARY=$(BIN) bash ./hack/deploy-remote.sh node-a

deploy-node-b: $(BIN)
	NODE_B_DEPLOY_MODE=$(NODE_B_DEPLOY_MODE) TCP_OVER_KAFKA_BINARY=$(BIN) bash ./hack/deploy-remote.sh node-b

deploy-all: $(BIN)
	DEPLOY_MODE=$(DEPLOY_MODE) \
	BROKER_DEPLOY_MODE=$(BROKER_DEPLOY_MODE) \
	NODE_A_DEPLOY_MODE=$(NODE_A_DEPLOY_MODE) \
	NODE_B_DEPLOY_MODE=$(NODE_B_DEPLOY_MODE) \
	TCP_OVER_KAFKA_BINARY=$(BIN) bash ./hack/deploy-remote.sh all

deploy-systemd:
	$(MAKE) deploy-all DEPLOY_MODE=systemd

deploy-docker:
	$(MAKE) deploy-all DEPLOY_MODE=docker

deploy-kubernetes:
	$(MAKE) deploy-all DEPLOY_MODE=kubernetes

e2e-test-ssh:
	bash ./hack/test/ssh.sh

e2e-test-https:
	bash ./hack/test/https.sh

e2e-test-file:
	bash ./hack/test/file-transfer.sh

e2e-test:
	bash ./hack/test/run-all.sh

clean:
	rm -rf $(BIN_DIR) tcp-over-kafka
