GO_IMPORT_PREFIX=github.com/CoreumFoundation
GO_SCAN_FILES := $(shell find . -type f -name '*.go' -not -name '*mock.go' -not -name '*_gen.go' -not -path "*/vendor/*")
ROOT_DIR:=$(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
CONTRACT_DIR:=$(ROOT_DIR)/contract
INTEGRATION_TESTS_DIR:=$(ROOT_DIR)/integration-tests
RELAYER_DIR:=$(ROOT_DIR)/relayer
BUILD_DIR?=$(ROOT_DIR)/build
GIT_TAG:=$(shell git describe --tags --exact-match 2>/dev/null)
GIT_SHA:=$(shell git rev-parse HEAD)
DOCKER_PUSH_TAG?=$(shell git describe --tags --exact-match 2>/dev/null || git rev-parse HEAD)
LD_FLAGS:="-extldflags=-static \
-X 'github.com/CoreumFoundation/coreumbridge-xrpl/relayer/buildinfo.VersionTag=$(GIT_TAG)' \
-X 'github.com/CoreumFoundation/coreumbridge-xrpl/relayer/buildinfo.GitCommit=$(GIT_SHA)' \
"
GOOS?=
GOARCH?=
BINARY_NAME?=coreumbridge-xrpl-relayer

###############################################################################
###                                  Build                                  ###
###############################################################################

.PHONY: build-relayer
build-relayer:
	cd $(RELAYER_DIR) && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build --trimpath -mod=readonly -ldflags $(LD_FLAGS)  -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd

.PHONY: release-relayer
release-relayer:
	@$(MAKE) build-relayer-in-docker GOOS=linux GOARCH=amd64 BINARY_NAME=relayer-linux-amd64
	@$(MAKE) build-relayer-in-docker GOOS=linux GOARCH=arm64 BINARY_NAME=relayer-linux-arm64
	@$(MAKE) build-relayer-in-docker GOOS=darwin GOARCH=amd64 BINARY_NAME=relayer-darwin-amd64
	@$(MAKE) build-relayer-in-docker GOOS=darwin GOARCH=arm64 BINARY_NAME=relayer-darwin-arm64

.PHONY: build-relayer-docker
build-relayer-docker:
	docker buildx build --build-arg GOOS=$(GOOS) --build-arg GOARCH=$(GOARCH) -f $(RELAYER_DIR)/Dockerfile . -t coreumbridge-xrpl-relayer:local

.PHONY: push-relayer-docker
push-relayer-docker: build-relayer-docker
	docker image tag coreumbridge-xrpl-relayer:local coreumfoundation/coreumbridge-xrpl-relayer:$(DOCKER_PUSH_TAG)
	docker image push coreumfoundation/coreumbridge-xrpl-relayer:$(DOCKER_PUSH_TAG)

.PHONY: build-relayer-in-docker
build-relayer-in-docker:
	make build-relayer-docker
	mkdir -p $(BUILD_DIR)
	docker run --rm --entrypoint cat coreumbridge-xrpl-relayer:local /bin/coreumbridge-xrpl-relayer > $(BUILD_DIR)/$(BINARY_NAME)

.PHONY: build-contract
build-contract:
	docker run --user $(id -u):$(id -g) --rm -v $(CONTRACT_DIR):/code \
      -v $(CONTRACT_DIR)/target:/target \
      -v $(CONTRACT_DIR)/target:/usr/local/cargo/registry \
      cosmwasm/optimizer:0.15.0
	mkdir -p $(BUILD_DIR)
	cp $(CONTRACT_DIR)/artifacts/coreumbridge_xrpl.wasm $(BUILD_DIR)/coreumbridge_xrpl.wasm

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

.PHONY: lint-go
lint-go:
	crust lint/current-dir

.PHONY: lint-contract
lint-contract:
	cd $(CONTRACT_DIR) && cargo clippy --verbose -- -D warnings || exit 1;

.PHONY: test-integration
test-integration:
	# test each directory separately to prevent faucet concurrent access
	for d in $(INTEGRATION_TESTS_DIR)/*/; \
	 do make test-single-integration TESTS_DIR="$$d" || exit 1; \
	done

.PHONY: test-single-integration
test-single-integration:
	cd $(TESTS_DIR) && go test -v --tags=integrationtests -mod=readonly -parallel=20 -timeout 10m ./... || exit 1;

.PHONY: test-relayer
test-relayer:
	cd $(RELAYER_DIR) && go clean -testcache && go test -v -mod=readonly -parallel=20 -timeout 5s ./...

.PHONY: test-contract
test-contract:
	cd $(CONTRACT_DIR) && cargo test --verbose

.PHONY: restart-dev-env
restart-dev-env:
	crust znet remove && crust znet start --profiles=1cored,xrpl --timeout-commit 0.3s

.PHONY: build-dev-env
build-dev-env:
	crust build/crust build/znet images/cored

.PHONY: smoke
smoke:
	make lint-contract
	make build-dev-contract
	make test-contract
	make mockgen-go
	make fmt-go
	make test-relayer
	make restart-dev-env
	make test-integration
	# last since checks the committed files
	make lint-go
