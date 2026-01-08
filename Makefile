PROJECT_NAME := namespace-isolator
MODULE := github.com/fulcro-cloud/namespace-isolator

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

REGISTRY ?= ghcr.io/fulcro-cloud
IMAGE_TAG ?= latest

AGENT_IMAGE := $(REGISTRY)/namespace-isolator-agent:$(IMAGE_TAG)
PLUGIN_IMAGE := $(REGISTRY)/nri-namespace-isolator:$(IMAGE_TAG)

GOOS ?= linux
GOARCH ?= amd64
CGO_ENABLED ?= 0
GO := CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) go

LDFLAGS := -w -s \
	-X main.Version=$(VERSION) \
	-X main.Commit=$(COMMIT) \
	-X main.BuildDate=$(BUILD_DATE)

BIN_DIR := bin
AGENT_BINARY := $(BIN_DIR)/namespace-isolator-agent
PLUGIN_BINARY := $(BIN_DIR)/nri-namespace-isolator

DEPLOY_DIR := deploy/kubernetes

.PHONY: all
all: build

.PHONY: build
build: build-agent build-plugin ## Build all binaries

.PHONY: build-agent
build-agent: $(AGENT_BINARY) ## Build the agent binary

$(AGENT_BINARY):
	@echo ">>> Building namespace-isolator-agent..."
	@mkdir -p $(BIN_DIR)
	$(GO) build -ldflags="$(LDFLAGS)" -o $(AGENT_BINARY) ./cmd/agent

.PHONY: build-plugin
build-plugin: $(PLUGIN_BINARY) ## Build the NRI plugin binary

$(PLUGIN_BINARY):
	@echo ">>> Building nri-namespace-isolator..."
	@mkdir -p $(BIN_DIR)
	$(GO) build -ldflags="$(LDFLAGS)" -o $(PLUGIN_BINARY) ./cmd/nri-plugin

.PHONY: docker
docker: docker-agent docker-plugin ## Build all Docker images

.PHONY: docker-agent
docker-agent: ## Build the agent Docker image
	@echo ">>> Building Docker image: $(AGENT_IMAGE)"
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-f Dockerfile.agent \
		-t $(AGENT_IMAGE) \
		.

.PHONY: docker-plugin
docker-plugin: ## Build the NRI plugin Docker image
	@echo ">>> Building Docker image: $(PLUGIN_IMAGE)"
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-f Dockerfile.nri-plugin \
		-t $(PLUGIN_IMAGE) \
		.

.PHONY: push
push: push-agent push-plugin ## Push all Docker images to registry

.PHONY: push-agent
push-agent: ## Push the agent Docker image
	@echo ">>> Pushing image: $(AGENT_IMAGE)"
	docker push $(AGENT_IMAGE)

.PHONY: push-plugin
push-plugin: ## Push the NRI plugin Docker image
	@echo ">>> Pushing image: $(PLUGIN_IMAGE)"
	docker push $(PLUGIN_IMAGE)

LOCAL_AGENT_IMAGE := namespace-isolator-agent:$(IMAGE_TAG)
LOCAL_PLUGIN_IMAGE := nri-namespace-isolator:$(IMAGE_TAG)

.PHONY: docker-local
docker-local: ## Build Docker images with local tags (no registry)
	@echo ">>> Building local Docker image: $(LOCAL_AGENT_IMAGE)"
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-f Dockerfile.agent \
		-t $(LOCAL_AGENT_IMAGE) \
		.
	@echo ">>> Building local Docker image: $(LOCAL_PLUGIN_IMAGE)"
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-f Dockerfile.nri-plugin \
		-t $(LOCAL_PLUGIN_IMAGE) \
		.

.PHONY: import-k3s
import-k3s: docker-local ## Build and import images directly into K3s
	@echo ">>> Exporting and importing images into K3s..."
	docker save $(LOCAL_AGENT_IMAGE) | sudo k3s ctr images import -
	docker save $(LOCAL_PLUGIN_IMAGE) | sudo k3s ctr images import -
	@echo ">>> Images imported successfully!"
	@sudo k3s ctr images ls | grep -E "namespace-isolator|nri-namespace"

.PHONY: deploy-local
deploy-local: import-k3s ## Build, import and deploy locally to K3s
	@echo ">>> Deploying to K3s (local)..."
	kubectl apply -f deploy/crds/
	kubectl apply -f deploy/kubernetes/rbac.yaml
	kubectl apply -f deploy/kubernetes/agent-daemonset-local.yaml
	kubectl apply -f deploy/kubernetes/nri-plugin-daemonset-local.yaml

.PHONY: deploy
deploy: ## Deploy to Kubernetes using kustomize
	@echo ">>> Deploying to Kubernetes..."
	kubectl apply -k $(DEPLOY_DIR)

.PHONY: deploy-crds
deploy-crds: ## Deploy only CRDs
	@echo ">>> Deploying CRDs..."
	kubectl apply -f deploy/crds/

.PHONY: undeploy
undeploy: ## Remove deployment from Kubernetes
	@echo ">>> Removing deployment..."
	kubectl delete -k $(DEPLOY_DIR) --ignore-not-found=true

.PHONY: test
test: ## Run tests
	@echo ">>> Running tests..."
	go test -v -race -coverprofile=coverage.out ./...

.PHONY: test-coverage
test-coverage: test ## Run tests and show coverage report
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

.PHONY: fmt
fmt: ## Format Go code
	go fmt ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run ./...

.PHONY: mod-tidy
mod-tidy: ## Tidy go modules
	go mod tidy

.PHONY: mod-download
mod-download: ## Download go modules
	go mod download

.PHONY: generate
generate: ## Run go generate
	go generate ./...

.PHONY: generate-deepcopy
generate-deepcopy: ## Generate deep copy functions
	controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./pkg/api/..."

.PHONY: clean
clean: ## Clean build artifacts
	rm -rf $(BIN_DIR)
	rm -f coverage.out coverage.html

.PHONY: clean-docker
clean-docker: ## Remove Docker images
	-docker rmi $(AGENT_IMAGE) 2>/dev/null
	-docker rmi $(PLUGIN_IMAGE) 2>/dev/null

.PHONY: clean-all
clean-all: clean clean-docker ## Clean everything

.PHONY: help
help: ## Show this help message
	@echo "NRI Namespace Isolator - Available targets:"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "Configuration variables:"
	@echo "  REGISTRY     = $(REGISTRY)"
	@echo "  IMAGE_TAG    = $(IMAGE_TAG)"
	@echo "  VERSION      = $(VERSION)"
	@echo "  GOOS         = $(GOOS)"
	@echo "  GOARCH       = $(GOARCH)"
