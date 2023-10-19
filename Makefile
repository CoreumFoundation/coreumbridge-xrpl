GO_IMPORT_PREFIX=github.com/CoreumFoundation
GO_SCAN_FILES := $(shell find . -type f -name '*.go' -not -name '*mock.go' -not -name '*_gen.go' -not -path "*/vendor/*")
ROOT_DIR:=$(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
CONTRACT_DIR:=$(ROOT_DIR)/contract
INTEGRATION_TESTS_DIR:=$(ROOT_DIR)/integration-tests
RELAYER_DIR:=$(ROOT_DIR)/relayer
BUILD_DIR:=$(ROOT_DIR)/build

###############################################################################
###                                  Build                                  ###
###############################################################################

.PHONY: build-relayer
build-relayer:
	cd $(RELAYER_DIR) && CGO_ENABLED=0 go build --trimpath -mod=readonly -ldflags '-extldflags=-static'  -o $(BUILD_DIR)/coreumbridge-xrpl-relayer ./cmd

.PHONY: build-relayer-docker
build-relayer-docker:
	docker build -f $(RELAYER_DIR)/Dockerfile . -t coreumbridge-xrpl-relayer:local

.PHONY: build-relayer-in-docker
build-relayer-in-docker:
	make build-relayer-docker
	mkdir -p $(BUILD_DIR)
	docker run --rm --entrypoint cat coreumbridge-xrpl-relayer:local /app/coreumbridge-xrpl-relayer > $(BUILD_DIR)/coreumbridge-xrpl-relayer

.PHONY: build-contract
build-contract:
	docker run --user $(id -u):$(id -g) --rm -v $(CONTRACT_DIR):/code \
      --mount type=volume,source="coreumbridge_xrpl_cache",target=/code/target \
      --mount type=volume,source=registry_cache,target=/usr/local/cargo/registry \
       cosmwasm/rust-optimizer:0.14.0
	mkdir -p $(BUILD_DIR)
	cp $(CONTRACT_DIR)/artifacts/coreumbridge_xrpl.wasm $(BUILD_DIR)/coreumbridge_xrpl.wasm

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

.PHONY: lint-go
lint-go:
	crust lint/current-dir

.PHONY: lint-contract
lint-contract:
	cd $(CONTRACT_DIR) && cargo clippy --verbose -- -D warnings

.PHONY: test-integration
test-integration:
    # test each directory separately to prevent faucet concurrent access
	for d in $(INTEGRATION_TESTS_DIR)/*/; \
	 do cd $$d && go clean -testcache && go test -v --tags=integrationtests -mod=readonly -parallel=4 ./... || exit 1; \
	done

.PHONY: test-relayer
test-relayer:
	cd $(RELAYER_DIR) && go clean -testcache && go test -v -mod=readonly -parallel=4 -timeout 500ms ./...

.PHONY: test-contract
test-contract:
	cd $(CONTRACT_DIR) && cargo test --verbose

.PHONY: restart-dev-env
restart-dev-env:
	crust znet remove && crust znet start --profiles=1cored,xrpl --timeout-commit 0.5s

.PHONY: rebuild-dev-env
rebuild-dev-env:
	crust build/crust images/cored
