GO_IMPORT_PREFIX=github.com/CoreumFoundation
GO_SCAN_FILES := $(shell find . -type f -name '*.go' -not -name '*mock.go' -not -name '*_gen.go' -not -path "*/vendor/*")
ROOT_DIR:=$(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
CONTRACT_DIR:=$(ROOT_DIR)/contract
INTEGRATION_TESTS_DIR:=$(ROOT_DIR)/integration-tests
RELAYER_DIR:=$(ROOT_DIR)/relayer
BUILD_DIR?=$(ROOT_DIR)/build
GIT_VERSION:=$(shell git describe --tags --exact-match 2>/dev/null || git rev-parse HEAD)

###############################################################################
###                                  Build                                  ###
###############################################################################

.PHONY: build-relayer
build-relayer:
	cd $(RELAYER_DIR) && CGO_ENABLED=0 go build --trimpath -mod=readonly -ldflags '-extldflags=-static'  -o $(BUILD_DIR)/coreumbridge-xrpl-relayer ./cmd

.PHONY: build-relayer-release
build-relayer-release:
	cd $(RELAYER_DIR) && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build --trimpath -mod=readonly -ldflags '-extldflags=-static'  -o $(BUILD_DIR)/relayer-linux-amd64 ./cmd
	cd $(RELAYER_DIR) && CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build --trimpath -mod=readonly -ldflags '-extldflags=-static'  -o $(BUILD_DIR)/relayer-darwin-amd64 ./cmd
	cd $(RELAYER_DIR) && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build --trimpath -mod=readonly -ldflags '-extldflags=-static'  -o $(BUILD_DIR)/relayer-darwin-arm64 ./cmd

.PHONY: build-relayer-docker
build-relayer-docker:
	docker buildx build -f $(RELAYER_DIR)/Dockerfile . -t coreumbridge-xrpl-relayer:local

.PHONY: push-relayer-docker
push-relayer-docker: build-relayer-docker
	docker image tag coreumbridge-xrpl-relayer:local coreumfoundation/coreumbridge-xrpl-relayer:GIT_VERSION
	docker image push coreumfoundation/coreumbridge-xrpl-relayer:GIT_VERSION

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
	 do cd $$d && go clean -testcache && go test -v --tags=integrationtests -mod=readonly -parallel=10 -timeout 5m ./... || exit 1; \
	done

.PHONY: test-relayer
test-relayer:
	cd $(RELAYER_DIR) && go clean -testcache && go test -v -mod=readonly -parallel=10 -timeout 500ms ./...

.PHONY: test-contract
test-contract:
	cd $(CONTRACT_DIR) && cargo test --verbose

.PHONY: restart-dev-env
restart-dev-env:
	crust znet remove && crust znet start --profiles=1cored,xrpl --timeout-commit 0.5s

.PHONY: build-dev-env
build-dev-env:
	crust build/crust images/cored

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
