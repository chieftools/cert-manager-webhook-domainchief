GO ?= go

IMAGE_NAME ?= ghcr.io/chieftools/cert-manager-webhook-domainchief
IMAGE_TAG ?= dev

CHART_DIR := charts/cert-manager-webhook-domainchief

LOCALBIN ?= $(shell pwd)/bin
ENVTEST ?= $(LOCALBIN)/setup-envtest
# Both derived from go.mod, e.g. controller-runtime v0.24.1 => release-0.24 and k8s.io/api v0.36.2 => 1.36.x.
ENVTEST_VERSION ?= $(shell $(GO) list -m -f '{{.Version}}' sigs.k8s.io/controller-runtime | sed -E 's/^v([0-9]+)\.([0-9]+).*/release-\1.\2/')
ENVTEST_K8S_VERSION ?= $(shell $(GO) list -m -f '{{.Version}}' k8s.io/api | sed -E 's/^v0\.([0-9]+).*/1.\1.x/')

.PHONY: test
test:
	$(GO) test ./...

# Runs the cert-manager conformance suite against the real Domain Chief API.
# Requires TEST_ZONE_NAME (e.g. example.com.) and DOMAINCHIEF_API_KEY to be set.
.PHONY: test-conformance
test-conformance: $(ENVTEST)
	@ASSETS="$$($(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" && \
	TEST_ASSET_ETCD="$$ASSETS/etcd" \
	TEST_ASSET_KUBE_APISERVER="$$ASSETS/kube-apiserver" \
	TEST_ASSET_KUBECTL="$$ASSETS/kubectl" \
	$(GO) test -tags conformance -run TestConformance -v -count=1 .

.PHONY: lint
lint:
	$(GO) vet ./...
	$(GO) vet -tags conformance ./...
	helm lint $(CHART_DIR)

.PHONY: build
build:
	docker build --build-arg VERSION=$(IMAGE_TAG) -t "$(IMAGE_NAME):$(IMAGE_TAG)" .

.PHONY: rendered-manifest.yaml
rendered-manifest.yaml:
	helm template cert-manager-webhook-domainchief $(CHART_DIR) \
		--namespace cert-manager \
		--set image.repository=$(IMAGE_NAME) \
		--set image.tag=$(IMAGE_TAG)

.PHONY: clean
clean:
	chmod -R u+w $(LOCALBIN) 2>/dev/null || true
	rm -rf $(LOCALBIN)

$(ENVTEST):
	mkdir -p $(LOCALBIN)
	GOBIN=$(LOCALBIN) $(GO) install sigs.k8s.io/controller-runtime/tools/setup-envtest@$(ENVTEST_VERSION)
