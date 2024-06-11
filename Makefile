BUILDER = ./bin/coreumbridge-xrpl-builder

GO_IMPORT_PREFIX=github.com/CoreumFoundation
GO_SCAN_FILES := $(shell find . -type f -name '*.go' -not -name '*mock.go' -not -name '*_gen.go' -not -path "*/vendor/*")
ROOT_DIR:=$(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
CONTRACT_DIR:=$(ROOT_DIR)/contract
INTEGRATION_TESTS_DIR:=$(ROOT_DIR)/integration-tests
RELAYER_DIR:=$(ROOT_DIR)/relayer
BUILD_DIR?=$(ROOT_DIR)/build
GIT_TAG:=$(shell git describe --tags --exact-match 2>/dev/null)
ifeq ($(GIT_TAG),)
GIT_TAG:=devel
endif
GIT_SHA:=$(shell git rev-parse HEAD)
DOCKER_PUSH_TAG?=$(shell git describe --tags --exact-match 2>/dev/null || git rev-parse HEAD)
LD_FLAGS:="-extldflags=-static \
-X 'github.com/CoreumFoundation/coreumbridge-xrpl/relayer/buildinfo.VersionTag=$(GIT_TAG)' \
-X 'github.com/CoreumFoundation/coreumbridge-xrpl/relayer/buildinfo.GitCommit=$(GIT_SHA)' \
"
GOOS?=
GOARCH?=
BINARY_NAME?=coreumbridge-xrpl-relayer
RELEASE_VERSION=v1.1.0

.PHONY: znet-start
znet-start:
	$(BUILDER) znet start --profiles=bridge-xrpl

.PHONY: znet-remove
znet-remove:
	$(BUILDER) znet remove

.PHONY: lint
lint:
	$(BUILDER) lint

.PHONY: test
test:
	$(BUILDER) test

.PHONY: fuzz-test
fuzz-test:
	$(BUILDER) fuzz-test

.PHONY: build-relayer
build-relayer:
	$(BUILDER) build/relayer

.PHONY: build-contract
build-contract:
	$(BUILDER) build/contract

.PHONY: images
images:
	$(BUILDER) images

.PHONY: release
release:
	$(BUILDER) release

.PHONY: release-images
release-images:
	$(BUILDER) release/images

.PHONY: dependencies
dependencies:
	$(BUILDER) download

.PHONY: integration-tests-xrpl
integration-tests-xrpl:
	$(BUILDER) integration-tests/xrpl

.PHONY: integration-tests-processes
integration-tests-processes:
	$(BUILDER) integration-tests/processes

.PHONY: integration-tests-contract
integration-tests-contract:
	$(BUILDER) integration-tests/contract

.PHONY: integration-tests-stress
integration-tests-stress:
	$(BUILDER) integration-tests/stress

###############################################################################
###                                  Build                                  ###
###############################################################################

.PHONY: build-dev-contract
build-dev-contract:
	rustup target add wasm32-unknown-unknown
	cargo install wasm-opt --locked
	# Those RUSTFLAGS reduce binary size considerably
	cd $(CONTRACT_DIR) && RUSTFLAGS='-C link-arg=-s' cargo wasm
	mkdir -p $(BUILD_DIR)
	cp $(CONTRACT_DIR)/target/wasm32-unknown-unknown/release/coreumbridge_xrpl.wasm $(BUILD_DIR)/coreumbridge_xrpl_not_opt.wasm
	wasm-opt -Os --signext-lowering $(BUILD_DIR)/coreumbridge_xrpl_not_opt.wasm -o $(BUILD_DIR)/coreumbridge_xrpl.wasm
	rm $(BUILD_DIR)/coreumbridge_xrpl_not_opt.wasm

###############################################################################
###                               Development                               ###
###############################################################################

.PHONY: fmt-go
fmt-go:
	which gofumpt || go install mvdan.cc/gofumpt@v0.5.0
	which gogroup || go install github.com/vasi-stripe/gogroup/cmd/gogroup@v0.0.0-20200806161525-b5d7f67a97b5
	gofumpt -lang=v2.1 -extra -w $(GO_SCAN_FILES)
	gogroup -order std,other,prefix=$(GO_IMPORT_PREFIX) -rewrite $(GO_SCAN_FILES)

.PHONY: mockgen-go
mockgen-go:
	which mockgen || go install github.com/golang/mock/mockgen@v1.6.0
	cd $(RELAYER_DIR) && go generate ./...
	make fmt-go

.PHONY: lint-contract
lint-contract:
	cd $(CONTRACT_DIR) && cargo clippy --verbose -- -D warnings || exit 1;

.PHONY: test-contract
test-contract:
	cd $(CONTRACT_DIR) && cargo test --verbose

.PHONY: restart-bridge-znet-env
restart-bridge-znet-env:
	docker compose -p bridge-znet -f ./infra/composer/docker-compose.yaml stop
	$(BUILDER) znet remove
	docker compose -p bridge-znet -f ./infra/composer/docker-compose.yaml down
	$(BUILDER)znet start --profiles=bridge-xrpl
	docker compose -p bridge-znet -f ./infra/composer/docker-compose.yaml up -d
