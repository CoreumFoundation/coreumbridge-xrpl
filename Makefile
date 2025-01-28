BUILDER = ./bin/coreumbridge-xrpl-builder

export GOTOOLCHAIN = go1.21.4

ROOT_DIR:=$(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
CONTRACT_DIR:=$(ROOT_DIR)/contract

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

# FIXME (wojtek): Builder does not support the following actions

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
