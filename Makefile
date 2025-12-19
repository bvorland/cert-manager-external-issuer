# Makefile for cert-manager External Issuer
#
# Common tasks for building, testing, and deploying the external issuer.
#

# Image configuration
IMG ?= external-issuer:latest
MOCKCA_IMG ?= mockca-server:latest
REGISTRY ?= <your-registry>.azurecr.io

# Kubernetes configuration  
NAMESPACE ?= external-issuer-system

# Go configuration
GOOS ?= linux
GOARCH ?= amd64
CGO_ENABLED ?= 0

.PHONY: all
all: build

##@ Development

.PHONY: fmt
fmt: ## Format Go source code
	go fmt ./...

.PHONY: vet
vet: ## Run Go vet
	go vet ./...

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run

.PHONY: test
test: ## Run unit tests
	go test ./... -v -coverprofile=coverage.out

.PHONY: test-coverage
test-coverage: test ## Show test coverage
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

##@ Build

.PHONY: build
build: fmt vet ## Build the controller binary
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o bin/controller ./cmd/controller

.PHONY: build-mockca
build-mockca: fmt vet ## Build the MockCA server binary
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o bin/mockca-server ./cmd/mockca

.PHONY: build-all
build-all: build build-mockca ## Build all binaries

.PHONY: build-local
build-local: ## Build for local OS
	go build -o bin/controller ./cmd/controller

.PHONY: build-mockca-local
build-mockca-local: ## Build MockCA server for local OS
	go build -o bin/mockca-server ./cmd/mockca

.PHONY: run
run: ## Run controller locally (requires kubeconfig)
	go run ./cmd/controller

.PHONY: run-mockca
run-mockca: ## Run MockCA server locally
	go run ./cmd/mockca --log-level=debug

##@ Docker

.PHONY: docker-build
docker-build: ## Build controller Docker image
	docker build -t $(IMG) .

.PHONY: docker-build-mockca
docker-build-mockca: ## Build MockCA server Docker image
	docker build -f Dockerfile.mockca -t $(MOCKCA_IMG) .

.PHONY: docker-build-all
docker-build-all: docker-build docker-build-mockca ## Build all Docker images

.PHONY: docker-push
docker-push: ## Push controller Docker image to registry
	docker tag $(IMG) $(REGISTRY)/$(IMG)
	docker push $(REGISTRY)/$(IMG)

.PHONY: docker-push-mockca
docker-push-mockca: ## Push MockCA Docker image to registry
	docker tag $(MOCKCA_IMG) $(REGISTRY)/$(MOCKCA_IMG)
	docker push $(REGISTRY)/$(MOCKCA_IMG)

.PHONY: docker-build-push
docker-build-push: docker-build docker-push ## Build and push controller Docker image

.PHONY: docker-push-all
docker-push-all: docker-push docker-push-mockca ## Push all Docker images to registry

##@ Docker Hub

# Docker Hub configuration
DOCKER_HUB_USER ?= <your-dockerhub-username>

.PHONY: dockerhub-tag
dockerhub-tag: ## Tag images for Docker Hub
	docker tag $(IMG) $(DOCKER_HUB_USER)/external-issuer:latest
	docker tag $(MOCKCA_IMG) $(DOCKER_HUB_USER)/mockca-server:latest

.PHONY: dockerhub-push
dockerhub-push: dockerhub-tag ## Push images to Docker Hub
	docker push $(DOCKER_HUB_USER)/external-issuer:latest
	docker push $(DOCKER_HUB_USER)/mockca-server:latest

.PHONY: dockerhub-build-push
dockerhub-build-push: docker-build-all dockerhub-push ## Build and push all images to Docker Hub

##@ Deployment

.PHONY: install-crds
install-crds: ## Install CRDs into cluster
	kubectl apply -f deploy/crds/

.PHONY: uninstall-crds
uninstall-crds: ## Uninstall CRDs from cluster
	kubectl delete -f deploy/crds/ --ignore-not-found

.PHONY: install-rbac
install-rbac: ## Install RBAC resources
	kubectl apply -f deploy/rbac/

.PHONY: deploy
deploy: install-crds install-rbac ## Deploy controller to cluster
	kubectl apply -f deploy/config/pki-config.yaml
	kubectl apply -f deploy/deployment.yaml

.PHONY: undeploy
undeploy: ## Remove controller from cluster
	kubectl delete -f deploy/deployment.yaml --ignore-not-found
	kubectl delete -f deploy/rbac/ --ignore-not-found
	kubectl delete -f deploy/config/ --ignore-not-found

.PHONY: deploy-issuer
deploy-issuer: ## Deploy example cluster issuer
	kubectl apply -f deploy/issuer/cluster-issuer.yaml

.PHONY: deploy-all
deploy-all: deploy deploy-issuer ## Deploy everything
	@echo "External Issuer deployed successfully!"
	@echo "Run 'kubectl get externalclusterissuers' to verify"

##@ ACR (Azure Container Registry)

.PHONY: acr-login
acr-login: ## Login to Azure Container Registry
	az acr login --name $(shell echo $(REGISTRY) | cut -d. -f1)

.PHONY: acr-build
acr-build: ## Build image using ACR Tasks
	az acr build --registry $(shell echo $(REGISTRY) | cut -d. -f1) --image $(IMG) .

##@ Verification

.PHONY: verify
verify: ## Verify deployment
	@echo "=== Checking Pods ==="
	kubectl get pods -n $(NAMESPACE)
	@echo ""
	@echo "=== Checking Issuers ==="
	kubectl get externalclusterissuers
	kubectl get externalissuers -A
	@echo ""
	@echo "=== Checking Certificates ==="
	kubectl get certificates -A

.PHONY: logs
logs: ## Show controller logs
	kubectl logs -n $(NAMESPACE) -l app.kubernetes.io/name=external-issuer -f

.PHONY: describe
describe: ## Describe controller pod
	kubectl describe pod -n $(NAMESPACE) -l app.kubernetes.io/name=external-issuer

##@ Examples

.PHONY: example-basic
example-basic: ## Create a basic example certificate
	kubectl apply -f examples/basic-certificate.yaml
	@echo "Certificate created. Run 'kubectl get certificate example-app-tls' to check status"

.PHONY: example-istio
example-istio: ## Deploy Istio integration example
	kubectl apply -f examples/istio-integration.yaml
	@echo "Istio Gateway and Certificate created"

.PHONY: clean-examples
clean-examples: ## Remove example resources
	kubectl delete -f examples/ --ignore-not-found

##@ MockCA Server

.PHONY: deploy-mockca
deploy-mockca: ## Deploy standalone MockCA server
	kubectl apply -f deploy/mockca-server.yaml
	@echo "MockCA server deployed to mockca-system namespace"

.PHONY: undeploy-mockca
undeploy-mockca: ## Remove MockCA server
	kubectl delete -f deploy/mockca-server.yaml --ignore-not-found

.PHONY: logs-mockca
logs-mockca: ## Show MockCA server logs
	kubectl logs -n mockca-system -l app.kubernetes.io/name=mockca-server -f

##@ Clean

.PHONY: clean
clean: ## Clean build artifacts
	rm -rf bin/
	rm -f coverage.out coverage.html

.PHONY: clean-all
clean-all: clean undeploy uninstall-crds ## Clean everything

##@ Help

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
