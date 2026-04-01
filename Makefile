.PHONY: build test test-e2e verify run package package-inspect

NFPM_VERSION ?= v2.46.0
PACKAGE_NAME ?= rook-servicechannel-gateway
PACKAGE_ARCH ?= $(shell go env GOARCH)
PACKAGE_VERSION ?= 0.0.0-dev
PACKAGE_RELEASE ?= 1
DIST_DIR ?= dist
PACKAGE_STAGING_DIR ?= $(DIST_DIR)/package
PACKAGE_TARGET ?= $(DIST_DIR)/$(PACKAGE_NAME)_$(PACKAGE_VERSION)-$(PACKAGE_RELEASE)_$(PACKAGE_ARCH).deb
PACKAGE_BINARY ?= $(PACKAGE_STAGING_DIR)/$(PACKAGE_NAME)

build:
	go build ./...

test:
	go test ./...

test-e2e:
	go test ./tests/e2e -v

verify:
	go test ./... && go build ./...

run:
	go run ./cmd/gateway

package:
	mkdir -p $(PACKAGE_STAGING_DIR)
	GOOS=linux GOARCH=$(PACKAGE_ARCH) go build -o $(PACKAGE_BINARY) ./cmd/gateway
	VERSION=$(PACKAGE_VERSION) RELEASE=$(PACKAGE_RELEASE) PACKAGE_ARCH=$(PACKAGE_ARCH) \
	go run github.com/goreleaser/nfpm/v2/cmd/nfpm@$(NFPM_VERSION) package \
		--packager deb \
		--config nfpm.yaml \
		--target $(PACKAGE_TARGET)

package-inspect: package
	@echo "Archive members:"
	@ar t $(PACKAGE_TARGET)
	@echo "--- control entries ---"
	@ar p $(PACKAGE_TARGET) control.tar.gz | tar -tzf -
	@echo "--- data entries ---"
	@ar p $(PACKAGE_TARGET) data.tar.gz | tar -tzf -
