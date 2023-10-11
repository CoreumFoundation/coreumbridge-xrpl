GO_IMPORT_PREFIX=github.com/CoreumFoundation
GO_SCAN_FILES := $(shell find . -type f -name '*.go' -not -name '*mock.go' -not -name '*_gen.go' -not -path "*/vendor/*")
ROOT_DIR:=$(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
CONTRACT_DIR:=$(ROOT_DIR)/contract
INTEGRATION_TESTS_DIR:=$(ROOT_DIR)/integration-tests
RELAYER_DIR:=$(ROOT_DIR)/relayer
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
	 do cd $$d && go clean -testcache && go test -v --tags=integrationtests -mod=readonly -parallel=4 ./... ; \
	done

.PHONY: test-relayer
test-relayer:
	cd $(RELAYER_DIR) && go clean -testcache && go test -v -mod=readonly -parallel=4 -timeout 500ms ./...

.PHONY: test-contract
test-contract:
	cd $(CONTRACT_DIR) && cargo test --verbose

.PHONY: restart-dev-env
restart-dev-env:
	crust znet remove && crust znet start --profiles=3cored,xrpl --timeout-commit 0.5s

.PHONY: rebuild-dev-env
rebuild-dev-env:
	crust build/crust images/cored

.PHONY: build-contract
build-contract:
	docker run --user $(id -u):$(id -g) --rm -v $(CONTRACT_DIR):/code \
      --mount type=volume,source="contract_cache",target=/code/target \
      --mount type=volume,source=registry_cache,target=/usr/local/cargo/registry \
       cosmwasm/rust-optimizer:0.14.0
