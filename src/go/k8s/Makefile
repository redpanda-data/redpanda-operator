# Image URL to use all building/pushing image targets
OPERATOR_IMG_LATEST ?= "localhost/redpanda-operator:dev"
CONFIGURATOR_IMG_LATEST ?= "localhost/configurator:dev"

HELM_CONTROLLER_VERSION ?= v0.37.2
SOURCE_CONTROLLER_VERSION ?= v1.2.3

# needed since we are using a private repo
SSH_AUTH_SOCK ?= ""

# default redpanda image to load
REDPANDA_IMG ?= "localhost/redpanda:dev"

# default redpanda namespace
NAMESPACE ?= "redpanda"

ifeq (aarch64,$(shell uname -m))
	TARGETARCH = arm64
else ifeq (x86_64,$(shell uname -m))
	TARGETARCH = amd64
else
	TARGETARCH ?= $(shell uname -m)
endif

TARGETOS ?= linux

SHELL := /bin/bash

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
	GOBIN=$(shell go env GOPATH)/bin
else
	GOBIN=$(shell go env GOBIN)
endif

ifneq ($(TEST_NAME), )
	TEST_ONLY_FLAG = --test $(TEST_NAME)
endif

all: build

# Run tests
test: manifests generate fmt
	cd ../../.. && nix develop -c task k8s:run-unit-tests

# Build manager binary
.PHONY: manager
manager: manifests generate fmt
	go build -o bin/manager cmd/main.go

# Build manager binary
.PHONY: configurator
configurator: manifests generate fmt
	go build -o bin/configurator cmd/configurator/main.go

.PHONY: build
build: manager configurator

.PHONY: fmt
fmt: ## Run go fmt against code.
	cd ../../.. && nix develop -c task fmt

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate vet manifests fmt
	go run ./cmd/main.go

# Run against the configured Kubernetes cluster in ~/.kube/config
run-v2: generate vet manifests fmt
	go run ./cmd/main.go --namespace=$(NAMESPACE) --log-level=debug

# Install CRDs into a cluster
install: manifests kustomize
	$(KUSTOMIZE) build config/crd | kubectl apply --server-side -f -

# Uninstall CRDs from a cluster
uninstall: manifests kustomize
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
deploy: manifests kustomize
	cd config/manager && $(KUSTOMIZE) edit set image vectorized/redpanda-operator=${OPERATOR_IMG_LATEST}
	$(KUSTOMIZE) build config/default | kubectl apply --server-side -f -
	kind load docker-image ${REDPANDA_IMG}

# Deploy pre loaded controller in the configured Kind Kubernetes cluster
deploy-to-kind: manifests kustomize push-to-kind deploy

# UnDeploy controller from the configured Kubernetes cluster in ~/.kube/config
undeploy:
	$(KUSTOMIZE) build config/default | kubectl delete -f -

# Generate manifests e.g. CRD, RBAC etc.
.PHONY: manifests
manifests:
	cd ../../.. && nix develop -c task k8s:generate-manifests

# Generate code
.PHONY: generate
generate:
	cd ../../.. && nix develop -c task k8s:generate-controller-code

# Creates kind cluster
kind-create:
	(kind get clusters | grep kind && echo "kind cluster already exists") || (kind create cluster --config kind.yaml && echo "kind cluster created")

# Install cert-manager
certmanager-install: kind-create
	./hack/install-cert-manager.sh

# Build the docker image
docker-build:
	echo "~~~ Building operator image :docker:"
	cd ../../.. && task k8s:build-operator-images

# Build the docker image
docker-build-configurator:
	echo "~~~ Building configurator image :docker:"
	cd ../../.. && task k8s:build-operator-images

# Preload controller image to kind cluster
push-to-kind: kind-create certmanager-install
	kind load docker-image ${OPERATOR_IMG_LATEST}
	kind load docker-image ${CONFIGURATOR_IMG_LATEST}

# Execute end to end tests
e2e-tests:
	cd ../../.. && nix develop -c task k8s:run-kuttl-tests

# Execute end to end tests v2
e2e-tests-v2: kuttl
	cd ../../.. && nix develop -c task k8s:run-k8s-v2-tests

# Create resources for the e2e-tests-v2-ci
create-e2e-tests-v2-helm:
	./hack/v2-helm-setup.sh

# Execute end to end tests v2
e2e-tests-v2-helm:
	echo "~~~ Running kuttl tests :k8s:redpanda"
	cd ../../.. && nix develop -c task k8s:run-k8s-v2-helm-tests

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUSTOMIZE ?= $(LOCALBIN)/kustomize

## Tool Versions
KUSTOMIZE_VERSION ?= v5.1.1

KUSTOMIZE_INSTALL_SCRIPT ?= "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	test -s $(LOCALBIN)/kustomize || { curl -Ss $(KUSTOMIZE_INSTALL_SCRIPT) | bash -s -- $(subst v,,$(KUSTOMIZE_VERSION)) $(LOCALBIN); }

.PHONY: lint
lint: 
	cd ../../.. && nix develop -c task lint

.PHONY: lint-fix
lint-fix:
	cd ../../.. && nix develop -c task lint-fix

.PHONY: install-prometheus
install-prometheus:
	kubectl create -f https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/e23ff77fceba6a5d9f190f5d1a123c87701dc964/bundle.yaml || true

.PHONY: update-external-crds
update-external-crds:
	echo "building source-controller crds..."
	kustomize build "https://github.com/fluxcd/source-controller//config/crd?ref=$(SOURCE_CONTROLLER_VERSION)" -o config/crd/bases/toolkit.fluxcd.io/source-controller.yaml
	echo "building helm-controller crds..."
	kustomize build "https://github.com/fluxcd/helm-controller//config/crd?ref=$(HELM_CONTROLLER_VERSION)" -o config/crd/bases/toolkit.fluxcd.io/helm-controller.yaml
