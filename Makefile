GO_IMPORT_PREFIX=github.com/CoreumFoundation
GO_SCAN_FILES := $(shell find . -type f -name '*.go' -not -name '*mock.go' -not -name '*_gen.go' -not -path "*/vendor/*")

###############################################################################
###                               Development                               ###
###############################################################################

.PHONY: go-fmt
go-fmt:
	which gofumpt || go install mvdan.cc/gofumpt@v0.5.0
	which gogroup || go install github.com/vasi-stripe/gogroup/cmd/gogroup@v0.0.0-20200806161525-b5d7f67a97b5
	gofumpt -lang=v2.1 -extra -w $(GO_SCAN_FILES)
	gogroup -order std,other,prefix=$(GO_IMPORT_PREFIX) -rewrite $(GO_SCAN_FILES)

.PHONY: go-mockgen
go-mockgen:
	which mockgen || go install github.com/golang/mock/mockgen@v1.6.0
	cd relayer && go generate ./...

.PHONY: go-lint
go-lint:
	crust lint/current-dir

.PHONY: test-integration
test-integration:
	cd integration-tests && go test -v --tags=integrationtests -mod=readonly -parallel=4 ./...

.PHONY: test-relayer
test-relayer:
	cd relayer && go test -v -mod=readonly -parallel=4 ./...

.PHONY: restart-dev-env
restart-dev-env:
	crust znet remove && crust znet start --profiles=integration-tests-modules,xrpl --timeout-commit 0.5s

.PHONY: rebuild-dev-env
rebuild-dev-env:
	crust build images
